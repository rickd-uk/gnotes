package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"gnotes/internal/db"
	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookieName = "gnotes_session"
	sessionLifetime   = 7 * 24 * time.Hour
	maxAuthBodyBytes  = 16 * 1024
	passwordHashCost  = 12
	maxAuthAttempts   = 10_000
	maxUserSessions   = 20
	maxPasswordWork   = 4
)

type authContextKey struct{}

type authSession struct {
	UserID    int
	Username  string
	Role      string
	CSRFToken string
}

type credentials struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	SetupToken string `json:"setup_token,omitempty"`
}

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{2,31}$`)
var dummyPasswordHash, _ = bcrypt.GenerateFromPassword([]byte("dummy password used only for timing"), passwordHashCost)
var passwordWork = make(chan struct{}, maxPasswordWork)

type authAttempt struct {
	count   int
	resetAt time.Time
}

var authAttempts = struct {
	sync.Mutex
	entries map[string]authAttempt
}{entries: make(map[string]authAttempt)}

func authConfigHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var userCount int
	var signupsEnabled string
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&userCount); err != nil {
		http.Error(w, "Could not load configuration", http.StatusInternalServerError)
		return
	}
	if err := db.DB.QueryRow("SELECT value FROM settings WHERE key = 'signups_enabled'").Scan(&signupsEnabled); err != nil {
		http.Error(w, "Could not load configuration", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"setup_required":       userCount == 0,
		"setup_token_required": userCount == 0 && setupTokenRequired(r),
		"signups_enabled":      userCount == 0 || signupsEnabled == "true",
	})
}

func registerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Use POST", http.StatusMethodNotAllowed)
		return
	}

	creds, ok := decodeCredentials(w, r)
	if !ok {
		return
	}
	if !allowAuthAttempt(r, "register", "", 10, time.Hour) {
		http.Error(w, "Too many attempts. Please try again later", http.StatusTooManyRequests)
		return
	}
	if !usernamePattern.MatchString(creds.Username) {
		http.Error(w, "Username must be 3-32 letters, numbers, underscores, or hyphens", http.StatusBadRequest)
		return
	}
	if passwordLength := len([]byte(creds.Password)); passwordLength < 12 || passwordLength > 72 {
		http.Error(w, "Password must be 12-72 bytes", http.StatusBadRequest)
		return
	}

	passwordHash, err := hashPassword(creds.Password)
	if err != nil {
		http.Error(w, "Could not create account", http.StatusInternalServerError)
		return
	}

	tx, err := db.DB.Begin()
	if err != nil {
		http.Error(w, "Could not create account", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var userCount int
	if err := tx.QueryRow("SELECT COUNT(*) FROM users").Scan(&userCount); err != nil {
		http.Error(w, "Could not create account", http.StatusInternalServerError)
		return
	}
	if userCount > 0 {
		var signupsEnabled string
		if err := tx.QueryRow("SELECT value FROM settings WHERE key = 'signups_enabled'").Scan(&signupsEnabled); err != nil {
			http.Error(w, "Could not create account", http.StatusInternalServerError)
			return
		}
		if signupsEnabled != "true" {
			http.Error(w, "New account registration is currently closed", http.StatusForbidden)
			return
		}
	}
	if userCount == 0 && !strings.EqualFold(creds.Username, "rick") {
		http.Error(w, "The first account must use the administrator username rick", http.StatusBadRequest)
		return
	}
	if userCount == 0 && setupTokenRequired(r) {
		expectedToken := os.Getenv("GNOTES_SETUP_TOKEN")
		if expectedToken == "" || !equalSecret(creds.SetupToken, expectedToken) {
			http.Error(w, "A valid administrator setup token is required", http.StatusForbidden)
			return
		}
	}
	role := "user"
	if userCount == 0 && strings.EqualFold(creds.Username, "rick") {
		role = "admin"
	}
	result, err := tx.Exec(
		"INSERT INTO users (username, password_hash, role, created_at) VALUES (?, ?, ?, ?)",
		creds.Username,
		passwordHash,
		role,
		time.Now(),
	)
	if err != nil {
		http.Error(w, "Username is unavailable", http.StatusConflict)
		return
	}
	userID, err := result.LastInsertId()
	if err != nil {
		http.Error(w, "Could not create account", http.StatusInternalServerError)
		return
	}

	// The first account to register claims notes created before accounts existed.
	if _, err := tx.Exec("UPDATE notes SET user_id = ? WHERE user_id IS NULL", userID); err != nil {
		http.Error(w, "Could not create account", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, "Could not create account", http.StatusInternalServerError)
		return
	}

	finishAuthentication(w, r, int(userID), creds.Username, role)
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Use POST", http.StatusMethodNotAllowed)
		return
	}

	creds, ok := decodeCredentials(w, r)
	if !ok {
		return
	}
	if !allowAuthAttempt(r, "login-ip", "", 20, 15*time.Minute) ||
		!allowAuthAttempt(r, "login-user", creds.Username, 10, 15*time.Minute) {
		http.Error(w, "Too many attempts. Please try again later", http.StatusTooManyRequests)
		return
	}

	var userID int
	var username string
	var role string
	var active bool
	var passwordHash []byte
	err := db.DB.QueryRow(
		"SELECT id, username, password_hash, role, active FROM users WHERE username = ?",
		creds.Username,
	).Scan(&userID, &username, &passwordHash, &role, &active)
	if errors.Is(err, sql.ErrNoRows) {
		// Do equivalent password work so account existence is not disclosed by timing.
		passwordMatches(dummyPasswordHash, creds.Password)
		http.Error(w, "Invalid username or password", http.StatusUnauthorized)
		return
	}
	if err != nil {
		http.Error(w, "Could not sign in", http.StatusInternalServerError)
		return
	}
	if !passwordMatches(passwordHash, creds.Password) {
		http.Error(w, "Invalid username or password", http.StatusUnauthorized)
		return
	}
	if !active {
		http.Error(w, "This account is disabled", http.StatusForbidden)
		return
	}

	if cost, err := bcrypt.Cost(passwordHash); err == nil && cost < passwordHashCost {
		if upgradedHash, err := hashPassword(creds.Password); err == nil {
			db.DB.Exec("UPDATE users SET password_hash = ? WHERE id = ?", upgradedHash, userID)
		}
	}
	clearAuthAttempts(r, "login-ip", "")
	clearAuthAttempts(r, "login-user", creds.Username)
	finishAuthentication(w, r, userID, username, role)
}

func hashPassword(password string) ([]byte, error) {
	passwordWork <- struct{}{}
	defer func() { <-passwordWork }()
	return bcrypt.GenerateFromPassword([]byte(password), passwordHashCost)
}

func passwordMatches(hash []byte, password string) bool {
	passwordWork <- struct{}{}
	defer func() { <-passwordWork }()
	return bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Use POST", http.StatusMethodNotAllowed)
		return
	}
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		db.DB.Exec("DELETE FROM sessions WHERE token_hash = ?", hashToken(cookie.Value))
	}
	clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func meHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	session := sessionFromContext(r)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"username":   session.Username,
		"role":       session.Role,
		"csrf_token": session.CSRFToken,
	})
}

func finishAuthentication(w http.ResponseWriter, r *http.Request, userID int, username, role string) {
	sessionToken, err := randomToken()
	if err != nil {
		http.Error(w, "Could not start session", http.StatusInternalServerError)
		return
	}
	csrfToken, err := randomToken()
	if err != nil {
		http.Error(w, "Could not start session", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	expiresAt := now.Add(sessionLifetime)
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		db.DB.Exec("DELETE FROM sessions WHERE token_hash = ?", hashToken(cookie.Value))
	}
	if _, err := db.DB.Exec(
		"INSERT INTO sessions (token_hash, user_id, csrf_token, created_at, expires_at) VALUES (?, ?, ?, ?, ?)",
		hashToken(sessionToken),
		userID,
		csrfToken,
		now,
		expiresAt,
	); err != nil {
		http.Error(w, "Could not start session", http.StatusInternalServerError)
		return
	}
	if _, err := db.DB.Exec(`
		DELETE FROM sessions
		 WHERE user_id = ?
		   AND token_hash NOT IN (
		       SELECT token_hash FROM sessions
		        WHERE user_id = ?
		        ORDER BY created_at DESC
		        LIMIT ?
		   )`, userID, userID, maxUserSessions); err != nil {
		db.DB.Exec("DELETE FROM sessions WHERE token_hash = ?", hashToken(sessionToken))
		http.Error(w, "Could not start session", http.StatusInternalServerError)
		return
	}

	db.DB.Exec("DELETE FROM sessions WHERE expires_at <= ?", now)
	setSessionCookie(w, r, sessionToken, expiresAt)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"username":   username,
		"role":       role,
		"csrf_token": csrfToken,
	})
}

func decodeCredentials(w http.ResponseWriter, r *http.Request) (credentials, bool) {
	var creds credentials
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return creds, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&creds); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return creds, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return creds, false
	}
	creds.Username = strings.TrimSpace(creds.Username)
	return creds, true
}

func protect(handler http.HandlerFunc, requireCSRF bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, err := authenticateRequest(r)
		if err != nil {
			clearSessionCookie(w, r)
			http.Error(w, "Authentication required", http.StatusUnauthorized)
			return
		}
		if requireCSRF && !validCSRFToken(r.Header.Get("X-CSRF-Token"), session.CSRFToken) {
			http.Error(w, "Invalid CSRF token", http.StatusForbidden)
			return
		}
		ctx := context.WithValue(r.Context(), authContextKey{}, session)
		handler(w, r.WithContext(ctx))
	}
}

func authenticateRequest(r *http.Request) (authSession, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return authSession{}, errors.New("missing session")
	}

	var session authSession
	var expiresAt time.Time
	err = db.DB.QueryRow(
		`SELECT sessions.user_id, users.username, users.role, sessions.csrf_token, sessions.expires_at
         FROM sessions JOIN users ON users.id = sessions.user_id
        WHERE sessions.token_hash = ? AND users.active = 1`,
		hashToken(cookie.Value),
	).Scan(&session.UserID, &session.Username, &session.Role, &session.CSRFToken, &expiresAt)
	if err != nil || !expiresAt.After(time.Now()) {
		if err == nil {
			db.DB.Exec("DELETE FROM sessions WHERE token_hash = ?", hashToken(cookie.Value))
		}
		return authSession{}, errors.New("invalid session")
	}
	return session, nil
}

func sessionFromContext(r *http.Request) authSession {
	return r.Context().Value(authContextKey{}).(authSession)
}

func userIDFromRequest(r *http.Request) int {
	return sessionFromContext(r).UserID
}

func validCSRFToken(provided, expected string) bool {
	if provided == "" || len(provided) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func authAttemptKey(r *http.Request, action, username string) string {
	host := clientIP(r)
	if action == "register" || action == "login-ip" {
		return action + ":" + host
	}
	return action + ":" + host + ":" + strings.ToLower(username)
}

func allowAuthAttempt(r *http.Request, action, username string, limit int, window time.Duration) bool {
	now := time.Now()
	key := authAttemptKey(r, action, username)
	authAttempts.Lock()
	defer authAttempts.Unlock()
	if len(authAttempts.entries) >= maxAuthAttempts {
		for entryKey, entry := range authAttempts.entries {
			if !entry.resetAt.After(now) {
				delete(authAttempts.entries, entryKey)
			}
		}
		if _, exists := authAttempts.entries[key]; !exists && len(authAttempts.entries) >= maxAuthAttempts {
			return false
		}
	}
	attempt := authAttempts.entries[key]
	if attempt.resetAt.Before(now) {
		attempt = authAttempt{resetAt: now.Add(window)}
	}
	attempt.count++
	authAttempts.entries[key] = attempt
	return attempt.count <= limit
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	remoteIP := net.ParseIP(host)
	if remoteIP != nil && remoteIP.IsLoopback() {
		forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]
		if forwardedIP := net.ParseIP(strings.TrimSpace(forwarded)); forwardedIP != nil {
			return forwardedIP.String()
		}
	}
	if remoteIP != nil {
		return remoteIP.String()
	}
	return host
}

func clearAuthAttempts(r *http.Request, action, username string) {
	authAttempts.Lock()
	delete(authAttempts.entries, authAttemptKey(r, action, username))
	authAttempts.Unlock()
}

func randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(sessionLifetime.Seconds()),
		HttpOnly: true,
		Secure:   secureCookies(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secureCookies(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func secureCookies(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(os.Getenv("GNOTES_SECURE_COOKIES"), "true")
}

func setupTokenRequired(r *http.Request) bool {
	if secureCookies(r) || r.Header.Get("Forwarded") != "" || r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("X-Forwarded-Proto") != "" {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsLoopback()
}

func equalSecret(provided, expected string) bool {
	providedHash := sha256.Sum256([]byte(provided))
	expectedHash := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(providedHash[:], expectedHash[:]) == 1
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		if secureCookies(r) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}
