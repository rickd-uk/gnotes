package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

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

type adminInvitation struct {
	ID        int        `json:"id"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedBy    *string    `json:"used_by,omitempty"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
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
	if err := rows.Close(); err != nil {
		http.Error(w, "Could not load users", http.StatusInternalServerError)
		return
	}
	policy, err := loadRegistrationPolicy(db.DB)
	if err != nil {
		http.Error(w, "Could not load registration policy", http.StatusInternalServerError)
		return
	}
	stats, err := loadSecurityStats(time.Now())
	if err != nil {
		http.Error(w, "Could not load security statistics", http.StatusInternalServerError)
		return
	}
	invitations, err := loadAdminInvitations()
	if err != nil {
		http.Error(w, "Could not load invitations", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"signups_enabled":     signupsEnabled == "true",
		"users":               users,
		"registration_policy": policy,
		"security_today":      stats,
		"invitations":         invitations,
	})
}

func adminRegistrationPolicyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Use PUT", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		DailyLimit       int  `json:"daily_limit"`
		IPDailyLimit     int  `json:"ip_daily_limit"`
		InvitationNeeded bool `json:"invite_required"`
	}
	if err := decodeJSONBody(w, r, &input); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}
	if input.DailyLimit < 1 || input.DailyLimit > 10_000 || input.IPDailyLimit < 1 || input.IPDailyLimit > 100 || input.IPDailyLimit > input.DailyLimit {
		http.Error(w, "Signup limits are out of range", http.StatusBadRequest)
		return
	}
	tx, err := db.DB.Begin()
	if err != nil {
		http.Error(w, "Could not update registration policy", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	updates := map[string]string{
		"signup_daily_limit":    strconv.Itoa(input.DailyLimit),
		"signup_ip_daily_limit": strconv.Itoa(input.IPDailyLimit),
		"invite_required":       strconv.FormatBool(input.InvitationNeeded),
	}
	for key, value := range updates {
		if _, err := tx.Exec("UPDATE settings SET value = ? WHERE key = ?", value, key); err != nil {
			http.Error(w, "Could not update registration policy", http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, "Could not update registration policy", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func adminCreateInvitationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Use POST", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		ValidDays int `json:"valid_days"`
	}
	if err := decodeJSONBody(w, r, &input); err != nil || input.ValidDays < 1 || input.ValidDays > 90 {
		http.Error(w, "Validity must be between 1 and 90 days", http.StatusBadRequest)
		return
	}
	token, err := randomToken()
	if err != nil {
		http.Error(w, "Could not create invitation", http.StatusInternalServerError)
		return
	}
	code := "gn_" + token
	now := time.Now().UTC()
	expiresAt := now.AddDate(0, 0, input.ValidDays)
	result, err := db.DB.Exec(
		"INSERT INTO invitations (code_hash, created_by, created_at, expires_at) VALUES (?, ?, ?, ?)",
		hashToken(code), userIDFromRequest(r), now, expiresAt,
	)
	if err != nil {
		http.Error(w, "Could not create invitation", http.StatusInternalServerError)
		return
	}
	id, err := result.LastInsertId()
	if err != nil {
		http.Error(w, "Could not create invitation", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"id":         id,
		"code":       code,
		"expires_at": expiresAt,
	})
}

func adminRevokeInvitationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Use DELETE", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id < 1 {
		http.Error(w, "Valid invitation ID required", http.StatusBadRequest)
		return
	}
	result, err := db.DB.Exec("DELETE FROM invitations WHERE id = ? AND used_at IS NULL", id)
	if err != nil {
		http.Error(w, "Could not revoke invitation", http.StatusInternalServerError)
		return
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		http.Error(w, "Invitation not found or already used", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func loadAdminInvitations() ([]adminInvitation, error) {
	rows, err := db.DB.Query(`
		SELECT invitations.id, invitations.created_at, invitations.expires_at,
		       users.username, invitations.used_at
		  FROM invitations
		  LEFT JOIN users ON users.id = invitations.used_by
		 ORDER BY invitations.created_at DESC
		 LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	invitations := make([]adminInvitation, 0)
	for rows.Next() {
		var invitation adminInvitation
		var usedBy sql.NullString
		var usedAt sql.NullTime
		if err := rows.Scan(&invitation.ID, &invitation.CreatedAt, &invitation.ExpiresAt, &usedBy, &usedAt); err != nil {
			return nil, err
		}
		if usedBy.Valid {
			value := usedBy.String
			invitation.UsedBy = &value
		}
		if usedAt.Valid {
			value := usedAt.Time
			invitation.UsedAt = &value
		}
		invitations = append(invitations, invitation)
	}
	return invitations, rows.Err()
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
	if _, err := tx.Exec("DELETE FROM drafts WHERE user_id = ?", targetID); err != nil {
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
