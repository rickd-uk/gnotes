package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"gnotes/internal/db"
)

func TestSaveLifecycleBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	recoveryFixture(t)
	mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at) VALUES(1,'Lifecycle','Original text',?)", time.Now().UTC())
	mux := http.NewServeMux()
	registerDictionaryRoutes(mux)
	mux.HandleFunc("/api/auth/config", authConfigHandler)
	mux.HandleFunc("/api/auth/login", loginHandler)
	mux.HandleFunc("/api/auth/me", protect(meHandler, false))
	mux.HandleFunc("/api/draft", protect(getDraftHandler, false))
	mux.HandleFunc("/api/draft/update", protect(updateDraftHandler, true))
	mux.HandleFunc("/api/draft/finalize", protect(finalizeDraftHandler, true))
	mux.HandleFunc("/api/notes/page", protect(pagedListNotesHandler, false))
	mux.HandleFunc("/api/notes/update", protect(updateNoteHandler, true))
	mux.HandleFunc("/api/tags", protect(tagsHandler, false))
	mux.Handle("/", http.FileServer(http.Dir("../../public")))
	server := httptest.NewServer(securityHeaders(mux))
	defer server.Close()
	browser := newRecoveryBrowser(t)
	browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setDeviceMetricsOverride", "params": map[string]any{"width": 320, "height": 800, "deviceScaleFactor": 1, "mobile": false}})
	browser.navigate(server.URL)
	browser.wait(`!document.getElementById('auth-screen').hidden`)
	browser.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
	browser.wait(`!notesLoading && loadedNotes.size===1 && spellingReady`)

	// Hold saves across visibility/page-exit events. The latest payloads must
	// wait for acknowledgements, use their revisions and reach the real DB.
	browser.script(`
      window.realSaveFetch=fetch.bind(window);window.heldSaves=[];
      window.isTextSave=url=>String(url).startsWith('/api/notes/update')||url==='/api/draft/update';
      window.fetch=(url,options={})=>isTextSave(url)?new Promise((resolve,reject)=>heldSaves.push({url,options,body:JSON.parse(options.body),release:()=>realSaveFetch(url,options).then(resolve,reject)})):realSaveFetch(url,options);
      openNoteEditor(1,false);richEdit.editor.commands.setContent('<p>Earlier note</p>');queueEditAutosave(1);
      document.getElementById('title').value='Earlier draft';scheduleDraftSave();queueDraftSave();
    `)
	browser.wait(`heldSaves.length===2`)
	browser.script(`
      richEdit.editor.commands.setContent('<p>Latest note at exit</p>');clearTimeout(editSaveTimer);
      document.getElementById('title').value='Latest draft at exit';scheduleDraftSave();clearTimeout(draftSaveTimer);
      Object.defineProperty(document,'visibilityState',{configurable:true,value:'hidden'});
      document.dispatchEvent(new Event('visibilitychange'));
      window.dispatchEvent(new PageTransitionEvent('pagehide'));
      window.dispatchEvent(new PageTransitionEvent('pageshow',{persisted:true}));
    `)
	browser.script(`if(heldSaves.length!==2)throw new Error('Exit bypassed outstanding autosaves');heldSaves[0].release();heldSaves[1].release();`)
	browser.wait(`heldSaves.length===4`)
	browser.script(`if(heldSaves.slice(2).some(save=>save.body.revision!==1||!save.options.keepalive))throw new Error('Exit used stale revision or lost keepalive');heldSaves[2].release();heldSaves[3].release();delete document.visibilityState;`)
	browser.wait(`draftSavedVersion===draftVersion && document.querySelector('.edit-hint').dataset.state==='saved' && saveKeepaliveBytes===0`)
	var saved string
	if err := db.DB.QueryRow("SELECT content FROM notes WHERE id=1").Scan(&saved); err != nil || saved != "Latest note at exit" {
		t.Fatalf("note after exit = %q, %v", saved, err)
	}
	if err := db.DB.QueryRow("SELECT title FROM drafts WHERE user_id=1").Scan(&saved); err != nil || saved != "Latest draft at exit" {
		t.Fatalf("draft after exit = %q, %v", saved, err)
	}
	// Returning to the same text does not make an old acknowledgement cover
	// the newer close: an intervening different write is still queued.
	browser.script(`heldSaves=[];richEdit.editor.commands.setContent('<p>Return to this text</p>');queueEditAutosave(1);`)
	browser.wait(`heldSaves.length===1`)
	browser.script(`richEdit.editor.commands.setContent('<p>Intermediate text</p>');queueEditAutosave(1);richEdit.editor.commands.setContent('<p>Return to this text</p>');updateNote(1);heldSaves[0].release();`)
	browser.wait(`heldSaves.length===2`)
	browser.script(`if(!pendingNoteEdits.has(1)||window.dispatchEvent(new Event('beforeunload',{cancelable:true})))throw new Error('Old acknowledgement cleared a newer close');heldSaves[1].release();`)
	browser.wait(`heldSaves.length===3`)
	browser.script(`heldSaves[2].release();`)
	browser.wait(`pendingNoteEdits.size===0 && !notesLoading && loadedNotes.get(1).content==='Return to this text'`)
	browser.script(`openNoteEditor(1,false);`)

	// Closing already-saved text must work even without a connection.
	browser.script(`window.saveRequests=[];window.failSaves=true;window.fetch=(url,options={})=>{if(isTextSave(url)){saveRequests.push({url,options});if(failSaves)return Promise.reject(new TypeError('Offline'));}return realSaveFetch(url,options);};updateNote(1);`)
	browser.wait(`editingNoteId===null && pendingNoteEdits.size===0 && !notesLoading`)
	browser.script(`if(saveRequests.length||document.getElementById('note-action-dialog').open)throw new Error('Unchanged close tried saving');openNoteEditor(1,false);richEdit.editor.commands.setContent('<p>Retained offline</p>');updateNote(1);`)
	browser.wait(`pendingNoteEdits.get(1)?.failed && !pendingNoteEdits.get(1).saving`)
	browser.script(`
      if(editingNoteId!==null||document.getElementById('note-action-dialog').open)throw new Error('Failed close interrupted the user');
      const notice=document.getElementById('pending-save-notice'),bounds=notice.getBoundingClientRect(),button=notice.querySelector('button').getBoundingClientRect();
      if(notice.hidden||bounds.width<1||bounds.left<0||bounds.right>innerWidth||button.height<44)throw new Error('Retry notice does not fit a phone');
      const leave=new Event('beforeunload',{cancelable:true});if(window.dispatchEvent(leave))throw new Error('Unconfirmed edits lack leave warning');
    `)
	browser.wait(`pendingNoteEdits.get(1)?.failed && !pendingNoteEdits.get(1).saving`)
	// Edit again before reconnecting. Retrying must not replay the retained
	// snapshot from the earlier close after the new text has been saved.
	browser.script(`openNoteEditor(1,false);if(!richEdit.getMarkdown().includes('Retained offline'))throw new Error('Reopen lost edits');richEdit.editor.commands.setContent('<p>Newer recovered edit</p>');queueEditAutosave(1);`)
	browser.wait(`document.querySelector('.edit-hint').dataset.state==='error'`)
	browser.script(`failSaves=false;window.dispatchEvent(new Event('online'));`)
	browser.wait(`pendingNoteEdits.size===0 && document.querySelector('.edit-hint').dataset.state==='saved'`)
	browser.script(`window.dispatchEvent(new PageTransitionEvent('pageshow',{persisted:true}));updateNote(1);`)
	browser.wait(`editingNoteId===null && !pendingNoteEdits.size && !notesLoading`)
	if err := db.DB.QueryRow("SELECT content FROM notes WHERE id=1").Scan(&saved); err != nil || saved != "Newer recovered edit" {
		t.Fatalf("reconnect replayed stale edits = %q, %v", saved, err)
	}
	browser.script(`if(!document.getElementById('pending-save-notice').hidden||!window.dispatchEvent(new Event('beforeunload',{cancelable:true})))throw new Error('Confirmed saves retain an unsaved warning');failSaves=true;openNoteEditor(1,false);richEdit.editor.commands.setContent('<p>Retry button edit</p>');updateNote(1);`)
	browser.wait(`pendingNoteEdits.get(1)?.failed && !pendingNoteEdits.get(1).saving`)
	browser.script(`failSaves=false;document.querySelector('#pending-save-notice button').click();`)
	browser.wait(`pendingNoteEdits.size===0 && !notesLoading && loadedNotes.get(1).content==='Retry button edit'`)

	// The 64 KiB Fetch limit is an aggregate byte budget. Test simultaneous
	// note/draft bodies and a single oversized UTF-8 body with actual writes.
	browser.script(`
      window.heldSaves=[];window.fetch=(url,options={})=>isTextSave(url)?new Promise((resolve,reject)=>heldSaves.push({url,options,release:()=>realSaveFetch(url,options).then(resolve,reject)})):realSaveFetch(url,options);
      applyEditorMode(true);openNoteEditor(1,false);
      document.getElementById('edit-content-1').value='a'.repeat(40000);queueEditAutosave(1);
      document.getElementById('content').value='b'.repeat(40000);scheduleDraftSave();queueDraftSave();
    `)
	browser.wait(`heldSaves.length===2`)
	browser.script(`if(heldSaves.filter(save=>save.options.keepalive).length!==1)throw new Error('Concurrent saves exceeded keepalive budget');heldSaves.forEach(save=>save.release());`)
	browser.wait(`draftSavedVersion===draftVersion && document.querySelector('.edit-hint').dataset.state==='saved' && saveKeepaliveBytes===0`)
	browser.script(`heldSaves=[];document.getElementById('edit-content-1').value='界'.repeat(30000);queueEditAutosave(1);`)
	browser.wait(`heldSaves.length===1`)
	browser.script(`if(heldSaves[0].options.keepalive)throw new Error('Oversized UTF-8 body used keepalive');heldSaves[0].release();`)
	browser.wait(`document.querySelector('.edit-hint').dataset.state==='saved' && saveKeepaliveBytes===0`)
	var length int
	if err := db.DB.QueryRow("SELECT length(content) FROM notes WHERE id=1").Scan(&length); err != nil || length != 30000 {
		t.Fatalf("large note was not saved: length %d, %v", length, err)
	}

	// Queued old-session saves must not run after signing in again. A blocked
	// old request must not prevent new-session saves from making progress.
	browser.script(`heldSaves=[];document.getElementById('edit-content-1').value='Old session in flight';queueEditAutosave(1);`)
	browser.wait(`heldSaves.length===1`)
	browser.script(`document.getElementById('edit-content-1').value='Old session queued';queueEditAutosave(1);showAuth();document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
	browser.wait(`currentUser?.username==='rick' && !notesLoading && loadedNotes.size===1`)
	browser.script(`openNoteEditor(1,false);document.getElementById('edit-content-1').value='New session edit';queueEditAutosave(1);`)
	browser.wait(`heldSaves.length===2`)
	browser.script(`heldSaves[1].release();`)
	browser.wait(`document.querySelector('.edit-hint').dataset.state==='saved'`)
	browser.script(`heldSaves[0].release();window.sessionQueueSettled=false;setTimeout(()=>sessionQueueSettled=true,100);`)
	browser.wait(`sessionQueueSettled && saveKeepaliveBytes===0`)
	browser.script(`if(heldSaves.length!==2||noteSaveStates.get(1).conflict||document.getElementById('note-action-dialog').open)throw new Error('Old session changed the new session');window.fetch=realSaveFetch;`)
	if err := db.DB.QueryRow("SELECT content FROM notes WHERE id=1").Scan(&saved); err != nil || saved != "New session edit" {
		t.Fatalf("old session overwrote new session = %q, %v", saved, err)
	}
}
