package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gnotes/internal/db"
)

func TestPersistentRateLimitSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "persistent-limit.db")
	db.InitDB(path)
	t.Cleanup(func() { db.DB.Close() })

	for attempt := 1; attempt <= 2; attempt++ {
		allowed, _, err := consumePersistentLimit("test", "203.0.113.40", 2, time.Hour)
		if err != nil || !allowed {
			t.Fatalf("attempt %d: allowed=%v err=%v", attempt, allowed, err)
		}
	}
	if err := db.DB.Close(); err != nil {
		t.Fatal(err)
	}
	db.InitDB(path)

	allowed, retryAfter, err := consumePersistentLimit("test", "203.0.113.40", 2, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if allowed || retryAfter <= 0 {
		t.Fatalf("limit after restart: allowed=%v retry=%s", allowed, retryAfter)
	}
	var storedKey string
	if err := db.DB.QueryRow("SELECT key_hash FROM rate_limits WHERE scope = 'test'").Scan(&storedKey); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(storedKey, "203.0.113.40") {
		t.Fatal("rate limiter stored a raw client IP")
	}
}

func TestLoginCooldownEscalatesAndClears(t *testing.T) {
	db.InitDB(filepath.Join(t.TempDir(), "cooldown.db"))
	t.Cleanup(func() { db.DB.Close() })

	for failure := 1; failure <= 5; failure++ {
		cooldown, err := recordLoginFailure("Alice")
		if err != nil {
			t.Fatal(err)
		}
		if failure < 5 && cooldown != 0 {
			t.Fatalf("failure %d produced early cooldown %s", failure, cooldown)
		}
		if failure == 5 && cooldown < 29*time.Second {
			t.Fatalf("fifth failure cooldown = %s", cooldown)
		}
	}
	if remaining, err := loginCooldown("alice"); err != nil || remaining <= 0 {
		t.Fatalf("active cooldown = %s, %v", remaining, err)
	}
	if err := clearLoginCooldown("ALICE"); err != nil {
		t.Fatal(err)
	}
	if remaining, err := loginCooldown("alice"); err != nil || remaining != 0 {
		t.Fatalf("cleared cooldown = %s, %v", remaining, err)
	}
}

func TestInvitationIsSingleUseAndStoredAsHash(t *testing.T) {
	db.InitDB(filepath.Join(t.TempDir(), "invitation.db"))
	t.Cleanup(func() { db.DB.Close() })

	rick := registerTestUser(t, "rick", "correct horse battery staple")
	if _, err := db.DB.Exec("UPDATE settings SET value = 'true' WHERE key = 'signups_enabled'"); err != nil {
		t.Fatal(err)
	}
	response := authenticatedRequest(
		t, protect(requireAdmin(adminCreateInvitationHandler), true), http.MethodPost,
		"/api/admin/invitations/create", `{"valid_days":7}`, rick, true,
	)
	if response.Code != http.StatusCreated {
		t.Fatalf("create invitation status = %d: %s", response.Code, response.Body.String())
	}
	var invitation struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &invitation); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(invitation.Code, "gn_") {
		t.Fatalf("unexpected invitation format %q", invitation.Code)
	}
	var storedHash string
	if err := db.DB.QueryRow("SELECT code_hash FROM invitations").Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash == invitation.Code || strings.Contains(storedHash, invitation.Code) {
		t.Fatal("invitation was stored in plaintext")
	}
	overview := authenticatedRequest(
		t, protect(requireAdmin(adminOverviewHandler), false), http.MethodGet,
		"/api/admin/overview", "", rick, false,
	)
	if overview.Code != http.StatusOK {
		t.Fatalf("admin overview status = %d: %s", overview.Code, overview.Body.String())
	}
	if strings.Contains(overview.Body.String(), invitation.Code) {
		t.Fatal("admin overview exposed the plaintext invitation after creation")
	}

	withoutInvite := registrationResponse(t, "alice", "", "198.51.100.10:1000")
	if withoutInvite.Code != http.StatusForbidden {
		t.Fatalf("registration without invitation = %d, want 403", withoutInvite.Code)
	}
	withInvite := registrationResponse(t, "alice", invitation.Code, "198.51.100.10:1001")
	if withInvite.Code != http.StatusOK {
		t.Fatalf("registration with invitation = %d: %s", withInvite.Code, withInvite.Body.String())
	}
	reused := registrationResponse(t, "bobby", invitation.Code, "198.51.100.11:1000")
	if reused.Code != http.StatusForbidden {
		t.Fatalf("reused invitation status = %d, want 403", reused.Code)
	}
}

func TestDailyAndPerIPSignupLimits(t *testing.T) {
	db.InitDB(filepath.Join(t.TempDir(), "signup-limits.db"))
	t.Cleanup(func() { db.DB.Close() })

	registerTestUser(t, "rick", "correct horse battery staple")
	if _, err := db.DB.Exec(`
		UPDATE settings SET value = 'true' WHERE key = 'signups_enabled';
		UPDATE settings SET value = 'false' WHERE key = 'invite_required';
		UPDATE settings SET value = '1' WHERE key = 'signup_daily_limit';
		UPDATE settings SET value = '1' WHERE key = 'signup_ip_daily_limit';`); err != nil {
		t.Fatal(err)
	}
	if response := registrationResponse(t, "alice", "", "198.51.100.20:1000"); response.Code != http.StatusOK {
		t.Fatalf("first registration = %d: %s", response.Code, response.Body.String())
	}
	if response := registrationResponse(t, "bobby", "", "198.51.100.21:1000"); response.Code != http.StatusTooManyRequests {
		t.Fatalf("global daily limit status = %d, want 429", response.Code)
	}

	if _, err := db.DB.Exec("UPDATE settings SET value = '10' WHERE key = 'signup_daily_limit'"); err != nil {
		t.Fatal(err)
	}
	if response := registrationResponse(t, "carol", "", "198.51.100.20:1002"); response.Code != http.StatusTooManyRequests {
		t.Fatalf("per-IP daily limit status = %d, want 429", response.Code)
	}
	if response := registrationResponse(t, "david", "", "198.51.100.22:1000"); response.Code != http.StatusOK {
		t.Fatalf("different-IP registration = %d: %s", response.Code, response.Body.String())
	}
}

func registrationResponse(t *testing.T, username, invitation, remoteAddress string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(credentials{
		Username:   username,
		Password:   "a sufficiently long password",
		InviteCode: invitation,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body))
	request.RemoteAddr = remoteAddress
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	registerHandler(response, request)
	return response
}
