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

func TestSaveConflictBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	for _, width := range []int{320, 1024} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			recoveryFixture(t)
			mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at) VALUES(1,'Original','Original text',?)", time.Now().UTC())
			mux := http.NewServeMux()
			registerDictionaryRoutes(mux)
			mux.HandleFunc("/api/auth/config", authConfigHandler)
			mux.HandleFunc("/api/auth/login", loginHandler)
			mux.HandleFunc("/api/auth/me", protect(meHandler, false))
			mux.HandleFunc("/api/draft", protect(getDraftHandler, false))
			mux.HandleFunc("/api/draft/update", protect(updateDraftHandler, true))
			mux.HandleFunc("/api/draft/finalize", protect(finalizeDraftHandler, true))
			mux.HandleFunc("/api/notes/page", protect(pagedListNotesHandler, false))
			mux.HandleFunc("/api/notes/get", protect(getNoteHandler, false))
			mux.HandleFunc("/api/notes/create", protect(createNoteHandler, true))
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
			browser.wait(`!notesLoading && loadedNotes.size===1 && spellingReady`)
			browser.script(`openNoteEditor(1,false);window.remoteSaved=false;fetch('/api/notes/update?id=1',{method:'PUT',headers:{'Content-Type':'application/json','X-CSRF-Token':csrfToken},body:JSON.stringify({title:'Saved elsewhere',content:'Other window text',revision:0})}).then(r=>{if(!r.ok)throw new Error('Remote write failed');window.remoteSaved=true;});`)
			browser.wait(`window.remoteSaved && Boolean(richEdit)`)
			browser.script(`richEdit.editor.commands.setContent('<p>My preserved text</p>');queueEditAutosave(1);`)
			browser.wait(`document.getElementById('note-action-dialog').open && !document.getElementById('note-conflict-copy').hidden`)
			browser.script(`if(!richEdit.getMarkdown().includes('My preserved text'))throw new Error('Conflict discarded text');const leave=new Event('beforeunload',{cancelable:true});if(window.dispatchEvent(leave))throw new Error('Unsaved conflict lacks leave warning');document.getElementById('note-conflict-copy').click();`)
			browser.wait(`editingNoteId===null && !notesLoading && loadedNotes.size===2 && !document.getElementById('note-action-dialog').open`)
			var content string
			if err := db.DB.QueryRow("SELECT content FROM notes WHERE id=1").Scan(&content); err != nil || content != "Other window text" {
				t.Fatalf("other window overwritten: %q %v", content, err)
			}
			if err := db.DB.QueryRow("SELECT content FROM notes WHERE id<>1").Scan(&content); err != nil || content != "My preserved text" {
				t.Fatalf("copy not saved: %q %v", content, err)
			}
			browser.script(`window.remoteDraftSaved=false;fetch('/api/draft/update',{method:'PUT',headers:{'Content-Type':'application/json','X-CSRF-Token':csrfToken},body:JSON.stringify({title:'Other draft',content:'Other draft text',version:100,revision:0})}).then(r=>{if(!r.ok)throw new Error('Remote draft failed');window.remoteDraftSaved=true;});`)
			browser.wait(`window.remoteDraftSaved`)
			browser.script(`document.getElementById('title').value='My draft copy';scheduleDraftSave();`)
			browser.wait(`draftConflict && !document.getElementById('draft-conflict-copy').hidden`)
			browser.script(`if(document.getElementById('title').value!=='My draft copy')throw new Error('Conflict discarded draft');const copy=document.getElementById('draft-conflict-copy'),bounds=copy.getBoundingClientRect();if(bounds.height<42||bounds.left<0||bounds.right>innerWidth||document.documentElement.scrollWidth>innerWidth)throw new Error('Conflict copy button is clipped or too small');copy.click();`)
			browser.wait(`!draftConflict && !notesLoading && loadedNotes.size===3 && document.getElementById('title').value==='Other draft'`)
			if err := db.DB.QueryRow("SELECT title FROM drafts WHERE user_id=1").Scan(&content); err != nil || content != "Other draft" {
				t.Fatalf("other draft overwritten: %q %v", content, err)
			}
			browser.script(`if(document.documentElement.scrollWidth>innerWidth)throw new Error('Conflict controls overflow');`)
			// Hold an acknowledged server write while the user types again. The
			// later queued save must use the acknowledged server revision even
			// though the response refers to an older local edit version.
			browser.script(`window.unheldFetch=fetch.bind(window);window.holdOneDraft=true;window.releaseDraftAck=null;window.fetch=(url,options={})=>url==='/api/draft/update'&&holdOneDraft?(holdOneDraft=false,unheldFetch(url,options).then(response=>new Promise(resolve=>{window.releaseDraftAck=()=>resolve(response);} ))):unheldFetch(url,options);document.getElementById('title').value='Typing before response';scheduleDraftSave();queueDraftSave();`)
			browser.wait(`typeof releaseDraftAck==='function'`)
			browser.script(`document.getElementById('title').value='Newest typing';scheduleDraftSave();queueDraftSave();releaseDraftAck();`)
			browser.wait(`!draftConflict && document.getElementById('draft-status').textContent==='Draft saved' && draftSavedVersion===draftVersion`)
			if err := db.DB.QueryRow("SELECT title FROM drafts WHERE user_id=1").Scan(&content); err != nil || content != "Newest typing" {
				t.Fatalf("typing during acknowledgement lost: %q %v", content, err)
			}
		})
	}
}
