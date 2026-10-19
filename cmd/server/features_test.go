package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"gnotes/internal/db"
)

func TestFeaturePreferencesDefaultsPersistenceAndIsolation(t *testing.T) {
	_, _, login, _ := recoveryFixture(t)
	handler := protect(featurePreferencesHandler, false)
	get := func(login testLogin) featurePreferences {
		t.Helper()
		response := authenticatedRequest(t, handler, "GET", "/api/features", "", login, false)
		var prefs featurePreferences
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &prefs) != nil {
			t.Fatalf("get: %d %s", response.Code, response.Body.String())
		}
		return prefs
	}
	if prefs := get(login); !prefs.Organization || !prefs.AdvancedEditing || prefs.Theme != "light" {
		t.Fatal("existing admin lost Full mode", prefs)
	}
	existing, err := loadFeaturePreferences(2)
	if err != nil || !existing.Organization || !existing.WritingTools {
		t.Fatal("existing regular user lost Full mode", existing, err)
	}
	mustRecoveryExec(t, "UPDATE settings SET value='true' WHERE key='signups_enabled'")
	mustRecoveryExec(t, "UPDATE settings SET value='false' WHERE key='invite_required'")
	registered := registrationResponse(t, "simpleuser", "", "198.51.100.24:1234")
	if registered.Code != 200 {
		t.Fatalf("registration: %d %s", registered.Code, registered.Body.String())
	}
	var auth struct {
		CSRF     string             `json:"csrf_token"`
		Features featurePreferences `json:"features"`
	}
	if err := json.Unmarshal(registered.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}
	if auth.Features.Organization || auth.Features.WritingTools || auth.Features.NoteControls || auth.Features.Appearance || auth.Features.AdvancedEditing || auth.Features.OfflineReading {
		t.Fatal("new account did not start Simple", auth.Features)
	}
	user := testLogin{cookie: registered.Result().Cookies()[0], csrf: auth.CSRF, role: "user"}
	if got := authenticatedRequest(t, handler, "PUT", "/api/features", `{"mode":"full"}`, user, false); got.Code != 403 {
		t.Fatal("missing CSRF accepted", got.Code)
	}
	for _, body := range []string{`{}`, `{"mode":"invalid"}`, `{"feature":"organization","enabled":null}`, `{"feature":"user_id","enabled":true}`, `{"feature":"organization","enabled":true,"mode":"full"}`, `{"theme":"unknown"}`, `{"mode":"full"} {}`, `{"mode":"full","user_id":1}`} {
		if got := authenticatedRequest(t, handler, "PUT", "/api/features", body, user, true); got.Code != 400 {
			t.Fatalf("accepted invalid %s: %d", body, got.Code)
		}
	}
	for _, body := range []string{`{"feature":"organization","enabled":true}`, `{"theme":"dark"}`} {
		if got := authenticatedRequest(t, handler, "PUT", "/api/features", body, user, true); got.Code != 200 {
			t.Fatalf("save: %d %s", got.Code, got.Body.String())
		}
	}
	prefs := get(user)
	if !prefs.Organization || prefs.WritingTools || prefs.Theme != "dark" {
		t.Fatal("individual choices not retained", prefs)
	}
	if get(login).Theme != "light" {
		t.Fatal("settings leaked to another account")
	}
	authenticatedRequest(t, handler, "PUT", "/api/features", `{"mode":"full"}`, user, true)
	if p := get(user); !p.Organization || !p.AdvancedEditing || !p.OfflineReading {
		t.Fatal("Full mode incomplete", p)
	}
	authenticatedRequest(t, handler, "PUT", "/api/features", `{"mode":"simple"}`, user, true)
	if p := get(user); p.Organization || p.AdvancedEditing || p.OfflineReading || p.Theme != "dark" {
		t.Fatal("Simple defaults failed", p)
	}
	// A new login reads settings from SQLite, independent of browser state.
	response := authenticatedRequest(t, protect(meHandler, false), "GET", "/api/auth/me", "", user, false)
	var profile struct {
		Features featurePreferences `json:"features"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &profile); err != nil || profile.Features.Theme != "dark" || profile.Features.Organization {
		t.Fatal("profile did not persist preferences", err, response.Body.String())
	}
	var count int
	if err := db.DB.QueryRow("SELECT count(*) FROM feature_preferences").Scan(&count); err != nil || count != 1 {
		t.Fatal("unexpected preference rows", count, err)
	}
}

func TestFeaturePreferencesUnauthenticated(t *testing.T) {
	recoveryFixture(t)
	response := authenticatedRequest(t, protect(featurePreferencesHandler, false), http.MethodGet, "/api/features", "", testLogin{cookie: &http.Cookie{Name: sessionCookieName, Value: "invalid"}}, false)
	if response.Code != 401 {
		t.Fatal("anonymous preferences allowed", response.Code)
	}
}
