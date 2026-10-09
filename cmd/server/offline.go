package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"gnotes/internal/db"
)

// This snapshot is fetched only on explicit request and must never be cached
// by HTTP or the service worker. The browser encrypts it before persisting it.
func offlineSnapshotHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		http.Error(w, "Use GET", http.StatusMethodNotAllowed)
		return
	}
	userID := userIDFromRequest(r)
	var username string
	if err := db.DB.QueryRow("SELECT username FROM users WHERE id = ?", userID).Scan(&username); err != nil {
		http.Error(w, "Could not load account", http.StatusInternalServerError)
		return
	}
	notes, err := loadTransferNotesFiltered(userID, nil, true)
	if err != nil {
		if errors.Is(err, errTransferTooLarge) {
			http.Error(w, "Offline copy exceeds the size limit", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "Could not prepare offline copy", http.StatusInternalServerError)
		}
		return
	}
	readable := make([]transferNote, 0, len(notes))
	for _, note := range notes {
		if note.DeletedAt == nil {
			readable = append(readable, note)
		}
	}
	body, err := json.Marshal(struct {
		Version  int            `json:"version"`
		Owner    int            `json:"owner"`
		Username string         `json:"username"`
		SavedAt  time.Time      `json:"saved_at"`
		Notes    []transferNote `json:"notes"`
	}{1, userID, username, time.Now().UTC(), readable})
	if err != nil || len(body) > maxTransferBytes {
		http.Error(w, "Offline copy exceeds the 64 MB limit", http.StatusRequestEntityTooLarge)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(body)
}
