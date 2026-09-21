package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"gnotes/internal/db"
)

type noteDraft struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Version int64  `json:"version"`
}

func getDraftHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	draft := noteDraft{}
	err := db.DB.QueryRow(
		"SELECT title, content, version FROM drafts WHERE user_id = ?",
		userIDFromRequest(r),
	).Scan(&draft.Title, &draft.Content, &draft.Version)
	if err != nil && err != sql.ErrNoRows {
		http.Error(w, "Could not load draft", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(draft)
}

func updateDraftHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Use PUT", http.StatusMethodNotAllowed)
		return
	}
	var draft noteDraft
	if err := decodeJSONBody(w, r, &draft); err != nil || draft.Version < 1 {
		http.Error(w, "Invalid draft", http.StatusBadRequest)
		return
	}
	if err := validateNoteSize(draft.Title, draft.Content); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	result, err := db.DB.Exec(`
		INSERT INTO drafts (user_id, title, content, version, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			title = excluded.title,
			content = excluded.content,
			version = excluded.version,
			updated_at = excluded.updated_at
		WHERE excluded.version >= drafts.version`,
		userIDFromRequest(r), draft.Title, draft.Content, draft.Version, time.Now(),
	)
	if err != nil {
		http.Error(w, "Could not save draft", http.StatusInternalServerError)
		return
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		http.Error(w, "A newer draft exists", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func finalizeDraftHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Use POST", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Version int64 `json:"version"`
	}
	if err := decodeJSONBody(w, r, &input); err != nil || input.Version < 1 {
		http.Error(w, "Invalid draft version", http.StatusBadRequest)
		return
	}
	userID := userIDFromRequest(r)
	tx, err := db.DB.Begin()
	if err != nil {
		http.Error(w, "Could not finalize draft", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var draft noteDraft
	err = tx.QueryRow(
		"SELECT title, content, version FROM drafts WHERE user_id = ?", userID,
	).Scan(&draft.Title, &draft.Content, &draft.Version)
	if err == sql.ErrNoRows {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		http.Error(w, "Could not finalize draft", http.StatusInternalServerError)
		return
	}
	if draft.Version != input.Version {
		http.Error(w, "A newer draft exists", http.StatusConflict)
		return
	}
	if err := validateNoteSize(draft.Title, draft.Content); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(draft.Title) == "" && strings.TrimSpace(draft.Content) == "" {
		if _, err := tx.Exec("DELETE FROM drafts WHERE user_id = ?", userID); err != nil {
			http.Error(w, "Could not clear draft", http.StatusInternalServerError)
			return
		}
		if err := tx.Commit(); err != nil {
			http.Error(w, "Could not clear draft", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	createdAt := time.Now()
	result, err := tx.Exec(
		"INSERT INTO notes (user_id, title, content, rendered_content, created_at) VALUES (?, ?, ?, ?, ?)",
		userID, draft.Title, draft.Content, mdToHTML(draft.Content), createdAt,
	)
	if err != nil {
		http.Error(w, "Could not create note", http.StatusInternalServerError)
		return
	}
	id, err := result.LastInsertId()
	if err != nil {
		http.Error(w, "Could not create note", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec("DELETE FROM drafts WHERE user_id = ?", userID); err != nil {
		http.Error(w, "Could not clear draft", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, "Could not finalize draft", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"id":         id,
		"title":      draft.Title,
		"content":    draft.Content,
		"created_at": createdAt,
	})
}
