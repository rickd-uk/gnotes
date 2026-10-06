package main

import (
	"encoding/json"
	"net/http"
	"time"

	"gnotes/internal/db"
)

// deleteActiveNotesHandler targets the same groups as the main notes view.
// Date groups exclude pinned notes, which have their own section and scope.
func deleteActiveNotesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Use POST", http.StatusMethodNotAllowed)
		return
	}
	params := r.URL.Query()
	scope := params.Get("scope")
	query := "UPDATE notes SET deleted_at = ? WHERE user_id = ? AND deleted_at IS NULL AND archived_at IS NULL"
	args := []any{time.Now().UTC(), userIDFromRequest(r)}
	switch scope {
	case "all", "pinned":
		if params.Get("date") != "" || params.Get("timezone") != "" {
			http.Error(w, "Date options require the date scope", http.StatusBadRequest)
			return
		}
		if scope == "pinned" {
			query += " AND pinned = 1"
		}
	case "date":
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
		date, err := time.Parse("2006-01-02", params.Get("date"))
		if err != nil {
			http.Error(w, "Invalid note date", http.StatusBadRequest)
			return
		}
		start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, location)
		end := start.AddDate(0, 0, 1)
		query += " AND pinned = 0 AND unixepoch(created_at) >= ? AND unixepoch(created_at) < ?"
		args = append(args, start.Unix(), end.Unix())
	default:
		http.Error(w, "Choose all notes, pinned notes, or a date", http.StatusBadRequest)
		return
	}

	tx, err := db.DB.Begin()
	if err != nil {
		http.Error(w, "Could not move notes to recycle bin", 500)
		return
	}
	defer tx.Rollback()
	rows, err := tx.Query(query+" RETURNING id", args...)
	if err != nil {
		http.Error(w, "Could not move notes to recycle bin", 500)
		return
	}
	ids := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			http.Error(w, "Could not move notes to recycle bin", 500)
			return
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil || closeErr != nil {
		http.Error(w, "Could not move notes to recycle bin", 500)
		return
	}
	var active, pinned int
	if err := tx.QueryRow("SELECT COUNT(*),COALESCE(SUM(pinned),0) FROM notes WHERE user_id=? AND deleted_at IS NULL AND archived_at IS NULL", userIDFromRequest(r)).Scan(&active, &pinned); err != nil {
		http.Error(w, "Could not count remaining notes", 500)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, "Could not move notes to recycle bin", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		Removed int   `json:"removed"`
		IDs     []int `json:"removed_ids"`
		Active  int   `json:"active_total"`
		Pinned  int   `json:"pinned_total"`
	}{len(ids), ids, active, pinned})
}
