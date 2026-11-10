package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"gnotes/internal/db"
)

func TestCollectionNotesBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	widths := []int{320, 1024}
	if os.Getenv("GNOTES_COLLECTION_BROWSER") == "firefox" {
		widths = []int{560, 1024}
	}
	for _, width := range widths {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			recoveryFixture(t)
			mustRecoveryExec(t, `INSERT INTO feature_preferences(user_id,organization,writing_tools,note_controls,appearance,advanced_editing,offline_reading) VALUES(2,1,0,0,0,0,0)`)
			stamp := time.Now().UTC().Add(-24 * time.Hour).Truncate(24 * time.Hour).Add(12 * time.Hour)
			for i := 1; i <= 4; i++ {
				mustRecoveryExec(t, `INSERT INTO notes(user_id,title,content,created_at,archived_at,favorited,locked) VALUES(2,?,?,?, ?,?,?)`,
					fmt.Sprintf("Archive %d", i), strings.Repeat("Original text long enough to wrap on phones.\n\n", 60)+"- [ ] Task", stamp, stamp, i >= 3, i == 4)
			}
			mustRecoveryExec(t, `INSERT INTO notes(user_id,title,content,created_at,favorited) VALUES(2,'Active favorite','Active text',?,1)`, stamp)
			mux := http.NewServeMux()
			registerDictionaryRoutes(mux)
			mux.HandleFunc("/api/auth/config", authConfigHandler)
			mux.HandleFunc("/api/auth/login", loginHandler)
			mux.HandleFunc("/api/auth/me", protect(meHandler, false))
			mux.HandleFunc("/api/features", protect(featurePreferencesHandler, false))
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
			browser := newCollectionBrowser(t, width)
			browser.navigate(server.URL)
			browser.wait(`!document.getElementById('auth-screen').hidden`)
			browser.script(`document.getElementById('auth-username').value='other';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
			browser.wait(`!notesLoading && currentUser?.username==='other' && !document.getElementById('app-shell').hidden`)
			browser.script(`toggleArchiveMode();`)
			browser.wait(`archiveMode && !notesLoading && loadedNotes.size===2`)
			browser.script(`
              window.visible=el=>Boolean(el && el.getClientRects().length && getComputedStyle(el).visibility!=='hidden');
              window.checkCollection=()=>{
                const failures=[],cards=[...document.querySelectorAll('.note-card')],heading=document.querySelector('.date-section-toggle');
                if(heading.disabled||!visible(heading.querySelector('svg')))failures.push('day folding disabled with Organization enabled');
                if(!visible(cards[0].querySelector('.note-roll-toggle'))||!GnotesFeatures.actionAllowed('note:roll')||!GnotesFeatures.elementAllowed(cards[0].querySelector('.note-roll-toggle')))failures.push('note folding hidden or unavailable');
                if(parseFloat(getComputedStyle(cards[1]).marginTop)<8)failures.push('same-day cards have no gap');
                if(failures.length)throw new Error(failures.join('; '));
                heading.click();if(!document.querySelector('.date-group').classList.contains('is-collapsed')||cards.some(visible))throw new Error('Day did not collapse');
                renderLoadedNotes();if(!document.querySelector('.date-group.is-collapsed'))throw new Error('Render lost collapsed day');
                document.querySelector('.date-section-toggle').click();
                const roll=cards[0].querySelector('.note-roll-toggle');roll.click();if(!cards[0].classList.contains('is-rolled-up'))throw new Error('Note did not roll up');roll.click();
              };
              checkCollection();`)
			collectionClick(browser, "#note-1 .note-content")
			browser.wait(`editingNoteId===1 && Boolean(richEdit)`)
			collectionCheckEditor(browser, 1)
			browser.script(`richEdit.editor.commands.setContent('<p>Edited in Archive</p>');updateNote(1);`)
			browser.wait(`editingNoteId===null && !pendingNoteEdits.size && loadedNotes.get(1)?.content==='Edited in Archive'`)
			collectionClick(browser, "#note-1 .note-view > h2")
			browser.wait(`editingNoteId===1 && Boolean(richEdit)`)
			browser.script(`if(!richEdit.getMarkdown().includes('Edited in Archive'))throw new Error('Archive reopen lost text');updateNote(1);`)
			browser.wait(`editingNoteId===null && !pendingNoteEdits.size`)
			browser.script(`toggleFavoritesMode();`)
			browser.wait(`favoritesMode && !notesLoading && loadedNotes.size===3`)
			browser.script(`checkCollection();document.querySelector('#note-4 .note-content').click();if(editingNoteId!==null)throw new Error('Locked archived favorite opened');`)
			collectionClick(browser, "#note-3 .note-content")
			browser.wait(`editingNoteId===3 && Boolean(richEdit)`)
			collectionCheckEditor(browser, 3)
			browser.script(`richEdit.editor.commands.setContent('<p>Edited archived favorite</p>');updateNote(3);`)
			browser.wait(`editingNoteId===null && !pendingNoteEdits.size && loadedNotes.get(3)?.content==='Edited archived favorite'`)
			collectionClick(browser, "#note-5 .note-content")
			browser.wait(`editingNoteId===5 && Boolean(richEdit)`)
			browser.script(`richEdit.editor.commands.setContent('<p>Edited active favorite</p>');updateNote(5);`)
			browser.wait(`editingNoteId===null && !pendingNoteEdits.size`)
			browser.navigate(server.URL)
			browser.wait(`!notesLoading && currentUser?.username==='other' && !document.getElementById('app-shell').hidden`)
			browser.script(`toggleFavoritesMode();`)
			browser.wait(`favoritesMode && !notesLoading && loadedNotes.size===3`)
			browser.script(`if(loadedNotes.get(3).content!=='Edited archived favorite'||!loadedNotes.get(3).archived_at||!loadedNotes.get(3).favorited||loadedNotes.get(5).content!=='Edited active favorite')throw new Error('Reload lost edited favorite or metadata');if(document.documentElement.scrollWidth>innerWidth)throw new Error('Collection overflows');`)
			for _, id := range []int{1, 3} {
				var archived time.Time
				if err := db.DB.QueryRow("SELECT archived_at FROM notes WHERE id=?", id).Scan(&archived); err != nil || !archived.Equal(stamp) {
					t.Fatalf("note %d archive date changed: %v (%v)", id, archived, err)
				}
			}
		})
	}
}

// Collection backgrounds and optional appearance settings must not override
// the editor's opaque surface or its viewport and scrolling constraints.
func collectionCheckEditor(b *recoveryBrowser, id int) {
	b.t.Helper()
	b.script(fmt.Sprintf(`
      window.checkEditorSurface=()=>{
        const card=document.getElementById('note-%d'),style=getComputedStyle(card),rect=card.getBoundingClientRect();
        const alpha=style.backgroundColor.startsWith('rgba')?Number(style.backgroundColor.split(',').pop().replace(')','')):1;
        if(alpha!==1)throw new Error('Collection editor background is transparent: '+style.backgroundColor);
        if(style.maxHeight==='none'||rect.top < -1||rect.bottom > innerHeight+1)throw new Error('Collection editor exceeds viewport: '+JSON.stringify({top:rect.top,bottom:rect.bottom,height:innerHeight,maxHeight:style.maxHeight}));
        const footer=card.querySelector('.editor-footer').getBoundingClientRect();
        if(footer.top<rect.top||footer.bottom>rect.bottom+1)throw new Error('Collection editor actions outside dialog');
        const title=card.querySelector('.edit-title').getBoundingClientRect();
        if(title.top<rect.top||title.bottom>rect.bottom)throw new Error('Collection editor title outside dialog');
      };
      for(const theme of ['light','dark']){
        document.documentElement.dataset.theme=theme;
        for(const color of ['','note-color-sage']){
          const card=document.getElementById('note-%d');
          if(color)card.classList.add(color);
          checkEditorSurface();
          if(color)card.classList.remove(color);
        }
      }
      document.documentElement.dataset.theme='light';
      const rich=richEdit.editor.view.dom;
      if(rich.scrollHeight<=rich.clientHeight)throw new Error('Long rich editor is not scrollable');
      rich.scrollTop=100;if(rich.scrollTop<=0)throw new Error('Long rich editor cannot scroll');
      applyEditorMode(true);
      checkEditorSurface();
      const source=document.getElementById('edit-content-%d');
      if(source.scrollHeight<=source.clientHeight)throw new Error('Long source editor is not scrollable');
      source.scrollTop=100;if(source.scrollTop<=0)throw new Error('Long source editor cannot scroll');
      applyEditorMode(false);
      toggleNotePreview(%d);`, id, id, id, id))
	b.wait(fmt.Sprintf(`!document.getElementById('edit-preview-%d').hidden && document.getElementById('edit-preview-%d').querySelector('p')`, id, id))
	b.script(fmt.Sprintf(`
      for(const theme of ['light','dark']){
        document.documentElement.dataset.theme=theme;
        checkEditorSurface();
      }
      document.documentElement.dataset.theme='light';
      const preview=document.getElementById('edit-preview-%d');
      if(preview.scrollHeight<=preview.clientHeight)throw new Error('Long preview is not scrollable');
      preview.scrollTop=100;if(preview.scrollTop<=0)throw new Error('Long preview cannot scroll');
      setNotePreview(%d,false);`, id, id))
}

// Run Firefox coverage with geckodriver on port 19516 and
// GNOTES_COLLECTION_BROWSER=firefox. Ordinary browser checks keep ChromeDriver.
func newCollectionBrowser(t *testing.T, width int) *recoveryBrowser {
	t.Helper()
	if os.Getenv("GNOTES_COLLECTION_BROWSER") != "firefox" {
		b := newRecoveryBrowser(t)
		b.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setDeviceMetricsOverride", "params": map[string]any{"width": width, "height": 900, "deviceScaleFactor": 1, "mobile": false}})
		return b
	}
	b := &recoveryBrowser{t: t, client: &http.Client{Timeout: 30 * time.Second}, endpoint: "http://127.0.0.1:19516"}
	result := b.call("POST", "", map[string]any{"capabilities": map[string]any{"alwaysMatch": map[string]any{
		"browserName": "firefox", "moz:firefoxOptions": map[string]any{"args": []string{"-headless"}},
	}}})
	b.session = result.(map[string]any)["sessionId"].(string)
	t.Cleanup(func() { b.call("DELETE", "", nil) })
	b.call("POST", "/window/rect", map[string]int{"width": width, "height": 900})
	return b
}

func collectionClick(b *recoveryBrowser, selector string) {
	b.t.Helper()
	b.script(fmt.Sprintf(`document.querySelector(%q).scrollIntoView({block:'center',behavior:'instant'});`, selector))
	element := b.call("POST", "/element", map[string]string{"using": "css selector", "value": selector}).(map[string]any)
	id := element["element-6066-11e4-a52e-4f735466cecf"].(string)
	b.call("POST", "/element/"+id+"/click", nil)
}
