package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"gnotes/internal/db"
)

func passwordPOST(t *testing.T, login testLogin, current, next, code string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(passwordChangeInput{CurrentPassword: current, NewPassword: next, Code: code})
	return authenticatedRequest(t, protect(changePasswordHandler, true), "POST", "/api/auth/password", string(body), login, true)
}

func TestPasswordChangeLifecycle(t *testing.T) {
	_, _, login, hash := recoveryFixture(t)
	other := mfaTestLogin(t, mfaSignIn(t, "other", "original password", ""))
	second := mfaTestLogin(t, mfaSignIn(t, "rick", "original password", ""))
	mustRecoveryExec(t, "INSERT INTO notes(user_id,content,created_at) VALUES(1,'Keep this note',?)", time.Now())
	for _, table := range []string{"password_reset_tokens", "email_verification_tokens"} {
		mustRecoveryExec(t, "INSERT INTO "+table+"(token_hash,user_id,email,password_hash,expires_at) VALUES(?,1,'old@example.com',?,?)", table, hash, time.Now().Add(time.Hour))
	}
	mfaTestSetup(t, login) // Pending setup must not survive its old password.
	w := passwordPOST(t, login, "original password", "my new secure password", "")
	newLogin := mfaTestLogin(t, w)
	if newLogin.cookie.Value == login.cookie.Value || newLogin.csrf == login.csrf || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("password change did not issue a private rotated session")
	}
	for _, old := range []testLogin{login, second} {
		if got := authenticatedRequest(t, protect(meHandler, false), "GET", "/api/auth/me", "", old, false); got.Code != 401 || got.Header().Get("Set-Cookie") != "" {
			t.Fatal("old session survived or could erase the rotated cookie")
		}
	}
	for _, active := range []testLogin{newLogin, other} {
		if got := authenticatedRequest(t, protect(meHandler, false), "GET", "/api/auth/me", "", active, false); got.Code != 200 {
			t.Fatal("current or unrelated account session was lost")
		}
	}
	for _, table := range []string{"password_reset_tokens", "email_verification_tokens", "mfa_factors"} {
		var count int
		if err := db.DB.QueryRow("SELECT count(*) FROM " + table + " WHERE user_id=1").Scan(&count); err != nil || count != 0 {
			t.Fatalf("old security material survived in %s: %v", table, err)
		}
	}
	var content string
	if err := db.DB.QueryRow("SELECT content FROM notes WHERE user_id=1").Scan(&content); err != nil || content != "Keep this note" {
		t.Fatal("note changed")
	}
	if got := mfaSignIn(t, "rick", "original password", ""); got.Code != 401 {
		t.Fatal("old password still works")
	}
	if got := mfaSignIn(t, "rick", "my new secure password", ""); got.Code != 200 || len(got.Result().Cookies()) == 0 {
		t.Fatal("new password cannot sign in")
	}
}

func TestPasswordChangeRejectsInvalidRequests(t *testing.T) {
	_, _, login, _ := recoveryFixture(t)
	handler := protect(changePasswordHandler, true)
	valid := `{"current_password":"original password","new_password":"a new secure password","code":""}`
	for _, test := range []struct {
		method, body string
		csrf         bool
		want         int
	}{
		{"GET", "", true, 405},
		{"POST", valid, false, 403},
		{"POST", valid + valid, true, 400},
		{"POST", `{"current_password":"original password","new_password":"short"}`, true, 400},
		{"POST", `{"current_password":"original password","new_password":"` + strings.Repeat("あ", 25) + `"}`, true, 400},
		{"POST", `{"current_password":"wrong password","new_password":"a new secure password"}`, true, 403},
		{"POST", `{"current_password":"original password","new_password":"a new secure password","user_id":2}`, true, 400},
	} {
		if got := authenticatedRequest(t, handler, test.method, "/api/auth/password", test.body, login, test.csrf); got.Code != test.want {
			t.Fatalf("%s %s = %d, want %d: %s", test.method, test.body, got.Code, test.want, got.Body.String())
		}
	}
	if got := recoveryPOST(handler, passwordChangeInput{CurrentPassword: "original password", NewPassword: "a new secure password"}); got.Code != 401 {
		t.Fatal("unauthenticated password change accepted")
	}
	for i := 0; i < 12; i++ {
		got := passwordPOST(t, login, "wrong password", "a new secure password", "")
		if got.Code == 429 {
			if got.Header().Get("Retry-After") == "" {
				t.Fatal("rate limit missing retry delay")
			}
			return
		}
	}
	t.Fatal("password guessing was not rate limited")
}

func TestPasswordChangeMFAAndRollback(t *testing.T) {
	_, _, original, _ := recoveryFixture(t)
	login, enabled, secret := mfaTestEnable(t, original)
	for _, code := range []string{"", "invalid"} {
		if got := passwordPOST(t, login, "original password", "my new secure password", code); got.Code != 403 {
			t.Fatal("MFA-enabled password change accepted without valid proof")
		}
	}
	// A later write failure must roll back the password and recovery-code spend.
	mustRecoveryExec(t, `CREATE TRIGGER reject_password_session BEFORE INSERT ON sessions BEGIN SELECT RAISE(ABORT,'test failure'); END`)
	if got := passwordPOST(t, login, "original password", "my new secure password", enabled.Codes[0]); got.Code != 500 {
		t.Fatalf("injected failure = %d", got.Code)
	}
	var remaining int
	if err := db.DB.QueryRow("SELECT count(*) FROM mfa_recovery_codes WHERE user_id=1").Scan(&remaining); err != nil || remaining != 10 {
		t.Fatal("failed change spent a recovery code")
	}
	mustRecoveryExec(t, "DROP TRIGGER reject_password_session")
	w := passwordPOST(t, login, "original password", "my new secure password", enabled.Codes[0])
	login = mfaTestLogin(t, w)
	if got := passwordPOST(t, login, "my new secure password", "another secure password", enabled.Codes[0]); got.Code != 403 {
		t.Fatal("used recovery code accepted")
	}
	code, _ := totp.GenerateCode(secret, time.Now().Add(30*time.Second))
	w = passwordPOST(t, login, "my new secure password", "another secure password", code)
	login = mfaTestLogin(t, w)
	if got := passwordPOST(t, login, "another secure password", "final secure password", code); got.Code != 403 {
		t.Fatal("authenticator replay accepted")
	}
	if !mfaTestJSON(t, mfaSignIn(t, "rick", "another secure password", "")).Required {
		t.Fatal("password change disabled MFA")
	}
}

func TestPasswordChangeConcurrentRequests(t *testing.T) {
	_, _, login, _ := recoveryFixture(t)
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- passwordPOST(t, login, "original password", "my new secure password", "").Code
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for code := range results {
		if code == 200 {
			successes++
		} else if code != 401 && code != 403 && code != 409 {
			t.Fatalf("concurrent change returned %d", code)
		}
	}
	if successes != 1 {
		t.Fatalf("successful simultaneous changes = %d", successes)
	}
}
