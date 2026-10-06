package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"gnotes/internal/db"
	"gnotes/internal/dictionary"
)

func registerDictionaryRoutes(mux *http.ServeMux) {
	registerSpellingRoutes(mux)
	mux.HandleFunc("/api/dictionary/lookup", protect(dictionaryLookupHandler, false))
	mux.HandleFunc("/api/dictionary/words", protect(dictionaryWordsHandler, false))
	mux.HandleFunc("/api/dictionary/save", protect(dictionarySaveHandler, true))
	mux.HandleFunc("/api/dictionary/remove", protect(dictionaryRemoveHandler, true))
	mux.HandleFunc("/api/dictionary/export", protect(dictionaryExportHandler, false))
}

func dictionaryJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(value)
}

func dictionaryLookupHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Use GET", 405)
		return
	}
	word, err := dictionary.Normalize(r.URL.Query().Get("word"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	entry, err := dictionary.Lookup(word)
	if err != nil {
		http.Error(w, "Could not load the dictionary", 500)
		return
	}
	var saved bool
	if err := db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM saved_words WHERE user_id=? AND word=?)", userIDFromRequest(r), word).Scan(&saved); err != nil {
		http.Error(w, "Could not load saved words", 500)
		return
	}
	dictionaryJSON(w, struct {
		dictionary.Entry
		Saved bool `json:"saved"`
	}{entry, saved})
}

type savedWord struct {
	Word      string `json:"word"`
	CreatedAt string `json:"created_at"`
}

func dictionaryWordsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Use GET", 405)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query != "" {
		normalized, err := dictionary.Normalize(query)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		query = normalized
	}
	cursor := r.URL.Query().Get("cursor")
	if cursor != "" {
		normalized, err := dictionary.Normalize(cursor)
		if err != nil || normalized != cursor {
			http.Error(w, "Invalid word cursor", 400)
			return
		}
	}
	userID := userIDFromRequest(r)
	var total int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM saved_words WHERE user_id=? AND instr(word,?)>0", userID, query).Scan(&total); err != nil {
		http.Error(w, "Could not load saved words", 500)
		return
	}
	rows, err := db.DB.Query("SELECT word,created_at FROM saved_words WHERE user_id=? AND instr(word,?)>0 AND word>? ORDER BY word LIMIT 51", userID, query, cursor)
	if err != nil {
		http.Error(w, "Could not load saved words", 500)
		return
	}
	defer rows.Close()
	items := []savedWord{}
	for rows.Next() {
		var item savedWord
		if err := rows.Scan(&item.Word, &item.CreatedAt); err != nil {
			http.Error(w, "Could not load saved words", 500)
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		http.Error(w, "Could not load saved words", 500)
		return
	}
	next := ""
	if len(items) > 50 {
		items = items[:50]
		next = items[49].Word
	}
	dictionaryJSON(w, struct {
		Words      []savedWord `json:"words"`
		NextCursor string      `json:"next_cursor"`
		Total      int         `json:"total"`
	}{items, next, total})
}

func dictionarySaveHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Use POST", 405)
		return
	}
	var input struct {
		Word string `json:"word"`
	}
	if err := decodeJSONBody(w, r, &input); err != nil {
		http.Error(w, "Invalid word", 400)
		return
	}
	word, err := dictionary.Normalize(input.Word)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	userID := userIDFromRequest(r)
	_, err = db.DB.Exec(`INSERT INTO saved_words (user_id,word,created_at) SELECT ?,?,? WHERE (SELECT COUNT(*) FROM saved_words WHERE user_id=?)<10000 ON CONFLICT(user_id,word) DO NOTHING`, userID, word, time.Now().UTC().Format(time.RFC3339Nano), userID)
	if err != nil {
		http.Error(w, "Could not save word", 500)
		return
	}
	var saved bool
	if err := db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM saved_words WHERE user_id=? AND word=?)", userID, word).Scan(&saved); err != nil {
		http.Error(w, "Could not save word", 500)
		return
	}
	if !saved {
		http.Error(w, "Your word list has reached its 10,000-word limit", 409)
		return
	}
	dictionaryJSON(w, map[string]any{"word": word, "saved": true})
}

func dictionaryRemoveHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Use DELETE", 405)
		return
	}
	word, err := dictionary.Normalize(r.URL.Query().Get("word"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if _, err := db.DB.Exec("DELETE FROM saved_words WHERE user_id=? AND word=?", userIDFromRequest(r), word); err != nil {
		http.Error(w, "Could not remove word", 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func dictionaryExportHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Use GET", 405)
		return
	}
	rows, err := db.DB.Query("SELECT word,created_at FROM saved_words WHERE user_id=? ORDER BY word", userIDFromRequest(r))
	if err != nil {
		http.Error(w, "Could not export words", 500)
		return
	}
	defer rows.Close()
	items := []savedWord{}
	for rows.Next() {
		var item savedWord
		if err := rows.Scan(&item.Word, &item.CreatedAt); err != nil {
			http.Error(w, "Could not export words", 500)
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		http.Error(w, "Could not export words", 500)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="gnotes-dictionary.json"`)
	dictionaryJSON(w, map[string]any{"format": "gnotes-dictionary", "version": 1, "words": items})
}
