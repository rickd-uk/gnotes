package main

import (
	"encoding/json"
	"net/http"
	"time"

	"gnotes/internal/db"
)

func favoriteNoteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Use PUT", http.StatusMethodNotAllowed)
		return
	}
	id, ok := noteIDFromRequest(w, r)
	if !ok {
		return
	}
	var input struct {
		Favorited *bool `json:"favorited"`
	}
	if err := decodeJSONBody(w, r, &input); err != nil || input.Favorited == nil {
		http.Error(w, "Choose whether to add to Favorites", http.StatusBadRequest)
		return
	}
	result, err := db.DB.Exec("UPDATE notes SET favorited = ? WHERE id = ? AND user_id = ? AND deleted_at IS NULL", *input.Favorited, id, userIDFromRequest(r))
	if err != nil {
		http.Error(w, "Could not update Favorites", http.StatusInternalServerError)
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "Could not update Favorites", http.StatusInternalServerError)
		return
	}
	if count == 0 {
		http.Error(w, "Note not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// favoriteDateNotesHandler covers a whole displayed date, independently of paging.
// Active date sections exclude pinned notes; archive dates use the archive time.
func favoriteDateNotesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Use POST", http.StatusMethodNotAllowed)
		return
	}
	params := r.URL.Query()
	zone := params.Get("timezone")
	if zone == "" || len(zone) > 64 {
		http.Error(w, "Invalid time zone", http.StatusBadRequest)
		return
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		http.Error(w, "Invalid time zone", http.StatusBadRequest)
		return
	}
	date, err := time.Parse("2006-01-02", params.Get("date"))
	if err != nil {
		http.Error(w, "Invalid note date", http.StatusBadRequest)
		return
	}
	if value := params.Get("archive"); value != "" && value != "0" && value != "1" {
		http.Error(w, "Invalid archive view", http.StatusBadRequest)
		return
	}
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, location)
	end := start.AddDate(0, 0, 1)
	column, condition := "created_at", "archived_at IS NULL AND pinned = 0"
	if params.Get("archive") == "1" {
		column, condition = "archived_at", "archived_at IS NOT NULL"
	}
	result, err := db.DB.Exec("UPDATE notes SET favorited = 1 WHERE user_id = ? AND deleted_at IS NULL AND favorited = 0 AND "+condition+" AND unixepoch("+column+") >= ? AND unixepoch("+column+") < ?", userIDFromRequest(r), start.Unix(), end.Unix())
	if err != nil {
		http.Error(w, "Could not add this date to Favorites", http.StatusInternalServerError)
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "Could not count Favorites", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		Added int64 `json:"added"`
	}{count})
}
