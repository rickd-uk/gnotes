package main

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image/png"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"gnotes/internal/db"
)

var (
	errMFARequired = errors.New("second factor required")
	errMFAInvalid  = errors.New("invalid second factor")
)

func registerMFARoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/auth/mfa", protect(mfaStatusHandler, false))
	mux.HandleFunc("/api/auth/mfa/setup", protect(mfaSetupHandler, true))
	mux.HandleFunc("/api/auth/mfa/cancel", protect(mfaCancelHandler, true))
	mux.HandleFunc("/api/auth/mfa/enable", protect(mfaEnableHandler, true))
	mux.HandleFunc("/api/auth/mfa/recovery-codes", protect(mfaRecoveryHandler, true))
	mux.HandleFunc("/api/auth/mfa/disable", protect(mfaDisableHandler, true))
}

func writeMFAJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(value)
}

// Verification and consumption run in the same transaction as session issuance
// or factor changes. A failed transaction never spends a recovery code.
func authorizeMFALogin(tx *sql.Tx, userID int, code string, now time.Time) error {
	var secret string
	var lastStep int64
	err := tx.QueryRow("SELECT secret,last_step FROM mfa_factors WHERE user_id=? AND enabled_at IS NOT NULL", userID).Scan(&secret, &lastStep)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if code == "" {
		return errMFARequired
	}
	return consumeMFAProof(tx, userID, secret, lastStep, code, now)
}

func matchingMFAStep(secret, code string, now time.Time, lastStep int64) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	for _, digit := range code {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}
	matched := int64(-1)
	for _, offset := range []int64{0, -1, 1} {
		step := now.Unix()/30 + offset
		expected, err := totp.GenerateCodeCustom(secret, time.Unix(step*30, 0), totp.ValidateOpts{Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
		if err == nil && subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 && step > lastStep {
			matched = step
		}
	}
	return matched, matched >= 0
}

func recoveryCodeHash(code string) string {
	code = strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(code), "-", ""), " ", ""))
	if len(code) != 32 {
		return ""
	}
	if _, err := hex.DecodeString(code); err != nil {
		return ""
	}
	return hashToken("mfa-recovery:" + code)
}

func consumeMFAProof(tx *sql.Tx, userID int, secret string, lastStep int64, code string, now time.Time) error {
	if step, ok := matchingMFAStep(secret, strings.TrimSpace(code), now, lastStep); ok {
		result, err := tx.Exec("UPDATE mfa_factors SET last_step=? WHERE user_id=? AND secret=? AND enabled_at IS NOT NULL AND last_step<?", step, userID, secret, step)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n == 1 {
			return nil
		}
		return errMFAInvalid
	}
	hash := recoveryCodeHash(code)
	if hash == "" {
		return errMFAInvalid
	}
	result, err := tx.Exec("DELETE FROM mfa_recovery_codes WHERE user_id=? AND code_hash=?", userID, hash)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 1 {
		return nil
	}
	return errMFAInvalid
}

func writeMFAFailure(w http.ResponseWriter, username string) {
	wait, err := recordLoginFailure(username)
	if err != nil {
		http.Error(w, "Could not check sign-in attempts", 500)
		return
	}
	if wait > 0 {
		setRetryAfter(w, wait)
		recordBlockedEvent("blocked_login")
		http.Error(w, loginWaitMessage(wait), 429)
		return
	}
	http.Error(w, "Invalid or already used code. Use a new authenticator code or an unused recovery code.", 401)
}

func mfaStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Use GET", 405)
		return
	}
	var enabled bool
	var remaining int
	err := db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM mfa_factors WHERE user_id=? AND enabled_at IS NOT NULL),
		(SELECT count(*) FROM mfa_recovery_codes WHERE user_id=?)`, userIDFromRequest(r), userIDFromRequest(r)).Scan(&enabled, &remaining)
	if err != nil {
		http.Error(w, "Could not load two-step verification", 500)
		return
	}
	writeMFAJSON(w, map[string]any{"enabled": enabled, "recovery_codes_remaining": remaining})
}

type mfaInput struct {
	Password string `json:"password"`
	Code     string `json:"code"`
	SetupID  string `json:"setup_id"`
}

func decodeMFAInput(w http.ResponseWriter, r *http.Request, input *mfaInput, passwordRequired bool) ([]byte, bool) {
	if !decodeRecoveryInput(w, r, input) {
		return nil, false
	}
	if len(input.Code) > 128 || len(input.SetupID) > 128 || len(input.Password) > 72 {
		http.Error(w, "Invalid input", 400)
		return nil, false
	}
	allowed, wait, err := consumePersistentLimit("mfa-manage", strconv.Itoa(userIDFromRequest(r)), 12, 15*time.Minute)
	if err != nil {
		http.Error(w, "Could not check request limits", 503)
		return nil, false
	}
	if !allowed {
		setRetryAfter(w, wait)
		http.Error(w, "Too many attempts. Try again later.", 429)
		return nil, false
	}
	if !passwordRequired {
		return nil, true
	}
	var hash []byte
	if err := db.DB.QueryRow("SELECT password_hash FROM users WHERE id=? AND active=1", userIDFromRequest(r)).Scan(&hash); err != nil {
		http.Error(w, "Account unavailable", 403)
		return nil, false
	}
	if !passwordMatches(hash, input.Password) {
		http.Error(w, "Current password is incorrect", 403)
		return nil, false
	}
	return hash, true
}

// Recheck the authenticated session and password after bcrypt and inside the
// transaction so sign-out, reset, disable and simultaneous setup cannot race it.
func freshMFASession(tx *sql.Tx, r *http.Request, expectedHash []byte) error {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return err
	}
	var hash []byte
	var expires time.Time
	err = tx.QueryRow(`SELECT users.password_hash,sessions.expires_at FROM sessions JOIN users ON users.id=sessions.user_id
		WHERE sessions.token_hash=? AND users.id=? AND users.active=1
		AND sessions.csrf_token=?`, hashToken(cookie.Value), userIDFromRequest(r), sessionFromContext(r).CSRFToken).Scan(&hash, &expires)
	if err != nil {
		return err
	}
	if !expires.After(time.Now()) {
		return errors.New("session expired")
	}
	if expectedHash != nil && subtle.ConstantTimeCompare(hash, expectedHash) != 1 {
		return errors.New("account changed")
	}
	return nil
}

func mfaSetupHandler(w http.ResponseWriter, r *http.Request) {
	var input mfaInput
	hash, ok := decodeMFAInput(w, r, &input, true)
	if !ok {
		return
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "gnotes", AccountName: sessionFromContext(r).Username, Period: 30, SecretSize: 20, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		http.Error(w, "Could not prepare authenticator", 500)
		return
	}
	qr, err := key.Image(240, 240)
	if err != nil {
		http.Error(w, "Could not prepare QR code", 500)
		return
	}
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, qr); err != nil {
		http.Error(w, "Could not prepare QR code", 500)
		return
	}
	id, err := randomToken()
	if err != nil {
		http.Error(w, "Could not prepare setup", 500)
		return
	}
	tx, err := db.DB.Begin()
	if err != nil {
		http.Error(w, "Could not prepare setup", 500)
		return
	}
	defer tx.Rollback()
	if freshMFASession(tx, r, hash) != nil {
		http.Error(w, "Session changed. Sign in again.", 403)
		return
	}
	expires := time.Now().UTC().Add(10 * time.Minute)
	cookie, _ := r.Cookie(sessionCookieName)
	result, err := tx.Exec(`INSERT INTO mfa_factors(user_id,secret,setup_id,setup_expires_at,setup_session_hash,setup_password_hash)
		VALUES(?,?,?,?,?,?) ON CONFLICT(user_id) DO UPDATE SET secret=excluded.secret,setup_id=excluded.setup_id,
		setup_expires_at=excluded.setup_expires_at,setup_session_hash=excluded.setup_session_hash,
		setup_password_hash=excluded.setup_password_hash,last_step=-1 WHERE mfa_factors.enabled_at IS NULL`,
		userIDFromRequest(r), key.Secret(), id, expires, hashToken(cookie.Value), hash)
	if err != nil {
		http.Error(w, "Could not prepare setup", 500)
		return
	}
	if n, _ := result.RowsAffected(); n != 1 {
		http.Error(w, "Two-step verification is already enabled", 409)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, "Could not prepare setup", 500)
		return
	}
	writeMFAJSON(w, map[string]any{"setup_id": id, "secret": key.Secret(), "qr_code": "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngData.Bytes()), "expires_at": expires})
}

func mfaCancelHandler(w http.ResponseWriter, r *http.Request) {
	var input mfaInput
	if !decodeRecoveryInput(w, r, &input) {
		return
	}
	cookie, _ := r.Cookie(sessionCookieName)
	_, err := db.DB.Exec(`DELETE FROM mfa_factors WHERE user_id=? AND enabled_at IS NULL AND setup_id=? AND setup_session_hash=?`, userIDFromRequest(r), input.SetupID, hashToken(cookie.Value))
	if err != nil {
		http.Error(w, "Could not cancel setup", 500)
		return
	}
	writeMFAJSON(w, map[string]any{"message": "Setup cancelled"})
}

func newMFARecoveryCodes() ([]string, error) {
	codes := make([]string, 10)
	for i := range codes {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		text := strings.ToUpper(hex.EncodeToString(b))
		codes[i] = text[:8] + "-" + text[8:16] + "-" + text[16:24] + "-" + text[24:]
	}
	return codes, nil
}

func replaceMFARecoveryCodes(tx *sql.Tx, userID int, codes []string) error {
	if _, err := tx.Exec("DELETE FROM mfa_recovery_codes WHERE user_id=?", userID); err != nil {
		return err
	}
	for _, code := range codes {
		if _, err := tx.Exec("INSERT INTO mfa_recovery_codes(user_id,code_hash) VALUES(?,?)", userID, recoveryCodeHash(code)); err != nil {
			return err
		}
	}
	return nil
}

type rotatedMFASession struct {
	token, csrf string
	expires     time.Time
}

func rotateMFASession(tx *sql.Tx, r *http.Request) (rotatedMFASession, error) {
	var result rotatedMFASession
	var err error
	result.token, err = randomToken()
	if err != nil {
		return result, err
	}
	result.csrf, err = randomToken()
	if err != nil {
		return result, err
	}
	result.expires = time.Now().Add(sessionLifetime)
	if _, err = tx.Exec("DELETE FROM sessions WHERE user_id=?", userIDFromRequest(r)); err != nil {
		return result, err
	}
	_, err = tx.Exec("INSERT INTO sessions(token_hash,user_id,csrf_token,created_at,expires_at) VALUES(?,?,?,?,?)", hashToken(result.token), userIDFromRequest(r), result.csrf, time.Now(), result.expires)
	return result, err
}

func finishMFAChange(w http.ResponseWriter, r *http.Request, tx *sql.Tx, codes []string, enabled bool) {
	session, err := rotateMFASession(tx, r)
	if err != nil {
		http.Error(w, "Could not update sessions", 500)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, "Could not save two-step verification", 500)
		return
	}
	setSessionCookie(w, r, session.token, session.expires)
	writeMFAJSON(w, map[string]any{"enabled": enabled, "recovery_codes": codes, "csrf_token": session.csrf})
}

func mfaEnableHandler(w http.ResponseWriter, r *http.Request) {
	var input mfaInput
	if _, ok := decodeMFAInput(w, r, &input, false); !ok {
		return
	}
	codes, err := newMFARecoveryCodes()
	if err != nil {
		http.Error(w, "Could not create recovery codes", 500)
		return
	}
	tx, err := db.DB.Begin()
	if err != nil {
		http.Error(w, "Could not enable two-step verification", 500)
		return
	}
	defer tx.Rollback()
	var secret string
	var hash []byte
	var expires time.Time
	cookie, _ := r.Cookie(sessionCookieName)
	now := time.Now().UTC()
	err = tx.QueryRow(`SELECT secret,setup_password_hash,setup_expires_at FROM mfa_factors WHERE user_id=? AND enabled_at IS NULL
		AND setup_id=? AND setup_session_hash=?`, userIDFromRequest(r), input.SetupID, hashToken(cookie.Value)).Scan(&secret, &hash, &expires)
	if err != nil || !expires.After(now) || freshMFASession(tx, r, hash) != nil {
		http.Error(w, "Setup expired or changed. Start setup again.", 409)
		return
	}
	step, ok := matchingMFAStep(secret, strings.TrimSpace(input.Code), now, -1)
	if !ok {
		http.Error(w, "Invalid authenticator code. Try the latest code.", 403)
		return
	}
	if _, err := tx.Exec(`UPDATE mfa_factors SET enabled_at=?,last_step=?,setup_id=NULL,setup_expires_at=NULL,
		setup_session_hash=NULL,setup_password_hash=NULL WHERE user_id=?`, now, step, userIDFromRequest(r)); err != nil {
		http.Error(w, "Could not enable two-step verification", 500)
		return
	}
	if err := replaceMFARecoveryCodes(tx, userIDFromRequest(r), codes); err != nil {
		http.Error(w, "Could not create recovery codes", 500)
		return
	}
	finishMFAChange(w, r, tx, codes, true)
}

func mfaRecoveryHandler(w http.ResponseWriter, r *http.Request) { mfaChangeHandler(w, r, false) }
func mfaDisableHandler(w http.ResponseWriter, r *http.Request)  { mfaChangeHandler(w, r, true) }
func mfaChangeHandler(w http.ResponseWriter, r *http.Request, disable bool) {
	var input mfaInput
	hash, ok := decodeMFAInput(w, r, &input, true)
	if !ok {
		return
	}
	codes, err := newMFARecoveryCodes()
	if err != nil {
		http.Error(w, "Could not create recovery codes", 500)
		return
	}
	tx, err := db.DB.Begin()
	if err != nil {
		http.Error(w, "Could not change two-step verification", 500)
		return
	}
	defer tx.Rollback()
	if freshMFASession(tx, r, hash) != nil {
		http.Error(w, "Session changed. Sign in again.", 403)
		return
	}
	var secret string
	var lastStep int64
	if err := tx.QueryRow("SELECT secret,last_step FROM mfa_factors WHERE user_id=? AND enabled_at IS NOT NULL", userIDFromRequest(r)).Scan(&secret, &lastStep); err != nil {
		http.Error(w, "Two-step verification is not enabled", 409)
		return
	}
	if err := consumeMFAProof(tx, userIDFromRequest(r), secret, lastStep, input.Code, time.Now()); err != nil {
		if errors.Is(err, errMFAInvalid) {
			http.Error(w, "Invalid or already used code. Use a new authenticator code or an unused recovery code.", 403)
		} else {
			http.Error(w, "Could not verify code", 500)
		}
		return
	}
	if disable {
		_, err = tx.Exec("DELETE FROM mfa_factors WHERE user_id=?", userIDFromRequest(r))
		codes = nil
	} else {
		err = replaceMFARecoveryCodes(tx, userIDFromRequest(r), codes)
	}
	if err != nil {
		http.Error(w, "Could not change two-step verification", 500)
		return
	}
	finishMFAChange(w, r, tx, codes, !disable)
}
