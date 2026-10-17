package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestIncrementalNoteRenderingBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	recoveryFixture(t)
	mux := http.NewServeMux()
	registerSpellingRoutes(mux)
	mux.HandleFunc("/api/auth/config", authConfigHandler)
	mux.HandleFunc("/api/auth/login", loginHandler)
	mux.HandleFunc("/api/auth/me", protect(meHandler, false))
	mux.HandleFunc("/api/draft", protect(getDraftHandler, false))
	mux.HandleFunc("/api/notes/page", protect(pagedListNotesHandler, false))
	mux.HandleFunc("/api/notes/trash-page", protect(pagedTrashNotesHandler, false))
	mux.HandleFunc("/api/tags", protect(tagsHandler, false))
	mux.Handle("/", http.FileServer(http.Dir("../../public")))
	server := httptest.NewServer(securityHeaders(mux))
	defer server.Close()
	browser := newRecoveryBrowser(t)
	browser.navigate(server.URL)
	browser.wait(`!document.getElementById('auth-screen').hidden`)
	browser.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
	browser.wait(`!document.getElementById('app-shell').hidden && !notesLoading && spellingReady`)
	browser.script(`window.renderRequests=0;window.renderFetch=fetch.bind(window);window.fetch=(url,options)=>{if(typeof url==='string'&&url.startsWith('/api/tags'))renderRequests++;return renderFetch(url,options);};loadedNotes=new Map(Array.from({length:500},(_,i)=>{const id=10000+i;return [id,{id,title:'Performance note '+i,content:'Tokyo New York ordinary text. '.repeat(15),html_content:'<p>'+('Tokyo New York ordinary text. '.repeat(15))+'</p>',tags:['example'],created_at:new Date(Date.UTC(2026,8,14,0,0,-i)).toISOString(),pinned:false,favorited:false,locked:false}];}));notesTotal=500;notesNextCursor='';window.coldRenderStart=performance.now();renderLoadedNotes();window.coldRenderMS=performance.now()-coldRenderStart;`)
	metrics := browser.script(`const median=values=>values.sort((a,b)=>a-b)[Math.floor(values.length/2)];const unchanged=[],changed=[];for(let i=0;i<5;i++){let start=performance.now();renderLoadedNotes();unchanged.push(performance.now()-start);loadedNotes.get(10001).title='Changed title '+i;start=performance.now();renderLoadedNotes();changed.push(performance.now()-start);}return {cards:document.querySelectorAll('#notes-list .note-card').length,cold_ms:coldRenderMS,unchanged_ms:median(unchanged),changed_ms:median(changed),tag_requests:renderRequests};`)
	t.Logf("500-note render timings (median of 5): %v", metrics)
	// DOM identity and native selection must survive refreshes and later pages.
	browser.script(`window.savedCards=new Map([...document.querySelectorAll('#notes-list .note-card')].map(card=>[card.id,card]));const root=document.getElementById('note-content-10002');const node=root.querySelector('p').firstChild;getSelection().setBaseAndExtent(node,0,node,5);renderLoadedNotes();for(const [id,card] of savedCards)if(document.getElementById(id)!==card)throw new Error('Unchanged render replaced '+id);if(getSelection().toString()!=='Tokyo')throw new Error('Render lost native selection');loadedNotes.get(10001).title='One changed note';renderLoadedNotes();if(document.getElementById('note-10001')===savedCards.get('note-10001')||document.getElementById('note-10002')!==savedCards.get('note-10002'))throw new Error('Single-note update replaced unrelated cards');for(let i=500;i<550;i++){const id=10000+i;loadedNotes.set(id,{...loadedNotes.get(10499),id,title:'Older note '+i,created_at:new Date(Date.UTC(2026,8,13,0,0,-i)).toISOString()});}notesTotal=550;renderLoadedNotes();if(document.querySelectorAll('#notes-list .note-card').length!==550||getSelection().toString()!=='Tokyo')throw new Error('Appending notes lost cards or selection');for(const [id,card] of savedCards)if(id!=='note-10001'&&document.getElementById(id)!==card)throw new Error('Append replaced '+id);`)
	browser.script(`loadedNotes.get(10003).pinned=true;renderLoadedNotes();if(document.querySelector('.pinned-section .note-card')?.id!=='note-10003'||document.getElementById('note-10002')!==savedCards.get('note-10002')||getSelection().toString()!=='Tokyo')throw new Error('Pinning rebuilt unrelated notes or lost selection');loadedNotes.delete(10004);notesTotal--;renderLoadedNotes();if(document.getElementById('note-10004')||getSelection().toString()!=='Tokyo')throw new Error('Removal lost unrelated selection');if(renderRequests)throw new Error('Local renders fetched tag aggregates');`)
	// A render triggered by another note must not remount the active editor.
	browser.script(`getSelection().removeAllRanges();openNoteEditor(10002,false,true);window.savedEditor=richEdit;window.savedEditorDOM=richEdit.editor.view.dom;richEdit.editor.commands.insertContent('Unsaved text ');clearTimeout(editSaveTimer);richEdit.editor.chain().focus().setTextSelection({from:1,to:6}).run();window.savedEditorText=richEdit.getMarkdown();richEdit.editor.view.focus();loadedNotes.get(10001).title='Another note changed';renderLoadedNotes();if(richEdit!==savedEditor||richEdit.editor.view.dom!==savedEditorDOM||richEdit.getMarkdown()!==savedEditorText||richEdit.editor.state.selection.from!==1||richEdit.editor.state.selection.to!==6||document.activeElement!==savedEditorDOM)throw new Error('Render disturbed active rich editor: '+JSON.stringify({instance:richEdit===savedEditor,dom:richEdit.editor.view.dom===savedEditorDOM,text:richEdit.getMarkdown()===savedEditorText,from:richEdit.editor.state.selection.from,to:richEdit.editor.state.selection.to,active:document.activeElement?.outerHTML.slice(0,150)}));applyEditorMode(true);const input=document.getElementById('edit-content-10002');input.focus();input.setSelectionRange(1,6);renderLoadedNotes();if(document.activeElement!==input||input.selectionStart!==1||input.selectionEnd!==6||input.value!==savedEditorText)throw new Error('Render disturbed active source editor');clearTimeout(editSaveTimer);destroyEditRichEditor();closeFocusedNote();editingNoteId=null;forgetEditingNote();applyEditorMode(false);`)
	// Reusing cards must neither nest decorations nor retain an old query.
	browser.script(`searchActive=true;highlightQuery='Tokyo';highlightScope='content';highlightMatchCase=false;renderLoadedNotes(1);window.highlightedCard=document.getElementById('note-10002');window.highlightCount=highlightedCard.querySelectorAll('mark.search-highlight').length;if(!highlightCount)throw new Error('Search highlighting missing');renderLoadedNotes(1);if(document.getElementById('note-10002')!==highlightedCard||highlightedCard.querySelectorAll('mark.search-highlight').length!==highlightCount||highlightedCard.querySelector('mark mark'))throw new Error('Repeated render nested highlights');highlightQuery='ordinary';renderLoadedNotes(1);if([...document.querySelectorAll('mark.search-highlight')].some(mark=>mark.textContent!=='ordinary'))throw new Error('Search retained old highlights');searchActive=false;highlightQuery='';renderLoadedNotes();if(document.querySelector('mark.search-highlight'))throw new Error('Search marks survived clearing');`)
}
