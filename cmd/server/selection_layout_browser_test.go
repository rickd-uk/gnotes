package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestSelectionOrientationBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	recoveryFixture(t)
	mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at) VALUES(1,'Selection test','Serendipity makes notes interesting.',?)", time.Now().UTC())
	mux := http.NewServeMux()
	registerDictionaryRoutes(mux)
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
	browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setTouchEmulationEnabled", "params": map[string]any{"enabled": true, "maxTouchPoints": 1}})
	browser.navigate(server.URL)
	browser.wait(`!document.getElementById('auth-screen').hidden`)
	browser.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
	browser.wait(`!notesLoading && !document.getElementById('app-shell').hidden && spellingReady`)
	for _, layout := range []struct {
		name          string
		width, height int
	}{
		{"phone portrait", 360, 800}, {"phone landscape", 800, 360},
		{"tablet portrait", 768, 1024}, {"tablet landscape", 1024, 768},
	} {
		t.Run(layout.name, func(t *testing.T) {
			browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setDeviceMetricsOverride", "params": map[string]any{"width": layout.width, "height": layout.height, "deviceScaleFactor": 1, "mobile": false}})
			browser.script(`getSelection().removeAllRanges();const p=document.querySelector('.note-content p');p.scrollIntoView({block:'center'});const r=document.createRange();r.setStart(p.firstChild,0);r.setEnd(p.firstChild,11);getSelection().addRange(r);`)
			browser.wait(`!document.getElementById('note-selection-actions').hidden`)
			browser.script(`const bar=document.getElementById('note-selection-actions'),r=bar.getBoundingClientRect(),v=visualViewport;if(!bar.classList.contains('is-docked')||r.left<v.offsetLeft||r.right>v.offsetLeft+v.width||r.bottom>v.offsetTop+v.height||r.top<v.offsetTop+v.height-100)throw new Error('Read-view bar is not within the bottom of the visible viewport');if(getSelection().toString()!=='Serendipity')throw new Error('Selection lost on rotation');window.layoutNoteID=Number(document.querySelector('.note-card').id.slice(5));openNoteEditor(layoutNoteID,false);`)
			browser.wait(`Boolean(richEdit)`)
			browser.script(`richEdit.editor.chain().focus().setTextSelection({from:1,to:12}).run();`)
			browser.wait(`!document.getElementById('note-selection-actions').hidden`)
			browser.script(`const r=document.getElementById('note-selection-actions').getBoundingClientRect(),f=document.querySelector('.note-card.is-editing .editor-actions').getBoundingClientRect(),v=visualViewport;if(r.left<v.offsetLeft||r.right>v.offsetLeft+v.width||r.top<v.offsetTop||r.bottom>f.top||r.bottom>v.offsetTop+v.height||!document.querySelector('.rich-context-menu').hidden)throw new Error('Editor bar overlaps selection or Copy/Done controls');`)
			t.Logf("Verified %s (%dx%d), read view and editor", layout.name, layout.width, layout.height)
			browser.script(`updateNote(layoutNoteID);`)
			browser.wait(`editingNoteId===null && !notesLoading`)
		})
	}
	// Also rotate an existing editor selection, rather than reselecting afterward.
	browser.script(`openNoteEditor(layoutNoteID,false);`)
	browser.wait(`Boolean(richEdit)`)
	browser.script(`richEdit.editor.chain().focus().setTextSelection({from:1,to:12}).run();`)
	for _, size := range [][2]int{{360, 800}, {800, 360}, {768, 1024}, {1024, 768}} {
		browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setDeviceMetricsOverride", "params": map[string]any{"width": size[0], "height": size[1], "deviceScaleFactor": 1, "mobile": false}})
		browser.wait(fmt.Sprintf(`innerWidth===%d && innerHeight===%d && !document.getElementById('note-selection-actions').hidden && document.getElementById('note-selection-actions').getBoundingClientRect().bottom <= document.querySelector('.note-card.is-editing .editor-actions').getBoundingClientRect().top`, size[0], size[1]))
		browser.script(`if(getSelection().toString()!=='Serendipity')throw new Error('Rotating cleared native selection');`)
	}
}
