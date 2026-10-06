package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestUntitledNotesBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	for _, width := range []int{320, 1024} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			recoveryFixture(t)
			mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at) VALUES(1,'','First line preview\nSecond line of text',?)", time.Now().UTC())
			mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at) VALUES(1,'Real title','Titled body',?)", time.Now().UTC().Add(-time.Minute))
			mux := http.NewServeMux()
			registerSpellingRoutes(mux)
			mux.HandleFunc("/api/auth/config", authConfigHandler)
			mux.HandleFunc("/api/auth/login", loginHandler)
			mux.HandleFunc("/api/auth/me", protect(meHandler, false))
			mux.HandleFunc("/api/draft", protect(getDraftHandler, false))
			mux.HandleFunc("/api/notes/page", protect(pagedListNotesHandler, false))
			mux.HandleFunc("/api/notes/trash-page", protect(pagedTrashNotesHandler, false))
			mux.HandleFunc("/api/notes/update", protect(updateNoteHandler, true))
			mux.HandleFunc("/api/tags", protect(tagsHandler, false))
			mux.Handle("/", http.FileServer(http.Dir("../../public")))
			server := httptest.NewServer(securityHeaders(mux))
			defer server.Close()
			browser := newRecoveryBrowser(t)
			browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setDeviceMetricsOverride", "params": map[string]any{"width": width, "height": 800, "deviceScaleFactor": 1, "mobile": false}})
			browser.navigate(server.URL)
			browser.wait(`!document.getElementById('auth-screen').hidden`)
			browser.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
			browser.wait(`!notesLoading && !document.getElementById('app-shell').hidden && document.querySelectorAll('.note-card').length===2`)
			browser.script(`window.untitledCard=document.querySelector('.note-preview-title').closest('.note-card');window.untitledID=Number(untitledCard.id.slice(5));window.preview=untitledCard.querySelector('.note-preview-title');window.body=untitledCard.querySelector('.note-content');if(getComputedStyle(preview).display!=='none'||preview.getBoundingClientRect().height!==0||getComputedStyle(body).display==='none')throw new Error('Expanded untitled note duplicates the body');if(!untitledCard.innerText.includes('Second line of text')||untitledCard.innerText.split('First line preview').length!==2)throw new Error('Body should appear exactly once');if(getComputedStyle(document.querySelector('.note-view > h2:not(.fallback-title)')).display==='none')throw new Error('Real titles must stay visible');`)
			browser.script(`toggleNoteRoll(untitledID);if(getComputedStyle(preview).display==='none'||getComputedStyle(body).display!=='none'||!untitledCard.innerText.includes('First line preview')||untitledCard.innerText.includes('Second line'))throw new Error('Folded note must show only its preview');toggleNoteRoll(untitledID);if(getComputedStyle(preview).display!=='none')throw new Error('Expanded preview stays visible');applyDensityMode(1);if(getComputedStyle(preview).display!=='none'||getComputedStyle(body).display==='none')throw new Error('Compact view duplicates preview');applyDensityMode(2);if(getComputedStyle(preview).display==='none'||getComputedStyle(body).display!=='none')throw new Error('Titles-only view needs a preview');applyDensityMode(0);if(getComputedStyle(preview).display!=='none')throw new Error('Switching to Full duplicates body');openNoteEditor(untitledID,false);`)
			browser.wait(`Boolean(richEdit)`)
			browser.script(`if(document.getElementById('edit-title-'+untitledID).value!==''||!richEdit.getMarkdown().includes('First line preview'))throw new Error('Preview changed stored note title/body');updateNote(untitledID);`)
			browser.wait(`editingNoteId===null && !notesLoading`)
			browser.navigate(server.URL)
			browser.wait(`!notesLoading && !document.getElementById('app-shell').hidden && document.querySelector('.note-preview-title')`)
			browser.script(`const title=document.querySelector('.note-preview-title');if(getComputedStyle(title).display!=='none'||title.closest('.note-card').innerText.split('First line preview').length!==2)throw new Error('Untitled behavior did not survive reload');if(document.documentElement.scrollWidth>innerWidth)throw new Error('Note overflow');`)
		})
	}
}
