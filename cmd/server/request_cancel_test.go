package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"gnotes/internal/db"
)

func TestPagedReadsCancelWhileDatabaseBusy(t *testing.T) {
	for _, endpoint := range []struct {
		name, path string
		handler    http.HandlerFunc
	}{
		{"recent", "/api/notes/page", pagedListNotesHandler},
		{"search", "/api/notes/search-page?q=cancel", pagedSearchNotesHandler},
		{"trash", "/api/notes/trash-page", pagedTrashNotesHandler},
		{"single", "/api/notes/get?id=1", getNoteHandler},
		{"authentication", "/api/notes/page", protect(pagedListNotesHandler, false)},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			db.InitDB(filepath.Join(t.TempDir(), "cancel.db"))
			t.Cleanup(func() { db.DB.Close() })
			userID := insertPageTestUser(t, "reader")
			// Hold the only connection. A context-free read cannot finish until
			// this transaction ends; a canceled read must leave the queue now.
			tx, err := db.DB.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), authContextKey{}, authSession{UserID: userID}))
			defer cancel()
			request := httptest.NewRequest(http.MethodGet, endpoint.path, nil).WithContext(ctx)
			request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "existing-session"})
			done := make(chan struct{})
			go func() {
				defer close(done)
				endpoint.handler(httptest.NewRecorder(), request)
			}()
			cancel()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				tx.Rollback()
				<-done
				t.Fatal("canceled read waited for the occupied database connection")
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			// Cancellation must leave the connection usable for the latest read.
			var one int
			if err := db.DB.QueryRow("SELECT 1").Scan(&one); err != nil || one != 1 {
				t.Fatalf("database unusable after cancellation: one=%d err=%v", one, err)
			}
		})
	}
}

func TestCanceledAuthenticationDoesNotClearSessionCookie(t *testing.T) {
	recoveryFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/notes/page", nil).WithContext(ctx)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "existing-session"})
	response := httptest.NewRecorder()
	called := false
	protect(func(http.ResponseWriter, *http.Request) { called = true }, false)(response, request)
	if called || response.Header().Get("Set-Cookie") != "" {
		t.Fatalf("canceled authentication called handler=%v or cleared cookie=%q", called, response.Header().Get("Set-Cookie"))
	}
}

func TestCanceledRenderCachePreservesConnectionSettings(t *testing.T) {
	db.InitDB(filepath.Join(t.TempDir(), "cache-cancel.db"))
	t.Cleanup(func() { db.DB.Close() })
	updates := make([]renderedCacheUpdate, 4000)
	for i := range updates {
		updates[i] = renderedCacheUpdate{ID: i + 1, Content: "body", HTML: "<p>body</p>"}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- saveRenderedCache(ctx, updates) }()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for db.DB.Stats().InUse == 0 {
		select {
		case err := <-done:
			t.Fatalf("cache write finished before cancellation: %v", err)
		case <-deadline.C:
			t.Fatal("cache write did not acquire a connection")
		default:
			runtime.Gosched()
		}
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled cache write unexpectedly succeeded")
		}
	case <-deadline.C:
		t.Fatal("cache write did not stop after cancellation")
	}
	for pragma, want := range map[string]int{"foreign_keys": 1, "synchronous": 2, "busy_timeout": 5000} {
		var got int
		if err := db.DB.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil || got != want {
			t.Errorf("cancellation changed %s: got=%d want=%d err=%v", pragma, got, want, err)
		}
	}
}
