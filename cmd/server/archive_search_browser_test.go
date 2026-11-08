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

func TestArchivedSearchEditingBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	for _, width := range []int{320, 1024} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			recoveryFixture(t)
			stamp := time.Now().UTC().Add(-24 * time.Hour)
			mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at,archived_at) VALUES(1,'Archived editable','ArchiveNeedle original text',?,?)", stamp.Add(-time.Hour), stamp)
			mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at,archived_at,locked) VALUES(1,'Archived locked','ArchiveNeedle locked text',?,?,1)", stamp.Add(-time.Hour), stamp)
			var originalArchive string
			if err := db.DB.QueryRow("SELECT archived_at FROM notes WHERE id=1").Scan(&originalArchive); err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			registerDictionaryRoutes(mux)
			mux.HandleFunc("/api/auth/config", authConfigHandler)
			mux.HandleFunc("/api/auth/login", loginHandler)
			mux.HandleFunc("/api/auth/me", protect(meHandler, false))
			mux.HandleFunc("/api/draft", protect(getDraftHandler, false))
			mux.HandleFunc("/api/notes/page", protect(pagedListNotesHandler, false))
			mux.HandleFunc("/api/notes/search-page", protect(pagedSearchNotesHandler, false))
			mux.HandleFunc("/api/notes/trash-page", protect(pagedTrashNotesHandler, false))
			mux.HandleFunc("/api/notes/update", protect(updateNoteHandler, true))
			mux.HandleFunc("/api/notes/preview", protect(previewNoteHandler, true))
			mux.HandleFunc("/api/tags", protect(tagsHandler, false))
			mux.Handle("/", http.FileServer(http.Dir("../../public")))
			server := httptest.NewServer(securityHeaders(mux))
			defer server.Close()
			browser := newRecoveryBrowser(t)
			browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setDeviceMetricsOverride", "params": map[string]any{"width": width, "height": 900, "deviceScaleFactor": 1, "mobile": false}})
			browser.navigate(server.URL)
			browser.wait(`!document.getElementById('auth-screen').hidden`)
			browser.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
			browser.wait(`!notesLoading && !document.getElementById('app-shell').hidden`)
			browser.script(`document.getElementById('search-query').value='ArchiveNeedle';runTextSearch();`)
			browser.wait(`searchActive && !notesLoading && loadedNotes.size===2 && loadedNotes.has(1)`)
			browser.script(`document.querySelector('#note-2 .note-content').click();if(editingNoteId!==null)throw new Error('Locked archived search result opened');const body=document.querySelector('#note-1 .note-content'),range=document.createRange();range.selectNodeContents(body);getSelection().removeAllRanges();getSelection().addRange(range);body.click();if(editingNoteId!==null)throw new Error('Selecting archived search text opened editor');getSelection().removeAllRanges();body.click();`)
			browser.wait(`editingNoteId===1 && Boolean(richEdit)`)
			browser.script(`richEdit.editor.commands.setContent('<p>ArchiveNeedle revised text</p>');document.querySelector('.edit-preview-toggle').click();`)
			browser.wait(`document.querySelector('.note-editor-preview:not([hidden])')?.textContent.includes('ArchiveNeedle revised text')`)
			browser.script(`document.querySelector('.edit-preview-toggle').click();updateNote(1);`)
			browser.wait(`editingNoteId===null && !pendingNoteEdits.size && !notesLoading && loadedNotes.get(1)?.content.includes('ArchiveNeedle revised text')`)
			browser.script(`runTextSearch();`)
			browser.wait(`!notesLoading && loadedNotes.size===2 && loadedNotes.get(1)?.content.includes('ArchiveNeedle revised text')`)
			browser.script(`if(!loadedNotes.get(1).archived_at)throw new Error('Search edit unarchived note');document.querySelector('#note-1 .note-view > h2').click();`)
			browser.wait(`editingNoteId===1 && Boolean(richEdit)`)
			browser.script(`if(!richEdit.getMarkdown().includes('ArchiveNeedle revised text'))throw new Error('Reopened archived editor lost saved text');updateNote(1);`)
			browser.wait(`editingNoteId===null && !pendingNoteEdits.size`)
			var archived, content string
			if err := db.DB.QueryRow("SELECT archived_at,content FROM notes WHERE id=1").Scan(&archived, &content); err != nil || archived != originalArchive || content != "ArchiveNeedle revised text" {
				t.Fatalf("archived search save did not retain archive/content: %v", err)
			}
		})
	}
}
