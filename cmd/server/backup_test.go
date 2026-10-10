package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func backupOtherLogin(t *testing.T, hash []byte) testLogin {
	t.Helper()
	w := httptest.NewRecorder()
	finishAuthentication(w, httptest.NewRequest("POST", "/api/auth/login", nil), 2, "other", "user", hash)
	var result struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return testLogin{cookie: w.Result().Cookies()[0], csrf: result.CSRF, role: "user"}
}

func backupTestDownload(t *testing.T, login testLogin, kind, password string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"kind": kind, "password": password})
	return authenticatedRequest(t, protect(backupDownloadHandler, true), "POST", "/api/backups/download", string(body), login, true)
}

func TestBackupAuthorizationAndSiteKey(t *testing.T) {
	_, _, admin, hash := recoveryFixture(t)
	other := backupOtherLogin(t, hash)
	path := filepath.Join(t.TempDir(), "site-recovery.env")
	const fixture = "RESTIC_PASSWORD=site-recovery-fixture\n"
	if err := os.WriteFile(path, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GNOTES_SITE_BACKUP_RECOVERY_FILE", path)
	for _, kind := range []string{"site-key", "personal-key", "personal-backup"} {
		w := httptest.NewRecorder()
		protect(backupDownloadHandler, true)(w, httptest.NewRequest("POST", "/api/backups/download", nil))
		if w.Code != 401 {
			t.Fatalf("anonymous %s status %d", kind, w.Code)
		}
	}
	if w := backupTestDownload(t, other, "site-key", "original password"); w.Code != 403 || strings.Contains(w.Body.String(), fixture) {
		t.Fatalf("regular user site key status %d", w.Code)
	}
	if w := backupTestDownload(t, admin, "site-key", "incorrect"); w.Code != 403 {
		t.Fatalf("wrong password status %d", w.Code)
	}
	w := authenticatedRequest(t, protect(backupDownloadHandler, true), "POST", "/api/backups/download", `{"kind":"site-key","password":"original password"}`, admin, false)
	if w.Code != 403 {
		t.Fatalf("missing CSRF status %d", w.Code)
	}
	w = backupTestDownload(t, admin, "site-key", "original password")
	if w.Code != 200 || w.Body.String() != fixture {
		t.Fatalf("admin download status %d", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatal("unsafe key response headers")
	}
	t.Setenv("GNOTES_SITE_BACKUP_RECOVERY_FILE", "")
	if w := backupTestDownload(t, admin, "site-key", "original password"); w.Code != 503 {
		t.Fatalf("unconfigured site key status %d", w.Code)
	}
}

func TestPersonalBackupIsolationAndRecovery(t *testing.T) {
	_, _, admin, hash := recoveryFixture(t)
	other := backupOtherLogin(t, hash)
	when := time.Now().UTC().Truncate(time.Second)
	mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at,archived_at,favorited,tags) VALUES(2,'Own archive','private other body',?,?,1,'[\"sample\"]')", when, when)
	mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at,deleted_at) VALUES(2,'Own recycled','recover me',?,?)", when, when)
	mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at) VALUES(1,'Admin secret','never in other backup',?)", when)
	downloadKey := func(login testLogin) personalRecoveryKey {
		w := backupTestDownload(t, login, "personal-key", "original password")
		if w.Code != 200 {
			t.Fatalf("key status %d: %s", w.Code, w.Body.String())
		}
		var key personalRecoveryKey
		if err := json.Unmarshal(w.Body.Bytes(), &key); err != nil {
			t.Fatal(err)
		}
		return key
	}
	key := downloadKey(other)
	again := downloadKey(other)
	adminKey := downloadKey(admin)
	if len(key.Key) != 32 || !bytes.Equal(key.Key, again.Key) || key.KeyID != again.KeyID || key.KeyID == adminKey.KeyID {
		t.Fatal("personal keys are unstable or shared")
	}
	downloadBackup := func() encryptedPersonalBackup {
		w := backupTestDownload(t, other, "personal-backup", "original password")
		if w.Code != 200 {
			t.Fatalf("backup status %d: %s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "private other body") || strings.Contains(w.Body.String(), "never in other backup") {
			t.Fatal("plaintext in encrypted backup")
		}
		var backup encryptedPersonalBackup
		if err := json.Unmarshal(w.Body.Bytes(), &backup); err != nil {
			t.Fatal(err)
		}
		return backup
	}
	backup, second := downloadBackup(), downloadBackup()
	if bytes.Equal(backup.Nonce, second.Nonce) || backup.KeyID != key.KeyID {
		t.Fatal("nonce reused or incorrect key ID")
	}
	block, _ := aes.NewCipher(key.Key)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, backup.Nonce, backup.Ciphertext, []byte("gnotes-personal-backup-v1"))
	if err != nil {
		t.Fatal(err)
	}
	var file transferFile
	if err = json.Unmarshal(plain, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Notes) != 2 || strings.Contains(string(plain), "Admin secret") || file.Notes[1].ArchivedAt == nil || !file.Notes[1].Favorited || len(file.Notes[1].Tags) != 1 || file.Notes[0].DeletedAt == nil {
		t.Fatal("personal backup lost details or crossed accounts")
	}
	wrongBlock, _ := aes.NewCipher(adminKey.Key)
	wrongGCM, _ := cipher.NewGCM(wrongBlock)
	if _, err := wrongGCM.Open(nil, backup.Nonce, backup.Ciphertext, []byte("gnotes-personal-backup-v1")); err == nil {
		t.Fatal("other user's key unlocked backup")
	}
	backup.Ciphertext[0] ^= 1
	if _, err := gcm.Open(nil, backup.Nonce, backup.Ciphertext, []byte("gnotes-personal-backup-v1")); err == nil {
		t.Fatal("modified ciphertext accepted")
	}
	w := authenticatedRequest(t, protect(importNotesHandler, true), "POST", "/api/notes/import?format=gnotes&duplicates=skip", string(plain), other, true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"skipped":2`) {
		t.Fatalf("decrypted import failed: %d %s", w.Code, w.Body.String())
	}
}

func TestBackupPasswordAttemptsAreLimited(t *testing.T) {
	_, _, admin, _ := recoveryFixture(t)
	for i := 0; i < 12; i++ {
		if w := backupTestDownload(t, admin, "personal-key", "wrong password"); w.Code != 403 {
			t.Fatalf("attempt %d status %d", i, w.Code)
		}
	}
	if w := backupTestDownload(t, admin, "personal-key", "wrong password"); w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("limit status %d", w.Code)
	}
}
