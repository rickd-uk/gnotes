package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestOfflineReadingBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	recoveryFixture(t)
	mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at) VALUES(1,'Offline active',?,?)", "Private offline text <img src=x onerror=alert(1)>", time.Now())
	mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at,archived_at,favorited) VALUES(1,'Offline archive','Archived private text',?,?,1)", time.Now(), time.Now())
	mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at,deleted_at) VALUES(1,'Recycled secret','Never saved offline',?,?)", time.Now(), time.Now())
	mux := http.NewServeMux()
	registerDictionaryRoutes(mux)
	mux.HandleFunc("/api/auth/config", authConfigHandler)
	mux.HandleFunc("/api/auth/login", loginHandler)
	mux.HandleFunc("/api/auth/logout", protect(logoutHandler, true))
	mux.HandleFunc("/api/auth/me", protect(meHandler, false))
	mux.HandleFunc("/api/draft", protect(getDraftHandler, false))
	mux.HandleFunc("/api/notes/page", protect(pagedListNotesHandler, false))
	mux.HandleFunc("/api/notes/trash-page", protect(pagedTrashNotesHandler, false))
	mux.HandleFunc("/api/notes/offline", protect(offlineSnapshotHandler, false))
	mux.HandleFunc("/api/tags", protect(tagsHandler, false))
	mux.Handle("/", http.FileServer(http.Dir("../../public")))
	var disconnected atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// CDP's page network emulation does not disconnect the separate worker
		// target. Close server connections too, so its fetch also sees a real failure.
		if disconnected.Load() {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		securityHeaders(mux).ServeHTTP(w, r)
	}))
	defer server.Close()
	browser := newRecoveryBrowser(t)
	browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setDeviceMetricsOverride", "params": map[string]any{"width": 360, "height": 800, "deviceScaleFactor": 1, "mobile": false}})
	browser.navigate(server.URL)
	browser.wait(`!document.getElementById('auth-screen').hidden`)
	browser.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
	browser.wait(`!notesLoading && spellingReady && !document.getElementById('app-shell').hidden`)
	browser.script(`if(!document.querySelector('a[href="/offline.html"]'))throw new Error('Offline reading missing from account menu');`)
	browser.navigate(server.URL + "/offline.html")
	browser.wait(`!document.getElementById('offline-setup').hidden`)
	browser.script(`document.getElementById('offline-new-passphrase').value='test offline passphrase';document.getElementById('offline-confirm-passphrase').value='test offline passphrase';document.getElementById('offline-consent').checked=true;document.getElementById('offline-save-form').requestSubmit();`)
	browser.wait(`document.getElementById('offline-status').textContent.startsWith('2 notes saved')`)
	browser.wait(`navigator.serviceWorker.controller!==null`)
	browser.script(`GnotesOfflineStore.get().then(record=>{window.encryptedRecord=record;window.cipherOnly=record.cipher instanceof ArrayBuffer&&Object.keys(record).sort().join(',')==='cipher,iv,salt,version'&&!new TextDecoder().decode(record.cipher).includes('Private offline text');});caches.keys().then(async names=>{window.cachedPaths=[];for(const name of names){for(const request of await(await caches.open(name)).keys())cachedPaths.push(new URL(request.url).pathname);}});`)
	browser.wait(`window.cipherOnly===true && window.cachedPaths?.length===4`)
	browser.script(`if(cachedPaths.some(path=>!['/offline.html','/offline.css','/offline-store.js','/offline-reader.js'].includes(path)))throw new Error('Private app or API response cached');document.getElementById('offline-passphrase').value='wrong passphrase';document.getElementById('offline-unlock-form').requestSubmit();`)
	browser.wait(`document.getElementById('offline-status').textContent.startsWith('Could not unlock')`)
	browser.script(`if(!document.getElementById('offline-reading').hidden)throw new Error('Wrong passphrase exposed notes');document.getElementById('offline-passphrase').value='test offline passphrase';document.getElementById('offline-unlock-form').requestSubmit();`)
	browser.wait(`document.querySelectorAll('#offline-notes article').length===2`)
	browser.script(`if(document.querySelector('#offline-notes img')||!document.getElementById('offline-notes').textContent.includes('<img src=x onerror=alert(1)>')||document.getElementById('offline-notes').textContent.includes('Recycled secret')||document.querySelector('#offline-reading textarea,[contenteditable=true]'))throw new Error('Unsafe or editable offline content');document.getElementById('offline-search').value='Archived private';document.getElementById('offline-search').dispatchEvent(new Event('input'));if(document.querySelectorAll('#offline-notes article').length!==1)throw new Error('Offline search failed');document.getElementById('offline-lock').click();`)
	disconnected.Store(true)
	// Navigation to the normal app must load the cached, locked reader without a network.
	browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Network.enable", "params": map[string]any{}})
	browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Network.emulateNetworkConditions", "params": map[string]any{"offline": true, "latency": 0, "downloadThroughput": 0, "uploadThroughput": 0}})
	browser.navigate(server.URL)
	browser.wait(`document.getElementById('offline-unlock-form') && !document.getElementById('offline-unlock-form').hidden`)
	browser.script(`if(!document.getElementById('offline-reading').hidden)throw new Error('Offline copy unlocked after reload');document.getElementById('offline-passphrase').value='test offline passphrase';document.getElementById('offline-unlock-form').requestSubmit();`)
	browser.wait(`document.querySelectorAll('#offline-notes article').length===2`)
	disconnected.Store(false)
	browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Network.emulateNetworkConditions", "params": map[string]any{"offline": false, "latency": 0, "downloadThroughput": -1, "uploadThroughput": -1}})
	browser.navigate(server.URL + "/offline.html")
	browser.wait(`!document.getElementById('offline-refresh').hidden`)
	// Failed refresh must leave the previously encrypted snapshot readable.
	browser.script(`window.originalFetch=fetch.bind(window);window.fetch=(url,options)=>url==='/api/notes/offline'?Promise.resolve(new Response('',{status:503})):originalFetch(url,options);document.getElementById('offline-refresh').click();document.getElementById('offline-new-passphrase').value='test offline passphrase';document.getElementById('offline-confirm-passphrase').value='test offline passphrase';document.getElementById('offline-consent').checked=true;document.getElementById('offline-save-form').requestSubmit();`)
	browser.wait(`document.getElementById('offline-status').textContent.startsWith('Could not prepare')`)
	browser.script(`GnotesOfflineStore.get().then(r=>GnotesOfflineStore.decrypt(r,'test offline passphrase')).then(s=>window.oldCopyIntact=s.notes.length===2);`)
	browser.wait(`window.oldCopyIntact===true`)
	browser.script(`window.fetch=originalFetch;document.getElementById('offline-refresh').click();document.getElementById('offline-new-passphrase').value='wrong offline passphrase';document.getElementById('offline-confirm-passphrase').value='wrong offline passphrase';document.getElementById('offline-consent').checked=true;document.getElementById('offline-save-form').requestSubmit();`)
	browser.wait(`document.getElementById('offline-status').textContent.startsWith('Use the existing offline passphrase')`)
	// Refresh removes notes recycled since the saved snapshot, without changing the server.
	mustRecoveryExec(t, "UPDATE notes SET deleted_at=? WHERE title='Offline active'", time.Now())
	browser.script(`document.getElementById('offline-new-passphrase').value='test offline passphrase';document.getElementById('offline-confirm-passphrase').value='test offline passphrase';document.getElementById('offline-consent').checked=true;document.getElementById('offline-save-form').requestSubmit();`)
	browser.wait(`document.getElementById('offline-status').textContent.startsWith('1 note saved')`)
	browser.script(`document.getElementById('offline-passphrase').value='test offline passphrase';document.getElementById('offline-unlock-form').requestSubmit();`)
	browser.wait(`document.querySelectorAll('#offline-notes article').length===1`)
	browser.script(`const channel=new BroadcastChannel('gnotes-offline');channel.postMessage('lock');channel.close();`)
	browser.wait(`document.getElementById('offline-reading').hidden && document.getElementById('offline-notes').childElementCount===0`)

	browser.navigate(server.URL)
	browser.wait(`!notesLoading && !document.getElementById('app-shell').hidden`)
	browser.script(`window.oldRevision=GnotesOfflineStore.revision();GnotesOfflineStore.get().then(r=>{window.beforeSignout=r;document.getElementById('logout-button').click();});`)
	browser.wait(`!document.getElementById('auth-screen').hidden`)
	browser.script(`GnotesOfflineStore.get().then(r=>window.offlineRemoved=r===undefined);`)
	browser.wait(`window.offlineRemoved===true`)
	browser.script(`GnotesOfflineStore.save(beforeSignout,oldRevision).then(()=>window.staleSaveRejected=false,()=>window.staleSaveRejected=true);`)
	browser.wait(`window.staleSaveRejected===true`)

}
