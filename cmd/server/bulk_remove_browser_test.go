package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"gnotes/internal/db"
)

func TestActiveBulkRemovalBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	for _, width := range []int{320, 1024} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			recoveryFixture(t)
			created := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
			for i := 0; i < 65; i++ {
				mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at) VALUES(1,'Bulk note','body',?)", created)
			}
			mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at,pinned) VALUES(1,'Pinned note','body',?,1)", created)
			mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at,archived_at) VALUES(1,'Archived note','body',?,?)", created, created)
			mux := http.NewServeMux()
			mux.HandleFunc("/api/auth/config", authConfigHandler)
			mux.HandleFunc("/api/auth/login", loginHandler)
			mux.HandleFunc("/api/auth/me", protect(meHandler, false))
			mux.HandleFunc("/api/draft", protect(getDraftHandler, false))
			mux.HandleFunc("/api/draft/update", protect(updateDraftHandler, true))
			mux.HandleFunc("/api/draft/finalize", protect(finalizeDraftHandler, true))
			mux.HandleFunc("/api/notes/page", protect(pagedListNotesHandler, false))
			mux.HandleFunc("/api/notes/trash-page", protect(pagedTrashNotesHandler, false))
			mux.HandleFunc("/api/notes/update", protect(updateNoteHandler, true))
			mux.HandleFunc("/api/notes/delete-active", protect(deleteActiveNotesHandler, true))
			mux.HandleFunc("/api/notes/delete-archived", protect(deleteArchivedNotesHandler, true))
			mux.HandleFunc("/api/notes/restore", protect(restoreNotesHandler, true))
			mux.HandleFunc("/api/tags", protect(tagsHandler, false))
			mux.Handle("/", http.FileServer(http.Dir("../../public")))
			server := httptest.NewServer(securityHeaders(mux))
			defer server.Close()
			browser := newRecoveryBrowser(t)
			browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setDeviceMetricsOverride", "params": map[string]any{"width": width, "height": 800, "deviceScaleFactor": 1, "mobile": false}})
			click := func(selector string) {
				t.Helper()
				node := browser.call("POST", "/element", map[string]string{"using": "css selector", "value": selector}).(map[string]any)
				browser.call("POST", "/element/"+node["element-6066-11e4-a52e-4f735466cecf"].(string)+"/click", nil)
			}
			browser.navigate(server.URL)
			browser.wait(`!document.getElementById('auth-screen').hidden`)
			browser.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
			browser.wait(`!notesLoading && document.querySelector('[data-remove-scope="date"]') && document.querySelector('[data-remove-scope="pinned"]')`)
			browser.script(`if(document.querySelectorAll('.note-card').length>=66) throw new Error('Fixture must span multiple pages');hiddenNoteIds.add(1);saveHiddenNoteIds();`)
			click(`[data-remove-scope="date"]`)
			browser.wait(`document.getElementById('archive-remove-dialog').open`)
			browser.script(`if(!document.getElementById('archive-remove-message').textContent.includes('Pinned notes stay'))throw new Error('Missing scope explanation');if(document.activeElement.id!=='archive-remove-cancel')throw new Error('Cancel should have focus');if(document.documentElement.scrollWidth>innerWidth)throw new Error('Overflow');`)
			click("#archive-remove-cancel")
			var remaining int
			db.DB.QueryRow("SELECT COUNT(*) FROM notes WHERE deleted_at IS NULL").Scan(&remaining)
			if remaining != 67 {
				t.Fatal("Cancel changed notes")
			}
			click(`[data-remove-scope="date"]`)
			browser.wait(`document.getElementById('archive-remove-dialog').open`)
			click("#archive-remove-confirm")
			browser.wait(`!activeRemovalBusy && !notesLoading && document.querySelectorAll('.note-card').length===1 && activeNoteCount===1 && pinnedNoteCount===1 && !hiddenNoteIds.has(1)`)
			db.DB.QueryRow("SELECT COUNT(*) FROM notes WHERE deleted_at IS NOT NULL").Scan(&remaining)
			if remaining != 65 {
				t.Fatalf("Removed %d, want all 65 including later page", remaining)
			}
			click(`[data-remove-scope="pinned"]`)
			browser.wait(`document.getElementById('archive-remove-dialog').open`)
			click("#archive-remove-confirm")
			browser.wait(`!activeRemovalBusy && !notesLoading && activeNoteCount===0 && pinnedNoteCount===0 && document.getElementById('active-notes-remove-all').hidden`)
			browser.script(`apiFetch('/api/notes/restore?id=all',{method:'POST'}).then(()=>loadNotes());`)
			browser.wait(`!notesLoading && activeNoteCount===66 && document.querySelector('[data-remove-scope="date"]')`)
			click("#active-notes-remove-all")
			browser.wait(`document.getElementById('archive-remove-dialog').open`)
			click("#archive-remove-confirm")
			browser.wait(`!activeRemovalBusy && !notesLoading && activeNoteCount===0`)
			db.DB.QueryRow("SELECT COUNT(*) FROM notes WHERE archived_at IS NOT NULL AND deleted_at IS NULL").Scan(&remaining)
			if remaining != 1 {
				t.Fatal("Active removal changed Archive")
			}
			browser.script(`toggleArchiveMode();`)
			browser.wait(`!notesLoading && archiveMode && document.querySelector('[data-archive-date]')`)
			click("[data-archive-date]")
			browser.wait(`document.getElementById('archive-remove-dialog').open && document.getElementById('archive-remove-dialog').dataset.mode==='archived'`)
			click("#archive-remove-confirm")
			browser.wait(`!notesLoading && document.querySelectorAll('.note-card').length===0`)
			db.DB.QueryRow("SELECT COUNT(*) FROM notes WHERE deleted_at IS NOT NULL").Scan(&remaining)
			if remaining != 67 {
				t.Fatal("Archive removal regression")
			}
		})
	}
}
