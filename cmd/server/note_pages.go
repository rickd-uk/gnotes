package main

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gnotes/internal/db"
	"gnotes/internal/models"
)

const (
	defaultNotePageSize = 50
	maxNotePageSize     = 100
	maxCursorBytes      = 512
)

type notePage struct {
	Notes      []models.Note `json:"notes"`
	NextCursor string        `json:"next_cursor,omitempty"`
	Total      int           `json:"total"`
	Pinned     int           `json:"pinned_total,omitempty"`
	MatchCount int           `json:"match_count,omitempty"`
}

type activeNoteCursor struct {
	Pinned    bool      `json:"p"`
	CreatedAt time.Time `json:"t"`
	ID        int       `json:"i"`
}

type trashNoteCursor struct {
	DeletedAt time.Time `json:"t"`
	ID        int       `json:"i"`
}

type renderedCacheUpdate struct {
	ID      int
	Content string
	HTML    string
}

func getNoteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, ok := noteIDFromRequest(w, r)
	if !ok {
		return
	}
	notes, _, err := queryActiveNotePage(`SELECT id, title, content, rendered_content, created_at, pinned
    FROM notes WHERE id = ? AND user_id = ? AND deleted_at IS NULL LIMIT ?`,
		[]any{id, userIDFromRequest(r)}, 1)
	if err != nil {
		http.Error(w, "Could not load note", http.StatusInternalServerError)
		return
	}
	if len(notes) == 0 {
		http.Error(w, "Note not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(notes[0])
}

func pagedListNotesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit, ok := notePageLimit(w, r)
	if !ok {
		return
	}
	cursor, hasCursor, err := decodeActiveNoteCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		http.Error(w, "Invalid cursor", http.StatusBadRequest)
		return
	}

	conditions := []string{"user_id = ?", "deleted_at IS NULL"}
	args := []any{userIDFromRequest(r)}
	if hasCursor {
		conditions = append(conditions, `(pinned < ? OR
			(pinned = ? AND unixepoch(created_at) < ?) OR
			(pinned = ? AND unixepoch(created_at) = ? AND id < ?))`)
		pinned := boolInt(cursor.Pinned)
		args = append(args, pinned, pinned, cursor.CreatedAt.Unix(), pinned, cursor.CreatedAt.Unix(), cursor.ID)
	}

	query := `SELECT id, title, content, rendered_content, created_at, pinned
    FROM notes WHERE ` + strings.Join(conditions, " AND ") + `
	ORDER BY pinned DESC, unixepoch(created_at) DESC, id DESC LIMIT ?`
	notes, nextCursor, err := queryActiveNotePage(query, args, limit)
	if err != nil {
		http.Error(w, "Could not load notes", http.StatusInternalServerError)
		return
	}
	var total int
	if err := db.DB.QueryRow(
		"SELECT COUNT(*) FROM notes WHERE user_id = ? AND deleted_at IS NULL",
		userIDFromRequest(r),
	).Scan(&total); err != nil {
		http.Error(w, "Could not count notes", http.StatusInternalServerError)
		return
	}
	var pinned int
	if err := db.DB.QueryRow(
		"SELECT COUNT(*) FROM notes WHERE user_id = ? AND deleted_at IS NULL AND pinned = 1",
		userIDFromRequest(r),
	).Scan(&pinned); err != nil {
		http.Error(w, "Could not count pinned notes", http.StatusInternalServerError)
		return
	}
	writeNotePage(w, notePage{Notes: notes, NextCursor: nextCursor, Total: total, Pinned: pinned})
}

func pagedSearchNotesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit, ok := notePageLimit(w, r)
	if !ok {
		return
	}
	cursor, hasCursor, err := decodeActiveNoteCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		http.Error(w, "Invalid cursor", http.StatusBadRequest)
		return
	}

	join, conditions, args, err := pagedSearchFilter(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	countConditions := append([]string(nil), conditions...)
	countArgs := append([]any(nil), args...)
	if hasCursor {
		conditions = append(conditions, `(n.pinned < ? OR
			(n.pinned = ? AND unixepoch(n.created_at) < ?) OR
			(n.pinned = ? AND unixepoch(n.created_at) = ? AND n.id < ?))`)
		pinned := boolInt(cursor.Pinned)
		args = append(args, pinned, pinned, cursor.CreatedAt.Unix(), pinned, cursor.CreatedAt.Unix(), cursor.ID)
	}

	fromClause := " FROM notes n " + join + " WHERE " + strings.Join(conditions, " AND ")
	query := `SELECT n.id, n.title, n.content, n.rendered_content, n.created_at, n.pinned` +
		fromClause + ` ORDER BY n.pinned DESC, unixepoch(n.created_at) DESC, n.id DESC LIMIT ?`
	notes, nextCursor, err := queryActiveNotePage(query, args, limit)
	if err != nil {
		http.Error(w, "Search failed", http.StatusInternalServerError)
		return
	}
	var total int
	matchCount := 0
	countFromClause := " FROM notes n " + join + " WHERE " + strings.Join(countConditions, " AND ")
	if expression, expressionArgs := searchOccurrenceExpression(r); expression != "" {
		queryArgs := append(expressionArgs, countArgs...)
		if err := db.DB.QueryRow("SELECT COUNT(*), "+expression+countFromClause, queryArgs...).Scan(&total, &matchCount); err != nil {
			http.Error(w, "Could not count search matches", http.StatusInternalServerError)
			return
		}
	} else if err := db.DB.QueryRow("SELECT COUNT(*)"+countFromClause, countArgs...).Scan(&total); err != nil {
		http.Error(w, "Could not count search results", http.StatusInternalServerError)
		return
	}
	writeNotePage(w, notePage{Notes: notes, NextCursor: nextCursor, Total: total, MatchCount: matchCount})
}

func searchOccurrenceExpression(r *http.Request) (string, []any) {
	queryText := r.URL.Query().Get("q")
	if queryText == "" {
		return "", nil
	}
	matchCase := r.URL.Query().Get("match_case") == "true"
	occurrences := func(column string) (string, []any) {
		text := "COALESCE(" + column + ", '')"
		needle := "?"
		if !matchCase {
			text = "lower(" + text + ")"
			needle = "lower(?)"
		}
		return "((length(" + text + ") - length(replace(" + text + ", " + needle + ", ''))) / length(" + needle + "))", []any{queryText, queryText}
	}

	scope := r.URL.Query().Get("scope")
	if scope == "title" {
		expression, args := occurrences("n.title")
		return "COALESCE(SUM(" + expression + "), 0)", args
	}
	if scope == "content" {
		expression, args := occurrences("n.content")
		return "COALESCE(SUM(" + expression + "), 0)", args
	}
	titleExpression, titleArgs := occurrences("n.title")
	contentExpression, contentArgs := occurrences("n.content")
	return "COALESCE(SUM(" + titleExpression + " + " + contentExpression + "), 0)", append(titleArgs, contentArgs...)
}

func pagedTrashNotesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit, ok := notePageLimit(w, r)
	if !ok {
		return
	}
	cursor, hasCursor, err := decodeTrashNoteCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		http.Error(w, "Invalid cursor", http.StatusBadRequest)
		return
	}

	conditions := []string{"user_id = ?", "deleted_at IS NOT NULL"}
	args := []any{userIDFromRequest(r)}
	if hasCursor {
		conditions = append(conditions, "(unixepoch(deleted_at) < ? OR (unixepoch(deleted_at) = ? AND id < ?))")
		args = append(args, cursor.DeletedAt.Unix(), cursor.DeletedAt.Unix(), cursor.ID)
	}
	query := `SELECT id, title, content, created_at, deleted_at
    FROM notes WHERE ` + strings.Join(conditions, " AND ") + `
	ORDER BY unixepoch(deleted_at) DESC, id DESC LIMIT ?`
	queryArgs := append(append([]any{}, args...), limit+1)
	rows, err := db.DB.Query(query, queryArgs...)
	if err != nil {
		http.Error(w, "Could not load recycle bin", http.StatusInternalServerError)
		return
	}
	trash := make([]models.Note, 0, limit+1)
	for rows.Next() {
		var note models.Note
		if err := rows.Scan(&note.ID, &note.Title, &note.Content, &note.CreatedAt, &note.DeletedAt); err != nil {
			rows.Close()
			http.Error(w, "Could not load recycle bin", http.StatusInternalServerError)
			return
		}
		trash = append(trash, note)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		http.Error(w, "Could not load recycle bin", http.StatusInternalServerError)
		return
	}
	rows.Close()

	nextCursor := ""
	if len(trash) > limit {
		trash = trash[:limit]
		last := trash[len(trash)-1]
		nextCursor, err = encodeCursor(trashNoteCursor{DeletedAt: *last.DeletedAt, ID: last.ID})
		if err != nil {
			http.Error(w, "Could not paginate recycle bin", http.StatusInternalServerError)
			return
		}
	}
	var total int
	if err := db.DB.QueryRow(
		"SELECT COUNT(*) FROM notes WHERE user_id = ? AND deleted_at IS NOT NULL",
		userIDFromRequest(r),
	).Scan(&total); err != nil {
		http.Error(w, "Could not count recycle bin", http.StatusInternalServerError)
		return
	}
	writeNotePage(w, notePage{Notes: trash, NextCursor: nextCursor, Total: total})
}

func pagedSearchFilter(r *http.Request) (string, []string, []any, error) {
	queryText := r.URL.Query().Get("q")
	if len(queryText) > maxSearchBytes {
		return "", nil, nil, errors.New("search text is too long")
	}
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "all"
	}
	if scope != "all" && scope != "title" && scope != "content" {
		return "", nil, nil, errors.New("invalid search scope")
	}

	conditions := []string{"n.user_id = ?", "n.deleted_at IS NULL"}
	args := []any{userIDFromRequest(r)}
	fromDate, toDate, err := pagedSearchTimes(r)
	if err != nil {
		return "", nil, nil, err
	}
	if !fromDate.IsZero() {
		conditions = append(conditions, "unixepoch(n.created_at) >= ?")
		args = append(args, fromDate.Unix())
	}
	if !toDate.IsZero() {
		conditions = append(conditions, "unixepoch(n.created_at) < ?")
		args = append(args, toDate.Unix())
	}

	join := ""
	if queryText != "" {
		matchCase := r.URL.Query().Get("match_case") == "true"
		column := ""
		switch scope {
		case "title":
			column = "n.title"
		case "content":
			column = "n.content"
		}
		if ftsSearchEligible(queryText) {
			join = "JOIN notes_fts ON notes_fts.rowid = n.id"
			ftsColumn := "{title content}"
			if scope != "all" {
				ftsColumn = scope
			}
			ftsPhrase := strings.ReplaceAll(queryText, `"`, `""`)
			conditions = append(conditions, "notes_fts MATCH ?")
			args = append(args, fmt.Sprintf(`%s : "%s"`, ftsColumn, ftsPhrase))
		}

		matchExpression := func(columnName string) string {
			if matchCase {
				return "instr(" + columnName + ", ?) > 0"
			}
			return "instr(lower(" + columnName + "), lower(?)) > 0"
		}
		if column != "" {
			conditions = append(conditions, matchExpression(column))
			args = append(args, queryText)
		} else {
			conditions = append(conditions, "("+matchExpression("n.title")+" OR "+matchExpression("n.content")+")")
			args = append(args, queryText, queryText)
		}
	}
	return join, conditions, args, nil
}

func pagedSearchTimes(r *http.Request) (time.Time, time.Time, error) {
	fromTimeValue := r.URL.Query().Get("from_time")
	toTimeValue := r.URL.Query().Get("to_time")
	if fromTimeValue != "" || toTimeValue != "" {
		if fromTimeValue == "" || toTimeValue == "" {
			return time.Time{}, time.Time{}, errors.New("both time boundaries are required")
		}
		fromTime, err := time.Parse(time.RFC3339, fromTimeValue)
		if err != nil {
			return time.Time{}, time.Time{}, errors.New("invalid start time")
		}
		toTime, err := time.Parse(time.RFC3339, toTimeValue)
		if err != nil {
			return time.Time{}, time.Time{}, errors.New("invalid end time")
		}
		if !fromTime.Before(toTime) {
			return time.Time{}, time.Time{}, errors.New("start time must be before end time")
		}
		return fromTime, toTime, nil
	}

	fromDate, err := parseSearchDate(r.URL.Query().Get("from"))
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("invalid start date")
	}
	toDate, err := parseSearchDate(r.URL.Query().Get("to"))
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("invalid end date")
	}
	if !fromDate.IsZero() && !toDate.IsZero() && fromDate.After(toDate) {
		return time.Time{}, time.Time{}, errors.New("start date must not be after end date")
	}
	if !toDate.IsZero() {
		toDate = toDate.AddDate(0, 0, 1)
	}
	return fromDate, toDate, nil
}

func ftsSearchEligible(query string) bool {
	return utf8.RuneCountInString(strings.TrimSpace(query)) >= 3
}

func queryActiveNotePage(query string, args []any, limit int) ([]models.Note, string, error) {
	queryArgs := append(append([]any{}, args...), limit+1)
	rows, err := db.DB.Query(query, queryArgs...)
	if err != nil {
		return nil, "", err
	}
	notes := make([]models.Note, 0, limit+1)
	cacheUpdates := make([]renderedCacheUpdate, 0)
	for rows.Next() {
		var note models.Note
		var rendered sql.NullString
		if err := rows.Scan(&note.ID, &note.Title, &note.Content, &rendered, &note.CreatedAt, &note.Pinned); err != nil {
			rows.Close()
			return nil, "", err
		}
		if rendered.Valid {
			note.HTMLContent = template.HTML(rendered.String)
		} else {
			renderedHTML := mdToHTML(note.Content)
			note.HTMLContent = template.HTML(renderedHTML)
			cacheUpdates = append(cacheUpdates, renderedCacheUpdate{ID: note.ID, Content: note.Content, HTML: renderedHTML})
		}
		notes = append(notes, note)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, "", err
	}
	if err := rows.Close(); err != nil {
		return nil, "", err
	}
	if err := saveRenderedCache(cacheUpdates); err != nil {
		return nil, "", err
	}

	nextCursor := ""
	if len(notes) > limit {
		notes = notes[:limit]
		last := notes[len(notes)-1]
		nextCursor, err = encodeCursor(activeNoteCursor{Pinned: last.Pinned, CreatedAt: last.CreatedAt, ID: last.ID})
		if err != nil {
			return nil, "", err
		}
	}
	return notes, nextCursor, nil
}

func saveRenderedCache(updates []renderedCacheUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, update := range updates {
		if _, err := tx.Exec(
			"UPDATE notes SET rendered_content = ? WHERE id = ? AND content = ? AND rendered_content IS NULL",
			update.HTML, update.ID, update.Content,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func notePageLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	value := r.URL.Query().Get("limit")
	if value == "" {
		return defaultNotePageSize, true
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > maxNotePageSize {
		http.Error(w, "Limit must be between 1 and 100", http.StatusBadRequest)
		return 0, false
	}
	return limit, true
}

func encodeCursor(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeActiveNoteCursor(value string) (activeNoteCursor, bool, error) {
	var cursor activeNoteCursor
	if value == "" {
		return cursor, false, nil
	}
	if err := decodeCursor(value, &cursor); err != nil {
		return cursor, false, err
	}
	if cursor.ID < 1 || cursor.CreatedAt.IsZero() {
		return cursor, false, errors.New("invalid active-note cursor")
	}
	return cursor, true, nil
}

func decodeTrashNoteCursor(value string) (trashNoteCursor, bool, error) {
	var cursor trashNoteCursor
	if value == "" {
		return cursor, false, nil
	}
	if err := decodeCursor(value, &cursor); err != nil {
		return cursor, false, err
	}
	if cursor.ID < 1 || cursor.DeletedAt.IsZero() {
		return cursor, false, errors.New("invalid trash cursor")
	}
	return cursor, true, nil
}

func decodeCursor(value string, destination any) error {
	if len(value) > maxCursorBytes {
		return errors.New("cursor is too long")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(decoded)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("cursor must contain one JSON object")
	}
	return nil
}

func writeNotePage(w http.ResponseWriter, page notePage) {
	if page.Notes == nil {
		page.Notes = make([]models.Note, 0)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(page)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
