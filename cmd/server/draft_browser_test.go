package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestDraftRetryAndKeyboardBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	recoveryFixture(t)
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
	mux.HandleFunc("/api/tags", protect(tagsHandler, false))
	mux.Handle("/", http.FileServer(http.Dir("../../public")))
	server := httptest.NewServer(securityHeaders(mux))
	defer server.Close()
	browser := newRecoveryBrowser(t)
	browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setDeviceMetricsOverride", "params": map[string]any{"width": 360, "height": 800, "deviceScaleFactor": 1, "mobile": false}})
	browser.navigate(server.URL)
	browser.wait(`!document.getElementById('auth-screen').hidden`)
	browser.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
	browser.wait(`!notesLoading && spellingReady && !document.getElementById('app-shell').hidden`)
	// Simulate the visual viewport shrinking and panning when a phone keyboard opens.
	browser.script(`window.realViewport=visualViewport;Object.defineProperty(window,'visualViewport',{configurable:true,value:{offsetTop:40,offsetLeft:0,width:360,height:400}});syncFocusedEditViewport();const nav=document.getElementById('primary-navigation').getBoundingClientRect();if(Math.abs(nav.bottom-440)>1)throw new Error('Navigation is behind keyboard');const account=document.getElementById('account-control');account.open=true;const panel=account.querySelector('.account-menu').getBoundingClientRect();if(panel.bottom>nav.top||panel.top<40)throw new Error('Profile panel outside visible screen');account.open=false;document.getElementById('view-toggle').click();const controls=document.getElementById('view-menu').getBoundingClientRect();if(controls.top<40||controls.bottom>nav.top)throw new Error('Controls outside visible screen');closeViewMenu();Object.defineProperty(window,'visualViewport',{configurable:true,value:realViewport});syncFocusedEditViewport();if(Math.abs(document.getElementById('primary-navigation').getBoundingClientRect().bottom-800)>1)throw new Error('Navigation did not return after keyboard closed');`)
	// A failed write must retain the text and retry the real endpoint automatically.
	browser.script(`window.realFetch=fetch.bind(window);window.draftCalls=0;window.fetch=(url,options={})=>url==='/api/draft/update'&&++draftCalls===1?Promise.reject(new TypeError('Network unavailable')):realFetch(url,options);document.getElementById('title').value='Recover this draft';scheduleDraftSave();`)
	browser.wait(`document.getElementById('draft-status').textContent.includes('Retrying')`)
	browser.wait(`draftCalls===2 && document.getElementById('draft-status').textContent==='Draft saved'`)
	browser.script(`realFetch('/api/draft').then(r=>r.json()).then(d=>window.persistedDraft=d);`)
	browser.wait(`window.persistedDraft?.title==='Recover this draft'`)
	browser.script(`document.getElementById('new-note-collapse').dispatchEvent(new PointerEvent('pointerdown',{bubbles:true}));document.getElementById('new-note-collapse').click();if(!document.getElementById('note-form').hidden||draftFinalizing||document.getElementById('title').value!=='Recover this draft')throw new Error('Collapsing lost or finalized draft');document.getElementById('new-note-collapse').click();if(document.getElementById('note-form').hidden||document.getElementById('title').value!=='Recover this draft')throw new Error('Expanding lost text');`)
	// Do not retry conflicts, and surface validation errors without discarding text.
	browser.script(`window.draftCalls=0;window.fetch=(url,options={})=>url==='/api/draft/update'?(draftCalls++,Promise.resolve(new Response('A newer draft exists',{status:409}))):realFetch(url,options);document.getElementById('title').value='Keep my text';scheduleDraftSave();`)
	browser.wait(`document.getElementById('draft-status').textContent==='Newer draft open in another tab'`)
	browser.script(`if(draftCalls!==1||draftRetryAttempt!==0||document.getElementById('title').value!=='Keep my text')throw new Error('Conflict retried or lost text');window.fetch=(url,options={})=>url==='/api/draft/update'?Promise.resolve(new Response('Title is too long',{status:400})):realFetch(url,options);scheduleDraftSave();`)
	browser.wait(`document.getElementById('draft-status').textContent==='Title is too long'`)
	// Reconnection retries the latest text, then finishing clears the pending retry.
	browser.script(`window.fetch=(url,options={})=>url==='/api/draft/update'?Promise.resolve(new Response('',{status:503})):realFetch(url,options);scheduleDraftSave();`)
	browser.wait(`document.getElementById('draft-status').textContent.includes('Retrying')`)
	browser.script(`window.fetch=realFetch;window.dispatchEvent(new Event('online'));`)
	browser.wait(`document.getElementById('draft-status').textContent==='Draft saved'`)
	browser.script(`finalizeDraft();`)
	browser.wait(`!draftFinalizing && draftVersion===0 && loadedNotes.size===1`)
	browser.script(`realFetch('/api/draft').then(r=>r.json()).then(d=>window.finishedDraft=d);`)
	browser.wait(`window.finishedDraft?.version===0 && finishedDraft.title===''`)
}
