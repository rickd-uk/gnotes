package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"gnotes/internal/db"
)

// Explicit state makes retries safe: a repeated lock request never unlocks.
func lockNoteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Use PUT", http.StatusMethodNotAllowed)
		return
	}
	id, ok := noteIDFromRequest(w, r)
	if !ok {
		return
	}
	var input struct {
		Locked *bool `json:"locked"`
	}
	if decodeJSONBody(w, r, &input) != nil || input.Locked == nil {
		http.Error(w, "Supply a locked boolean", http.StatusBadRequest)
		return
	}
	result, err := db.DB.Exec("UPDATE notes SET locked=? WHERE id=? AND user_id=? AND deleted_at IS NULL", *input.Locked, id, userIDFromRequest(r))
	if err != nil {
		http.Error(w, "Could not change note lock", 500)
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "Could not change note lock", 500)
		return
	}
	if count == 0 {
		http.Error(w, "Note not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]bool{"locked": *input.Locked})
}

// Mutation queries include locked=0 so concurrent locks cannot be bypassed.
func noteEditRejected(w http.ResponseWriter, r *http.Request, id int) {
	var locked bool
	err := db.DB.QueryRow("SELECT locked FROM notes WHERE id=? AND user_id=? AND deleted_at IS NULL", id, userIDFromRequest(r)).Scan(&locked)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		http.Error(w, "Note not found", 404)
	case err != nil:
		http.Error(w, "Could not check note lock", 500)
	case locked:
		http.Error(w, "Note is locked. Unlock it before editing.", http.StatusLocked)
	default:
		http.Error(w, "Note not found", 404)
	}
}
