package main

import (
	"encoding/json"
	"net/http"
	"strconv"

	"gnotes/internal/db"
)

type adminUser struct {
	ID        int    `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	Active    bool   `json:"active"`
	CreatedAt string `json:"created_at"`
	NoteCount int    `json:"note_count"`
}

func requireAdmin(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if sessionFromContext(r).Role != "admin" {
			http.Error(w, "Administrator access required", http.StatusForbidden)
			return
		}
		handler(w, r)
	}
}

func adminOverviewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var signupsEnabled string
	if err := db.DB.QueryRow("SELECT value FROM settings WHERE key = 'signups_enabled'").Scan(&signupsEnabled); err != nil {
		http.Error(w, "Could not load settings", http.StatusInternalServerError)
		return
	}
	rows, err := db.DB.Query(`
		SELECT users.id, users.username, users.role, users.active, users.created_at, COUNT(notes.id)
		FROM users LEFT JOIN notes ON notes.user_id = users.id
		GROUP BY users.id
		ORDER BY users.created_at ASC`)
	if err != nil {
		http.Error(w, "Could not load users", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	users := make([]adminUser, 0)
	for rows.Next() {
		var user adminUser
		if err := rows.Scan(&user.ID, &user.Username, &user.Role, &user.Active, &user.CreatedAt, &user.NoteCount); err != nil {
			http.Error(w, "Could not load users", http.StatusInternalServerError)
			return
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "Could not load users", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"signups_enabled": signupsEnabled == "true",
		"users":           users,
	})
}

func adminSignupsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Use PUT", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSONBody(w, r, &input); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}
	value := "false"
	if input.Enabled {
		value = "true"
	}
	if _, err := db.DB.Exec("UPDATE settings SET value = ? WHERE key = 'signups_enabled'", value); err != nil {
		http.Error(w, "Could not update signups", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func adminUserStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Use PUT", http.StatusMethodNotAllowed)
		return
	}
	targetID, ok := adminTargetID(w, r)
	if !ok {
		return
	}
	if targetID == userIDFromRequest(r) {
		http.Error(w, "You cannot disable your own account", http.StatusBadRequest)
		return
	}
	var input struct {
		Active bool `json:"active"`
	}
	if err := decodeJSONBody(w, r, &input); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}
	result, err := db.DB.Exec("UPDATE users SET active = ? WHERE id = ? AND role != 'admin'", input.Active, targetID)
	if err != nil {
		http.Error(w, "Could not update user", http.StatusInternalServerError)
		return
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		http.Error(w, "User not found or protected", http.StatusNotFound)
		return
	}
	if !input.Active {
		db.DB.Exec("DELETE FROM sessions WHERE user_id = ?", targetID)
	}
	w.WriteHeader(http.StatusNoContent)
}

func adminRevokeSessionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Use POST", http.StatusMethodNotAllowed)
		return
	}
	targetID, ok := adminTargetID(w, r)
	if !ok {
		return
	}
	if _, err := db.DB.Exec("DELETE FROM sessions WHERE user_id = ?", targetID); err != nil {
		http.Error(w, "Could not revoke sessions", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func adminDeleteUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Use DELETE", http.StatusMethodNotAllowed)
		return
	}
	targetID, ok := adminTargetID(w, r)
	if !ok {
		return
	}
	if targetID == userIDFromRequest(r) {
		http.Error(w, "You cannot delete your own account", http.StatusBadRequest)
		return
	}

	tx, err := db.DB.Begin()
	if err != nil {
		http.Error(w, "Could not delete user", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	var role string
	if err := tx.QueryRow("SELECT role FROM users WHERE id = ?", targetID).Scan(&role); err != nil || role == "admin" {
		http.Error(w, "User not found or protected", http.StatusNotFound)
		return
	}
	if _, err := tx.Exec("DELETE FROM sessions WHERE user_id = ?", targetID); err != nil {
		http.Error(w, "Could not delete user", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec("DELETE FROM notes WHERE user_id = ?", targetID); err != nil {
		http.Error(w, "Could not delete user", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec("DELETE FROM users WHERE id = ?", targetID); err != nil {
		http.Error(w, "Could not delete user", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, "Could not delete user", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func adminTargetID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id < 1 {
		http.Error(w, "Valid user ID required", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}
