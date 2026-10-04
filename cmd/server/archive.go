package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"gnotes/internal/db"
	"gnotes/internal/models"
)

type archiveBucket struct {
	Key   string    `json:"key"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Count int       `json:"count"`
}

type archiveResponse struct {
	Total  int             `json:"total"`
	Months []archiveBucket `json:"months"`
	Weeks  []archiveBucket `json:"weeks"`
}

func noteArchiveHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if !validCSRFToken(r.Header.Get("X-CSRF-Token"), sessionFromContext(r).CSRFToken) {
			http.Error(w, "Invalid CSRF token", http.StatusForbidden)
			return
		}
		archiveOneOrAllNotes(w, r, true)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	timezone := r.URL.Query().Get("timezone")
	if timezone == "" {
		timezone = "UTC"
	}
	if len(timezone) > 64 {
		http.Error(w, "Invalid time zone", http.StatusBadRequest)
		return
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		http.Error(w, "Invalid time zone", http.StatusBadRequest)
		return
	}

	rows, err := db.DB.Query(`SELECT created_at FROM notes
	    WHERE user_id = ? AND deleted_at IS NULL AND archived_at IS NULL ORDER BY created_at DESC`, userIDFromRequest(r))
	if err != nil {
		http.Error(w, "Could not load archive", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	months := make(map[string]*archiveBucket)
	weeks := make(map[string]*archiveBucket)
	total := 0
	for rows.Next() {
		var createdAt time.Time
		if err := rows.Scan(&createdAt); err != nil {
			http.Error(w, "Could not load archive", http.StatusInternalServerError)
			return
		}
		local := createdAt.In(location)
		monthStart := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
		monthKey := monthStart.Format("2006-01")
		incrementArchiveBucket(months, monthKey, monthStart, monthStart.AddDate(0, 1, 0))

		weekdayOffset := (int(local.Weekday()) + 6) % 7
		weekStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location).AddDate(0, 0, -weekdayOffset)
		isoYear, isoWeek := local.ISOWeek()
		weekKey := fmt.Sprintf("%04d-W%02d", isoYear, isoWeek)
		incrementArchiveBucket(weeks, weekKey, weekStart, weekStart.AddDate(0, 0, 7))
		total++
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "Could not load archive", http.StatusInternalServerError)
		return
	}

	response := archiveResponse{
		Total:  total,
		Months: sortedArchiveBuckets(months),
		Weeks:  sortedArchiveBuckets(weeks),
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func archivedNotesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rows, err := db.DB.Query(`SELECT id, title, content, created_at, archived_at, pinned, background_color
    FROM notes WHERE user_id = ? AND deleted_at IS NULL AND archived_at IS NOT NULL
    ORDER BY archived_at DESC, id DESC`, userIDFromRequest(r))
	if err != nil {
		http.Error(w, "Could not load archived notes", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	notes := make([]models.Note, 0)
	for rows.Next() {
		var note models.Note
		if err := rows.Scan(&note.ID, &note.Title, &note.Content, &note.CreatedAt, &note.ArchivedAt, &note.Pinned, &note.BackgroundColor); err != nil {
			http.Error(w, "Could not load archived notes", http.StatusInternalServerError)
			return
		}
		notes = append(notes, note)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "Could not load archived notes", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"notes": notes, "total": len(notes)})
}

func unarchiveNoteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	archiveOneOrAllNotes(w, r, false)
}

func deleteArchivedNotesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Use POST", http.StatusMethodNotAllowed)
		return
	}
	params := r.URL.Query()
	idValue, dateValue := params.Get("id"), params.Get("date")
	if (idValue == "") == (dateValue == "") {
		http.Error(w, "Choose one note, one archive date, or all archived notes", http.StatusBadRequest)
		return
	}

	query := "UPDATE notes SET deleted_at = ?, archived_at = NULL WHERE user_id = ? AND deleted_at IS NULL AND archived_at IS NOT NULL"
	args := []any{time.Now(), userIDFromRequest(r)}
	if dateValue != "" {
		timezone := params.Get("timezone")
		if timezone == "" || len(timezone) > 64 {
			http.Error(w, "Invalid time zone", http.StatusBadRequest)
			return
		}
		location, err := time.LoadLocation(timezone)
		if err != nil {
			http.Error(w, "Invalid time zone", http.StatusBadRequest)
			return
		}
		date, err := time.Parse("2006-01-02", dateValue)
		if err != nil {
			http.Error(w, "Invalid archive date", http.StatusBadRequest)
			return
		}
		start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, location)
		end := start.AddDate(0, 0, 1)
		query += " AND unixepoch(archived_at) >= ? AND unixepoch(archived_at) < ?"
		args = append(args, start.Unix(), end.Unix())
	} else if idValue != "all" {
		id, err := strconv.Atoi(idValue)
		if err != nil || id < 1 {
			http.Error(w, "Invalid note id", http.StatusBadRequest)
			return
		}
		query += " AND id = ?"
		args = append(args, id)
	}

	result, err := db.DB.Exec(query, args...)
	if err != nil {
		http.Error(w, "Could not move archived notes to recycle bin", http.StatusInternalServerError)
		return
	}
	removed, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "Could not count moved notes", http.StatusInternalServerError)
		return
	}
	if idValue != "" && idValue != "all" && removed == 0 {
		http.Error(w, "Archived note not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int64{"removed": removed})
}

func archiveOneOrAllNotes(w http.ResponseWriter, r *http.Request, archive bool) {
	value := r.URL.Query().Get("id")
	userID := userIDFromRequest(r)
	var result sql.Result
	var err error
	if value == "all" {
		if archive {
			result, err = db.DB.Exec("UPDATE notes SET archived_at = ? WHERE user_id = ? AND deleted_at IS NULL AND archived_at IS NULL", time.Now(), userID)
		} else {
			result, err = db.DB.Exec("UPDATE notes SET archived_at = NULL WHERE user_id = ? AND deleted_at IS NULL AND archived_at IS NOT NULL", userID)
		}
	} else {
		id, parseErr := strconv.Atoi(value)
		if parseErr != nil || id < 1 {
			http.Error(w, "Invalid note id", http.StatusBadRequest)
			return
		}
		if archive {
			result, err = db.DB.Exec("UPDATE notes SET archived_at = ? WHERE id = ? AND user_id = ? AND deleted_at IS NULL AND archived_at IS NULL", time.Now(), id, userID)
		} else {
			result, err = db.DB.Exec("UPDATE notes SET archived_at = NULL WHERE id = ? AND user_id = ? AND deleted_at IS NULL AND archived_at IS NOT NULL", id, userID)
		}
	}
	if err != nil {
		http.Error(w, "Could not update archive", http.StatusInternalServerError)
		return
	}
	if value != "all" {
		if affected, _ := result.RowsAffected(); affected == 0 {
			http.Error(w, "Note not found", http.StatusNotFound)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func incrementArchiveBucket(buckets map[string]*archiveBucket, key string, start, end time.Time) {
	if bucket := buckets[key]; bucket != nil {
		bucket.Count++
		return
	}
	buckets[key] = &archiveBucket{Key: key, Start: start, End: end, Count: 1}
}

func sortedArchiveBuckets(source map[string]*archiveBucket) []archiveBucket {
	buckets := make([]archiveBucket, 0, len(source))
	for _, bucket := range source {
		buckets = append(buckets, *bucket)
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].Start.After(buckets[j].Start) })
	return buckets
}
