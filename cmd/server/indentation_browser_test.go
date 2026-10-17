package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestOrdinaryIndentationBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	recoveryFixture(t)
	mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at) VALUES(1,'Indentation','Original text',?)", time.Now().UTC())
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
	browser.script(`window.indentID=[...loadedNotes.keys()][0];`)
	for _, width := range []int{320, 1024} {
		browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setDeviceMetricsOverride", "params": map[string]any{"width": width, "height": 800, "deviceScaleFactor": 1, "mobile": false}})
		// Ordinary spaces, tabs and indentation crossing mark boundaries remain
		// paragraph text through server preview/save and rich-editor reopening.
		browser.script(`
          applyEditorMode(false);openNoteEditor(indentID,false,true);
          richEdit.editor.commands.setContent({type:'doc',content:[
            {type:'paragraph',content:[{type:'text',text:'    first'},{type:'hardBreak'},{type:'text',text:'        second'}]},
            {type:'paragraph',content:[{type:'text',text:'  '},{type:'text',text:'  bold',marks:[{type:'bold'}]}]},
            {type:'paragraph',content:[{type:'text',text:'\tTabbed'}]},
            {type:'paragraph',content:[{type:'text',text:'Code '},{type:'text',text:'a    b',marks:[{type:'code'}]}]}
          ]});
          window.beforeIndentSave=richEdit.getMarkdown();
          if(!beforeIndentSave.startsWith('\u00a0'.repeat(4)+'first'))throw new Error('Paragraph serialized as indented Markdown code');
          if(!beforeIndentSave.includes(String.fromCharCode(96)+'a    b'+String.fromCharCode(96)))throw new Error('Inline code spaces changed');
          clearTimeout(editSaveTimer);toggleNotePreview(indentID);
        `)
		browser.wait(`Boolean(document.querySelector('.note-editor-preview strong'))`)
		browser.script(`const preview=document.getElementById('edit-preview-'+indentID);if(preview.querySelector('pre')||!preview.textContent.startsWith('\u00a0'.repeat(4)+'first'))throw new Error('Preview converted ordinary indentation to code');updateNote(indentID);`)
		browser.wait(`editingNoteId===null && pendingNoteEdits.size===0 && !notesLoading`)
		browser.script(`
          const body=document.getElementById('note-content-'+indentID),paragraphs=body.querySelectorAll('p');
          if(body.querySelector('pre,.note-code-block')||paragraphs.length!==4||!paragraphs[0].textContent.startsWith('\u00a0'.repeat(4)+'first')||!paragraphs[0].textContent.includes('\u00a0'.repeat(8)+'second')||!paragraphs[1].querySelector('strong')||paragraphs[2].textContent!=='\u00a0'.repeat(4)+'Tabbed')throw new Error('Saved indentation/formatting changed: '+body.innerHTML);
          openNoteEditor(indentID,false,true);
          if(!richEdit||richEdit.editor.state.doc.firstChild.type.name!=='paragraph'||richEdit.getMarkdown()!==beforeIndentSave)throw new Error('Reopening changed indented text to code');
          applyEditorMode(true);
          const source=document.getElementById('edit-content-'+indentID);
          source.value='first\nsecond';source.setSelectionRange(0,source.value.length);
          source.dispatchEvent(new KeyboardEvent('keydown',{key:'Tab',bubbles:true,cancelable:true}));
          if(source.value!=='\u00a0'.repeat(4)+'first\n'+'\u00a0'.repeat(4)+'second'||source.selectionStart!==4||source.selectionEnd!==source.value.length)throw new Error('Source Tab lost indentation or selection');
          updateNote(indentID);
        `)
		browser.wait(`editingNoteId===null && pendingNoteEdits.size===0 && !notesLoading`)
		browser.script(`
          const body=document.getElementById('note-content-'+indentID);
          if(body.querySelector('pre')||!body.textContent.includes('\u00a0'.repeat(4)+'second'))throw new Error('Source Tab saved as code');
          openNoteEditor(indentID,false,true);applyEditorMode(false);
          if(!richEdit)throw new Error('Source indentation forced fallback');
          richEdit.editor.commands.setContent({type:'doc',content:[{type:'paragraph',content:[{type:'text',text:'    explicit code'}]}]});
          richEdit.editor.commands.setTextSelection(1);richEdit.format('codeBlock');
          if(!richEdit.getMarkdown().startsWith(String.fromCharCode(96).repeat(3)))throw new Error('Explicit code command lost fence');
          updateNote(indentID);
        `)
		browser.wait(`editingNoteId===null && pendingNoteEdits.size===0 && !notesLoading`)
		browser.script(`const code=document.querySelector('#note-content-'+indentID+' pre code');if(!code||code.textContent!=='    explicit code\n'||!document.querySelector('#note-content-'+indentID+' .note-code-language'))throw new Error('Explicit code indentation or label changed');`)
	}
	// New notes use the same serialization boundary as existing-note saves.
	browser.script(`applyEditorMode(false);document.getElementById('title').value='Indented new note';richDraft.editor.commands.setContent({type:'doc',content:[{type:'paragraph',content:[{type:'text',text:'    new note'}]}]});finalizeDraft();`)
	browser.wait(`loadedNotes.size===2 && !draftFinalizing && !notesLoading`)
	browser.script(`const note=[...loadedNotes.values()].find(n=>n.title==='Indented new note'),body=document.getElementById('note-content-'+note.id);if(body.querySelector('pre')||!body.textContent.startsWith('\u00a0'.repeat(4)+'new note'))throw new Error('New note indentation became code');`)
}
