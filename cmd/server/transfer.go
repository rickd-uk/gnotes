package main

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gnotes/internal/db"
	"gnotes/internal/models"
)

// The format deliberately excludes server IDs and rendered HTML. IDs belong to
// the destination database; HTML is derived from Markdown when notes are read.
const transferVersion = 3
const maxTransferBytes = 64 << 20
const maxTransferNotes = 10000

var errTransferTooLarge = errors.New("transfer exceeds the size limit")

type transferNote struct {
	SourceID        int         `json:"source_id,omitempty"`
	Title           string      `json:"title"`
	Content         string      `json:"content"`
	CreatedAt       time.Time   `json:"created_at"`
	DeletedAt       *time.Time  `json:"deleted_at,omitempty"`
	ArchivedAt      *time.Time  `json:"archived_at,omitempty"`
	Favorited       bool        `json:"favorited"`
	Pinned          bool        `json:"pinned"`
	BackgroundColor string      `json:"background_color"`
	Tags            models.Tags `json:"tags,omitempty"`
}

type transferFile struct {
	Format     string         `json:"format"`
	Version    int            `json:"version"`
	ExportedAt time.Time      `json:"exported_at"`
	Notes      []transferNote `json:"notes"`
}

func exportNotesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Use GET", http.StatusMethodNotAllowed)
		return
	}
	format := r.URL.Query().Get("format")
	if format != "gnotes" && format != "markdown" && format != "text" {
		http.Error(w, "Choose gnotes, markdown, or text", http.StatusBadRequest)
		return
	}
	ids, err := parseTransferIDs(r.URL.Query().Get("ids"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	notes, err := loadTransferNotes(userIDFromRequest(r), ids)
	if err != nil {
		if errors.Is(err, errTransferTooLarge) {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "Could not export notes", http.StatusInternalServerError)
		return
	}
	if ids != nil && len(notes) != len(ids) {
		http.Error(w, "One or more notes were not found", http.StatusNotFound)
		return
	}
	if len(notes) > maxTransferNotes {
		http.Error(w, "Too many notes for one export", http.StatusRequestEntityTooLarge)
		return
	}
	file := transferFile{Format: "gnotes", Version: transferVersion, ExportedAt: time.Now().UTC(), Notes: notes}
	if format == "gnotes" {
		body, err := json.MarshalIndent(file, "", "  ")
		if err != nil || len(body) > maxTransferBytes {
			http.Error(w, "Export exceeds the size limit", http.StatusRequestEntityTooLarge)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="gnotes-export.json"`)
		w.Header().Set("Cache-Control", "no-store")
		w.Write(body)
		return
	}
	if len(notes) == 1 && len(notes[0].Tags) == 0 {
		extension := ".md"
		if format == "text" {
			extension = ".txt"
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s%s"`, safeTransferName(notes[0].Title), extension))
		w.Header().Set("Cache-Control", "no-store")
		w.Write([]byte(notes[0].Content))
		return
	}
	// A manifest preserves title, timestamps, and note state in readable ZIP
	// exports, so importing the archive can restore those details exactly.
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	manifest, _ := json.MarshalIndent(file, "", "  ")
	if len(manifest) > maxTransferBytes {
		http.Error(w, "Export exceeds the size limit", http.StatusRequestEntityTooLarge)
		return
	}
	entry, err := archive.Create("gnotes-manifest.json")
	if err == nil {
		_, err = entry.Write(manifest)
	}
	for index, note := range notes {
		if err != nil {
			break
		}
		extension := ".md"
		if format == "text" {
			extension = ".txt"
		}
		entry, err = archive.Create(fmt.Sprintf("%04d-%s%s", index+1, safeTransferName(note.Title), extension))
		if err == nil {
			_, err = entry.Write([]byte(note.Content))
		}
		if output.Len() > maxTransferBytes {
			err = errors.New("export too large")
		}
	}
	if err == nil {
		err = archive.Close()
	}
	if err != nil || output.Len() > maxTransferBytes {
		http.Error(w, "Export exceeds the size limit", http.StatusRequestEntityTooLarge)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="gnotes-export.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Write(output.Bytes())
}

func transferListHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Use GET", http.StatusMethodNotAllowed)
		return
	}
	rows, err := db.DB.Query(`SELECT id, title, created_at, deleted_at, archived_at FROM notes
		WHERE user_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, userIDFromRequest(r), maxTransferNotes+1)
	if err != nil {
		http.Error(w, "Could not list notes", 500)
		return
	}
	defer rows.Close()
	type item struct {
		ID        int       `json:"id"`
		Title     string    `json:"title"`
		CreatedAt time.Time `json:"created_at"`
		State     string    `json:"state"`
	}
	items := make([]item, 0)
	for rows.Next() {
		var value item
		var deleted, archived sql.NullTime
		if err := rows.Scan(&value.ID, &value.Title, &value.CreatedAt, &deleted, &archived); err != nil {
			http.Error(w, "Could not list notes", 500)
			return
		}
		value.State = "active"
		if archived.Valid {
			value.State = "archived"
		}
		if deleted.Valid {
			value.State = "recycled"
		}
		items = append(items, value)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "Could not list notes", 500)
		return
	}
	if len(items) > maxTransferNotes {
		http.Error(w, "Too many notes to list", http.StatusRequestEntityTooLarge)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(items)
}

func parseTransferIDs(raw string) ([]int, error) {
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > maxTransferNotes {
		return nil, errors.New("too many note IDs")
	}
	ids := make([]int, 0, len(parts))
	seen := make(map[int]bool)
	for _, part := range parts {
		id, err := strconv.Atoi(part)
		if err != nil || id < 1 || seen[id] {
			return nil, errors.New("invalid or repeated note ID")
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, nil
}

func loadTransferNotes(userID int, ids []int) ([]transferNote, error) {
	query := "SELECT id, title, content, created_at, deleted_at, archived_at, pinned, background_color, tags, favorited FROM notes WHERE user_id = ?"
	args := []any{userID}
	if ids != nil {
		query += " AND id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"
		for _, id := range ids {
			args = append(args, id)
		}
	}
	query += " ORDER BY created_at DESC, id DESC"
	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	notes := make([]transferNote, 0)
	textBytes := 0
	for rows.Next() {
		var note transferNote
		var deleted, archived sql.NullTime
		if err := rows.Scan(&note.SourceID, &note.Title, &note.Content, &note.CreatedAt, &deleted, &archived, &note.Pinned, &note.BackgroundColor, &note.Tags, &note.Favorited); err != nil {
			return nil, err
		}
		if deleted.Valid {
			note.DeletedAt = &deleted.Time
		}
		if archived.Valid {
			note.ArchivedAt = &archived.Time
		}
		notes = append(notes, note)
		textBytes += len(note.Title) + len(note.Content)
		if len(notes) > maxTransferNotes || textBytes > maxTransferBytes {
			return nil, errTransferTooLarge
		}
	}
	return notes, rows.Err()
}

var unsafeTransferName = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func safeTransferName(title string) string {
	name := strings.Trim(unsafeTransferName.ReplaceAllString(title, "-"), "-")
	if len(name) > 60 {
		name = name[:60]
	}
	if name == "" {
		name = "untitled-note"
	}
	return name
}

func importNotesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Use POST", http.StatusMethodNotAllowed)
		return
	}
	mode := r.URL.Query().Get("duplicates")
	if mode != "skip" && mode != "copy" {
		http.Error(w, "Choose duplicates=skip or duplicates=copy", http.StatusBadRequest)
		return
	}
	format := r.URL.Query().Get("format")
	if format != "gnotes" && format != "markdown" && format != "text" && format != "zip" {
		http.Error(w, "Unsupported import format", http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxTransferBytes))
	if err != nil {
		http.Error(w, "Import exceeds the size limit", http.StatusRequestEntityTooLarge)
		return
	}
	var file transferFile
	switch format {
	case "gnotes":
		err = decodeTransferFile(body, &file)
	case "zip":
		var reader *zip.Reader
		reader, err = zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err == nil {
			err = errors.New("archive is missing gnotes-manifest.json")
			for _, item := range reader.File {
				if item.Name != "gnotes-manifest.json" {
					continue
				}
				if item.UncompressedSize64 > maxTransferBytes {
					break
				}
				var stream io.ReadCloser
				stream, err = item.Open()
				if err != nil {
					break
				}
				var manifest []byte
				manifest, err = io.ReadAll(io.LimitReader(stream, maxTransferBytes+1))
				stream.Close()
				if err == nil && len(manifest) <= maxTransferBytes {
					err = decodeTransferFile(manifest, &file)
				}
				break
			}
		}
	case "markdown", "text":
		if !utf8.Valid(body) {
			err = errors.New("text must be UTF-8")
		} else {
			title := r.URL.Query().Get("title")
			file = transferFile{Format: "gnotes", Version: transferVersion, Notes: []transferNote{{Title: title, Content: string(body), CreatedAt: time.Now().UTC()}}}
		}
	}
	if err != nil {
		http.Error(w, "Invalid import: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(file.Notes) > maxTransferNotes {
		http.Error(w, "Too many notes", http.StatusRequestEntityTooLarge)
		return
	}
	for _, note := range file.Notes {
		if err := validateTransferNote(note); err != nil {
			http.Error(w, "Invalid import: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	tx, err := db.DB.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "Import failed", 500)
		return
	}
	defer tx.Rollback()
	imported, skipped := 0, 0
	for _, note := range file.Notes {
		tags, _ := normalizeTags(note.Tags)
		if mode == "skip" {
			var exists int
			if format == "markdown" || format == "text" {
				err = tx.QueryRow(`SELECT 1 FROM notes WHERE user_id = ? AND title = ? AND content = ? LIMIT 1`,
					userIDFromRequest(r), note.Title, note.Content).Scan(&exists)
			} else {
				err = tx.QueryRow(`SELECT 1 FROM notes WHERE user_id = ? AND title = ? AND content = ? AND created_at = ?
				AND deleted_at IS ? AND archived_at IS ? AND pinned = ? AND background_color = ? AND tags = ? AND favorited = ? LIMIT 1`,
					userIDFromRequest(r), note.Title, note.Content, note.CreatedAt, note.DeletedAt, note.ArchivedAt, note.Pinned, note.BackgroundColor, tags, note.Favorited).Scan(&exists)
			}
			if err == nil {
				skipped++
				continue
			}
			if !errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "Import failed", 500)
				return
			}
		}
		_, err = tx.Exec(`INSERT INTO notes (user_id, title, content, rendered_content, created_at, deleted_at, archived_at, pinned, background_color, tags, favorited)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, userIDFromRequest(r), note.Title, note.Content, mdToHTML(note.Content), note.CreatedAt,
			note.DeletedAt, note.ArchivedAt, note.Pinned, note.BackgroundColor, tags, note.Favorited)
		if err != nil {
			http.Error(w, "Import failed", 500)
			return
		}
		imported++
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, "Import failed", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"imported": imported, "skipped": skipped})
}

func decodeTransferFile(body []byte, file *transferFile) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(file); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("expected one JSON object")
	}
	if file.Format != "gnotes" || (file.Version != 1 && file.Version != 2 && file.Version != transferVersion) {
		return errors.New("unsupported gnotes format version")
	}
	if file.Notes == nil {
		return errors.New("notes array is required")
	}
	return nil
}

func validateTransferNote(note transferNote) error {
	if _, err := normalizeTags(note.Tags); err != nil {
		return err
	}
	if !utf8.ValidString(note.Title) || !utf8.ValidString(note.Content) {
		return errors.New("note text must be UTF-8")
	}
	if err := validateNoteText(note.Title, note.Content); err != nil {
		return err
	}
	if note.CreatedAt.IsZero() || note.CreatedAt.After(time.Now().Add(24*time.Hour)) {
		return errors.New("invalid created_at")
	}
	if _, ok := allowedNoteColors[note.BackgroundColor]; !ok {
		return errors.New("invalid note color")
	}
	if note.DeletedAt != nil && note.ArchivedAt != nil {
		return errors.New("note cannot be archived and recycled")
	}
	return nil
}
