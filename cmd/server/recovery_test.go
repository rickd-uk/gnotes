package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"gnotes/internal/db"
	"golang.org/x/crypto/bcrypt"
)

type recoveryTransport func(*http.Request) (*http.Response, error)

func (f recoveryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type capturedRecoveryMail struct {
	Sender struct {
		Email string `json:"email"`
	} `json:"sender"`
	To []struct {
		Email string `json:"email"`
	} `json:"to"`
	Text string `json:"textContent"`
}

func recoveryFixture(t *testing.T) (*recoveryService, chan capturedRecoveryMail, testLogin, []byte) {
	t.Helper()
	db.InitDB(filepath.Join(t.TempDir(), "recovery.db"))
	t.Cleanup(func() { db.DB.Close() })
	t.Setenv("BREVO_API_KEY", "test-key")
	t.Setenv("GNOTES_EMAIL_FROM", "noreply@example.com")
	t.Setenv("GNOTES_PUBLIC_URL", "https://notes.example.com")
	config, err := loadRecoveryConfig()
	if err != nil {
		t.Fatal(err)
	}
	s := newRecoveryService(db.DB, config)
	messages := make(chan capturedRecoveryMail, 20)
	s.client.Transport = recoveryTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.brevo.com/v3/smtp/email" || r.Header.Get("api-key") != "test-key" || r.Method != http.MethodPost {
			t.Error("incorrect Brevo endpoint or authentication")
		}
		var message capturedRecoveryMail
		if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
			t.Error(err)
		}
		messages <- message
		return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(`{"messageId":"test"}`)), Header: make(http.Header)}, nil
	})
	hash, _ := bcrypt.GenerateFromPassword([]byte("original password"), bcrypt.MinCost)
	if _, err := db.DB.Exec("INSERT INTO users (id, username, email, password_hash, role, created_at) VALUES (1, 'rick', 'old@example.com', ?, 'admin', ?)", hash, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("INSERT INTO users (id, username, email, password_hash, created_at) VALUES (2, 'other', 'other@example.com', ?, ?)", hash, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	response := httptest.NewRecorder()
	finishAuthentication(response, request, 1, "rick", "admin", hash)
	var result struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return s, messages, testLogin{cookie: response.Result().Cookies()[0], csrf: result.CSRF, role: "admin"}, hash
}

func recoveryPOST(handler http.HandlerFunc, body any) *httptest.ResponseRecorder {
	encoded, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "https://attacker.invalid/api/auth/recovery", bytes.NewReader(encoded))
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}

func recoveryMailToken(t *testing.T, messages chan capturedRecoveryMail, action string) string {
	t.Helper()
	var message capturedRecoveryMail
	select {
	case message = <-messages:
	case <-time.After(2 * time.Second):
		t.Fatal("no recovery email")
	}
	if message.Sender.Email != "noreply@example.com" || len(message.To) != 1 || strings.Contains(message.Text, "attacker.invalid") {
		t.Fatal("incorrect sender, recipient count, or untrusted reset origin")
	}
	match := regexp.MustCompile(`https://notes\.example\.com/#` + action + `=([A-Za-z0-9_-]{43})`).FindStringSubmatch(message.Text)
	if len(match) != 2 {
		t.Fatal("email has no valid account link")
	}
	return match[1]
}

func mustRecoveryExec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := db.DB.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryVerificationAndPasswordReset(t *testing.T) {
	s, mail, login, oldHash := recoveryFixture(t)
	body := `{"email":"new@example.com","password":"original password"}`
	if got := authenticatedRequest(t, protect(s.requestVerification, true), http.MethodPost, "/api/auth/email/request", body, login, false); got.Code != http.StatusForbidden {
		t.Fatal("email change accepted without CSRF")
	}
	if got := authenticatedRequest(t, protect(s.requestVerification, true), http.MethodPost, "/api/auth/email/request", `{"email":"new@example.com","password":"incorrect"}`, login, true); got.Code != http.StatusForbidden {
		t.Fatal("email change accepted without current password")
	}
	got := authenticatedRequest(t, protect(s.requestVerification, true), http.MethodPost, "/api/auth/email/request", body, login, true)
	if got.Code != http.StatusOK {
		t.Fatalf("verification request: %d %s", got.Code, got.Body.String())
	}
	verifyToken := recoveryMailToken(t, mail, "verify-email")
	var email string
	var storedToken string
	if err := db.DB.QueryRow("SELECT email FROM users WHERE id = 1").Scan(&email); err != nil {
		t.Fatal(err)
	}
	if email != "old@example.com" {
		t.Fatal("email changed before verification")
	}
	if err := db.DB.QueryRow("SELECT token_hash FROM email_verification_tokens WHERE user_id = 1").Scan(&storedToken); err != nil {
		t.Fatal(err)
	}
	if storedToken == verifyToken || storedToken != hashToken(verifyToken) {
		t.Fatal("verification token not hashed")
	}
	if got := recoveryPOST(s.verifyEmail, map[string]string{"token": verifyToken}); got.Code != http.StatusOK {
		t.Fatalf("verify: %d %s", got.Code, got.Body.String())
	}
	if got := recoveryPOST(s.verifyEmail, map[string]string{"token": verifyToken}); got.Code != http.StatusBadRequest {
		t.Fatal("verification token reused")
	}
	profile := authenticatedRequest(t, protect(meHandler, false), http.MethodGet, "/api/auth/me", "", login, false)
	if !strings.Contains(profile.Body.String(), `"email_verified":true`) || !strings.Contains(profile.Body.String(), `"email":"new@example.com"`) {
		t.Fatal("profile does not show verified recovery email")
	}
	createTestNote(t, login, "keep my note")
	mustRecoveryExec(t, "INSERT INTO notes (user_id, title, content, created_at) VALUES (2, 'other private note', 'private', ?)", time.Now().UTC())
	mustRecoveryExec(t, "INSERT INTO sessions (token_hash,user_id,csrf_token,created_at,expires_at) VALUES ('other-session',2,'other-csrf',?,?)", time.Now().UTC(), time.Now().UTC().Add(time.Hour))
	mustRecoveryExec(t, "INSERT INTO login_cooldowns (username_hash,failures,last_failed_at,blocked_until) VALUES (?,5,?,?)", hashUsername("rick"), time.Now().UTC(), time.Now().UTC().Add(time.Hour))
	got = recoveryPOST(s.requestReset, map[string]string{"username": "rick", "email": "NEW@example.com"})
	if got.Code != http.StatusAccepted || !strings.Contains(got.Body.String(), recoveryResponse) {
		t.Fatal("unexpected recovery response")
	}
	if len(mail) != 0 {
		t.Fatal("recovery request synchronously sent email")
	}
	if err := s.issueResetEmail(context.Background(), <-s.jobs); err != nil {
		t.Fatal(err)
	}
	token := recoveryMailToken(t, mail, "reset-password")
	if err := db.DB.QueryRow("SELECT token_hash FROM password_reset_tokens WHERE user_id=1").Scan(&storedToken); err != nil {
		t.Fatal(err)
	}
	if storedToken == token || storedToken != hashToken(token) {
		t.Fatal("reset token not hashed")
	}
	if got := recoveryPOST(s.resetPassword, map[string]string{"token": token, "password": "short"}); got.Code != http.StatusBadRequest {
		t.Fatal("short password accepted")
	}
	got = recoveryPOST(s.resetPassword, map[string]string{"token": token, "password": "replacement password"})
	if got.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", got.Code, got.Body.String())
	}
	if got := recoveryPOST(s.resetPassword, map[string]string{"token": token, "password": "another password"}); got.Code != http.StatusBadRequest {
		t.Fatal("reset token reused")
	}
	var hash, otherHash []byte
	if err := db.DB.QueryRow("SELECT password_hash FROM users WHERE id=1").Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if !passwordMatches(hash, "replacement password") || passwordMatches(hash, "original password") {
		t.Fatal("password reset failed")
	}
	if err := db.DB.QueryRow("SELECT password_hash FROM users WHERE id=2").Scan(&otherHash); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(otherHash, oldHash) {
		t.Fatal("other account password changed")
	}
	for _, check := range []struct {
		query string
		want  int
	}{
		{"SELECT COUNT(*) FROM sessions WHERE user_id=1", 0}, {"SELECT COUNT(*) FROM sessions WHERE user_id=2", 1},
		{"SELECT COUNT(*) FROM password_reset_tokens", 0}, {"SELECT COUNT(*) FROM login_cooldowns", 0}, {"SELECT COUNT(*) FROM notes", 2},
	} {
		var count int
		if err := db.DB.QueryRow(check.query).Scan(&count); err != nil || count != check.want {
			t.Fatalf("%s: %d, want %d (%v)", check.query, count, check.want, err)
		}
	}
	// A login that checked the old password before reset must not create a new session afterward.
	w := httptest.NewRecorder()
	finishAuthentication(w, httptest.NewRequest(http.MethodPost, "/api/auth/login", nil), 1, "rick", "admin", oldHash)
	if w.Code != http.StatusUnauthorized {
		t.Fatal("stale login created session after password reset")
	}
}

func TestResetRejectsExpiredAndChangedAccounts(t *testing.T) {
	s, _, _, hash := recoveryFixture(t)
	for _, scenario := range []string{"expired", "disabled", "unverified", "email changed", "password changed"} {
		t.Run(scenario, func(t *testing.T) {
			mustRecoveryExec(t, "UPDATE users SET active=1,email='old@example.com',email_verified_at=?,password_hash=? WHERE id=1", time.Now().UTC(), hash)
			token, _ := randomToken()
			mustRecoveryExec(t, `INSERT OR REPLACE INTO password_reset_tokens (token_hash,user_id,email,password_hash,expires_at) VALUES (?,1,'old@example.com',?,?)`, hashToken(token), hash, time.Now().UTC().Add(time.Hour))
			switch scenario {
			case "expired":
				mustRecoveryExec(t, "UPDATE password_reset_tokens SET expires_at=?", time.Now().UTC().Add(-time.Minute))
			case "disabled":
				mustRecoveryExec(t, "UPDATE users SET active=0 WHERE id=1")
			case "unverified":
				mustRecoveryExec(t, "UPDATE users SET email_verified_at=NULL WHERE id=1")
			case "email changed":
				mustRecoveryExec(t, "UPDATE users SET email='changed@example.com' WHERE id=1")
			case "password changed":
				mustRecoveryExec(t, "UPDATE users SET password_hash=? WHERE id=1", []byte("changed"))
			}
			if got := recoveryPOST(s.resetPassword, map[string]string{"token": token, "password": "replacement password"}); got.Code != http.StatusBadRequest {
				t.Fatalf("invalid reset accepted: %d", got.Code)
			}
		})
	}
}

func TestRecoveryGenericResponsesAndPersistentLimits(t *testing.T) {
	s, messages, _, _ := recoveryFixture(t)
	mustRecoveryExec(t, "UPDATE users SET email_verified_at=? WHERE id=1", time.Now().UTC())
	var reply string
	for _, input := range []map[string]string{
		{"username": "rick", "email": "old@example.com"}, {"username": "missing", "email": "old@example.com"},
		{"username": "other", "email": "other@example.com"}, {"username": "rick", "email": "wrong@example.com"},
	} {
		got := recoveryPOST(s.requestReset, input)
		if got.Code != http.StatusAccepted {
			t.Fatalf("request rejected: %d", got.Code)
		}
		if reply == "" {
			reply = got.Body.String()
		} else if got.Body.String() != reply {
			t.Fatal("account existence disclosed")
		}
	}
	for len(s.jobs) > 0 {
		if err := s.issueResetEmail(context.Background(), <-s.jobs); err != nil {
			t.Fatal(err)
		}
	}
	if len(messages) != 1 {
		t.Fatalf("sent %d emails; only verified matching account should receive one", len(messages))
	}
	// Reconstructing the service must not bypass the per-account limiter.
	s2 := newRecoveryService(db.DB, s.config)
	if got := recoveryPOST(s2.requestReset, map[string]string{"username": "RICK", "email": "old@example.com"}); got.Code != http.StatusTooManyRequests || got.Header().Get("Retry-After") == "" || len(s2.jobs) != 0 {
		t.Fatal("persistent account limiter bypassed")
	}
	for i := 0; i < 6; i++ {
		recoveryPOST(s.requestReset, map[string]string{"username": "missing", "email": "old@example.com"})
	}
	if got := recoveryPOST(s.requestReset, map[string]string{"username": "missing", "email": "old@example.com"}); got.Code != http.StatusTooManyRequests || got.Header().Get("Retry-After") == "" {
		t.Fatal("IP limiter missing")
	}
}

func TestRecoveryCorrectedEmailCanRequestImmediately(t *testing.T) {
	s, messages, _, _ := recoveryFixture(t)
	mustRecoveryExec(t, "UPDATE users SET email_verified_at=? WHERE id=1", time.Now().UTC())
	for _, email := range []string{"wrong@example.com", "old@example.com"} {
		input := map[string]string{"username": "rick", "email": email}
		if got := recoveryPOST(s.requestReset, input); got.Code != http.StatusAccepted {
			t.Fatalf("initial request for %s: %d", email, got.Code)
		}
		if got := recoveryPOST(s.requestReset, input); got.Code != http.StatusTooManyRequests || got.Header().Get("Retry-After") == "" {
			t.Fatal("repeat request must explain the cooldown for both matching and incorrect addresses")
		}
	}
	var attempts int
	if err := db.DB.QueryRow("SELECT SUM(attempts) FROM rate_limits WHERE scope='recovery-pair-hour'").Scan(&attempts); err != nil || attempts != 2 {
		t.Fatalf("minute cooldown consumed hourly attempts: %d, %v", attempts, err)
	}
	for len(s.jobs) > 0 {
		if err := s.issueResetEmail(context.Background(), <-s.jobs); err != nil {
			t.Fatal(err)
		}
	}
	if len(messages) != 1 {
		t.Fatalf("corrected address should receive exactly one email, got %d", len(messages))
	}
	recoveryMailToken(t, messages, "reset-password")
}

func TestRecoveryMailFailureAndVerificationInvalidation(t *testing.T) {
	s, messages, login, hash := recoveryFixture(t)
	s.client.Transport = recoveryTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader("provider details and secrets")), Header: make(http.Header)}, nil
	})
	body := `{"email":"new@example.com","password":"original password"}`
	got := authenticatedRequest(t, protect(s.requestVerification, true), http.MethodPost, "/api/auth/email/request", body, login, true)
	if got.Code != http.StatusServiceUnavailable || strings.Contains(got.Body.String(), "provider details") {
		t.Fatal("provider failure not sanitized")
	}
	mustRecoveryExec(t, "UPDATE users SET email_verified_at=? WHERE id=1", time.Now().UTC())
	if err := s.issueResetEmail(context.Background(), resetMailJob{"rick", "old@example.com"}); err == nil {
		t.Fatal("provider failure ignored")
	}
	var count int
	if err := db.DB.QueryRow("SELECT (SELECT COUNT(*) FROM password_reset_tokens)+(SELECT COUNT(*) FROM email_verification_tokens)").Scan(&count); err != nil || count != 0 {
		t.Fatal("failed mail left a usable token")
	}
	for _, scenario := range []string{"expired", "password changed", "disabled"} {
		mustRecoveryExec(t, "UPDATE users SET active=1,password_hash=? WHERE id=1", hash)
		token, _ := randomToken()
		mustRecoveryExec(t, "INSERT OR REPLACE INTO email_verification_tokens (token_hash,user_id,email,password_hash,expires_at) VALUES (?,1,'new@example.com',?,?)", hashToken(token), hash, time.Now().UTC().Add(time.Hour))
		switch scenario {
		case "expired":
			mustRecoveryExec(t, "UPDATE email_verification_tokens SET expires_at=?", time.Now().UTC().Add(-time.Minute))
		case "password changed":
			mustRecoveryExec(t, "UPDATE users SET password_hash=? WHERE id=1", []byte("changed"))
		case "disabled":
			mustRecoveryExec(t, "UPDATE users SET active=0 WHERE id=1")
		}
		if got := recoveryPOST(s.verifyEmail, map[string]string{"token": token}); got.Code != http.StatusBadRequest {
			t.Fatalf("%s verification accepted", scenario)
		}
	}
	if len(messages) != 0 {
		t.Fatal("unexpected outgoing email")
	}
}

func TestResetConcurrentConsumption(t *testing.T) {
	s, _, _, hash := recoveryFixture(t)
	mustRecoveryExec(t, "UPDATE users SET email_verified_at=? WHERE id=1", time.Now().UTC())
	token, _ := randomToken()
	mustRecoveryExec(t, "INSERT INTO password_reset_tokens (token_hash,user_id,email,password_hash,expires_at) VALUES (?,1,'old@example.com',?,?)", hashToken(token), hash, time.Now().UTC().Add(time.Hour))
	results := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- recoveryPOST(s.resetPassword, map[string]string{"token": token, "password": "replacement password"}).Code
		}()
	}
	wg.Wait()
	a, b := <-results, <-results
	if !((a == 200 && b == 400) || (a == 400 && b == 200)) {
		t.Fatalf("concurrent results: %d %d", a, b)
	}
}

func TestRecoveryWorkerAndConfig(t *testing.T) {
	s, messages, _, _ := recoveryFixture(t)
	mustRecoveryExec(t, "UPDATE users SET email_verified_at=? WHERE id=1", time.Now().UTC())
	ctx, cancel := context.WithCancel(context.Background())
	s.start(ctx)
	t.Cleanup(func() { cancel(); s.wg.Wait() })
	if got := recoveryPOST(s.requestReset, map[string]string{"username": "rick", "email": "old@example.com"}); got.Code != 202 {
		t.Fatal("request failed")
	}
	recoveryMailToken(t, messages, "reset-password")
	for _, origin := range []string{"http://notes.example.com", "https://notes.example.com/path", "https://user:pass@notes.example.com", "https://notes.example.com?query=1", "https://notes.example.com/#fragment"} {
		t.Setenv("GNOTES_PUBLIC_URL", origin)
		if _, err := loadRecoveryConfig(); err == nil {
			t.Fatalf("unsafe origin accepted: %s", origin)
		}
	}
	t.Setenv("GNOTES_PUBLIC_URL", "http://127.0.0.1:8080")
	if _, err := loadRecoveryConfig(); err != nil {
		t.Fatal("localhost recovery rejected")
	}
	t.Setenv("GNOTES_EMAIL_FROM", "")
	if _, err := loadRecoveryConfig(); err == nil {
		t.Fatal("missing sender accepted")
	}
	t.Setenv("BREVO_API_KEY", "")
	config, err := loadRecoveryConfig()
	if err != nil || config.enabled() {
		t.Fatal("unconfigured recovery not disabled")
	}
	s.config = config
	if got := recoveryPOST(s.requestReset, map[string]string{"username": "rick", "email": "old@example.com"}); got.Code != http.StatusServiceUnavailable {
		t.Fatal("unconfigured request accepted")
	}
}
