package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gnotes/internal/db"
)

func vimBrowserMux() *http.ServeMux {
	mux := http.NewServeMux()
	registerDictionaryRoutes(mux)
	mux.HandleFunc("/api/auth/config", authConfigHandler)
	mux.HandleFunc("/api/auth/login", loginHandler)
	mux.HandleFunc("/api/auth/logout", logoutHandler)
	mux.HandleFunc("/api/auth/me", protect(meHandler, false))
	mux.HandleFunc("/api/draft", protect(getDraftHandler, false))
	mux.HandleFunc("/api/draft/update", protect(updateDraftHandler, true))
	mux.HandleFunc("/api/draft/finalize", protect(finalizeDraftHandler, true))
	mux.HandleFunc("/api/notes/page", protect(pagedListNotesHandler, false))
	mux.HandleFunc("/api/notes/trash-page", protect(pagedTrashNotesHandler, false))
	mux.HandleFunc("/api/notes/update", protect(updateNoteHandler, true))
	mux.HandleFunc("/api/notes/preview", protect(previewNoteHandler, true))
	mux.HandleFunc("/api/tags", protect(tagsHandler, false))
	mux.Handle("/", http.FileServer(http.Dir("../../public")))
	return mux
}

func TestVimLoadLifecycleBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	recoveryFixture(t)
	mux := vimBrowserMux()
	var requests atomic.Int32
	gate, started := make(chan struct{}), make(chan struct{})
	var release sync.Once
	mux.HandleFunc("/vim-editor.js", func(w http.ResponseWriter, r *http.Request) {
		switch requests.Add(1) {
		case 1:
			http.Error(w, "Temporary failure", http.StatusServiceUnavailable)
			return
		case 2:
			close(started)
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
		}
		http.ServeFile(w, r, "../../public/vim-editor.js")
	})
	server := httptest.NewServer(securityHeaders(mux))
	defer func() { release.Do(func() { close(gate) }); server.Close() }()
	browser := newRecoveryBrowser(t)
	browser.navigate(server.URL)
	browser.wait(`!document.getElementById('auth-screen').hidden`)
	browser.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
	browser.wait(`!notesLoading && spellingReady && Boolean(richDraft)`)
	browser.script(`richDraft.editor.commands.insertContent('Keep unsaved draft');clearTimeout(draftSaveTimer);document.getElementById('draft-vim-toggle').click();`)
	browser.wait(`!vimModeRequested && !vimModeEnabled && document.getElementById('note-action-toast').textContent.includes('Vim editor unavailable')`)
	browser.script(`if(currentDraft().content!=='Keep unsaved draft'||!richDraft||vimDraft)throw new Error('Bundle failure disturbed text/editor');document.getElementById('draft-vim-toggle').click();`)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("Vim bundle retry never started")
	}
	browser.script(`richDraft.editor.commands.setContent('<p>Edited while Vim loads</p>');clearTimeout(draftSaveTimer);document.getElementById('draft-vim-toggle').click();`)
	release.Do(func() { close(gate) })
	browser.wait(`Boolean(window.GnotesVimEditor)`)
	browser.script(`if(vimModeEnabled||vimModeRequested||vimDraft||!richDraft||currentDraft().content!=='Edited while Vim loads')throw new Error('Canceled load enabled Vim or lost latest text');toggleVimMode();toggleVimMode();`)
	browser.wait(`!vimModeRequested && !vimModeEnabled`)
	browser.script(`if(vimDraft||!richDraft)throw new Error('Rapid toggles leaked an editor');window.originalVimLoader=loadVimEditor;loadVimEditor=()=>new Promise(resolve=>window.finishVimLoad=resolve);toggleVimMode();showAuth();finishVimLoad();loadVimEditor=originalVimLoader;`)
	browser.wait(`!currentUser && !vimDraft && !vimEdit && !vimModeEnabled`)
	browser.script(`document.getElementById('auth-username').value='other';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
	browser.wait(`currentUser?.username==='other' && Boolean(richDraft) && !notesLoading`)
	browser.script(`if(vimModeEnabled||vimModeRequested||vimDraft||vimEdit||currentDraft().content!=='')throw new Error('Late load crossed account boundary');`)
}

func vimBrowserKeys(b *recoveryBrowser, selector, keys string) {
	b.t.Helper()
	element := b.call("POST", "/element", map[string]string{"using": "css selector", "value": selector}).(map[string]any)
	id := element["element-6066-11e4-a52e-4f735466cecf"].(string)
	b.call("POST", "/element/"+id+"/value", map[string]string{"text": keys})
}

func TestVimEditorBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	for _, width := range []int{320, 1024} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			recoveryFixture(t)
			mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at) VALUES(1,'Vim note','Original text',?)", time.Now().UTC())
			server := httptest.NewServer(securityHeaders(vimBrowserMux()))
			defer server.Close()
			browser := newRecoveryBrowser(t)
			browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setDeviceMetricsOverride", "params": map[string]any{"width": width, "height": 800, "deviceScaleFactor": 1, "mobile": false}})
			browser.navigate(server.URL)
			browser.wait(`!document.getElementById('auth-screen').hidden`)
			browser.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
			browser.wait(`!notesLoading && spellingReady && Boolean(richDraft) && loadedNotes.size===1`)
			browser.script(`if(window.GnotesVimEditor||performance.getEntriesByType('resource').some(r=>r.name.endsWith('/vim-editor.js')))throw new Error('Vim bundle loaded before opting in');window.vimErrors=[];window.addEventListener('error',event=>{if(event.message)vimErrors.push(event.message);});window.vimNoteID=[...loadedNotes.keys()][0];document.getElementById('draft-vim-toggle').click();`)
			browser.wait(`vimModeEnabled && Boolean(vimDraft)`)
			browser.script(`if(richDraft||getComputedStyle(document.getElementById('content')).display!=='none'||document.getElementById('draft-vim-toggle').getAttribute('aria-pressed')!=='true')throw new Error('Vim toggle did not replace draft editor');`)
			vimBrowserKeys(browser, "#new-vim-editor .cm-content", "iVim draft text\ue00c")
			browser.script(`if(vimDraft.getMarkdown()!=='Vim draft text'||vimDraft.view.cm.state.vim.insertMode)throw new Error('Vim Insert/Escape failed: '+JSON.stringify({text:vimDraft.getMarkdown(),mode:vimDraft.view.cm.state.vim.insertMode,active:document.activeElement?.outerHTML.slice(0,200)}));clearTimeout(draftSaveTimer);`)
			vimBrowserKeys(browser, "#new-vim-editor .cm-content", "\ue00cgg0dw")
			browser.script(`if(vimDraft.getMarkdown()!=='draft text'||loadedNotes.size!==1||draftFinalizing)throw new Error('Normal Escape/motions/delete failed');`)
			vimBrowserKeys(browser, "#new-vim-editor .cm-content", "u")
			browser.script(`if(vimDraft.getMarkdown()!=='Vim draft text')throw new Error('Vim undo failed');`)
			vimBrowserKeys(browser, "#new-vim-editor .cm-content", "\ue009r\ue000")
			browser.script(`if(vimDraft.getMarkdown()!=='draft text')throw new Error('Ctrl-R did not redo Vim edit');`)
			vimBrowserKeys(browser, "#new-vim-editor .cm-content", "u/text\ue007\ue00c")
			browser.script(`if(vimDraft.getMarkdown()!=='Vim draft text'||!document.getElementById('search-panel').hidden)throw new Error('Vim search triggered app search or changed text');`)

			vimBrowserKeys(browser, "#new-vim-editor .cm-content", ":w\ue007")
			browser.wait(`draftSavedVersion===draftVersion && !draftFinalizing`)
			var draft string
			if err := db.DB.QueryRow("SELECT content FROM drafts WHERE user_id=1").Scan(&draft); err != nil || draft != "Vim draft text" {
				t.Fatalf("Vim :w did not save the draft: %q %v", draft, err)
			}
			vimBrowserKeys(browser, "#new-vim-editor .cm-content", ":wq\ue007")
			browser.wait(`loadedNotes.size===2 && !notesLoading && !draftFinalizing && draftVersion===0`)
			browser.script(`if(vimDraft.getMarkdown()!==''||document.getElementById('content').value!=='')throw new Error('Finalized Vim draft not cleared');openNoteEditor(vimNoteID,false,true);`)
			browser.wait(`Boolean(vimEdit)`)
			browser.script(`document.getElementById('edit-title-'+vimNoteID).value='';vimEdit.view.dispatch({changes:{from:0,to:vimEdit.view.state.doc.length,insert:''}});clearTimeout(editSaveTimer);`)
			vimBrowserKeys(browser, ".note-editor .cm-content", ":wq\ue007")
			browser.script(`if(editingNoteId!==vimNoteID||!document.getElementById('note-action-toast').textContent.includes('Write a title'))throw new Error('Empty Vim note closed or hid validation');document.getElementById('edit-title-'+vimNoteID).value='Vim note';vimEdit.setMarkdown('Original text');`)
			vimBrowserKeys(browser, ".note-editor .cm-content", "gg0iFresh \ue00c")
			browser.script(`if(vimEdit.getMarkdown()!=='Fresh Original text'||editingNoteId!==vimNoteID)throw new Error('Vim note insert or Escape failed');window.savedVim=vimEdit;window.savedVimDOM=vimEdit.view.dom;vimEdit.selectRange(0,5);renderLoadedNotes();if(vimEdit!==savedVim||vimEdit.view.dom!==savedVimDOM||vimEdit.view.state.selection.main.to!==5||document.activeElement!==vimEdit.view.contentDOM)throw new Error('Refresh disturbed Vim text/selection/focus');vimEdit.selectRange(0);`)
			browser.script(`window.clipboardBeforeVim=navigator.clipboard.writeText.bind(navigator.clipboard);window.vimCopied='';navigator.clipboard.writeText=async text=>{vimCopied=text;};vimEdit.selectRange(0,5);document.querySelector('.note-editor .edit-copy').click();`)
			browser.wait(`vimCopied==='Fresh'`)
			browser.script(`navigator.clipboard.writeText=clipboardBeforeVim;vimEdit.selectRange(0);`)

			vimBrowserKeys(browser, ".note-editor .cm-content", ":w\ue007")
			browser.wait(`document.querySelector('.note-editor .edit-hint').textContent==='Saved'`)
			browser.script(`
              const noteText='Immediate Vim page exit',draftText='Immediate Vim draft exit';
              vimEdit.view.dispatch({changes:{from:0,to:vimEdit.view.state.doc.length,insert:noteText}});
              vimDraft.view.dispatch({changes:{from:0,to:vimDraft.view.state.doc.length,insert:draftText}});
              clearTimeout(editSaveTimer);clearTimeout(draftSaveTimer);
              const original=window.fetch,requests=[];
              window.fetch=(url,options)=>{
                if(options?.keepalive){requests.push({url:String(url),body:JSON.parse(options.body)});return Promise.resolve(new Response('{}',{status:200}));}
                return original(url,options);
              };
              window.dispatchEvent(new PageTransitionEvent('pagehide'));
              window.fetch=original;
              if(!requests.some(r=>r.url==='/api/notes/update?id='+vimNoteID&&r.body.content===noteText)||!requests.some(r=>r.url==='/api/draft/update'&&r.body.content===draftText))throw new Error('Page exit missed latest Vim text');
              vimDraft.setMarkdown('');document.getElementById('content').value='';draftVersion=draftSavedVersion=0;setDraftStatus('');
            `)

			browser.script(`if(editingNoteId!==vimNoteID||!vimEdit)throw new Error(':w closed note');window.vimSource='## Heading\n\n**Vim preview**\n\n| A | B |\n| --- | --- |\n| one | two |';vimEdit.view.dispatch({changes:{from:0,to:vimEdit.view.state.doc.length,insert:'Long line\n'.repeat(500)}});clearTimeout(editSaveTimer);const longCard=document.querySelector('.note-card.is-editing').getBoundingClientRect();if(longCard.bottom>innerHeight||vimEdit.view.scrollDOM.scrollHeight<=vimEdit.view.scrollDOM.clientHeight)throw new Error('Long Vim note does not fit and scroll: '+JSON.stringify(longCard));vimEdit.view.dispatch({changes:{from:0,to:vimEdit.view.state.doc.length,insert:vimSource}});clearTimeout(editSaveTimer);toggleNotePreview(vimNoteID);`)
			browser.wait(`Boolean(document.querySelector('.note-editor-preview table'))`)
			browser.script(`if(getComputedStyle(document.getElementById('vim-editor-'+vimNoteID)).display!=='none'||!document.querySelector('.note-editor-preview strong'))throw new Error('Vim preview did not replace source');toggleNotePreview(vimNoteID);document.querySelector('.note-editor .vim-toggle').click();`)
			browser.wait(`!vimModeEnabled && !vimEdit`)
			browser.script(`if(document.getElementById('edit-content-'+vimNoteID).value!==vimSource||!document.getElementById('edit-content-'+vimNoteID).classList.contains('force-source'))throw new Error('Switching lost unsupported Markdown');document.querySelector('.note-editor .vim-toggle').click();`)
			browser.wait(`vimModeEnabled && Boolean(vimEdit)`)
			browser.script(`if(vimEdit.getMarkdown()!==vimSource)throw new Error('Switching back lost source');const card=document.querySelector('.note-card.is-editing'),toggle=card.querySelector('.vim-toggle');for(const node of [card,toggle,vimEdit.view.dom]){const rect=node.getBoundingClientRect();if(rect.left<0||rect.right>innerWidth||rect.bottom>innerHeight)throw new Error('Vim controls overflow: '+node.className+' '+JSON.stringify(rect));}applyEditorMode(true);if(vimEdit.getMarkdown()!==vimSource||getComputedStyle(document.getElementById('edit-content-'+vimNoteID)).display!=='none')throw new Error('Source preference disrupted Vim');`)
			vimBrowserKeys(browser, ".note-editor .cm-content", ":wq\ue007")
			browser.wait(`editingNoteId===null && pendingNoteEdits.size===0 && !notesLoading`)
			browser.script(`if(vimEdit||document.querySelector('#notes-list .vim-editor'))throw new Error('Closed note retained Vim editor');if(vimErrors.length)throw new Error('Vim runtime errors: '+vimErrors.join('; '));`)
			browser.navigate(server.URL)
			browser.wait(`vimModeEnabled && Boolean(vimDraft) && !notesLoading`)
			browser.script(`window.vimNoteID=[...loadedNotes.values()].find(n=>n.title==='Vim note').id;window.vimErrors=[];window.addEventListener('error',event=>{if(event.message)vimErrors.push(event.message);});openNoteEditor(vimNoteID,false,true);if(!vimEdit||vimEdit.getMarkdown()!==loadedNotes.get(vimNoteID).content)throw new Error('Reload lost Vim preference or note text');`)
			// The existing failed-save recovery must recreate a Vim editor too.
			browser.script(`window.vimFetch=fetch.bind(window);window.failVimSave=true;window.fetch=(url,options)=>failVimSave&&String(url).startsWith('/api/notes/update')?Promise.resolve(new Response('Unavailable',{status:503})):vimFetch(url,options);vimEdit.view.dispatch({changes:{from:0,to:vimEdit.view.state.doc.length,insert:'Retain failed Vim save'}});clearTimeout(editSaveTimer);`)
			vimBrowserKeys(browser, ".note-editor .cm-content", ":wq\ue007")
			browser.wait(`editingNoteId===vimNoteID && Boolean(vimEdit) && document.getElementById('note-action-dialog').open`)
			browser.script(`if(vimEdit.getMarkdown()!=='Retain failed Vim save')throw new Error('Failed save lost Vim source');document.getElementById('note-action-dialog').close();failVimSave=false;window.fetch=vimFetch;`)
			vimBrowserKeys(browser, ".note-editor .cm-content", ":q\ue007")
			browser.wait(`editingNoteId===null && pendingNoteEdits.size===0 && !notesLoading`)
			browser.script(`vimDraft.setMarkdown('Private register text');vimDraft.focus();`)
			vimBrowserKeys(browser, "#new-vim-editor .cm-content", "gg0yy")
			browser.script(`vimDraft.setMarkdown('');document.getElementById('draft-vim-toggle').click();`)
			browser.wait(`!vimModeEnabled && !vimDraft`)
			browser.script(`showAuth();document.getElementById('auth-username').value='other';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
			browser.wait(`currentUser?.username==='other' && !notesLoading && Boolean(richDraft)`)
			browser.script(`if(vimModeEnabled||vimDraft||vimEdit||document.querySelector('.cm-editor'))throw new Error('Vim preference or editor leaked to another account');if(vimErrors.length)throw new Error('Vim runtime errors: '+vimErrors.join('; '));document.getElementById('draft-vim-toggle').click();`)
			browser.wait(`Boolean(vimDraft)`)
			vimBrowserKeys(browser, "#new-vim-editor .cm-content", "p")
			browser.script(`if(vimDraft.getMarkdown()!=='')throw new Error('Vim register leaked previous account text');`)
		})
	}
}
