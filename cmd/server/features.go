package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"gnotes/internal/db"
)

var featureColumns = map[string]bool{
	"organization": true, "writing_tools": true, "note_controls": true,
	"appearance": true, "advanced_editing": true, "offline_reading": true,
}

type featurePreferences struct {
	Organization    bool   `json:"organization"`
	WritingTools    bool   `json:"writing_tools"`
	NoteControls    bool   `json:"note_controls"`
	Appearance      bool   `json:"appearance"`
	AdvancedEditing bool   `json:"advanced_editing"`
	OfflineReading  bool   `json:"offline_reading"`
	Theme           string `json:"theme"`
}

func featurePreferencesHandler(w http.ResponseWriter, r *http.Request) {
	session := sessionFromContext(r)
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Method == http.MethodPut {
		if !validCSRFToken(r.Header.Get("X-CSRF-Token"), session.CSRFToken) {
			http.Error(w, "Invalid CSRF token", http.StatusForbidden)
			return
		}
		mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if mediaType != "application/json" {
			http.Error(w, "Use application/json", http.StatusUnsupportedMediaType)
			return
		}
		var change struct {
			Mode    string `json:"mode"`
			Feature string `json:"feature"`
			Enabled *bool  `json:"enabled"`
			Theme   string `json:"theme"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&change); err != nil {
			http.Error(w, "Invalid preference", 400)
			return
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			http.Error(w, "Invalid preference", 400)
			return
		}
		var query string
		var value any
		switch {
		case (change.Mode == "simple" || change.Mode == "full") && change.Feature == "" && change.Enabled == nil && change.Theme == "":
			query = `UPDATE feature_preferences SET organization=?, writing_tools=?, note_controls=?, appearance=?, advanced_editing=?, offline_reading=? WHERE user_id=?`
			value = change.Mode == "full"
		case featureColumns[change.Feature] && change.Enabled != nil && change.Mode == "" && change.Theme == "":
			// Column names come exclusively from the fixed allowlist above.
			query = `UPDATE feature_preferences SET ` + change.Feature + `=? WHERE user_id=?`
			value = *change.Enabled
		case (change.Theme == "light" || change.Theme == "dark") && change.Mode == "" && change.Feature == "" && change.Enabled == nil:
			query = `UPDATE feature_preferences SET theme=? WHERE user_id=?`
			value = change.Theme
		default:
			http.Error(w, "Invalid preference", 400)
			return
		}
		tx, err := db.DB.BeginTx(r.Context(), nil)
		if err != nil {
			http.Error(w, "Could not save preferences", 500)
			return
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(r.Context(), "INSERT OR IGNORE INTO feature_preferences(user_id) VALUES (?)", session.UserID); err == nil {
			args := []any{value, session.UserID}
			if change.Mode != "" {
				args = []any{value, value, value, value, value, value, session.UserID}
			}
			_, err = tx.ExecContext(r.Context(), query, args...)
		}
		if err == nil {
			err = tx.Commit()
		}
		if err != nil {
			http.Error(w, "Could not save preferences", 500)
			return
		}
	}
	prefs, err := loadFeaturePreferences(session.UserID)
	if err != nil {
		http.Error(w, "Could not load preferences", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(prefs)
}

func loadFeaturePreferences(userID int) (featurePreferences, error) {
	// Missing rows identify accounts created before Simple mode.
	prefs := featurePreferences{true, true, true, true, true, true, "light"}
	err := db.DB.QueryRow(`SELECT organization,writing_tools,note_controls,appearance,advanced_editing,offline_reading,theme FROM feature_preferences WHERE user_id=?`, userID).Scan(&prefs.Organization, &prefs.WritingTools, &prefs.NoteControls, &prefs.Appearance, &prefs.AdvancedEditing, &prefs.OfflineReading, &prefs.Theme)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return prefs, err
}
