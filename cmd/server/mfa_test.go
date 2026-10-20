package main

import (
	"encoding/base32"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"gnotes/internal/db"
)

type mfaTestResponse struct {
	SetupID   string   `json:"setup_id"`
	Secret    string   `json:"secret"`
	QR        string   `json:"qr_code"`
	CSRF      string   `json:"csrf_token"`
	Codes     []string `json:"recovery_codes"`
	Enabled   bool     `json:"enabled"`
	Required  bool     `json:"mfa_required"`
	Remaining int      `json:"recovery_codes_remaining"`
}

func mfaTestPOST(t *testing.T, handler http.HandlerFunc, login testLogin, input mfaInput) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(input)
	return authenticatedRequest(t, protect(handler, true), "POST", "/api/auth/mfa", string(body), login, true)
}
func mfaTestJSON(t *testing.T, w *httptest.ResponseRecorder) mfaTestResponse {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("MFA request status %d: %s", w.Code, w.Body.String())
	}
	var data mfaTestResponse
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	return data
}
func mfaTestLogin(t *testing.T, w *httptest.ResponseRecorder) testLogin {
	t.Helper()
	data := mfaTestJSON(t, w)
	if data.CSRF == "" || len(w.Result().Cookies()) != 1 {
		t.Fatal("missing rotated session")
	}
	return testLogin{cookie: w.Result().Cookies()[0], csrf: data.CSRF, role: "admin"}
}
func mfaTestSetup(t *testing.T, login testLogin) mfaTestResponse {
	t.Helper()
	return mfaTestJSON(t, mfaTestPOST(t, mfaSetupHandler, login, mfaInput{Password: "original password"}))
}
func mfaTestEnable(t *testing.T, login testLogin) (testLogin, mfaTestResponse, string) {
	t.Helper()
	setup := mfaTestSetup(t, login)
	code, err := totp.GenerateCode(setup.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	w := mfaTestPOST(t, mfaEnableHandler, login, mfaInput{Code: code, SetupID: setup.SetupID})
	return mfaTestLogin(t, w), mfaTestJSON(t, w), setup.Secret
}
func mfaSignIn(t *testing.T, username, password, code string) *httptest.ResponseRecorder {
	t.Helper()
	return recoveryPOST(loginHandler, credentials{Username: username, Password: password, MFACode: code})
}

func TestMFAEnrollmentAndRecoveryLifecycle(t *testing.T) {
	_, _, original, _ := recoveryFixture(t)
	missingCSRF := authenticatedRequest(t, protect(mfaSetupHandler, true), "POST", "/api/auth/mfa/setup", `{"password":"original password"}`, original, false)
	if missingCSRF.Code != 403 {
		t.Fatalf("setup without CSRF = %d", missingCSRF.Code)
	}
	badPassword := mfaTestPOST(t, mfaSetupHandler, original, mfaInput{Password: "wrong password"})
	if badPassword.Code != 403 {
		t.Fatalf("setup without password = %d", badPassword.Code)
	}
	setup := mfaTestSetup(t, original)
	if setup.SetupID == "" || len(setup.Secret) < 32 || !strings.HasPrefix(setup.QR, "data:image/png;base64,") {
		t.Fatal("setup material missing")
	}
	status := authenticatedRequest(t, protect(mfaStatusHandler, false), "GET", "/api/auth/mfa", "", original, false)
	if mfaTestJSON(t, status).Enabled || strings.Contains(status.Body.String(), setup.Secret) {
		t.Fatal("pending setup enabled MFA or exposed secret")
	}
	badCode := mfaTestPOST(t, mfaEnableHandler, original, mfaInput{Code: "not-a-code", SetupID: setup.SetupID})
	if badCode.Code != 403 {
		t.Fatalf("bad setup code = %d", badCode.Code)
	}
	code, _ := totp.GenerateCode(setup.Secret, time.Now())
	enabledResponse := mfaTestPOST(t, mfaEnableHandler, original, mfaInput{Code: code, SetupID: setup.SetupID})
	enabled := mfaTestJSON(t, enabledResponse)
	login := mfaTestLogin(t, enabledResponse)
	if !enabled.Enabled || len(enabled.Codes) != 10 || login.cookie.Value == original.cookie.Value || login.csrf == original.csrf {
		t.Fatal("activation did not rotate session and create codes")
	}
	old := authenticatedRequest(t, protect(meHandler, false), "GET", "/api/auth/me", "", original, false)
	if old.Code != 401 {
		t.Fatal("old session survived activation")
	}
	if old.Header().Get("Set-Cookie") != "" {
		t.Fatal("old request could erase the rotated session cookie")
	}
	var stored string
	if err := db.DB.QueryRow("SELECT code_hash FROM mfa_recovery_codes LIMIT 1").Scan(&stored); err != nil || len(stored) != 64 {
		t.Fatalf("recovery hash: %v", err)
	}
	for _, value := range enabled.Codes {
		if stored == value {
			t.Fatal("recovery code stored in plaintext")
		}
	}
	passwordOnly := mfaSignIn(t, "rick", "original password", "")
	if !mfaTestJSON(t, passwordOnly).Required || len(passwordOnly.Result().Cookies()) != 0 {
		t.Fatal("password alone issued a session")
	}
	if bad := mfaSignIn(t, "rick", "wrong password", enabled.Codes[0]); bad.Code != 401 {
		t.Fatalf("recovery bypassed password: %d", bad.Code)
	}
	// The enrollment code has already been spent. Use the next accepted step.
	nextCode, _ := totp.GenerateCode(setup.Secret, time.Now().Add(30*time.Second))
	verified := mfaSignIn(t, "rick", "original password", nextCode)
	mfaTestLogin(t, verified)
	replay := mfaSignIn(t, "rick", "original password", nextCode)
	if replay.Code != 401 || len(replay.Result().Cookies()) != 0 {
		t.Fatal("TOTP replay issued a session")
	}
	login = mfaTestLogin(t, mfaSignIn(t, "rick", "original password", strings.ToLower(enabled.Codes[0])))
	if repeated := mfaSignIn(t, "rick", "original password", enabled.Codes[0]); repeated.Code != 401 {
		t.Fatalf("recovery code replay = %d", repeated.Code)
	}
	login = mfaTestLogin(t, mfaSignIn(t, "rick", "original password", enabled.Codes[1]))
	status = authenticatedRequest(t, protect(mfaStatusHandler, false), "GET", "/api/auth/mfa", "", login, false)
	if mfaTestJSON(t, status).Remaining != 8 {
		t.Fatal("recovery codes were not consumed")
	}
	regeneratedResponse := mfaTestPOST(t, mfaRecoveryHandler, login, mfaInput{Password: "original password", Code: enabled.Codes[2]})
	regenerated := mfaTestJSON(t, regeneratedResponse)
	login = mfaTestLogin(t, regeneratedResponse)
	if len(regenerated.Codes) != 10 {
		t.Fatal("recovery generation failed")
	}
	if oldCode := mfaSignIn(t, "rick", "original password", enabled.Codes[3]); oldCode.Code != 401 {
		t.Fatalf("old recovery code survived replacement: %d", oldCode.Code)
	}
	login = mfaTestLogin(t, mfaSignIn(t, "rick", "original password", regenerated.Codes[0]))
	disabled := mfaTestPOST(t, mfaDisableHandler, login, mfaInput{Password: "original password", Code: regenerated.Codes[1]})
	if mfaTestJSON(t, disabled).Enabled {
		t.Fatal("MFA not disabled")
	}
	var count int
	if err := db.DB.QueryRow("SELECT count(*) FROM mfa_recovery_codes WHERE user_id=1").Scan(&count); err != nil || count != 0 {
		t.Fatal("disabling retained recovery codes")
	}
	mfaTestLogin(t, mfaSignIn(t, "rick", "original password", ""))
}

func TestMFASetupOwnershipExpiryAndRaces(t *testing.T) {
	for _, kind := range []string{"expired", "other-session", "other-account", "password-changed", "signed-out", "superseded", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			_, _, login, hash := recoveryFixture(t)
			setup := mfaTestSetup(t, login)
			code, _ := totp.GenerateCode(setup.Secret, time.Now())
			switch kind {
			case "expired":
				mustRecoveryExec(t, "UPDATE mfa_factors SET setup_expires_at=?", time.Now().Add(-time.Minute))
			case "other-session", "other-account":
				id, name, role := 1, "rick", "admin"
				if kind == "other-account" {
					id, name, role = 2, "other", "user"
				}
				w := httptest.NewRecorder()
				finishAuthentication(w, httptest.NewRequest("POST", "/", nil), id, name, role, hash)
				login = mfaTestLogin(t, w)
			case "password-changed":
				mustRecoveryExec(t, "UPDATE users SET password_hash=? WHERE id=1", []byte("changed"))
			case "signed-out":
				mustRecoveryExec(t, "DELETE FROM sessions WHERE user_id=1")
			case "superseded":
				mfaTestSetup(t, login)
			case "cancelled":
				mfaTestJSON(t, mfaTestPOST(t, mfaCancelHandler, login, mfaInput{SetupID: setup.SetupID}))
			}
			w := mfaTestPOST(t, mfaEnableHandler, login, mfaInput{Code: code, SetupID: setup.SetupID})
			if w.Code == 200 {
				t.Fatal("stale or foreign setup enabled MFA")
			}
			var count int
			db.DB.QueryRow("SELECT count(*) FROM mfa_factors WHERE enabled_at IS NOT NULL").Scan(&count)
			if count != 0 {
				t.Fatal("setup changed MFA despite rejection")
			}
		})
	}
}

func TestMFACodeConsumptionIsAtomicWithSession(t *testing.T) {
	_, _, _, hash := recoveryFixture(t)
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	mustRecoveryExec(t, "INSERT INTO mfa_factors(user_id,secret,enabled_at) VALUES(1,?,?)", secret, time.Now())
	code, _ := totp.GenerateCode(secret, time.Now())
	bad := httptest.NewRecorder()
	finishAuthenticationWithMFA(bad, httptest.NewRequest("POST", "/", nil), 1, "rick", "admin", code, []byte("stale-password-hash"))
	if bad.Code != 401 {
		t.Fatalf("stale password accepted: %d", bad.Code)
	}
	var step int64
	db.DB.QueryRow("SELECT last_step FROM mfa_factors WHERE user_id=1").Scan(&step)
	if step != -1 {
		t.Fatal("failed session spent TOTP")
	}
	var wg sync.WaitGroup
	results := make(chan bool, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			finishAuthenticationWithMFA(w, httptest.NewRequest("POST", "/", nil), 1, "rick", "admin", code, hash)
			results <- len(w.Result().Cookies()) == 1
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for ok := range results {
		if ok {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("same TOTP created %d sessions", success)
	}
	codes, _ := newMFARecoveryCodes()
	tx, _ := db.DB.Begin()
	replaceMFARecoveryCodes(tx, 1, codes)
	tx.Commit()
	bad = httptest.NewRecorder()
	finishAuthenticationWithMFA(bad, httptest.NewRequest("POST", "/", nil), 1, "rick", "admin", codes[0], []byte("stale-password-hash"))
	var count int
	db.DB.QueryRow("SELECT count(*) FROM mfa_recovery_codes WHERE code_hash=?", recoveryCodeHash(codes[0])).Scan(&count)
	if count != 1 {
		t.Fatal("failed session spent recovery code")
	}
	results = make(chan bool, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			finishAuthenticationWithMFA(w, httptest.NewRequest("POST", "/", nil), 1, "rick", "admin", codes[0], hash)
			results <- len(w.Result().Cookies()) == 1
		}()
	}
	wg.Wait()
	close(results)
	success = 0
	for ok := range results {
		if ok {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("same recovery code created %d sessions", success)
	}
}

func TestMFACooldownAndPasswordRecovery(t *testing.T) {
	s, _, login, hash := recoveryFixture(t)
	_, _, _ = mfaTestEnable(t, login)
	for i := 0; i < 3; i++ {
		mfaSignIn(t, "rick", "original password", "not-a-valid-code")
	}
	if w := mfaSignIn(t, "rick", "original password", ""); w.Code != 429 {
		t.Fatalf("MFA attempts bypassed account cooldown: %d", w.Code)
	}
	token, _ := randomToken()
	// Use the current hash, since a password-only first step may upgrade bcrypt.
	if err := db.DB.QueryRow("SELECT password_hash FROM users WHERE id=1").Scan(&hash); err != nil {
		t.Fatal(err)
	}
	mustRecoveryExec(t, "UPDATE users SET email_verified_at=? WHERE id=1", time.Now())
	mustRecoveryExec(t, "INSERT INTO password_reset_tokens(token_hash,user_id,email,password_hash,expires_at) VALUES(?,1,'old@example.com',?,?)", hashToken(token), hash, time.Now().Add(time.Minute))
	w := recoveryPOST(s.resetPassword, map[string]string{"token": token, "password": "new password after reset"})
	if w.Code != 200 {
		t.Fatalf("password reset = %d", w.Code)
	}
	firstStep := mfaSignIn(t, "rick", "new password after reset", "")
	if !mfaTestJSON(t, firstStep).Required || len(firstStep.Result().Cookies()) != 0 {
		t.Fatal("email password reset bypassed MFA")
	}
}

func TestMFAManagementLimitsAndCodeValidation(t *testing.T) {
	_, _, login, _ := recoveryFixture(t)
	login, data, _ := mfaTestEnable(t, login)
	bad := mfaTestPOST(t, mfaDisableHandler, login, mfaInput{Password: "original password", Code: "invalid"})
	if bad.Code != 403 {
		t.Fatalf("MFA disable without proof = %d", bad.Code)
	}
	wrong := mfaTestPOST(t, mfaDisableHandler, login, mfaInput{Password: "wrong password", Code: data.Codes[0]})
	if wrong.Code != 403 {
		t.Fatalf("MFA disable without password = %d", wrong.Code)
	}
	for i := 0; i < 12; i++ {
		mfaTestPOST(t, mfaDisableHandler, login, mfaInput{Password: "wrong password"})
	}
	limited := mfaTestPOST(t, mfaDisableHandler, login, mfaInput{Password: "original password", Code: data.Codes[0]})
	if limited.Code != 429 {
		t.Fatalf("management rate limit = %d", limited.Code)
	}
}

func TestMFARFC6238AndReplayWindow(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	for _, v := range []struct {
		seconds int64
		code    string
	}{{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"}, {1234567890, "005924"}, {2000000000, "279037"}, {20000000000, "353130"}} {
		step, ok := matchingMFAStep(secret, v.code, time.Unix(v.seconds, 0), -1)
		if !ok || step != v.seconds/30 {
			t.Fatalf("RFC test failed at %d", v.seconds)
		}
		if _, ok := matchingMFAStep(secret, v.code, time.Unix(v.seconds, 0), step); ok {
			t.Fatal("used timestep accepted")
		}
		if _, ok := matchingMFAStep(secret, v.code, time.Unix(v.seconds+90, 0), -1); ok {
			t.Fatal("expired code accepted")
		}
	}
}
