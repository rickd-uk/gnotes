package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const resetLifetime = 30 * time.Minute
const verificationLifetime = time.Hour
const recoveryResponse = "If those details match an active account with a verified recovery email, a reset link will arrive shortly."

type recoveryConfig struct {
	APIKey, Sender, SenderName, PublicURL string
}

func (c recoveryConfig) enabled() bool { return c.APIKey != "" }

func recoveryEnabled() bool {
	c, err := loadRecoveryConfig()
	return err == nil && c.enabled()
}

func loadRecoveryConfig() (recoveryConfig, error) {
	c := recoveryConfig{
		APIKey:     strings.TrimSpace(os.Getenv("BREVO_API_KEY")),
		Sender:     strings.TrimSpace(os.Getenv("GNOTES_EMAIL_FROM")),
		SenderName: strings.TrimSpace(os.Getenv("GNOTES_EMAIL_FROM_NAME")),
		PublicURL:  strings.TrimRight(strings.TrimSpace(os.Getenv("GNOTES_PUBLIC_URL")), "/"),
	}
	// A sender and public URL may be prepared before installing the secret.
	if !c.enabled() {
		return c, nil
	}
	if !validRecoveryEmail(c.Sender) {
		return c, errors.New("GNOTES_EMAIL_FROM must be an email address")
	}
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return c, errors.New("GNOTES_PUBLIC_URL must be the application origin without a path, query, or fragment")
	}
	local := u.Hostname() == "localhost" || net.ParseIP(u.Hostname()).IsLoopback()
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return c, errors.New("GNOTES_PUBLIC_URL requires HTTPS except on localhost")
	}
	if c.SenderName == "" {
		c.SenderName = "gnotes"
	}
	return c, nil
}

func validRecoveryEmail(email string) bool {
	address, err := mail.ParseAddress(email)
	return len(email) <= 254 && err == nil && address.Address == email && !strings.ContainsAny(email, "\r\n")
}

type resetMailJob struct{ username, email string }

type recoveryService struct {
	database *sql.DB
	config   recoveryConfig
	client   *http.Client
	jobs     chan resetMailJob
	wg       sync.WaitGroup
}

func newRecoveryService(database *sql.DB, config recoveryConfig) *recoveryService {
	return &recoveryService{
		database: database, config: config, jobs: make(chan resetMailJob, 32),
		client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

func (s *recoveryService) start(ctx context.Context) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case job := <-s.jobs:
				if err := s.issueResetEmail(ctx, job); err != nil && ctx.Err() == nil {
					// Do not log addresses, tokens, provider response bodies, or credentials.
					log.Print("password recovery email could not be sent")
				}
			}
		}
	}()
}

func recoveryLimit(w http.ResponseWriter, r *http.Request, scope string, perIP, global int) bool {
	for _, limit := range []struct {
		scope, key string
		count      int
	}{
		{scope + "-global", "all", global}, {scope + "-ip", clientIP(r), perIP},
	} {
		allowed, wait, err := consumePersistentLimit(limit.scope, limit.key, limit.count, time.Hour)
		if err != nil {
			http.Error(w, "Could not check request limits", http.StatusServiceUnavailable)
			return false
		}
		if !allowed {
			setRetryAfter(w, wait)
			http.Error(w, "Too many attempts. Please try again later", http.StatusTooManyRequests)
			return false
		}
	}
	return true
}

func decodeRecoveryInput(w http.ResponseWriter, r *http.Request, destination any) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "Use POST", http.StatusMethodNotAllowed)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
	if err := decodeJSONBody(w, r, destination); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return false
	}
	return true
}

func writeRecoveryJSON(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"message": message})
}

func (s *recoveryService) requestReset(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Email    string `json:"email"`
	}
	if !decodeRecoveryInput(w, r, &input) {
		return
	}
	if !s.config.enabled() {
		http.Error(w, "Email recovery is not configured", http.StatusServiceUnavailable)
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	input.Email = strings.TrimSpace(input.Email)
	if !usernamePattern.MatchString(input.Username) || !validRecoveryEmail(input.Email) {
		http.Error(w, "Enter your username and recovery email address", http.StatusBadRequest)
		return
	}
	if !recoveryLimit(w, r, "recovery-request", 10, 100) {
		return
	}
	// Limit the submitted pair before any account lookup. A mistyped email must
	// not block its correction. Unknown and matching pairs follow identical rules.
	pair := strings.ToLower(input.Username) + ":" + strings.ToLower(input.Email)
	for _, limit := range []struct {
		scope  string
		count  int
		window time.Duration
	}{
		{"recovery-pair-minute", 1, time.Minute},
		{"recovery-pair-hour", 3, time.Hour},
	} {
		allowed, wait, err := consumePersistentLimit(limit.scope, pair, limit.count, limit.window)
		if err != nil {
			http.Error(w, "Could not request recovery", http.StatusServiceUnavailable)
			return
		}
		if !allowed {
			setRetryAfter(w, wait)
			http.Error(w, loginWaitMessage(wait), http.StatusTooManyRequests)
			return
		}
	}
	select {
	case s.jobs <- resetMailJob{input.Username, input.Email}:
	default:
		http.Error(w, "Recovery requests are busy. Please try again shortly", http.StatusServiceUnavailable)
		return
	}
	writeRecoveryJSON(w, http.StatusAccepted, recoveryResponse)
}

func (s *recoveryService) issueResetEmail(ctx context.Context, job resetMailJob) error {
	var userID int
	var email string
	var passwordHash []byte
	err := s.database.QueryRowContext(ctx, `SELECT id, email, password_hash FROM users
		WHERE username = ? AND lower(email) = lower(?) AND active = 1 AND email_verified_at IS NOT NULL`, job.username, job.email).Scan(&userID, &email, &passwordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	token, err := randomToken()
	if err != nil {
		return err
	}
	if _, err = s.database.ExecContext(ctx, `INSERT INTO password_reset_tokens (token_hash, user_id, email, password_hash, expires_at)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT(user_id) DO UPDATE SET token_hash=excluded.token_hash, email=excluded.email,
		password_hash=excluded.password_hash, expires_at=excluded.expires_at`, hashToken(token), userID, email, passwordHash, time.Now().UTC().Add(resetLifetime)); err != nil {
		return err
	}
	err = s.sendEmail(ctx, email, "Reset your gnotes password", "A password reset was requested for your gnotes account ("+job.username+").\n\n"+
		"Open this link to choose a new password within 30 minutes:\n"+s.config.PublicURL+"/#reset-password="+token+
		"\n\nIf you did not request this, ignore this email. Your password has not changed.")
	if err != nil {
		_, _ = s.database.Exec("DELETE FROM password_reset_tokens WHERE token_hash = ?", hashToken(token))
	}
	return err
}

func validRecoveryToken(token string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(decoded) == 32 && len(token) == 43
}

func (s *recoveryService) resetPassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !decodeRecoveryInput(w, r, &input) {
		return
	}
	if !s.config.enabled() {
		http.Error(w, "Email recovery is not configured", http.StatusServiceUnavailable)
		return
	}
	if !recoveryLimit(w, r, "recovery-complete", 20, 200) {
		return
	}
	if !validRecoveryToken(input.Token) {
		invalidRecoveryLink(w)
		return
	}
	if len(input.Password) < 12 || len(input.Password) > 72 {
		http.Error(w, "Password must be 12–72 bytes", http.StatusBadRequest)
		return
	}
	var userID int
	var username string
	err := s.database.QueryRow(`SELECT users.id, users.username FROM users JOIN password_reset_tokens t ON t.user_id = users.id
		WHERE t.token_hash = ? AND t.expires_at > ? AND users.active = 1 AND users.email_verified_at IS NOT NULL
		AND t.email = users.email AND t.password_hash = users.password_hash`, hashToken(input.Token), time.Now().UTC()).Scan(&userID, &username)
	if errors.Is(err, sql.ErrNoRows) {
		invalidRecoveryLink(w)
		return
	}
	if err != nil {
		http.Error(w, "Could not reset password", http.StatusInternalServerError)
		return
	}
	passwordHash, err := hashPassword(input.Password)
	if err != nil {
		http.Error(w, "Could not reset password", http.StatusInternalServerError)
		return
	}
	tx, err := s.database.Begin()
	if err != nil {
		http.Error(w, "Could not reset password", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	// Recheck in the write transaction so simultaneous consumption cannot reuse
	// a token, or overwrite a password changed while bcrypt was running.
	result, err := tx.Exec(`UPDATE users SET password_hash = ? WHERE id = ? AND active = 1 AND email_verified_at IS NOT NULL
		AND EXISTS (SELECT 1 FROM password_reset_tokens t WHERE t.user_id = users.id AND t.token_hash = ?
		AND t.expires_at > ? AND t.email = users.email AND t.password_hash = users.password_hash)`, passwordHash, userID, hashToken(input.Token), time.Now().UTC())
	if err != nil {
		http.Error(w, "Could not reset password", http.StatusInternalServerError)
		return
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		invalidRecoveryLink(w)
		return
	}
	for _, query := range []string{"DELETE FROM sessions WHERE user_id = ?", "DELETE FROM password_reset_tokens WHERE user_id = ?", "DELETE FROM email_verification_tokens WHERE user_id = ?"} {
		if _, err = tx.Exec(query, userID); err != nil {
			http.Error(w, "Could not reset password", http.StatusInternalServerError)
			return
		}
	}
	if _, err = tx.Exec("DELETE FROM login_cooldowns WHERE username_hash = ?", hashUsername(username)); err != nil {
		http.Error(w, "Could not reset password", http.StatusInternalServerError)
		return
	}
	if err = tx.Commit(); err != nil {
		http.Error(w, "Could not reset password", http.StatusInternalServerError)
		return
	}
	clearSessionCookie(w, r)
	writeRecoveryJSON(w, http.StatusOK, "Password changed. Sign in with your new password. Other sessions have been signed out.")
}

func invalidRecoveryLink(w http.ResponseWriter) {
	http.Error(w, "This link is invalid, expired, or already used. Request a new link.", http.StatusBadRequest)
}

func (s *recoveryService) requestVerification(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeRecoveryInput(w, r, &input) {
		return
	}
	if !s.config.enabled() {
		http.Error(w, "Email recovery is not configured", http.StatusServiceUnavailable)
		return
	}
	input.Email = strings.TrimSpace(input.Email)
	if !validRecoveryEmail(input.Email) {
		http.Error(w, "Enter a valid email address", http.StatusBadRequest)
		return
	}
	if !recoveryLimit(w, r, "email-verification", 5, 50) {
		return
	}
	userID := userIDFromRequest(r)
	var passwordHash []byte
	if err := s.database.QueryRow("SELECT password_hash FROM users WHERE id = ? AND active = 1", userID).Scan(&passwordHash); err != nil {
		http.Error(w, "Could not verify account", http.StatusInternalServerError)
		return
	}
	if !passwordMatches(passwordHash, input.Password) {
		http.Error(w, "Current password is incorrect", http.StatusForbidden)
		return
	}
	allowed, wait, err := consumePersistentLimit("email-verification-account", fmt.Sprint(userID), 3, time.Hour)
	if err != nil {
		http.Error(w, "Could not request verification", http.StatusInternalServerError)
		return
	}
	if !allowed {
		setRetryAfter(w, wait)
		http.Error(w, "Too many verification requests. Please try again later", http.StatusTooManyRequests)
		return
	}
	token, err := randomToken()
	if err != nil {
		http.Error(w, "Could not request verification", http.StatusInternalServerError)
		return
	}
	_, err = s.database.Exec(`INSERT INTO email_verification_tokens (token_hash, user_id, email, password_hash, expires_at)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT(user_id) DO UPDATE SET token_hash=excluded.token_hash, email=excluded.email,
		password_hash=excluded.password_hash, expires_at=excluded.expires_at`, hashToken(token), userID, input.Email, passwordHash, time.Now().UTC().Add(verificationLifetime))
	if err != nil {
		http.Error(w, "Could not request verification", http.StatusInternalServerError)
		return
	}
	err = s.sendEmail(r.Context(), input.Email, "Verify your gnotes recovery email", "Confirm this email address for password recovery on your gnotes account ("+sessionFromContext(r).Username+").\n\n"+
		"Open this link and confirm within one hour:\n"+s.config.PublicURL+"/#verify-email="+token+
		"\n\nIf you did not request this, ignore this email. Your recovery email has not changed.")
	if err != nil {
		_, _ = s.database.Exec("DELETE FROM email_verification_tokens WHERE token_hash = ?", hashToken(token))
		log.Print("recovery email verification message could not be sent")
		http.Error(w, "Could not send verification email. Please try again later", http.StatusServiceUnavailable)
		return
	}
	writeRecoveryJSON(w, http.StatusOK, "Check your inbox for a verification link. It expires in one hour.")
}

func (s *recoveryService) verifyEmail(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token string `json:"token"`
	}
	if !decodeRecoveryInput(w, r, &input) {
		return
	}
	if !s.config.enabled() {
		http.Error(w, "Email recovery is not configured", http.StatusServiceUnavailable)
		return
	}
	if !recoveryLimit(w, r, "email-confirmation", 20, 200) {
		return
	}
	if !validRecoveryToken(input.Token) {
		invalidRecoveryLink(w)
		return
	}
	tx, err := s.database.Begin()
	if err != nil {
		http.Error(w, "Could not verify email", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	var userID int
	var email string
	err = tx.QueryRow(`SELECT t.user_id, t.email FROM email_verification_tokens t JOIN users ON users.id=t.user_id
		WHERE t.token_hash = ? AND t.expires_at > ? AND users.active = 1 AND t.password_hash = users.password_hash`, hashToken(input.Token), time.Now().UTC()).Scan(&userID, &email)
	if errors.Is(err, sql.ErrNoRows) {
		invalidRecoveryLink(w)
		return
	}
	if err != nil {
		http.Error(w, "Could not verify email", http.StatusInternalServerError)
		return
	}
	if _, err = tx.Exec("UPDATE users SET email = ?, email_verified_at = ? WHERE id = ?", email, time.Now().UTC(), userID); err != nil {
		http.Error(w, "Could not verify email", http.StatusInternalServerError)
		return
	}
	for _, query := range []string{"DELETE FROM email_verification_tokens WHERE user_id = ?", "DELETE FROM password_reset_tokens WHERE user_id = ?"} {
		if _, err = tx.Exec(query, userID); err != nil {
			http.Error(w, "Could not verify email", http.StatusInternalServerError)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		http.Error(w, "Could not verify email", http.StatusInternalServerError)
		return
	}
	writeRecoveryJSON(w, http.StatusOK, "Recovery email verified. You can now request a password reset from the sign-in screen.")
}

func (s *recoveryService) sendEmail(ctx context.Context, recipient, subject, text string) error {
	body, err := json.Marshal(map[string]any{
		"sender": map[string]string{"email": s.config.Sender, "name": s.config.SenderName},
		"to":     []map[string]string{{"email": recipient}}, "subject": subject, "textContent": text,
	})
	if err != nil {
		return errors.New("could not prepare email")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.brevo.com/v3/smtp/email", bytes.NewReader(body))
	if err != nil {
		return errors.New("could not prepare email")
	}
	req.Header.Set("api-key", s.config.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	response, err := s.client.Do(req)
	if err != nil {
		return errors.New("email service unavailable")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusCreated {
		return errors.New("email service rejected request")
	}
	return nil
}
