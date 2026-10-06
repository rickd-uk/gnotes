package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
)

func TestSpellingMigrationBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	for _, width := range []int{320, 1024} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			recoveryFixture(t)
			mustRecoveryExec(t, "INSERT INTO spelling_entries VALUES(1,'word','serverword','serverword','word','2026-10-06'),(1,'name','server name','Server Name','company','2026-10-06')")
			mux := http.NewServeMux()
			registerSpellingRoutes(mux)
			mux.HandleFunc("/api/auth/config", authConfigHandler)
			mux.HandleFunc("/api/auth/login", loginHandler)
			mux.HandleFunc("/api/auth/me", protect(meHandler, false))
			mux.HandleFunc("/api/draft", protect(getDraftHandler, false))
			mux.HandleFunc("/api/notes/page", protect(pagedListNotesHandler, false))
			mux.HandleFunc("/api/notes/trash-page", protect(pagedTrashNotesHandler, false))
			mux.HandleFunc("/api/tags", protect(tagsHandler, false))
			mux.Handle("/", http.FileServer(http.Dir("../../public")))
			var failMigration, failSave atomic.Bool
			failMigration.Store(true)
			server := httptest.NewServer(securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if (r.URL.Path == "/api/spelling/migrate" && failMigration.Load()) || (r.URL.Path == "/api/spelling" && r.Method == "POST" && failSave.Load()) {
					http.Error(w, "Temporary sync failure", 503)
					return
				}
				mux.ServeHTTP(w, r)
			})))
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
			browser.script(`localStorage.setItem('gnotes-ui:rick',JSON.stringify({ignoredSpellingWords:['Legacyword'],spellingNames:[{name:'Old Name',kind:'person'}]}));document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
			browser.wait(`!document.getElementById('app-shell').hidden && !spellingBusy`)
			browser.script(`const state=JSON.parse(localStorage.getItem('gnotes-ui:rick'));if(spellingReady||spellingListsMigrated||state.spellingListsMigrated||state.ignoredSpellingWords.length!==1||state.spellingNames.length!==1)throw new Error('Failed migration discarded legacy lists');document.getElementById('spelling-open').click();`)
			browser.wait(`document.getElementById('spelling-dialog').open && !spellingBusy && !document.getElementById('spelling-retry').hidden`)
			failMigration.Store(false)
			click("#spelling-retry")
			browser.wait(`spellingReady && !spellingBusy && ignoredSpellingWords.length===2 && spellingNames.length===2`)
			browser.script(`const state=JSON.parse(localStorage.getItem('gnotes-ui:rick'));if(!state.spellingListsMigrated || 'ignoredSpellingWords' in state || 'spellingNames' in state)throw new Error('Migrated lists should be removed from local storage');`)
			// A failed save must not appear successful or discard entered text.
			failSave.Store(true)
			browser.script(`document.getElementById('spelling-word').value='Unsavedword';document.getElementById('spelling-ignore-form').requestSubmit();`)
			browser.wait(`!spellingBusy && document.getElementById('note-action-dialog').open`)
			browser.script(`if(ignoredSpellingWords.includes('unsavedword') || !document.getElementById('spelling-word').value)throw new Error('Failed save changed lists or discarded input');`)
			click("#note-action-dialog-close")
			failSave.Store(false)
			click("#spelling-close")
			// New browser storage still gets account lists from the database.
			browser.script(`localStorage.clear();`)
			browser.navigate(server.URL)
			browser.wait(`!document.getElementById('app-shell').hidden && spellingReady && !spellingBusy && ignoredSpellingWords.length===2 && spellingNames.length===2`)
			// Another device's additions appear on focus, with no replacement writes.
			mustRecoveryExec(t, "INSERT INTO spelling_entries VALUES(1,'name','remote place','Remote Place','place','2026-10-06')")
			browser.script(`window.dispatchEvent(new Event('focus'));`)
			browser.wait(`!spellingBusy && spellingNames.length===3`)
			browser.script(`document.getElementById('spelling-open').click();`)
			browser.wait(`document.getElementById('spelling-dialog').open && !spellingBusy`)
			click(`[aria-label="Remove Old Name from names"]`)
			browser.wait(`!spellingBusy && spellingNames.length===2 && !spellingNames.some(entry=>entry.name==='Old Name')`)
			click("#spelling-close")
			browser.navigate(server.URL)
			browser.wait(`!document.getElementById('app-shell').hidden && spellingReady && !spellingBusy && spellingNames.length===2`)
			browser.script(`if(spellingNames.some(entry=>entry.name==='Old Name'))throw new Error('Deleted migrated name was resurrected');`)
		})
	}
}
