package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestSimpleModeBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	for _, width := range []int{320, 1024} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			recoveryFixture(t)
			mustRecoveryExec(t, `INSERT INTO feature_preferences(user_id,organization,writing_tools,note_controls,appearance,advanced_editing,offline_reading) VALUES(2,0,0,0,0,0,0)`)
			mustRecoveryExec(t, `INSERT INTO notes(user_id,title,content,tags,created_at) VALUES(2,'Simple test','Keep my note','["work"]',?)`, time.Now().UTC())
			mux := http.NewServeMux()
			registerDictionaryRoutes(mux)
			mux.HandleFunc("/api/auth/config", authConfigHandler)
			mux.HandleFunc("/api/auth/login", loginHandler)
			mux.HandleFunc("/api/auth/me", protect(meHandler, false))
			mux.HandleFunc("/api/auth/logout", protect(logoutHandler, true))
			mux.HandleFunc("/api/features", protect(featurePreferencesHandler, false))
			mux.HandleFunc("/api/draft", protect(getDraftHandler, false))
			mux.HandleFunc("/api/draft/update", protect(updateDraftHandler, true))
			mux.HandleFunc("/api/notes/page", protect(pagedListNotesHandler, false))
			mux.HandleFunc("/api/notes/trash-page", protect(pagedTrashNotesHandler, false))
			mux.HandleFunc("/api/notes/update", protect(updateNoteHandler, true))
			mux.HandleFunc("/api/notes/preview", protect(previewNoteHandler, true))
			mux.HandleFunc("/api/tags", protect(tagsHandler, false))
			mux.Handle("/", http.FileServer(http.Dir("../../public")))
			server := httptest.NewServer(securityHeaders(mux))
			defer server.Close()
			browser := newRecoveryBrowser(t)
			browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setDeviceMetricsOverride", "params": map[string]any{"width": width, "height": 800, "deviceScaleFactor": 1, "mobile": false}})
			browser.navigate(server.URL)
			browser.wait(`!document.getElementById('auth-screen').hidden`)
			browser.script(`document.getElementById('auth-username').value='other';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
			browser.wait(`!document.getElementById('app-shell').hidden && loadedNotes.size===1 && !notesLoading && spellingReady`)
			browser.script(`window.visible=selector=>[...document.querySelectorAll(selector)].some(el=>el.getClientRects().length && getComputedStyle(el).visibility!=='hidden');
     if(GnotesFeatures.enabled('organization')||visible('#favorites-quick-open')||visible('.vim-toggle')||visible('.date-section-favorite'))throw new Error('Advanced controls leaked into Simple mode');
     if(!visible('.delete-btn')||!visible('#trash-toggle')||!visible('#account-control'))throw new Error('Simple essentials missing');
     if(GnotesFeatures.actionAllowed('note:archive')||GnotesFeatures.actionAllowed('control:archived-quick-open')||GnotesFeatures.actionAllowed('vim-mode'))throw new Error('Hidden shortcuts available');
     if(document.documentElement.scrollWidth>innerWidth)throw new Error('Page overflows');
     window.simpleNote=[...loadedNotes.keys()][0];openNoteEditor(simpleNote,true);`)
			browser.wait(`Boolean(richEdit)`)
			browser.script(`richEdit.editor.commands.setContent('<p>Edited in Simple mode</p>');updateNote(simpleNote);`)
			browser.wait(`editingNoteId===null && !pendingNoteEdits.size && loadedNotes.get(simpleNote).content.includes('Edited in Simple mode')`)
			browser.script(`GnotesFeatures.open();document.querySelector('[data-feature="organization"]').click();`)
			browser.wait(`GnotesFeatures.enabled('organization') && document.getElementById('features-status').textContent.includes('Saved')`)
			browser.script(`document.getElementById('features-close').click();toggleNoteActions(simpleNote);if(!visible('#favorites-quick-open')||!visible('[aria-label="Edit note tags"]')||!GnotesFeatures.actionAllowed('note:archive')||visible('.vim-toggle'))throw new Error('Individual feature choices inconsistent');localStorage.clear();`)
			browser.navigate(server.URL)
			browser.wait(`!document.getElementById('app-shell').hidden && !notesLoading && loadedNotes.size===1`)
			browser.script(`window.visible=selector=>[...document.querySelectorAll(selector)].some(el=>el.getClientRects().length && getComputedStyle(el).visibility!=='hidden');window.simpleNote=[...loadedNotes.keys()][0];if(!GnotesFeatures.enabled('organization')||GnotesFeatures.enabled('writing_tools')||!loadedNotes.get(simpleNote)?.tags.includes('work'))throw new Error('Preferences or note tags lost across reload');GnotesFeatures.open();document.getElementById('features-full').click();`)
			browser.wait(`GnotesFeatures.enabled('advanced_editing') && document.getElementById('features-status').textContent.includes('Saved')`)
			browser.script(`if(!visible('.vim-toggle')||!GnotesFeatures.actionAllowed('control:archived-quick-open'))throw new Error('Full mode incomplete');applyNewNoteArea(false);applyDensityMode(2);rolledNoteIds.add(simpleNote);renderLoadedNotes();document.getElementById('features-simple').click();`)
			browser.wait(`!GnotesFeatures.enabled('organization') && document.getElementById('features-status').textContent.includes('Saved')`)
			browser.script(`if(document.getElementById('note-form').hidden||document.getElementById('notes-list').classList.contains('density-titles')||document.querySelector('.note-card.is-rolled-up')||!rolledNoteIds.has(simpleNote)||densityIndex!==2)throw new Error('Simple mode hid writing/reading or discarded saved display choices');document.getElementById('features-close').click();GnotesFeatures.save({theme:'dark'});`)
			browser.wait(`document.documentElement.dataset.theme==='dark'`)
			browser.script(`if(!loadedNotes.get(simpleNote).tags.includes('work')||!loadedNotes.get(simpleNote).content.includes('Edited in Simple mode'))throw new Error('Switching modes changed data');document.getElementById('view-toggle').click();`)
			browser.wait(`document.getElementById('view-control').classList.contains('is-open') && visible('#note-body-size') && visible('#feature-theme')`)
			browser.script(`if(!visible('#note-body-size')||!visible('#feature-theme')||visible('#font-choices')||visible('#note-max-height'))throw new Error('Simple controls wrong '+JSON.stringify({text:visible('#note-body-size'),theme:visible('#feature-theme'),fonts:visible('#font-choices'),height:visible('#note-max-height'),open:document.getElementById('font-settings').open}));document.getElementById('view-toggle').click();document.getElementById('logout-button').click();`)
			browser.wait(`!document.getElementById('auth-screen').hidden`)
			browser.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
			browser.wait(`!document.getElementById('app-shell').hidden && currentUser?.username==='rick' && !notesLoading`)
			browser.script(`if(!GnotesFeatures.enabled('organization')||!GnotesFeatures.enabled('advanced_editing')||document.documentElement.dataset.theme!=='light')throw new Error('Account preferences leaked to admin');document.getElementById('account-control').open=true;if(!visible('#admin-open'))throw new Error('Administrator controls missing');`)
		})
	}
}
