package main

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"gnotes/internal/db"
)

type passwordChangeInput struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
	Code            string `json:"code"`
}

func changePasswordHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var input passwordChangeInput
	if !decodeRecoveryInput(w, r, &input) {
		return
	}
	if len(input.CurrentPassword) > 72 || len(input.Code) > 128 || len(input.NewPassword) < 12 || len(input.NewPassword) > 72 {
		http.Error(w, "New password must be 12–72 bytes; check the current password and verification code too.", http.StatusBadRequest)
		return
	}
	userID := userIDFromRequest(r)
	allowed, wait, err := consumePersistentLimit("password-change", strconv.Itoa(userID), 12, 15*time.Minute)
	if err != nil {
		http.Error(w, "Could not check request limits", http.StatusServiceUnavailable)
		return
	}
	if !allowed {
		setRetryAfter(w, wait)
		http.Error(w, "Too many password-change attempts. Try again later.", http.StatusTooManyRequests)
		return
	}
	var previousHash []byte
	if err := db.DB.QueryRow("SELECT password_hash FROM users WHERE id=? AND active=1", userID).Scan(&previousHash); err != nil {
		http.Error(w, "Account unavailable", http.StatusForbidden)
		return
	}
	if !passwordMatches(previousHash, input.CurrentPassword) {
		http.Error(w, "Current password is incorrect", http.StatusForbidden)
		return
	}
	newHash, err := hashPassword(input.NewPassword)
	if err != nil {
		http.Error(w, "Could not change password", http.StatusInternalServerError)
		return
	}
	tx, err := db.DB.Begin()
	if err != nil {
		http.Error(w, "Could not change password", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	// Share MFA's transaction-time session/password recheck. A sign-out,
	// password reset, disable or concurrent password change wins over stale proof.
	if freshMFASession(tx, r, previousHash) != nil {
		http.Error(w, "Your session or password changed. Sign in again.", http.StatusForbidden)
		return
	}
	if err := authorizeMFALogin(tx, userID, input.Code, time.Now()); err != nil {
		if errors.Is(err, errMFARequired) || errors.Is(err, errMFAInvalid) {
			http.Error(w, "Enter a fresh authenticator code or an unused recovery code.", http.StatusForbidden)
		} else {
			http.Error(w, "Could not verify two-step code", http.StatusInternalServerError)
		}
		return
	}
	result, err := tx.Exec("UPDATE users SET password_hash=? WHERE id=? AND active=1 AND password_hash=?", newHash, userID, previousHash)
	if err != nil {
		http.Error(w, "Could not change password", http.StatusInternalServerError)
		return
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		http.Error(w, "Your account changed. Sign in again.", http.StatusConflict)
		return
	}
	for _, query := range []string{
		"DELETE FROM password_reset_tokens WHERE user_id=?",
		"DELETE FROM email_verification_tokens WHERE user_id=?",
		"DELETE FROM mfa_factors WHERE user_id=? AND enabled_at IS NULL",
	} {
		if _, err := tx.Exec(query, userID); err != nil {
			http.Error(w, "Could not update account security", http.StatusInternalServerError)
			return
		}
	}
	// Preserve this device with a fresh cookie/CSRF pair and revoke every
	// previous session in the same transaction as the password and MFA proof.
	session, err := rotateMFASession(tx, r)
	if err != nil {
		http.Error(w, "Could not update sessions", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, "Could not save password", http.StatusInternalServerError)
		return
	}
	setSessionCookie(w, r, session.token, session.expires)
	writeMFAJSON(w, map[string]string{
		"message":    "Password changed. Other devices have been signed out. This device stays signed in.",
		"csrf_token": session.csrf,
	})
}
