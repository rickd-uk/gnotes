package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestLatestSearchBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	for _, width := range []int{320, 1024} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			recoveryFixture(t)
			mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at) VALUES(1,'Alpha','Alpha body',?),(1,'Bravo','Bravo body',?)", time.Now().UTC(), time.Now().UTC())
			mux := http.NewServeMux()
			registerSpellingRoutes(mux)
			mux.HandleFunc("/api/auth/config", authConfigHandler)
			mux.HandleFunc("/api/auth/login", loginHandler)
			mux.HandleFunc("/api/auth/me", protect(meHandler, false))
			mux.HandleFunc("/api/draft", protect(getDraftHandler, false))
			mux.HandleFunc("/api/notes/page", protect(pagedListNotesHandler, false))
			mux.HandleFunc("/api/notes/search-page", protect(pagedSearchNotesHandler, false))
			mux.HandleFunc("/api/notes/get", protect(getNoteHandler, false))
			mux.HandleFunc("/api/notes/trash-page", protect(pagedTrashNotesHandler, false))
			mux.HandleFunc("/api/tags", protect(tagsHandler, false))
			mux.Handle("/", http.FileServer(http.Dir("../../public")))
			server := httptest.NewServer(securityHeaders(mux))
			defer server.Close()
			browser := newRecoveryBrowser(t)
			browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setDeviceMetricsOverride", "params": map[string]any{"width": width, "height": 800, "deviceScaleFactor": 1, "mobile": false}})
			browser.navigate(server.URL)
			browser.wait(`!document.getElementById('auth-screen').hidden`)
			browser.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
			browser.wait(`!document.getElementById('app-shell').hidden && !notesLoading && spellingReady && loadedNotes.size===2`)
			browser.script(`
        window.alpha=[...loadedNotes.values()].find(note=>note.title==='Alpha');
        window.bravo=[...loadedNotes.values()].find(note=>note.title==='Bravo');
        window.heldSearches=[];
        window.searchFetch=fetch.bind(window);
        // Ignore abort deliberately, like a late cache/service-worker response.
        // Ownership guards must work even when transport cancellation does not.
        window.fetch=(url,options={})=>typeof url==='string'&&/^\/api\/notes\/(?:page|search-page|get)(?:\?|$)/.test(url)
          ?new Promise((resolve,reject)=>{
            heldSearches.push({url,signal:options.signal,resolve,reject,startedAt:performance.now()});
            if(window.honorSearchAbort)options.signal.addEventListener('abort',()=>reject(new DOMException('Canceled','AbortError')));
          })
          :searchFetch(url,options);
        window.searchPage=(note,extra={})=>({notes:[note],view:'recent',total:1,match_count:1,...extra});
        window.answerSearch=(index,page,status=200)=>heldSearches[index].resolve(new Response(JSON.stringify(page),{status,headers:{'Content-Type':'application/json'}}));
        window.startSearch=query=>{
          const input=document.getElementById('search-query');input.value=query;
          input.dispatchEvent(new Event('input',{bubbles:true}));clearTimeout(liveSearchTimer);
          const run={settled:false};run.promise=runLiveSearch().then(result=>{run.result=result;run.settled=true;});return run;
        };
        window.assertBravo=()=>{
          if(notesLoading||loadedNotes.size!==1||!loadedNotes.has(bravo.id)||document.querySelector('.note-view h2')?.textContent!=='Bravo')throw new Error('Latest query did not own results');
          if(!document.getElementById('notes-load-status').hidden||document.getElementById('load-more-notes').disabled||document.getElementById('search-status').textContent)throw new Error('Latest query did not own loading/error state');
        };
        window.oldSearch=startSearch('Alpha');window.newSearch=startSearch('Bravo');
        if(heldSearches.length!==2||!heldSearches[0].signal.aborted)throw new Error('Latest query waited for the old request');
        answerSearch(1,searchPage(bravo));
      `)
			browser.wait(`newSearch.settled`)
			browser.script(`assertBravo();answerSearch(0,searchPage(alpha));`)
			browser.wait(`oldSearch.settled`)
			browser.script(`assertBravo();`)
			// Real input retains the debounce but never polls for the old request.
			browser.script(`heldSearches=[];oldSearch=startSearch('Alpha');window.latestTypedAt=performance.now();const input=document.getElementById('search-query');input.value='Bravo';input.dispatchEvent(new Event('input',{bubbles:true}));if(heldSearches.length!==1||!heldSearches[0].signal.aborted)throw new Error('Typing bypassed debounce or failed to cancel');`)
			browser.wait(`heldSearches.length===2`)
			t.Logf("latest query starts while the old response is still held; debounce delay (ms): %v", browser.script(`return heldSearches[1].startedAt-latestTypedAt;`))
			browser.script(`answerSearch(1,searchPage(bravo));`)
			browser.wait(`!notesLoading && loadedNotes.size===1 && loadedNotes.has(bravo.id)`)
			browser.script(`answerSearch(0,searchPage(alpha));`)
			browser.wait(`oldSearch.settled`)
			browser.script(`assertBravo();`)
			// A stale error must neither sign out the user nor clear a newer
			// request's loading indicator while that newer response is pending.
			for _, status := range []int{401, 503} {
				browser.script(fmt.Sprintf(`heldSearches=[];oldSearch=startSearch('Alpha');newSearch=startSearch('Bravo');answerSearch(0,{error:'stale error'},%d);`, status))
				browser.wait(`oldSearch.settled`)
				browser.script(`if(!notesLoading||!document.getElementById('load-more-notes').disabled||document.getElementById('app-shell').hidden||!document.getElementById('notes-load-status').hidden||document.getElementById('search-status').textContent!=='Searching…')throw new Error('Stale error disturbed newer request');answerSearch(1,searchPage(bravo));`)
				browser.wait(`newSearch.settled`)
				browser.script(`assertBravo();`)
			}
			browser.script(`heldSearches=[];window.honorSearchAbort=true;oldSearch=startSearch('Alpha');newSearch=startSearch('Bravo');`)
			browser.wait(`oldSearch.settled`)
			browser.script(`if(!notesLoading||!document.getElementById('notes-load-status').hidden)throw new Error('Abort rejection disturbed the latest request');answerSearch(1,searchPage(bravo));window.honorSearchAbort=false;`)
			browser.wait(`newSearch.settled`)
			browser.script(`assertBravo();`)
			// Ownership must also be checked after parsing a response body.
			for _, body := range []string{"json", "text"} {
				browser.script(fmt.Sprintf(`heldSearches=[];window.releaseStaleBody=null;oldSearch=startSearch('Alpha');heldSearches[0].resolve({ok:%t,status:%d,%s:()=>new Promise(resolve=>window.releaseStaleBody=resolve)});`, body == "json", map[string]int{"json": 200, "text": 503}[body], body))
				browser.wait(`Boolean(releaseStaleBody)`)
				browser.script(`newSearch=startSearch('Bravo');releaseStaleBody(searchPage(alpha));`)
				browser.wait(`oldSearch.settled`)
				browser.script(`if(!notesLoading||!document.getElementById('load-more-notes').disabled||!document.getElementById('notes-load-status').hidden)throw new Error('Late body disturbed newer request');answerSearch(1,searchPage(bravo));`)
				browser.wait(`newSearch.settled`)
				browser.script(`assertBravo();`)
			}
			// Real current failures still show an error and permit retry.
			browser.script(`heldSearches=[];oldSearch=startSearch('Alpha');heldSearches[0].reject(new TypeError('offline'));`)
			browser.wait(`oldSearch.settled`)
			browser.script(`if(notesLoading||document.getElementById('notes-load-status').hidden||document.getElementById('load-more-notes').disabled)throw new Error('Current failure cannot be retried');newSearch=startSearch('Bravo');answerSearch(1,searchPage(bravo,{next_cursor:'cursor-bravo'}));`)
			browser.wait(`newSearch.settled`)
			// A page from the old query may finish after a new first page.
			browser.script(`window.appendSettled=false;loadNotes(true).then(()=>window.appendSettled=true);const oldIndex=heldSearches.length-1;if(!heldSearches[oldIndex].url.includes('cursor='))throw new Error('Pagination was not requested');newSearch=startSearch('Bravo body');if(!heldSearches[oldIndex].signal.aborted)throw new Error('New search did not cancel pagination');answerSearch(heldSearches.length-1,searchPage(bravo,{next_cursor:'cursor-bravo-body'}));`)
			browser.wait(`newSearch.settled`)
			browser.script(`answerSearch(heldSearches.length-2,{notes:[alpha],view:'recent',total:2,next_cursor:'obsolete-cursor'});`)
			browser.wait(`appendSettled`)
			browser.script(`assertBravo();if(notesNextCursor!=='cursor-bravo-body')throw new Error('Old pagination replaced current cursor');`)
			// Do not send a cursor from the old filter during the debounce gap.
			browser.script(`const count=heldSearches.length;document.getElementById('search-query').value='Alpha';scheduleLiveSearch();clearTimeout(liveSearchTimer);loadNotes(true);if(heldSearches.length!==count)throw new Error('Old cursor sent with a new filter');oldSearch=startSearch('Alpha');newSearch=startSearch('');if(!heldSearches.at(-1).url.startsWith('/api/notes/page?'))throw new Error('Clearing did not request recent notes');answerSearch(heldSearches.length-1,{notes:[alpha,bravo],view:'recent',total:2});`)
			browser.wait(`newSearch.settled && !notesLoading && loadedNotes.size===2`)
			browser.script(`answerSearch(heldSearches.length-2,searchPage(alpha));`)
			browser.wait(`oldSearch.settled`)
			browser.script(`if(searchActive||loadedNotes.size!==2||highlightQuery)throw new Error('Old search replaced cleared results');`)
			// Restored editors are auxiliary requests and must stage locally.
			browser.script(`heldSearches=[];rememberEditingNote(alpha.id);oldSearch=startSearch('Bravo');answerSearch(0,searchPage(bravo));`)
			browser.wait(`heldSearches.length===2 && heldSearches[1].url.includes('/api/notes/get?')`)
			browser.script(`if(loadedNotes.size!==2)throw new Error('Incomplete restore published partial results');forgetEditingNote();newSearch=startSearch('Bravo');answerSearch(2,searchPage(bravo));`)
			browser.wait(`newSearch.settled`)
			browser.script(`answerSearch(1,alpha);`)
			browser.wait(`oldSearch.settled`)
			browser.script(`assertBravo();if(editingNoteId!==null||rememberedEditingNoteID()!==null)throw new Error('Old restore reopened an editor');`)
			// Switching collections must not wait for the stale search either.
			browser.script(`heldSearches=[];oldSearch=startSearch('Alpha');window.archiveSettled=false;toggleArchiveMode().then(()=>window.archiveSettled=true);if(heldSearches.length!==2||!heldSearches[0].signal.aborted||!heldSearches[1].url.includes('archive=1'))throw new Error('Archive navigation waited for old search');window.archived={...alpha,id:9000,title:'Archived',archived_at:new Date().toISOString()};answerSearch(1,{notes:[archived],view:'archived',total:1});`)
			browser.wait(`archiveSettled && !notesLoading`)
			browser.script(`answerSearch(0,searchPage(alpha));`)
			browser.wait(`oldSearch.settled`)
			browser.script(`if(!archiveMode||loadedNotes.size!==1||!loadedNotes.has(archived.id))throw new Error('Old search replaced archive');heldSearches=[];oldSearch=startSearch('Alpha');showAuth('Signed out for test');if(!heldSearches[0].signal.aborted)throw new Error('Sign-out did not cancel request');answerSearch(0,searchPage(alpha));`)
			browser.wait(`oldSearch.settled`)
			browser.script(`if(currentUser||loadedNotes.size||notesLoading||!document.getElementById('app-shell').hidden)throw new Error('Late response restored signed-out data');`)
		})
	}
}
