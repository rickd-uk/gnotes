package main

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"gnotes/internal/db"
)

func TestMFABrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	for _, width := range []int{320, 1024} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			recoveryFixture(t)
			mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at) VALUES(1,'Kept during MFA','My note remains here',?)", time.Now().UTC())
			mux := http.NewServeMux()
			registerDictionaryRoutes(mux)
			registerMFARoutes(mux)
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
			b := newRecoveryBrowser(t)
			b.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setDeviceMetricsOverride", "params": map[string]any{"width": width, "height": 850, "deviceScaleFactor": 1, "mobile": false}})
			b.navigate(server.URL)
			b.wait(`window.GnotesMFA && !document.getElementById('auth-screen').hidden`)
			b.script(`if(document.getElementById('auth-mfa-label').getClientRects().length)throw new Error('MFA field leaked into initial sign-in');document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
			b.wait(`!document.getElementById('app-shell').hidden && !notesLoading && loadedNotes.size===1`)
			b.script(`openProfilePanel();`)
			b.wait(`document.getElementById('profile-joined').textContent!=='Loading…'`)
			b.script(`document.getElementById('mfa-open').click();`)
			b.wait(`document.getElementById('mfa-dialog').open && !document.getElementById('mfa-setup-form').hidden && !document.getElementById('mfa-close').disabled`)
			b.script(`const dialog=document.getElementById('mfa-dialog'),r=dialog.getBoundingClientRect();if(r.left<0||r.right>innerWidth||r.top<0||r.bottom>innerHeight||dialog.scrollWidth>dialog.clientWidth)throw new Error('MFA settings overflow');`)
			b.script(`window.realMFAViewport=visualViewport;Object.defineProperty(window,'visualViewport',{configurable:true,value:{offsetTop:40,offsetLeft:0,width:innerWidth,height:400}});syncFocusedEditViewport();const r=document.getElementById('mfa-dialog').getBoundingClientRect();if(r.top<40||r.bottom>440)throw new Error('MFA dialog is behind the keyboard');Object.defineProperty(window,'visualViewport',{configurable:true,value:realMFAViewport});syncFocusedEditViewport();`)
			// Preview only the non-secret setup screen.
			encoded := b.call("GET", "/screenshot", nil).(string)
			data, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(fmt.Sprintf("/tmp/gnotes-mfa-settings-%d.png", width), data, 0600); err != nil {
				t.Fatal(err)
			}
			b.script(`document.getElementById('mfa-setup-password').value='wrong password';document.getElementById('mfa-setup-form').requestSubmit();`)
			b.wait(`document.getElementById('mfa-status').dataset.error==='true' && !document.getElementById('mfa-close').disabled`)
			b.script(`if(!document.getElementById('mfa-enable-form').hidden)throw new Error('Incorrect password exposed setup');document.getElementById('mfa-setup-password').value='original password';document.getElementById('mfa-setup-form').requestSubmit();`)
			b.wait(`!document.getElementById('mfa-enable-form').hidden && document.getElementById('mfa-qr').naturalWidth>0 && !document.getElementById('mfa-close').disabled`)
			b.script(`const qr=document.getElementById('mfa-qr'),r=qr.getBoundingClientRect();if(!qr.src.startsWith('data:image/png;base64,')||r.left<0||r.right>innerWidth||document.getElementById('mfa-setup-password').value)throw new Error('Setup QR/password handling failed');`)
			var secret string
			if err := db.DB.QueryRow("SELECT secret FROM mfa_factors WHERE user_id=1").Scan(&secret); err != nil {
				t.Fatal(err)
			}
			code, _ := totp.GenerateCode(secret, time.Now())
			b.script(`window.beforeMFAcsrf=csrfToken;window.realMFAFetch=fetch.bind(window);window.fetch=(url,options)=>url==='/api/auth/mfa/enable'?new Promise(resolve=>window.releaseMFAEnable=()=>realMFAFetch(url,options).then(resolve)):realMFAFetch(url,options);document.getElementById('mfa-enable-code').value='` + code + `';document.getElementById('mfa-enable-form').requestSubmit();`)
			b.wait(`typeof releaseMFAEnable==='function'`)
			b.script(`document.getElementById('mfa-dialog').dispatchEvent(new Event('cancel',{cancelable:true}));if(!document.getElementById('mfa-dialog').open||!document.getElementById('mfa-close').disabled)throw new Error('MFA change could be dismissed in flight');releaseMFAEnable();`)
			b.wait(`document.querySelectorAll('#mfa-recovery-codes li').length===10 && csrfToken!==beforeMFAcsrf`)
			b.script(`window.fetch=realMFAFetch;if(!document.getElementById('mfa-close').disabled||document.getElementById('mfa-secret').value||document.getElementById('mfa-qr').hasAttribute('src')||loadedNotes.size!==1)throw new Error('Activation lost data or retained setup secret');window.privateCodes=[...document.querySelectorAll('#mfa-recovery-codes li')].map(n=>n.textContent);if(Object.values(localStorage).concat(Object.values(sessionStorage)).some(value=>privateCodes.some(code=>value.includes(code))))throw new Error('Recovery codes persisted in browser storage');apiFetch('/api/auth/me').then(r=>window.rotatedSessionWorks=r.ok);`)
			b.wait(`window.rotatedSessionWorks===true`)
			codes := b.script(`return [...document.querySelectorAll('#mfa-recovery-codes li')].map(n=>n.textContent);`).([]any)
			first, second := codes[0].(string), codes[1].(string)
			b.script(`document.getElementById('mfa-codes-saved').checked=true;document.getElementById('mfa-codes-saved').dispatchEvent(new Event('change'));document.getElementById('mfa-close').click();`)
			b.wait(`!document.getElementById('mfa-dialog').open && document.querySelectorAll('#mfa-recovery-codes li').length===0`)
			b.script(`document.getElementById('logout-button').click();`)
			b.wait(`!document.getElementById('auth-screen').hidden && !document.getElementById('auth-submit').disabled`)
			b.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
			b.wait(`!document.getElementById('auth-mfa-label').hidden && !document.getElementById('auth-submit').disabled`)
			b.script(`if(!document.getElementById('app-shell').hidden||document.activeElement.id!=='auth-mfa-code')throw new Error('Password alone entered account');fetch('/api/auth/me').then(r=>window.passwordOnlyStatus=r.status);document.getElementById('auth-mfa-code').value='invalid';document.getElementById('auth-form').requestSubmit();`)
			b.wait(`window.passwordOnlyStatus===401 && document.getElementById('auth-status').textContent.includes('Invalid') && !document.getElementById('auth-submit').disabled`)
			b.script(`document.getElementById('auth-mfa-code').value='` + first + `';document.getElementById('auth-form').requestSubmit();`)
			b.wait(`!document.getElementById('app-shell').hidden && loadedNotes.size===1 && !notesLoading`)
			b.script(`if(document.getElementById('auth-password').value||document.getElementById('auth-mfa-code').value||!document.getElementById('auth-mfa-label').hidden)throw new Error('Sign-in credentials not cleared');GnotesMFA.open();`)
			b.wait(`!document.getElementById('mfa-manage-form').hidden && !document.getElementById('mfa-close').disabled`)
			b.script(`if(!document.getElementById('mfa-summary').textContent.includes('9 recovery codes'))throw new Error('Recovery code not consumed');document.getElementById('mfa-manage-password').value='original password';document.getElementById('mfa-manage-code').value='` + second + `';document.getElementById('mfa-manage-form').requestSubmit(document.querySelector('#mfa-manage-form button[value="recovery-codes"]'));`)
			b.wait(`document.querySelectorAll('#mfa-recovery-codes li').length===10 && !document.getElementById('mfa-status').textContent.includes('Working')`)
			newCode := b.script(`return document.querySelector('#mfa-recovery-codes li').textContent;`).(string)
			if strings.EqualFold(newCode, first) {
				t.Fatal("codes were not regenerated")
			}
			b.script(`document.getElementById('mfa-codes-saved').checked=true;document.getElementById('mfa-codes-saved').dispatchEvent(new Event('change'));document.getElementById('mfa-close').click();`)
			b.wait(`!document.getElementById('mfa-dialog').open`)
			b.script(`GnotesMFA.open();`)
			b.wait(`!document.getElementById('mfa-manage-form').hidden && !document.getElementById('mfa-close').disabled`)
			b.script(`document.getElementById('mfa-manage-password').value='original password';document.getElementById('mfa-manage-code').value='` + newCode + `';document.getElementById('mfa-manage-form').requestSubmit(document.querySelector('.mfa-disable'));`)
			b.wait(`document.getElementById('mfa-summary').textContent==='Not enabled' && !document.getElementById('mfa-close').disabled`)
			b.script(`document.getElementById('mfa-close').click();document.getElementById('logout-button').click();`)
			b.wait(`!document.getElementById('auth-screen').hidden`)
			b.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
			b.wait(`!document.getElementById('app-shell').hidden && loadedNotes.size===1 && !notesLoading`)
		})
	}
}
