//go:build cgo && (sqlite_fts5 || fts5)

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestEncryptedNoteBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	for _, width := range []int{320, 1024} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			encryptedFixture(t)
			mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at) VALUES(1,'Encrypted browser note','Original private text',?)", time.Now().UTC())
			mux := http.NewServeMux()
			registerDictionaryRoutes(mux)
			mux.HandleFunc("/api/auth/config", authConfigHandler)
			mux.HandleFunc("/api/auth/login", loginHandler)
			mux.HandleFunc("/api/auth/me", protect(meHandler, false))
			mux.HandleFunc("/api/auth/logout", protect(logoutHandler, true))
			mux.HandleFunc("/api/draft", protect(getDraftHandler, false))
			mux.HandleFunc("/api/draft/update", protect(updateDraftHandler, true))
			mux.HandleFunc("/api/notes/page", protect(pagedListNotesHandler, false))
			mux.HandleFunc("/api/notes/search-page", protect(pagedSearchNotesHandler, false))
			mux.HandleFunc("/api/notes/update", protect(updateNoteHandler, true))
			mux.HandleFunc("/api/notes/lock", protect(lockNoteHandler, true))
			mux.HandleFunc("/api/tags", protect(tagsHandler, false))
			mux.Handle("/", http.FileServer(http.Dir("../../public")))
			server := httptest.NewServer(securityHeaders(mux))
			defer server.Close()
			browser := newRecoveryBrowser(t)
			browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setDeviceMetricsOverride", "params": map[string]any{"width": width, "height": 800, "deviceScaleFactor": 1, "mobile": false}})
			browser.navigate(server.URL)
			browser.wait(`!document.getElementById('auth-screen').hidden`)
			browser.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
			browser.wait(`!notesLoading && loadedNotes.size===1 && spellingReady`)
			browser.script(`openNoteEditor(1,false);`)
			browser.wait(`Boolean(richEdit)`)
			browser.script(`richEdit.editor.commands.setContent('<p>Encrypted browser saved text</p>');updateNote(1);`)
			browser.wait(`editingNoteId===null && pendingNoteEdits.size===0 && !notesLoading`)
			browser.navigate(server.URL)
			browser.wait(`!notesLoading && loadedNotes.get(1)?.content.includes('Encrypted browser saved text')`)
			browser.script(`toggleNoteLock(1);`)
			browser.wait(`loadedNotes.get(1)?.locked && !pendingNoteActions.size`)
			browser.script(`const b=document.querySelector('.note-lock-indicator');if(b.textContent.trim()||!b.title.includes('Read-only'))throw new Error('Read-only indicator must be icon-only');if(document.documentElement.scrollWidth>innerWidth)throw new Error('Encrypted view overflows');const logout=document.getElementById('logout-button');if(logout.textContent.trim()||logout.getAttribute('aria-label')!=='Sign out'||logout.title!=='Sign out')throw new Error('Sign-out icon lacks accessible name');logout.click();`)
			browser.wait(`!document.getElementById('auth-screen').hidden`)
		})
	}
}
