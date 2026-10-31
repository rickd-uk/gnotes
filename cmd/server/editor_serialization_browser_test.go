package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestEditorSerializationCostBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	server := httptest.NewServer(http.FileServer(http.Dir("../../public")))
	defer server.Close()
	browser := newRecoveryBrowser(t)
	browser.navigate(server.URL)
	browser.wait(`Boolean(window.GnotesRichEditor)`)
	metrics := browser.script(`
      const markdown = Array.from({length:500}, () => 'Tokyo New York ordinary text. '.repeat(15)).join('\n\n');
      const results = {};
      for (const deferred of [false, true]) {
        const host = document.createElement('div'); host.hidden = true; document.body.append(host);
        let dirty = 0, eager = 0, serializations = 0;
        const editor = GnotesRichEditor.mount(host, markdown, () => eager++, () => {}, () => {},
          deferred ? {onDirty: () => dirty++} : {});
        editor.setSpelling(false, [], [], false);
        const serialize = editor.editor.getMarkdown.bind(editor.editor);
        editor.editor.getMarkdown = () => { serializations++; return serialize(); };
        const timings = [];
        for (let i=0;i<25;i++) {
          const start=performance.now();
          editor.editor.view.dispatch(editor.editor.state.tr.insertText('x',10));
          if (i>=5) timings.push(performance.now()-start);
        }
        const duringTyping = serializations;
        const text = editor.getMarkdown();
        if (editor.getMarkdown()!==text) throw new Error('Cached Markdown differs');
        editor.editor.commands.setTextSelection(2);
        editor.getMarkdown();
        if (deferred && (duringTyping!==0 || serializations!==1 || dirty!==25 || eager!==0))
          throw new Error('Deferred serialization did extra work: '+JSON.stringify({duringTyping,serializations,dirty,eager}));
        if (!deferred && (duringTyping!==25 || eager!==25)) throw new Error('Eager callback contract changed');
        timings.sort((a,b)=>a-b);
        results[deferred?'deferred':'eager'] = {median_edit_ms:timings[10],serializations_during_typing:duringTyping};
        editor.setMarkdown('Reset **formatting**');
        if (editor.getMarkdown()!=='Reset **formatting**') throw new Error('Programmatic reset retained cached text');
        for (const html of ['', '<p>   </p>', '<p><br></p>', '<h2></h2>', '<hr>', '<pre><code></code></pre>',
            '<blockquote><p></p></blockquote>', '<p><strong> </strong></p>', '<ul><li><p></p></li></ul>']) {
          editor.editor.commands.setContent(html);
          if (editor.hasContent()!==Boolean(editor.getMarkdown().trim()))
            throw new Error('Content detection changed Save availability: '+JSON.stringify({html,markdown:editor.getMarkdown()}));
        }
        editor.destroy(); host.remove();
      }
      return results;
    `)
	t.Logf("500-paragraph browser edit timings (median of 20, hidden DOM, spelling disabled): %v", metrics)
}

func TestDeferredEditorReadsBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	recoveryFixture(t)
	mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at) VALUES(1,'Deferred note','Original text',?)", time.Now().UTC())
	mux := http.NewServeMux()
	registerDictionaryRoutes(mux)
	mux.HandleFunc("/api/auth/config", authConfigHandler)
	mux.HandleFunc("/api/auth/login", loginHandler)
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
	server := httptest.NewServer(securityHeaders(mux))
	defer server.Close()
	browser := newRecoveryBrowser(t)
	browser.navigate(server.URL)
	browser.wait(`!document.getElementById('auth-screen').hidden`)
	browser.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
	browser.wait(`!notesLoading && spellingReady && Boolean(richDraft) && loadedNotes.size===1`)
	browser.script(`
      window.deferredID=[...loadedNotes.keys()][0];
      openNoteEditor(deferredID,false);
      window.noteSerializationCount=0;
      const original=richEdit.editor.getMarkdown.bind(richEdit.editor);
      richEdit.editor.getMarkdown=()=>{noteSerializationCount++;return original();};
      richEdit.editor.commands.setContent('<p>Latest <strong>unsaved</strong> text</p>');
      clearTimeout(editSaveTimer);
      if(noteSerializationCount)throw new Error('App serialized during the edit');
      if(currentEdit(deferredID).content!=='Latest **unsaved** text'||noteSerializationCount!==1)throw new Error('Read did not flush latest Markdown');
      currentEdit(deferredID);richEdit.getMarkdown();
      if(noteSerializationCount!==1)throw new Error('Repeated reads serialized unchanged text');
      document.getElementById('edit-content-'+deferredID).value='Explicit source change';
      if(currentEdit(deferredID).content!=='Explicit source change'||currentEdit(deferredID).content!=='Explicit source change')
        throw new Error('Read overwrote an explicit source edit');
      richEdit.editor.commands.setContent('<p>Newest preview text</p>');
      toggleNotePreview(deferredID);
    `)
	browser.wait(`document.getElementById('edit-preview-'+deferredID).textContent.trim()==='Newest preview text'`)
	browser.script(`
      setNotePreview(deferredID,false);
      richEdit.editor.commands.setContent('<p>Switch source immediately</p>');
      applyEditorMode(true);
      if(document.getElementById('edit-content-'+deferredID).value!=='Switch source immediately')throw new Error('Mode switch lost text');
      applyEditorMode(false);
      richEdit.editor.commands.setContent('<p>Save immediately</p>');
      updateNote(deferredID);
    `)
	browser.wait(`editingNoteId===null && !pendingNoteEdits.size && !notesLoading && loadedNotes.get(deferredID).content==='Save immediately'`)
	browser.script(`
      openNoteEditor(deferredID,false);
      richEdit.editor.commands.setContent('<p>Capture note on page exit</p>');
      clearTimeout(editSaveTimer);
      window.draftSerializationCount=0;
      const original=richDraft.editor.getMarkdown.bind(richDraft.editor);
      richDraft.editor.getMarkdown=()=>{draftSerializationCount++;return original();};
      richDraft.editor.commands.setContent('<p>Recover <strong>this</strong> draft</p>');
      clearTimeout(draftSaveTimer);
      if(draftSerializationCount||!draftHasText()||document.getElementById('note-save').hidden)throw new Error('Draft typing serialized or failed to enable Save');
      window.pagehideWrites=[];window.deferredFetch=fetch.bind(window);
      window.fetch=(url,options={})=>{if(options.keepalive)pagehideWrites.push({url,body:JSON.parse(options.body)});return deferredFetch(url,options);};
      window.dispatchEvent(new Event('pagehide'));
    `)
	browser.wait(`pagehideWrites.length===2 && draftSavedVersion===draftVersion && document.querySelector('.edit-hint').dataset.state==='saved'`)
	browser.script(`
      if(pagehideWrites.find(write=>write.url==='/api/notes/update?id='+deferredID)?.body.content!=='Capture note on page exit')throw new Error('Page exit did not flush latest note');
      if(pagehideWrites.find(write=>write.url==='/api/draft/update')?.body.content!=='Recover **this** draft'||draftSerializationCount!==1)throw new Error('Page exit did not flush latest draft');
      window.fetch=deferredFetch;
      updateNote(deferredID);
    `)
	browser.wait(`editingNoteId===null && !pendingNoteEdits.size && !notesLoading && loadedNotes.get(deferredID).content==='Capture note on page exit'`)
	browser.script(`finalizeDraft();`)
	browser.wait(`!draftFinalizing && draftVersion===0 && !notesLoading && [...loadedNotes.values()].some(note=>note.content==='Recover **this** draft')`)
}
