package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
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
			registerSpellingRoutes(mux)
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
			mux.HandleFunc("/api/notes/archive", protect(noteArchiveHandler, false))
			mux.HandleFunc("/api/tags", protect(tagsHandler, false))
			mux.Handle("/", http.FileServer(http.Dir("../../public")))
			server := httptest.NewServer(securityHeaders(mux))
			defer server.Close()
			browser := newRecoveryBrowser(t)
			browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setDeviceMetricsOverride", "params": map[string]any{"width": width, "height": 800, "deviceScaleFactor": 1, "mobile": false}})
			click := func(selector string) {
				t.Helper()
				browser.script("document.querySelector(" + strconv.Quote(selector) + ").scrollIntoView({block:'center'});")
				node := browser.call("POST", "/element", map[string]string{"using": "css selector", "value": selector}).(map[string]any)
				browser.call("POST", "/element/"+node["element-6066-11e4-a52e-4f735466cecf"].(string)+"/click", nil)
			}
			browser.navigate(server.URL)
			browser.wait(`!document.getElementById('auth-screen').hidden`)
			browser.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
			browser.wait(`!notesLoading && document.querySelector('[data-remove-scope="date"]') && document.querySelector('[data-remove-scope="pinned"]')`)
			outsideClick := func() {
				t.Helper()
				browser.call("POST", "/actions", map[string]any{"actions": []any{map[string]any{"type": "pointer", "id": "outside", "parameters": map[string]string{"pointerType": "mouse"}, "actions": []any{map[string]any{"type": "pointerMove", "duration": 0, "x": 2, "y": 2}, map[string]any{"type": "pointerDown", "button": 0}, map[string]any{"type": "pointerUp", "button": 0}}}}})
			}
			for _, id := range []string{"tags-dialog", "spelling-dialog", "dictionary-dialog", "note-action-dialog", "archive-remove-dialog", "rich-link-dialog"} {
				browser.script(fmt.Sprintf(`document.getElementById(%q).showModal();`, id))
				browser.script(fmt.Sprintf(`const d=document.getElementById(%q),r=d.getBoundingClientRect();d.dispatchEvent(new MouseEvent('click',{bubbles:true,clientX:r.left+2,clientY:r.top+2}));if(!d.open)throw new Error('Clicking inside must keep modal open');`, id))
				outsideClick()
				browser.wait(fmt.Sprintf(`!document.getElementById(%q).open`, id))
			}
			browser.script(`showWelcome();`)
			outsideClick()
			browser.wait(`document.getElementById('welcome-overlay').hidden`)
			for _, id := range []string{"transfer-panel", "profile-panel", "help-panel", "archive-panel", "admin-panel"} {
				browser.script(fmt.Sprintf(`document.getElementById(%q).hidden=false;`, id))
				outsideClick()
				browser.wait(fmt.Sprintf(`document.getElementById(%q).hidden`, id))
			}
			browser.script(`for(const id of ['active-notes-archive-all','active-notes-remove-all']){const b=document.getElementById(id);if(b.textContent.trim() || !b.querySelector('svg') || !b.getAttribute('aria-label'))throw new Error('Bulk action must be an accessible icon');}`)
			browser.script(`if(document.querySelectorAll('.note-card').length>=66) throw new Error('Fixture must span multiple pages');hiddenNoteIds.add(1);saveHiddenNoteIds();`)
			click(`[data-remove-scope="date"]`)
			browser.wait(`document.getElementById('archive-remove-dialog').open`)
			browser.script(`if(!document.getElementById('archive-remove-message').textContent.includes('Pinned notes stay'))throw new Error('Missing scope explanation');if(document.activeElement.id!=='archive-remove-cancel')throw new Error('Cancel should have focus');if(document.documentElement.scrollWidth>innerWidth)throw new Error('Overflow: '+JSON.stringify([...document.querySelectorAll('body *')].filter(e=>e.getBoundingClientRect().right>innerWidth+1).slice(0,12).map(e=>[e.tagName,e.id,e.className,e.getBoundingClientRect().right])));`)
			browser.script(`const s=getComputedStyle(document.getElementById('archive-remove-cancel'));if(s.borderStyle==='none'||parseFloat(s.borderWidth)<1||s.backgroundColor==='rgba(0, 0, 0, 0)')throw new Error('Cancel must look like a button');`)
			outsideClick()
			browser.wait(`!document.getElementById('archive-remove-dialog').open`)
			click(`[data-remove-scope="date"]`)
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
			// Restore the fixture, then archive every active note from the footer.
			browser.script(`apiFetch('/api/notes/restore?id=all',{method:'POST'}).then(()=>toggleArchiveMode());`)
			browser.wait(`!notesLoading && !archiveMode && activeNoteCount===67`)
			mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at) VALUES(2,'Other owner','body',?)", created)
			mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at,deleted_at) VALUES(1,'Already recycled','body',?,?)", created, created)
			browser.script(`hiddenNoteIds.add(1);saveHiddenNoteIds();`)
			click("#active-notes-archive-all")
			browser.wait(`document.getElementById('archive-remove-dialog').open && document.getElementById('archive-remove-dialog').dataset.mode==='archive-active'`)
			outsideClick()
			browser.wait(`!document.getElementById('archive-remove-dialog').open && activeNoteCount===67`)
			click("#active-notes-archive-all")
			click("#archive-remove-confirm")
			browser.wait(`!activeRemovalBusy && !notesLoading && activeNoteCount===0 && !hiddenNoteIds.has(1) && document.getElementById('active-notes-bulk-actions').hidden`)
			db.DB.QueryRow("SELECT COUNT(*) FROM notes WHERE user_id=1 AND archived_at IS NOT NULL AND deleted_at IS NULL").Scan(&remaining)
			if remaining != 67 {
				t.Fatalf("Archived %d, want 67 including pinned, hidden, and later-page notes", remaining)
			}
			db.DB.QueryRow("SELECT COUNT(*) FROM notes WHERE (user_id=2 AND archived_at IS NULL AND deleted_at IS NULL) OR (user_id=1 AND deleted_at IS NOT NULL AND archived_at IS NULL)").Scan(&remaining)
			if remaining != 2 {
				t.Fatal("Archive all changed another account or recycled notes")
			}
		})
	}
}
