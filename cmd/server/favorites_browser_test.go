package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFavoritesLayoutBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	for _, width := range []int{320, 600, 1024} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			recoveryFixture(t)
			mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at,tags) VALUES(1,'Long note',?,?,?)", strings.Repeat("A long paragraph to scroll through.\n\n", 90), time.Now().UTC(), `["work","a-very-long-category-name-for-small-screens"]`)
			mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at,tags,favorited,archived_at) VALUES(1,'Saved archive','Archived important text',?,?,1,?)", time.Now().UTC(), `["reading"]`, time.Now().UTC())
			mux := http.NewServeMux()
			registerDictionaryRoutes(mux)
			mux.HandleFunc("/api/auth/config", authConfigHandler)
			mux.HandleFunc("/api/auth/login", loginHandler)
			mux.HandleFunc("/api/auth/me", protect(meHandler, false))
			mux.HandleFunc("/api/draft", protect(getDraftHandler, false))
			mux.HandleFunc("/api/notes/page", protect(pagedListNotesHandler, false))
			mux.HandleFunc("/api/notes/search-page", protect(pagedSearchNotesHandler, false))
			mux.HandleFunc("/api/notes/trash-page", protect(pagedTrashNotesHandler, false))
			mux.HandleFunc("/api/notes/favorite", protect(favoriteNoteHandler, true))
			mux.HandleFunc("/api/notes/update", protect(updateNoteHandler, true))
			mux.HandleFunc("/api/notes/tags", protect(updateNoteTagsHandler, true))
			mux.HandleFunc("/api/notes/archive", protect(noteArchiveHandler, false))
			mux.HandleFunc("/api/notes/unarchive", protect(unarchiveNoteHandler, true))
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
			browser.script(`window.longID=[...loadedNotes.keys()][0];const range=document.getElementById('note-max-height');range.value='240';range.dispatchEvent(new Event('input',{bubbles:true}));const body=document.querySelector('.note-content');if(body.clientHeight>241||body.scrollHeight<=body.clientHeight)throw new Error('Long note is not capped and scrollable');if(document.documentElement.scrollWidth>innerWidth)throw new Error('Page overflows');applyDensityMode(2);const iconSize=parseFloat(getComputedStyle(document.querySelector('.note-quick-actions .icon')).width);if(iconSize<17||iconSize>=20)throw new Error('Titles icons must be between the small and original sizes');if(innerWidth<=700){const title=document.querySelector('.note-view > h2').getBoundingClientRect(),tags=document.querySelector('.note-tags').getBoundingClientRect(),tag=document.querySelector('[data-tag=\"a-very-long-category-name-for-small-screens\"]');if(tags.top-title.bottom<8)throw new Error('Mobile title and tags overlap');if(tag.dataset.shortLabel!=='#a-very-lon…'||getComputedStyle(tag.querySelector('.note-tag-label')).display!=='none'||getComputedStyle(tag,'::after').content==='none')throw new Error('Mobile tag was not shortened');if(tag.dataset.tag!=='a-very-long-category-name-for-small-screens')throw new Error('Tag value was truncated');}else{const tag=document.querySelector('[data-tag=\"a-very-long-category-name-for-small-screens\"]');if(getComputedStyle(tag).whiteSpace==='nowrap')throw new Error('Desktop tag was shortened');}applyDensityMode(0);openNoteEditor(longID,false);`)
			browser.wait(`richEdit && document.querySelector('.is-editing .tiptap')`)
			// Real wheel input must scroll the editing surface, without moving the footer.
			browser.script(`window.scrollElement=document.querySelector('.is-editing .tiptap');scrollElement.scrollTop=0;scrollElement.focus();scrollElement.scrollTop=0;window.scrollBounds=scrollElement.getBoundingClientRect();`)
			coords := browser.script(`return {x:Math.round(scrollBounds.left+scrollBounds.width/2),y:Math.round(scrollBounds.top+scrollBounds.height/2)}`).(map[string]any)
			browser.call("POST", "/actions", map[string]any{"actions": []any{map[string]any{"type": "wheel", "id": "scroll-editor", "actions": []any{map[string]any{"type": "scroll", "x": coords["x"], "y": coords["y"], "deltaX": 0, "deltaY": 600, "duration": 200}}}}})
			browser.wait(`scrollElement.scrollTop>100`)
			if width == 320 {
				browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setTouchEmulationEnabled", "params": map[string]any{"enabled": true}})
				browser.script(`scrollElement.scrollTop=0;`)
				x, y := coords["x"].(float64), coords["y"].(float64)
				browser.call("POST", "/actions", map[string]any{"actions": []any{map[string]any{"type": "pointer", "id": "touch-scroll", "parameters": map[string]any{"pointerType": "touch"}, "actions": []any{
					map[string]any{"type": "pointerMove", "x": x, "y": y + 80, "duration": 0}, map[string]any{"type": "pointerDown", "button": 0},
					map[string]any{"type": "pointerMove", "x": x, "y": y - 100, "duration": 250}, map[string]any{"type": "pointerUp", "button": 0},
				}}}})
				browser.wait(`scrollElement.scrollTop>30`)
			}

			browser.script(`updateNote(longID);`)
			browser.wait(`editingNoteId===null && pendingNoteEdits.size===0 && !notesLoading`)
			browser.script(`applyEditorMode(true);openNoteEditor(longID,false);window.scrollElement=document.getElementById('edit-content-'+longID);scrollElement.scrollTop=0;window.scrollBounds=scrollElement.getBoundingClientRect();if(scrollElement.scrollHeight<=scrollElement.clientHeight)throw new Error('Source editor cannot scroll');`)
			coords = browser.script(`return {x:Math.round(scrollBounds.left+scrollBounds.width/2),y:Math.round(scrollBounds.top+scrollBounds.height/2)}`).(map[string]any)
			browser.call("POST", "/actions", map[string]any{"actions": []any{map[string]any{"type": "wheel", "id": "scroll-source", "actions": []any{map[string]any{"type": "scroll", "x": coords["x"], "y": coords["y"], "deltaX": 0, "deltaY": 600, "duration": 200}}}}})
			browser.wait(`scrollElement.scrollTop>100`)
			browser.script(`updateNote(longID);applyEditorMode(false);`)
			browser.wait(`pendingNoteEdits.size===0 && !notesLoading`)
			browser.script(`toggleFavorite(longID);if(!loadedNotes.get(longID).favorited)throw new Error('Favorite waits for server');`)
			browser.wait(`pendingNoteActions.size===0 && !notesLoading && loadedNotes.get(longID).favorited`)
			browser.script(`document.getElementById('favorites-quick-open').click();`)
			browser.wait(`favoritesMode && !notesLoading && loadedNotes.size===2 && document.getElementById('favorites-category').options.length>=3`)
			browser.script(`if(document.documentElement.scrollWidth>innerWidth)throw new Error('Favorites overflows');const archived=[...loadedNotes.values()].find(n=>n.archived_at);window.archivedID=archived.id;const card=document.getElementById('note-'+archivedID);setNoteActionsOpen(card,true);if(getComputedStyle(card.querySelector('[aria-label=\"Edit note tags\"]')).display==='none')throw new Error('Archived categories unavailable');setNoteActionsOpen(card,false);openNoteEditor(archivedID,false);if(editingNoteId!==null)throw new Error('Archived favorite was editable');const select=document.getElementById('favorites-category');select.value='reading';select.dispatchEvent(new Event('change',{bubbles:true}));`)
			browser.wait(`!notesLoading && loadedNotes.size===1 && loadedNotes.has(archivedID)`)
			browser.script(`const select=document.getElementById('favorites-category');select.value='';select.dispatchEvent(new Event('change',{bubbles:true}));`)
			browser.wait(`!notesLoading && loadedNotes.size===2`)
			browser.script(`document.getElementById('favorites-query').value='Archived';document.getElementById('favorites-query').dispatchEvent(new Event('input',{bubbles:true}));`)
			browser.wait(`!notesLoading && loadedNotes.size===1 && loadedNotes.has(archivedID)`)
			browser.script(`window.originalFetch=fetch.bind(window);window.fetch=(url,options={})=>typeof url==='string'&&url.startsWith('/api/notes/favorite')?new Promise(resolve=>window.failFavorite=()=>resolve(new Response('',{status:503}))):originalFetch(url,options);toggleFavorite(archivedID);if(loadedNotes.has(archivedID))throw new Error('Remove favorite waits for server');`)
			browser.wait(`typeof failFavorite==='function'`)
			browser.script(`failFavorite();`)
			browser.wait(`pendingNoteActions.size===0 && !notesLoading && loadedNotes.has(archivedID) && document.getElementById('note-action-dialog').open`)
			browser.script(`document.getElementById('note-action-dialog').close();window.fetch=originalFetch;toggleFavorite(archivedID);`)
			browser.wait(`pendingNoteActions.size===0 && !notesLoading && loadedNotes.size===0`)
			browser.navigate(server.URL)
			browser.wait(`!notesLoading && loadedNotes.size===1`)
			browser.script(`if(document.getElementById('note-max-height').value!=='240')throw new Error('Note height preference did not persist');`)
		})
	}
}
