package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"gnotes/internal/db"
)

func TestOptimisticNoteBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	recoveryFixture(t)
	mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at) VALUES(1,'Latency test','Original text',?)", time.Now().UTC())
	mux := http.NewServeMux()
	registerDictionaryRoutes(mux)
	mux.HandleFunc("/api/auth/config", authConfigHandler)
	mux.HandleFunc("/api/auth/login", loginHandler)
	mux.HandleFunc("/api/auth/me", protect(meHandler, false))
	mux.HandleFunc("/api/draft", protect(getDraftHandler, false))
	mux.HandleFunc("/api/notes/page", protect(pagedListNotesHandler, false))
	mux.HandleFunc("/api/notes/trash-page", protect(pagedTrashNotesHandler, false))
	mux.HandleFunc("/api/notes/update", protect(updateNoteHandler, true))
	mux.HandleFunc("/api/notes/pin", protect(pinNoteHandler, true))
	mux.HandleFunc("/api/notes/color", protect(updateNoteColorHandler, true))
	mux.HandleFunc("/api/notes/archive", protect(noteArchiveHandler, false))
	mux.HandleFunc("/api/notes/unarchive", protect(unarchiveNoteHandler, true))
	mux.HandleFunc("/api/notes/delete", protect(deleteNoteHandler, true))
	mux.HandleFunc("/api/tags", protect(tagsHandler, false))
	mux.Handle("/", http.FileServer(http.Dir("../../public")))
	server := httptest.NewServer(securityHeaders(mux))
	defer server.Close()
	browser := newRecoveryBrowser(t)
	browser.navigate(server.URL)
	browser.wait(`!document.getElementById('auth-screen').hidden`)
	browser.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
	browser.wait(`!notesLoading && loadedNotes.size===1 && spellingReady`)
	// Hold writes indefinitely until the test releases them. An immediate UI
	// assertion therefore cannot accidentally pass because the server was fast.
	browser.script(`window.testNoteID=[...loadedNotes.keys()][0];window.originalFetch=fetch.bind(window);window.heldRequests=[];window.fetch=(url,options={})=>{if(typeof url==='string'&&url.startsWith('/api/notes/')&&options.method&&options.method!=='GET'){return new Promise((resolve,reject)=>heldRequests.push({release:()=>originalFetch(url,options).then(resolve,reject),fail:()=>resolve(new Response('',{status:503}))}));}return originalFetch(url,options);};openNoteEditor(testNoteID,false);richEdit.editor.commands.setContent('<p>First edit</p>');document.getElementById('note-edit-backdrop').dispatchEvent(new PointerEvent('pointerdown',{bubbles:true,pointerType:'mouse'}));if(editingNoteId!==null||!document.getElementById('note-edit-backdrop').hidden)throw new Error('Outside click waited for the server');if(!document.querySelector('.note-content').textContent.includes('First edit'))throw new Error('Edited text was not displayed immediately');`)
	browser.wait(`heldRequests.length===1`)
	browser.script(`openNoteEditor(testNoteID,false);if(!richEdit.getMarkdown().includes('First edit'))throw new Error('Reopen lost unsaved text');richEdit.editor.commands.setContent('<p>Newest edit</p>');updateNote(testNoteID);heldRequests[0].release();`)
	browser.wait(`heldRequests.length===2`)
	browser.script(`if(!loadedNotes.get(testNoteID).content.includes('Newest edit'))throw new Error('Old save overwrote newer edit');heldRequests[1].release();`)
	browser.wait(`pendingNoteEdits.size===0 && !notesLoading && loadedNotes.get(testNoteID).content.includes('Newest edit')`)
	var content string
	if err := db.DB.QueryRow("SELECT content FROM notes WHERE title='Latency test'").Scan(&content); err != nil || content != "Newest edit" {
		t.Fatalf("persisted content = %q, error = %v", content, err)
	}
	browser.script(`heldRequests=[];openNoteEditor(testNoteID,false);richEdit.editor.commands.setContent('<p>Keep failed edit</p>');updateNote(testNoteID);if(editingNoteId!==null)throw new Error('Done waited for the server');`)
	browser.wait(`heldRequests.length===1`)
	browser.script(`heldRequests[0].fail();`)
	browser.wait(`document.getElementById('note-action-dialog').open && editingNoteId===testNoteID`)
	browser.script(`if(!richEdit.getMarkdown().includes('Keep failed edit'))throw new Error('Failed save lost edits');document.getElementById('note-action-dialog').close();heldRequests=[];archiveAllActiveNotes();`)
	browser.wait(`document.getElementById('note-action-dialog').open && !activeRemovalBusy`)
	browser.script(`if(heldRequests.length||!loadedNotes.has(testNoteID))throw new Error('Bulk archive moved a note with unsaved edits');document.getElementById('note-action-dialog').close();updateNote(testNoteID);`)
	browser.wait(`heldRequests.length===1`)
	browser.script(`heldRequests[0].release();`)
	browser.wait(`pendingNoteEdits.size===0 && !notesLoading`)
	// Failed pin, color, archive and recycle writes must roll back the visible
	// change, rather than pretending that it was persisted.
	for _, action := range []struct{ name, run, immediate, restored string }{
		{"pin", `togglePin(testNoteID)`, `document.querySelector('.note-card').classList.contains('pinned-note')`, `!loadedNotes.get(testNoteID).pinned && pinnedNoteCount===0`},
		{"color", `setNoteColor(testNoteID,'sage')`, `document.querySelector('.note-card').classList.contains('note-color-sage')`, `!document.querySelector('.note-card').classList.contains('note-color-sage')`},
		{"archive", `archiveNote(testNoteID)`, `!document.getElementById('note-'+testNoteID) && activeNoteCount===0`, `Boolean(document.getElementById('note-'+testNoteID)) && activeNoteCount===1`},
		{"recycle", `deleteNote(testNoteID)`, `!document.getElementById('note-'+testNoteID) && activeNoteCount===0`, `Boolean(document.getElementById('note-'+testNoteID)) && activeNoteCount===1`},
	} {
		t.Run(action.name, func(t *testing.T) {
			browser.script(`heldRequests=[];` + action.run + `;if(!(` + action.immediate + `))throw new Error('Action waited for the server');`)
			browser.wait(`heldRequests.length===1`)
			browser.script(`heldRequests[0].fail();`)
			browser.wait(`document.getElementById('note-action-dialog').open && pendingNoteActions.size===0 && !notesLoading`)
			browser.script(`if(!(` + action.restored + `))throw new Error('Failed action did not roll back');document.getElementById('note-action-dialog').close();`)
		})
	}
	browser.script(`heldRequests=[];togglePin(testNoteID);`)
	browser.wait(`heldRequests.length===1`)
	browser.script(`heldRequests[0].release();`)
	browser.wait(`pendingNoteActions.size===0 && !notesLoading && loadedNotes.get(testNoteID).pinned`)
	browser.script(`heldRequests=[];setNoteColor(testNoteID,'sage');`)
	browser.wait(`heldRequests.length===1`)
	browser.script(`heldRequests[0].release();`)
	browser.wait(`pendingNoteActions.size===0 && !notesLoading && loadedNotes.get(testNoteID).background_color==='sage'`)
	browser.script(`heldRequests=[];archiveNote(testNoteID);`)
	browser.wait(`heldRequests.length===1 && activeNoteCount===0 && pinnedNoteCount===0`)
	browser.script(`heldRequests[0].release();`)
	browser.wait(`pendingNoteActions.size===0 && !notesLoading && loadedNotes.size===0`)
	browser.script(`toggleArchiveMode();`)
	browser.wait(`archiveMode && !notesLoading && loadedNotes.has(testNoteID)`)
	browser.script(`heldRequests=[];unarchiveNote(testNoteID);if(document.getElementById('note-'+testNoteID)||activeNoteCount!==1||pinnedNoteCount!==1)throw new Error('Unarchive waited for the server');`)
	browser.wait(`heldRequests.length===1`)
	browser.script(`heldRequests[0].fail();`)
	browser.wait(`document.getElementById('note-action-dialog').open && !notesLoading && pendingNoteActions.size===0`)
	browser.script(`if(!loadedNotes.has(testNoteID)||activeNoteCount!==0||pinnedNoteCount!==0)throw new Error('Unarchive did not roll back');document.getElementById('note-action-dialog').close();heldRequests=[];unarchiveNote(testNoteID);`)
	browser.wait(`heldRequests.length===1`)
	browser.script(`heldRequests[0].release();`)
	browser.wait(`pendingNoteActions.size===0 && !notesLoading && activeNoteCount===1`)
	browser.script(`toggleArchiveMode();`)
	browser.wait(`!archiveMode && !notesLoading && loadedNotes.has(testNoteID)`)
	browser.script(`heldRequests=[];deleteNote(testNoteID);`)
	browser.wait(`heldRequests.length===1 && activeNoteCount===0`)
	browser.script(`heldRequests[0].release();`)
	browser.wait(`pendingNoteActions.size===0 && !notesLoading && !trashLoading && trashedNoteCount===1`)
	var removed int
	if err := db.DB.QueryRow("SELECT count(*) FROM notes WHERE deleted_at IS NOT NULL AND pinned=1 AND background_color='sage' AND content='Keep failed edit'").Scan(&removed); err != nil || removed != 1 {
		t.Fatalf("confirmed saved and recycled notes = %d, error = %v", removed, err)
	}
}
