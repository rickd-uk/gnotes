package main

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"gnotes/internal/db"
)

func TestEditorFooterAndWidthBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	for _, width := range []int{320, 560, 600, 1023, 1024, 1920, 2560} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			recoveryFixture(t)
			mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at) VALUES(1,'Editor actions','Original text',?)", time.Now().UTC())
			mux := http.NewServeMux()
			registerDictionaryRoutes(mux)
			mux.HandleFunc("/api/auth/config", authConfigHandler)
			mux.HandleFunc("/api/auth/login", loginHandler)
			mux.HandleFunc("/api/auth/me", protect(meHandler, false))
			mux.HandleFunc("/api/draft", protect(getDraftHandler, false))
			mux.HandleFunc("/api/notes/page", protect(pagedListNotesHandler, false))
			mux.HandleFunc("/api/notes/trash-page", protect(pagedTrashNotesHandler, false))
			mux.HandleFunc("/api/notes/update", protect(updateNoteHandler, true))
			mux.HandleFunc("/api/notes/preview", protect(previewNoteHandler, true))
			mux.HandleFunc("/api/notes/pin", protect(pinNoteHandler, true))
			mux.HandleFunc("/api/notes/color", protect(updateNoteColorHandler, true))
			mux.HandleFunc("/api/notes/favorite", protect(favoriteNoteHandler, true))
			mux.HandleFunc("/api/notes/tags", protect(updateNoteTagsHandler, true))
			mux.HandleFunc("/api/notes/archive", protect(noteArchiveHandler, false))
			mux.HandleFunc("/api/notes/lock", protect(lockNoteHandler, true))
			mux.HandleFunc("/api/tags", protect(tagsHandler, false))
			mux.Handle("/", http.FileServer(http.Dir("../../public")))
			server := httptest.NewServer(securityHeaders(mux))
			defer server.Close()
			browser := newRecoveryBrowser(t)
			browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setDeviceMetricsOverride", "params": map[string]any{"width": width, "height": 900, "deviceScaleFactor": 1, "mobile": false}})
			browser.navigate(server.URL)
			browser.wait(`!document.getElementById('auth-screen').hidden`)
			browser.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
			browser.wait(`!notesLoading && loadedNotes.size===1 && spellingReady`)
			browser.script(`window.testID=[...loadedNotes.keys()][0];window.checkBounds=()=>{const shell=document.getElementById('app-shell').getBoundingClientRect();if(shell.left<0||shell.right>innerWidth||document.documentElement.scrollWidth>innerWidth)throw new Error('Page overflows');};const slider=document.getElementById('note-area-width');slider.value=slider.max;slider.dispatchEvent(new Event('input',{bubbles:true}));checkBounds();if(Number(slider.max)>innerWidth)throw new Error('Width maximum exceeds screen');if(innerWidth>560&&Math.abs(document.getElementById('app-shell').getBoundingClientRect().width-Number(slider.max))>1)throw new Error('Maximum width not applied');if(innerWidth>560){slider.value=slider.min;slider.dispatchEvent(new Event('input',{bubbles:true}));if(Math.abs(document.getElementById('app-shell').getBoundingClientRect().width-Number(slider.min))>1)throw new Error('Narrow width not applied');checkBounds();slider.value=slider.max;slider.dispatchEvent(new Event('input',{bubbles:true}));}openNoteEditor(testID,false);`)
			browser.wait(`Boolean(richEdit)`)
			browser.script(`window.checkEditorTools=()=>{const card=document.querySelector('.is-editing'),toggle=card.querySelector('.note-actions-toggle'),tools=card.querySelector('.note-tools'),wide=innerWidth>=1024;if(Boolean(toggle.getClientRects().length)===wide)throw new Error('Wrong editor More button visibility');if(wide){if(!tools.getClientRects().length)throw new Error('Wide editor tools are hidden');for(const button of tools.querySelectorAll('button')){if(button.closest('[hidden]'))continue;const r=button.getBoundingClientRect(),c=card.getBoundingClientRect();if(!r.width||r.left<c.left||r.right>c.right||r.top<c.top||r.bottom>c.bottom)throw new Error('Wide editor action hidden or outside editor: '+button.outerHTML);}}else if(tools.getClientRects().length)throw new Error('Small editor menu should start closed');};checkEditorTools();if(innerWidth>=1024){applyNoteEditorWidth(480);checkEditorTools();applyNoteEditorWidth(1040);checkEditorTools();}`)
			browser.script(`updateNote(testID);`)
			browser.wait(`editingNoteId===null && !pendingNoteEdits.size && !notesLoading`)
			browser.script(`document.querySelector('[data-width-target="reading"][data-width-preset="compact"]').click();if(noteAreaWidth!==640||noteAreaFullWidth)throw new Error('Compact reading preset failed');document.querySelector('[data-width-target="editing"][data-width-preset="comfortable"]').click();if(noteEditorWidth!==1040||noteAreaWidth!==640)throw new Error('Widths are not independent');document.querySelector('[data-width-target="reading"][data-width-preset="full"]').click();checkBounds();if(!noteAreaFullWidth)throw new Error('Full width preset failed');document.getElementById('reset-note-widths').click();if(noteAreaWidth!==880||noteEditorWidth!==1040||noteAreaFullWidth||noteEditorFullWidth)throw new Error('Width reset failed');document.querySelector('[data-width-target="reading"][data-width-preset="full"]').click();document.getElementById('view-toggle').click();document.querySelector('.note-width-controls').scrollIntoView({block:'center'});const controls=document.getElementById('view-menu');if(controls.scrollWidth>controls.clientWidth)throw new Error('Width presets overflow Controls');`)
			if width == 320 || width == 1920 {
				if width == 320 {
					browser.wait(`document.getElementById('view-menu').getBoundingClientRect().left>=-1 || editingNoteId!==null`)
				}
				encoded := browser.call("GET", "/screenshot", nil).(string)
				data, err := base64.StdEncoding.DecodeString(encoded)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(fmt.Sprintf("/tmp/gnotes-width-controls-%d.png", width), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			browser.script(`closeViewMenu();openNoteEditor(testID,false);`)
			browser.wait(`Boolean(richEdit)`)
			browser.script(`if(innerWidth>480){applyNoteEditorWidth(480);const r=document.querySelector('.is-editing').getBoundingClientRect();if(Math.abs(r.width-480)>1||r.left<0||r.right>innerWidth)throw new Error('Editing width ignored');applyNoteEditorWidth(1040);saveUIState();}applyQuickActions(['pin','color','archive']);richEdit.editor.commands.setContent('<p>Latest unsaved editor text</p>');const card=document.querySelector('.is-editing'),actions=card.querySelector('.note-actions'),footer=card.querySelector('.editor-footer'),a=actions.getBoundingClientRect(),controls=card.querySelector('.editor-actions').getBoundingClientRect(),r=card.getBoundingClientRect();if(!footer.contains(actions)||!a.width||r.left<0||r.right>innerWidth||r.bottom>innerHeight)throw new Error('Editor tools unavailable or outside viewport');if(controls.top<a.bottom-1?controls.left-a.right<24:controls.top-a.bottom<14)throw new Error('Missing gap between note and editor controls');checkBounds();card.querySelector('.pin-btn').click();`)
			browser.wait(`!pendingNoteActions.size && loadedNotes.get(testID).pinned && document.querySelector('.is-editing .pin-btn.is-pinned')`)
			browser.script(`if(editingNoteId!==testID||!richEdit.getMarkdown().includes('Latest unsaved'))throw new Error('Pin lost editor text');document.querySelector('.is-editing .color-btn').click();const menu=document.querySelector('.is-editing .note-color-menu');if(menu.hidden)throw new Error('Color menu unavailable');const r=menu.getBoundingClientRect();if(r.left<0||r.right>innerWidth||r.top<0)throw new Error('Color menu overflows');menu.querySelector('[data-color="sage"]').click();`)
			browser.wait(`!pendingNoteActions.size && loadedNotes.get(testID).background_color==='sage'`)
			// Favorites and tag dialogs retain the same live editor, even when
			// favoriting would normally remove the note from the recent list.
			browser.script(`window.sameEditor=richEdit;toggleNoteActions(testID);document.querySelector('.is-editing .favorite-btn').click();`)
			browser.wait(`!pendingNoteActions.size && loadedNotes.get(testID)?.favorited`)
			browser.script(`if(editingNoteId!==testID||richEdit!==sameEditor||document.querySelector('.is-editing .favorite-btn').getAttribute('aria-pressed')!=='true')throw new Error('Favorites closed or replaced editor');document.querySelector('.is-editing .favorite-btn').click();`)
			browser.wait(`!pendingNoteActions.size && !loadedNotes.get(testID)?.favorited`)
			browser.script(`richEdit.editor.commands.setContent('<p>Unsaved text while tagging</p>');clearTimeout(editSaveTimer);document.querySelector('.is-editing [aria-label="Edit note tags"]').click();`)
			browser.wait(`document.getElementById('tags-dialog').open`)
			browser.script(`document.getElementById('note-tags-input').dispatchEvent(new PointerEvent('pointerdown',{bubbles:true,pointerType:'mouse'}));if(editingNoteId!==testID)throw new Error('Tag dialog pointer closed editor');document.getElementById('note-tags-input').value='editor-test';document.getElementById('tags-form').requestSubmit();`)
			browser.wait(`!document.getElementById('tags-dialog').open && loadedNotes.get(testID)?.tags.includes('editor-test')`)
			browser.script(`if(editingNoteId!==testID||richEdit!==sameEditor||!richEdit.getMarkdown().includes('Unsaved text while tagging'))throw new Error('Tagging lost live edits');richEdit.editor.commands.setContent('<p>Latest unsaved editor text</p>');`)
			browser.script(`document.querySelector('.is-editing .note-actions-toggle').click();const tools=document.querySelector('.is-editing .note-tools').getBoundingClientRect();if(!tools.width||tools.left<0||tools.right>innerWidth||tools.top<0)throw new Error('More menu unavailable or overflows');document.querySelector('.is-editing .note-actions-toggle').click();document.querySelector('.edit-preview-toggle').click();`)
			browser.wait(`document.querySelector('.note-editor-preview:not([hidden])')?.textContent.includes('Latest unsaved')`)
			browser.script(`document.querySelector('.edit-preview-toggle').click();`)
			if width == 320 || width == 1920 {
				encoded := browser.call("GET", "/screenshot", nil).(string)
				data, err := base64.StdEncoding.DecodeString(encoded)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(fmt.Sprintf("/tmp/gnotes-editor-footer-%d.png", width), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			// Older saves must not report newer text as Saved. Network failures
			// expose Retry and recover without closing or replacing the editor.
			browser.script(`window.realStatusFetch=fetch.bind(window);window.saveRequests=[];window.fetch=(url,options={})=>String(url).startsWith('/api/notes/update')?new Promise(resolve=>saveRequests.push(()=>realStatusFetch(url,options).then(resolve))):realStatusFetch(url,options);richEdit.editor.commands.setContent('<p>First pending save</p>');clearTimeout(editSaveTimer);window.firstSaveDone=false;queueEditAutosave(testID).then(()=>firstSaveDone=true);`)
			browser.wait(`saveRequests.length===1`)
			browser.script(`richEdit.editor.commands.setContent('<p>Newer pending save</p>');clearTimeout(editSaveTimer);saveRequests[0]();`)
			browser.wait(`firstSaveDone`)
			browser.script(`if(document.querySelector('.is-editing .edit-hint').dataset.state!=='saving')throw new Error('Old save marked newer text Saved');queueEditAutosave(testID);`)
			browser.wait(`saveRequests.length===2`)
			browser.script(`saveRequests[1]();`)
			browser.wait(`document.querySelector('.is-editing .edit-hint').dataset.state==='saved'`)
			browser.script(`window.fetch=(url,options={})=>String(url).startsWith('/api/notes/update')?Promise.reject(new Error('Offline test')):realStatusFetch(url,options);richEdit.editor.commands.setContent('<p>Retry retains this text</p>');clearTimeout(editSaveTimer);queueEditAutosave(testID);`)
			browser.wait(`document.querySelector('.is-editing .edit-hint').dataset.state==='error' && !document.querySelector('.is-editing .edit-retry').hidden`)
			browser.script(`if(!richEdit.getMarkdown().includes('Retry retains this text'))throw new Error('Failed autosave lost text');window.fetch=realStatusFetch;document.querySelector('.is-editing .edit-retry').click();`)
			browser.wait(`document.querySelector('.is-editing .edit-hint').dataset.state==='saved' && document.querySelector('.is-editing .edit-retry').hidden`)
			browser.script(`toggleNoteActions(testID);document.querySelector('.is-editing .customize-note-buttons').click();`)
			browser.wait(`editingNoteId===null && !pendingNoteEdits.size && document.getElementById('view-control').classList.contains('is-open') && document.activeElement.matches('#tool-visibility .tool-choice')`)
			browser.script(`if(!document.activeElement.matches('#tool-visibility .tool-choice'))throw new Error('Customize did not focus quick actions: '+document.activeElement.tagName+'#'+document.activeElement.id+' '+document.activeElement.className+' visibility='+getComputedStyle(document.querySelector('#tool-visibility .tool-choice')).visibility);closeViewMenu();openNoteEditor(testID,false);`)
			browser.wait(`Boolean(richEdit)`)
			// A failed save must prevent an archive action and retain the editor text.
			browser.script(`window.realFetch=fetch.bind(window);window.archiveWrites=0;window.fetch=(url,options={})=>{if(String(url).startsWith('/api/notes/update'))return Promise.resolve(new Response('',{status:503}));if(String(url).startsWith('/api/notes/archive'))archiveWrites++;return realFetch(url,options);};richEdit.editor.commands.setContent('<p>Keep text after failed save</p>');document.querySelector('.is-editing .archive-btn').click();`)
			browser.wait(`!document.getElementById('pending-save-notice').hidden && editingNoteId===null`)
			browser.script(`openNoteEditor(testID,false);if(archiveWrites||!richEdit.getMarkdown().includes('Keep text after failed save'))throw new Error('Action discarded failed edits');window.fetch=realFetch;document.querySelector('.is-editing .archive-btn').click();`)
			browser.wait(`!pendingNoteActions.size && !pendingNoteEdits.size && !notesLoading && loadedNotes.size===0 && editingNoteId===null`)
			var content string
			if err := db.DB.QueryRow("SELECT content FROM notes WHERE archived_at IS NOT NULL").Scan(&content); err != nil || content != "Keep text after failed save" {
				t.Fatalf("archive content = %q, error = %v", content, err)
			}
			browser.navigate(server.URL)
			browser.wait(`!notesLoading && !document.getElementById('app-shell').hidden`)
			browser.script(`const saved=JSON.parse(localStorage.getItem(uiStateStorageKey()));if(noteAreaWidth!==saved.noteAreaWidth)throw new Error('Width preference not restored');`)
			browser.script(`window.preResizeWidth=noteAreaWidth;`)
			browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setDeviceMetricsOverride", "params": map[string]any{"width": 320, "height": 700, "deviceScaleFactor": 1, "mobile": false}})
			browser.wait(`document.getElementById('note-area-width').max==='288'`)
			browser.script(`if(noteAreaWidth!==preResizeWidth||document.documentElement.scrollWidth>innerWidth)throw new Error('Resize lost width preference or overflowed');`)
		})
	}
}
