package main

import (
	"net/http"

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
