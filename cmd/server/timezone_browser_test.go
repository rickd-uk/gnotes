package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestTimeZonePreferenceBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	recoveryFixture(t)
	mux := http.NewServeMux()
	registerSpellingRoutes(mux)
	mux.HandleFunc("/api/auth/config", authConfigHandler)
	mux.HandleFunc("/api/auth/login", loginHandler)
	mux.HandleFunc("/api/auth/me", protect(meHandler, false))
	mux.HandleFunc("/api/auth/logout", protect(logoutHandler, true))
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
	browser.script(`localStorage.setItem('gnotes-ui:rick',JSON.stringify({timeZoneChoice:'UTC'}));document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
	browser.wait(`!document.getElementById('app-shell').hidden && !spellingBusy && timeZoneChoice==='UTC'`)
	first := browser.call("GET", "/window", nil).(string)
	second := browser.call("POST", "/window/new", map[string]string{"type": "tab"}).(map[string]any)["handle"].(string)
	browser.call("POST", "/window", map[string]string{"handle": second})
	browser.navigate(server.URL)
	browser.wait(`!document.getElementById('app-shell').hidden && !spellingBusy && timeZoneChoice==='UTC'`)
	browser.call("POST", "/window", map[string]string{"handle": first})
	browser.script(`const s=document.getElementById('note-time-zone');s.value='Asia/Tokyo';s.dispatchEvent(new Event('change',{bubbles:true}));`)
	browser.wait(`!notesLoading && JSON.parse(localStorage.getItem('gnotes-ui:rick')).timeZoneChoice==='Asia/Tokyo'`)
	browser.call("POST", "/window", map[string]string{"handle": second})
	// An older tab's unrelated spelling refresh used to rewrite the timezone.
	browser.script(`window.zoneRefreshDone=false;loadSpellingLists().then(()=>window.zoneRefreshDone=true);`)
	browser.wait(`window.zoneRefreshDone`)
	browser.script(`if(JSON.parse(localStorage.getItem('gnotes-ui:rick')).timeZoneChoice!=='Asia/Tokyo')throw new Error('Older tab reset Tokyo during unrelated settings save');`)
	browser.navigate(server.URL)
	browser.wait(`!document.getElementById('app-shell').hidden && !spellingBusy && timeZoneChoice==='Asia/Tokyo' && document.getElementById('note-time-zone').value==='Asia/Tokyo'`)
	// Explicit Browser/default is also a saved choice, not a missing preference.
	browser.script(`const s=document.getElementById('note-time-zone');s.value='';s.dispatchEvent(new Event('change',{bubbles:true}));`)
	browser.wait(`!notesLoading && timeZoneChoice==='' && JSON.parse(localStorage.getItem('gnotes-ui:rick')).timeZoneChoice===''`)
	browser.navigate(server.URL)
	browser.wait(`!document.getElementById('app-shell').hidden && !spellingBusy && timeZoneChoice===''`)
}
