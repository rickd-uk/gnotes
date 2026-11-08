package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"gnotes/internal/db"
)

func TestPasswordChangeBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	for _, width := range []int{320, 1024} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/mfa-%t", width, enabled), func(t *testing.T) {
				_, _, login, _ := recoveryFixture(t)
				codes := []string{"", "", ""}
				if enabled {
					_, state, _ := mfaTestEnable(t, login)
					codes = state.Codes
				}
				mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at) VALUES(1,'Kept note','Keep my note',?)", time.Now().UTC())
				mux := http.NewServeMux()
				registerDictionaryRoutes(mux)
				registerMFARoutes(mux)
				mux.HandleFunc("/api/auth/config", authConfigHandler)
				mux.HandleFunc("/api/auth/login", loginHandler)
				mux.HandleFunc("/api/auth/me", protect(meHandler, false))
				mux.HandleFunc("/api/auth/logout", protect(logoutHandler, true))
				mux.HandleFunc("/api/auth/password", protect(changePasswordHandler, true))
				mux.HandleFunc("/api/draft", protect(getDraftHandler, false))
				mux.HandleFunc("/api/draft/update", protect(updateDraftHandler, true))
				mux.HandleFunc("/api/notes/page", protect(pagedListNotesHandler, false))
				mux.HandleFunc("/api/notes/trash-page", protect(pagedTrashNotesHandler, false))
				mux.HandleFunc("/api/tags", protect(tagsHandler, false))
				mux.Handle("/", http.FileServer(http.Dir("../../public")))
				server := httptest.NewServer(securityHeaders(mux))
				defer server.Close()
				b := newRecoveryBrowser(t)
				b.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setDeviceMetricsOverride", "params": map[string]any{"width": width, "height": 850, "deviceScaleFactor": 1, "mobile": false}})
				b.navigate(server.URL)
				b.wait(`window.GnotesPassword && !document.getElementById('auth-screen').hidden`)
				b.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
				if enabled {
					b.wait(`!document.getElementById('auth-mfa-label').hidden && !document.getElementById('auth-submit').disabled`)
					b.script(`document.getElementById('auth-mfa-code').value='` + codes[0] + `';document.getElementById('auth-form').requestSubmit();`)
				}
				b.wait(`!document.getElementById('app-shell').hidden && !notesLoading && loadedNotes.size===1`)
				b.script(`openProfilePanel();`)
				b.wait(`document.getElementById('profile-joined').textContent!=='Loading…'`)
				b.script(`document.getElementById('password-open').click();`)
				b.wait(`document.getElementById('password-dialog').open && !document.getElementById('password-submit').disabled`)
				b.script(fmt.Sprintf(`if(document.getElementById('password-code-label').hidden===%t)throw new Error('Wrong MFA field visibility');const r=document.getElementById('password-dialog').getBoundingClientRect();if(r.left<0||r.right>innerWidth||r.top<0||r.bottom>innerHeight)throw new Error('Password dialog outside viewport');window.fillPassword=(current,next,confirm,code='')=>{document.getElementById('password-current').value=current;document.getElementById('password-new').value=next;document.getElementById('password-confirm').value=confirm;document.getElementById('password-code').value=code;};fillPassword('original password','changed inside gnotes','does not match','%s');document.getElementById('password-form').requestSubmit();`, enabled, codes[1]))
				b.wait(`document.getElementById('password-status').textContent.includes('do not match')`)
				b.script(`fillPassword('wrong password','changed inside gnotes','changed inside gnotes','` + codes[1] + `');document.getElementById('password-form').requestSubmit();`)
				b.wait(`document.getElementById('password-status').textContent.includes('Current password is incorrect') && !document.getElementById('password-submit').disabled`)
				b.script(`if(!currentUser||document.getElementById('password-current').value!=='wrong password')throw new Error('Wrong password ended session');document.getElementById('password-close').click();if(document.getElementById('password-current').value||document.getElementById('password-new').value)throw new Error('Cancel retained passwords');closeProfilePanel();`)
				b.wait(`Boolean(richDraft)`)
				b.script(`richDraft.editor.commands.setContent('<p>Draft survives password change</p>');clearTimeout(draftSaveTimer);openProfilePanel();`)
				b.wait(`document.getElementById('profile-joined').textContent!=='Loading…'`)
				b.script(`document.getElementById('password-open').click();`)
				b.wait(`document.getElementById('password-dialog').open && !document.getElementById('password-submit').disabled`)
				if width == 1024 && !enabled {
					b.script(`window.passwordRealFetch=fetch.bind(window);window.passwordWrites=0;window.fetch=(url,options={})=>{if(String(url).startsWith('/api/auth/password'))passwordWrites++;return String(url).startsWith('/api/draft/update')?Promise.resolve(new Response('',{status:503})):passwordRealFetch(url,options);};fillPassword('original password','changed inside gnotes','changed inside gnotes');document.getElementById('password-form').requestSubmit();`)
					b.wait(`document.getElementById('password-status').textContent.includes('Save your outstanding note changes') && !document.getElementById('password-submit').disabled`)
					b.script(`if(passwordWrites||draftVersion===draftSavedVersion)throw new Error('Failed draft save allowed password rotation');window.fetch=(url,options={})=>String(url).startsWith('/api/auth/password')?Promise.reject(new TypeError('Fixture offline')):passwordRealFetch(url,options);document.getElementById('password-form').requestSubmit();`)
					b.wait(`document.getElementById('password-status').textContent.includes('Could not confirm the change') && !document.getElementById('password-submit').disabled`)
					b.script(`if(!currentUser||document.getElementById('password-new').value!=='changed inside gnotes')throw new Error('Connection failure discarded form/session');window.fetch=passwordRealFetch;`)
				}
				b.script(`window.previousCSRF=csrfToken;fillPassword('original password','changed inside gnotes','changed inside gnotes','` + codes[1] + `');document.getElementById('password-form').requestSubmit();`)
				b.wait(`document.getElementById('password-status').textContent.includes('Password changed.') && !document.getElementById('password-close').disabled`)
				b.script(`if(csrfToken===previousCSRF||!currentUser||draftVersion!==draftSavedVersion)throw new Error('Session rotation or draft preservation failed');for(const id of ['password-current','password-new','password-confirm','password-code'])if(document.getElementById(id).value)throw new Error('Secret retained after success');for(const storage of [localStorage,sessionStorage])for(let i=0;i<storage.length;i++){const value=storage.getItem(storage.key(i));if(value.includes('changed inside gnotes')||value.includes('original password'))throw new Error('Password leaked into browser storage');}document.getElementById('password-close').click();closeProfilePanel();document.getElementById('logout-button').click();`)
				b.wait(`!document.getElementById('auth-screen').hidden`)
				b.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='changed inside gnotes';document.getElementById('auth-form').requestSubmit();`)
				if enabled {
					b.wait(`!document.getElementById('auth-mfa-label').hidden && !document.getElementById('auth-submit').disabled`)
					b.script(`document.getElementById('auth-mfa-code').value='` + codes[2] + `';document.getElementById('auth-form').requestSubmit();`)
				}
				b.wait(`!document.getElementById('app-shell').hidden && !notesLoading && loadedNotes.size===1 && currentDraft().content.includes('Draft survives password change')`)
				var content string
				if err := db.DB.QueryRow("SELECT content FROM notes WHERE user_id=1").Scan(&content); err != nil || content != "Keep my note" {
					t.Fatal("password change modified note data")
				}
			})
		}
	}
}
