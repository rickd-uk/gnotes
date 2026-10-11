package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestNoteLockBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	for _, width := range []int{320, 1024} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			recoveryFixture(t)
			mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at) VALUES(1,'Lock test',?,?)", "- [ ] Original task\n\n```go\noriginal code\n```", time.Now().UTC())
			mux := http.NewServeMux()
			registerDictionaryRoutes(mux)
			mux.HandleFunc("/api/auth/config", authConfigHandler)
			mux.HandleFunc("/api/auth/login", loginHandler)
			mux.HandleFunc("/api/auth/me", protect(meHandler, false))
			mux.HandleFunc("/api/draft", protect(getDraftHandler, false))
			mux.HandleFunc("/api/draft/update", protect(updateDraftHandler, true))
			mux.HandleFunc("/api/notes/page", protect(pagedListNotesHandler, false))
			mux.HandleFunc("/api/notes/trash-page", protect(pagedTrashNotesHandler, false))
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
			browser.script(`window.lockNoteID=[...loadedNotes.keys()][0];toggleNoteActions(lockNoteID);`)
			node := browser.call("POST", "/element", map[string]string{"using": "css selector", "value": ".lock-btn"}).(map[string]any)
			browser.call("POST", "/element/"+node["element-6066-11e4-a52e-4f735466cecf"].(string)+"/click", nil)
			browser.wait(`loadedNotes.get(lockNoteID).locked && !pendingNoteActions.size && !notesLoading`)
			browser.script(`const card=document.getElementById('note-'+lockNoteID);if(openNoteEditor(lockNoteID)!==false||editingNoteId!==null)throw new Error('Locked note opened for editing');if(!card.querySelector('.note-lock-indicator')||!card.querySelector('input[type=checkbox]').disabled||!card.querySelector('[aria-label="Edit note tags"]').disabled||!card.querySelector('.color-btn').disabled||!card.querySelector('.note-code-language').disabled)throw new Error('Locked edits still enabled');const rect=card.querySelector('.note-lock-indicator').getBoundingClientRect();if(rect.left<0||rect.right>innerWidth)throw new Error('Lock indicator overflows');window.lockDensity=notesList.className;window.lockTools=showNoteTools;for(const density of ['density-full','density-compact','density-titles']){notesList.className=density;applyNoteToolsVisibility(false);if(!card.querySelector('.note-lock-indicator').getClientRects().length)throw new Error('Hidden unlock in '+density);}notesList.className=lockDensity;applyNoteToolsVisibility(lockTools);GnotesShortcuts.open();if(!GnotesShortcuts.actions.has('note:lock'))throw new Error('Lock shortcut absent');document.getElementById('shortcuts-dialog').close();`)
			browser.navigate(server.URL)
			browser.wait(`!notesLoading && loadedNotes.size===1 && [...loadedNotes.values()][0].locked`)
			browser.script(`window.lockNoteID=[...loadedNotes.keys()][0];document.querySelector('.note-lock-indicator').click();`)
			browser.wait(`!loadedNotes.get(lockNoteID).locked && !pendingNoteActions.size && !notesLoading`)
			browser.script(`if(document.querySelector('.note-card input[type=checkbox]').disabled)throw new Error('Unlock did not enable checklist');openNoteEditor(lockNoteID,false);if(!richEdit)throw new Error('Unlock did not enable editing');window.otherWindowLocked=false;apiFetch('/api/notes/lock?id='+lockNoteID,{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({locked:true})}).then(r=>window.otherWindowLocked=r.ok);`)
			browser.wait(`window.otherWindowLocked===true`)
			browser.script(`richEdit.editor.commands.setContent('<p>Keep this unsaved edit</p>');updateNote(lockNoteID);`)
			browser.wait(`loadedNotes.get(lockNoteID).locked && pendingNoteEdits.size===1 && document.getElementById('note-action-dialog').open`)
			browser.script(`if(!document.getElementById('note-action-dialog-message').textContent.includes('locked in another window'))throw new Error('Missing lock recovery explanation');document.getElementById('note-action-dialog').close();document.querySelector('.note-lock-indicator').click();`)
			browser.wait(`editingNoteId===lockNoteID && Boolean(richEdit) && !pendingNoteActions.size`)
			browser.script(`if(!richEdit.getMarkdown().includes('Keep this unsaved edit'))throw new Error('Lock rejection lost unsaved text');updateNote(lockNoteID);`)
			browser.wait(`editingNoteId===null && pendingNoteEdits.size===0 && !notesLoading`)
			browser.script(`if(!loadedNotes.get(lockNoteID).content.includes('Keep this unsaved edit')||loadedNotes.get(lockNoteID).locked)throw new Error('Recovered save failed');`)
		})
	}
}
