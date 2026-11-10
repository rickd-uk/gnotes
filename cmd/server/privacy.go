package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"gnotes/internal/db"
)

type privacySettings struct {
	Hash       []byte
	HideTitles bool
	Revision   int64
}

func loadPrivacySettings(userID int) (privacySettings, error) {
	var settings privacySettings
	err := db.DB.QueryRow("SELECT code_hash,hide_titles,revision FROM privacy_settings WHERE user_id=?", userID).Scan(&settings.Hash, &settings.HideTitles, &settings.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return settings, nil
	}
	return settings, err
}

func privacyStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Use GET", 405)
		return
	}
	settings, err := loadPrivacySettings(userIDFromRequest(r))
	if err != nil {
		http.Error(w, "Could not load privacy settings", 500)
		return
	}
	var count int
	var mfa bool
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM notes WHERE user_id=? AND privacy_locked=1", userIDFromRequest(r)).Scan(&count); err != nil {
		http.Error(w, "Could not count private notes", 500)
		return
	}
	if err := db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM mfa_factors WHERE user_id=? AND enabled_at IS NOT NULL)", userIDFromRequest(r)).Scan(&mfa); err != nil {
		http.Error(w, "Could not load account security", 500)
		return
	}
	writeMFAJSON(w, map[string]any{"configured": len(settings.Hash) > 0, "hide_titles": settings.HideTitles, "locked_count": count, "mfa_enabled": mfa})
}

// Count proof attempts persistently; successful proofs clear their own budget.
func privacyProofAllowed(w http.ResponseWriter, r *http.Request, scope string) bool {
	allowed, wait, err := consumePersistentLimit(scope, strconv.Itoa(userIDFromRequest(r)), 20, 15*time.Minute)
	if err != nil {
		http.Error(w, "Could not check request limits", 503)
		return false
	}
	if !allowed {
		setRetryAfter(w, wait)
		http.Error(w, "Too many attempts. Try again later.", 429)
		return false
	}
	return true
}

func clearPrivacyProofLimit(tx *sql.Tx, userID int, scope string) error {
	_, err := tx.Exec("DELETE FROM rate_limits WHERE scope=? AND key_hash=?", scope, hashToken(scope+":"+strconv.Itoa(userID)))
	return err
}

func validPrivacyCode(code string) bool {
	if len(code) < 4 || len(code) > 12 {
		return false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func privacyCodeHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Action     string `json:"action"`
		Code       string `json:"code"`
		NewCode    string `json:"new_code"`
		Password   string `json:"password"`
		MFACode    string `json:"mfa_code"`
		HideTitles bool   `json:"hide_titles"`
	}
	if !decodeRecoveryInput(w, r, &input) {
		return
	}
	if !validPrivacyCode(input.NewCode) || len(input.Password) > 72 || len(input.Code) > 12 || len(input.MFACode) > 128 {
		http.Error(w, "Use a privacy code of 4–12 digits.", 400)
		return
	}
	userID := userIDFromRequest(r)
	settings, err := loadPrivacySettings(userID)
	if err != nil {
		http.Error(w, "Could not load privacy settings", 500)
		return
	}
	if input.Action != "setup" && input.Action != "change" && input.Action != "reset" {
		http.Error(w, "Choose setup, change, or reset", 400)
		return
	}
	if (input.Action == "setup") != (len(settings.Hash) == 0) {
		http.Error(w, "Privacy settings changed. Reopen this dialog.", 409)
		return
	}
	scope := "privacy-code"
	var passwordHash []byte
	if input.Action == "change" {
		if !privacyProofAllowed(w, r, scope) {
			return
		}
		if !validPrivacyCode(input.Code) || !passwordMatches(settings.Hash, input.Code) {
			http.Error(w, "Privacy code is incorrect.", 403)
			return
		}
	} else {
		scope = "privacy-recovery"
		if !privacyProofAllowed(w, r, scope) {
			return
		}
		if err := db.DB.QueryRow("SELECT password_hash FROM users WHERE id=? AND active=1", userID).Scan(&passwordHash); err != nil {
			http.Error(w, "Account unavailable", 403)
			return
		}
		if !passwordMatches(passwordHash, input.Password) {
			http.Error(w, "Account password is incorrect.", 403)
			return
		}
	}
	newHash, err := hashPassword(input.NewCode)
	if err != nil {
		http.Error(w, "Could not prepare privacy code", 500)
		return
	}
	tx, err := db.DB.Begin()
	if err != nil {
		http.Error(w, "Could not change privacy code", 500)
		return
	}
	defer tx.Rollback()
	if freshMFASession(tx, r, passwordHash) != nil {
		http.Error(w, "Your session changed. Sign in again.", 403)
		return
	}
	if input.Action != "change" {
		if err := authorizeMFALogin(tx, userID, input.MFACode, time.Now()); err != nil {
			if errors.Is(err, errMFARequired) || errors.Is(err, errMFAInvalid) {
				http.Error(w, "Enter a fresh authenticator code or unused recovery code.", 403)
			} else {
				http.Error(w, "Could not verify account security", 500)
			}
			return
		}
	}
	if settings.Revision == 0 {
		_, err = tx.Exec("INSERT INTO privacy_settings(user_id,code_hash,hide_titles) VALUES(?,?,?)", userID, newHash, input.HideTitles)
	} else {
		var result sql.Result
		result, err = tx.Exec("UPDATE privacy_settings SET code_hash=?,hide_titles=?,revision=revision+1 WHERE user_id=? AND revision=?", newHash, input.HideTitles, userID, settings.Revision)
		if err == nil {
			count, countErr := result.RowsAffected()
			if countErr != nil || count != 1 {
				http.Error(w, "Privacy code changed. Reopen this dialog.", 409)
				return
			}
		}
	}
	if err != nil {
		http.Error(w, "Privacy settings changed. Reopen this dialog.", 409)
		return
	}
	if _, err = tx.Exec("UPDATE notes SET privacy_hide_title=? WHERE user_id=? AND privacy_locked=1", input.HideTitles, userID); err != nil {
		http.Error(w, "Could not change privacy settings", 500)
		return
	}
	// Password recovery also clears a forgotten code's failed-attempt budget.
	if clearPrivacyProofLimit(tx, userID, scope) != nil || clearPrivacyProofLimit(tx, userID, "privacy-code") != nil {
		http.Error(w, "Could not update privacy limits", 500)
		return
	}
	if err = tx.Commit(); err != nil {
		http.Error(w, "Could not save privacy settings", 500)
		return
	}
	writeMFAJSON(w, map[string]string{"message": "Privacy code saved. Existing private notes stay locked and use the new code."})
}

func privacyNoteHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID     int    `json:"id"`
		All    bool   `json:"all"`
		Locked *bool  `json:"locked"`
		Code   string `json:"code"`
	}
	if !decodeRecoveryInput(w, r, &input) {
		return
	}
	if input.Locked == nil || len(input.Code) > 12 || (!input.All && input.ID < 1) || (input.All && *input.Locked) {
		http.Error(w, "Choose a note to lock or unlock, or unlock all notes.", 400)
		return
	}
	userID := userIDFromRequest(r)
	settings, err := loadPrivacySettings(userID)
	if err != nil {
		http.Error(w, "Could not load privacy settings", 500)
		return
	}
	if len(settings.Hash) == 0 {
		http.Error(w, "Set a privacy code first.", 409)
		return
	}
	if !*input.Locked {
		if !privacyProofAllowed(w, r, "privacy-code") {
			return
		}
		if !validPrivacyCode(input.Code) || !passwordMatches(settings.Hash, input.Code) {
			http.Error(w, "Privacy code is incorrect.", 403)
			return
		}
	}
	tx, err := db.DB.Begin()
	if err != nil {
		http.Error(w, "Could not change privacy lock", 500)
		return
	}
	defer tx.Rollback()
	if freshMFASession(tx, r, nil) != nil {
		http.Error(w, "Your session changed. Sign in again.", 403)
		return
	}
	var revision int64
	if err := tx.QueryRow("SELECT revision FROM privacy_settings WHERE user_id=?", userID).Scan(&revision); err != nil || revision != settings.Revision {
		http.Error(w, "Privacy code changed. Try again.", 409)
		return
	}
	query := "UPDATE notes SET privacy_locked=?,privacy_hide_title=? WHERE user_id=?"
	args := []any{*input.Locked, settings.HideTitles, userID}
	if !input.All {
		query += " AND id=?"
		args = append(args, input.ID)
	}
	result, err := tx.Exec(query, args...)
	if err != nil {
		http.Error(w, "Could not change privacy lock", 500)
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "Could not change privacy lock", 500)
		return
	}
	if !input.All && count == 0 {
		http.Error(w, "Note not found", 404)
		return
	}
	if !*input.Locked && clearPrivacyProofLimit(tx, userID, "privacy-code") != nil {
		http.Error(w, "Could not update privacy limits", 500)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, "Could not save privacy lock", 500)
		return
	}
	writeMFAJSON(w, map[string]any{"privacy_locked": *input.Locked, "changed": count, "hide_titles": settings.HideTitles})
}
