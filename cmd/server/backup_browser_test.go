package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gnotes/internal/db"
)

func TestProfileBackupBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	for _, width := range []int{320, 1024} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			recoveryFixture(t)
			socket := filepath.Join(t.TempDir(), "control.sock")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			actions := make(chan map[string]any, 4)
			bridge := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/action" {
					var data map[string]any
					json.NewDecoder(r.Body).Decode(&data)
					actions <- data
					w.WriteHeader(202)
					w.Write([]byte(`{"message":"accepted"}`))
				} else {
					w.Write([]byte(`{"configured":false,"schedule":"Daily 04:15–04:30 Asia/Tokyo","message":"Fixture ready","snapshots":[]}`))
				}
			})}
			go bridge.Serve(listener)
			defer bridge.Close()
			t.Setenv("GNOTES_BACKUP_CONTROL_SOCKET", socket)

			mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at,archived_at,favorited,tags) VALUES(1,'Personal archive','Sample backup body',?,?,1,'[\"restore\"]')", time.Now(), time.Now())
			mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at) VALUES(2,'Other private note','Never in admin personal export',?)", time.Now())
			siteKey := filepath.Join(t.TempDir(), "recovery.env")
			if err := os.WriteFile(siteKey, []byte("RESTIC_PASSWORD=fixture-site-key\n"), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("GNOTES_SITE_BACKUP_RECOVERY_FILE", siteKey)
			mux := http.NewServeMux()
			registerSpellingRoutes(mux)
			mux.HandleFunc("/api/auth/config", authConfigHandler)
			mux.HandleFunc("/api/auth/login", loginHandler)
			mux.HandleFunc("/api/auth/logout", protect(logoutHandler, true))
			mux.HandleFunc("/api/auth/me", protect(meHandler, false))
			mux.HandleFunc("/api/draft", protect(getDraftHandler, false))
			mux.HandleFunc("/api/notes/page", protect(pagedListNotesHandler, false))
			mux.HandleFunc("/api/notes/trash-page", protect(pagedTrashNotesHandler, false))
			mux.HandleFunc("/api/notes/transfer-list", protect(transferListHandler, false))
			mux.HandleFunc("/api/notes/import", protect(importNotesHandler, true))
			mux.HandleFunc("/api/tags", protect(tagsHandler, false))
			mux.HandleFunc("/api/backups/download", protect(backupDownloadHandler, true))
			mux.HandleFunc("/api/admin/backups/status", protect(requireAdmin(backupControlStatusHandler), false))
			mux.HandleFunc("/api/admin/backups/action", protect(requireAdmin(backupControlActionHandler), true))

			mux.Handle("/", http.FileServer(http.Dir("../../public")))
			server := httptest.NewServer(securityHeaders(mux))
			defer server.Close()
			browser := newRecoveryBrowser(t)
			browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setDeviceMetricsOverride", "params": map[string]any{"width": width, "height": 800, "deviceScaleFactor": 1, "mobile": false}})
			downloads := t.TempDir()
			browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Browser.setDownloadBehavior", "params": map[string]any{"behavior": "allow", "downloadPath": downloads}})
			browser.navigate(server.URL)
			browser.wait(`!document.getElementById('auth-screen').hidden`)
			browser.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
			browser.wait(`!document.getElementById('app-shell').hidden&&!notesLoading`)
			browser.script(`document.getElementById('account-menu-username').click();`)
			browser.wait(`!document.getElementById('profile-site-backup').hidden&&!document.getElementById('profile-site-key').disabled`)

			browser.script(`document.getElementById('site-backup-admin').open=true;`)
			browser.wait(`document.getElementById('site-backup-message').textContent==='Fixture ready'`)
			browser.script(`document.getElementById('site-backup-wizard').open=true;`)
			browser.wait(`!document.querySelector('[data-backup-step="0"]').hidden`)
			browser.script(`document.getElementById('site-backup-bucket').value='fixture-bucket';document.getElementById('site-backup-prefix').value='gnotes/new';document.getElementById('site-backup-next').click();`)
			browser.wait(`!document.querySelector('[data-backup-step="1"]').hidden`)
			browser.script(`const policy=JSON.parse(document.getElementById('site-backup-policy').value);if(policy.Statement[2].Resource!=='arn:aws:s3:::fixture-bucket/gnotes/new/*')throw new Error('Wrong policy');document.getElementById('site-backup-next').click();document.getElementById('site-backup-access').value='TESTACCESSKEY123456';document.getElementById('site-backup-secret').value='FixtureSecretCredential123456';document.getElementById('site-backup-next').click();document.getElementById('site-backup-create').checked=true;document.getElementById('site-backup-setup').requestSubmit();`)

			browser.wait(`document.getElementById('site-backup-setup-feedback').textContent.includes('Enter your current gnotes password')`)
			browser.script(`if(!document.getElementById('site-backup-secret').value)throw new Error('Empty password discarded storage credentials');document.getElementById('site-backup-setup-password').value='wrong password';document.getElementById('site-backup-setup').requestSubmit();`)
			browser.wait(`document.getElementById('site-backup-setup-feedback').textContent==='Current password is incorrect'`)
			browser.script(`window.backupPollChecked=false;refreshBackupStatus().then(()=>{window.backupPollChecked=true;});`)
			browser.wait(`window.backupPollChecked&&document.getElementById('site-backup-setup-feedback').textContent==='Current password is incorrect'&&!document.getElementById('site-backup-test').disabled`)
			browser.script(`if(document.getElementById('site-backup-setup-feedback').dataset.tone!=='error')throw new Error('Missing error style');document.getElementById('site-backup-access').value='TESTACCESSKEY123456';document.getElementById('site-backup-secret').value='FixtureSecretCredential123456';document.getElementById('site-backup-setup-password').value='original password';document.getElementById('site-backup-setup').requestSubmit();`)
			browser.wait(`document.getElementById('site-backup-message').textContent==='Fixture ready'&&!document.getElementById('site-backup-test').disabled`)

			select {
			case action := <-actions:
				if action["action"] != "configure" || action["prefix"] != "gnotes/new" || action["allow_create"] != true || action["secret_key"] != "FixtureSecretCredential123456" {
					t.Fatalf("wizard action: %v", action["action"])
				}
				if _, ok := action["password"]; ok {
					t.Fatal("wizard forwarded login password")
				}
			case <-time.After(10 * time.Second):
				t.Fatal("wizard did not submit")
			}
			browser.script(`if(document.getElementById('site-backup-access').value||document.getElementById('site-backup-secret').value||document.getElementById('site-backup-setup-password').value)throw new Error('Credentials retained');if(document.documentElement.scrollWidth>innerWidth+1)throw new Error('Wizard page overflows');if(innerWidth>560&&document.getElementById('profile-panel').getBoundingClientRect().width<Math.min(1200,innerWidth-40))throw new Error('Profile modal too narrow');if(!document.querySelector('#site-backup-actions [value="enable"]').disabled||!document.querySelector('#site-backup-actions [value="run"]').disabled)throw new Error('Unconfigured controls enabled');for(const button of document.querySelectorAll('#site-backup-admin button'))if(!button.querySelector('svg'))throw new Error('Missing backup icon');const colors=['enable','run','pause','stop'].map(value=>getComputedStyle(document.querySelector('#site-backup-actions [value="'+value+'"]')).backgroundColor);if(new Set(colors).size!==4)throw new Error('Backup action colors missing');`)
			browser.script(`document.getElementById('site-backup-wizard').open=false;document.getElementById('site-backup-admin').open=false;`)
			browser.script(`document.getElementById('profile-backups').open=true;document.getElementById('profile-backup-password').value='wrong password';document.getElementById('profile-backup-form').requestSubmit(document.querySelector('[value="personal-key"]'));`)
			browser.wait(`document.getElementById('profile-backup-status').textContent==='Current password is incorrect'`)
			browser.script(`if(document.getElementById('app-shell').hidden||document.getElementById('profile-backup-password').value)throw new Error('Wrong password signed out or persisted');`)
			download := func(kind string) {
				t.Helper()
				browser.script(fmt.Sprintf(`document.getElementById('profile-backup-password').value='original password';document.getElementById('profile-backup-form').requestSubmit(document.querySelector('[value=%q]'));`, kind))
				browser.wait(`!document.getElementById('profile-backup-form').dataset.busy&&document.getElementById('profile-backup-status').textContent.includes('downloaded')`)
			}
			readDownload := func(pattern string) []byte {
				t.Helper()
				for i := 0; i < 100; i++ {
					files, _ := filepath.Glob(filepath.Join(downloads, pattern))
					if len(files) > 0 {
						body, _ := os.ReadFile(files[0])
						if len(body) > 0 {
							return body
						}
					}
					time.Sleep(25 * time.Millisecond)
				}
				t.Fatal("download did not finish: " + pattern)
				return nil
			}
			download("site-key")
			if string(readDownload("*.env")) != "RESTIC_PASSWORD=fixture-site-key\n" {
				t.Fatal("wrong site key download")
			}
			download("personal-key")
			key := readDownload("gnotes-rick-recovery-key.json")
			download("personal-backup")
			backup := readDownload("*.gnotes-backup")
			browser.script(fmt.Sprintf(`window.testBackup=new File([%q],'restore.gnotes-backup');window.testKey=new File([%q],'recovery.json');decryptPersonalBackup(testBackup,testKey).then(b=>b.text()).then(t=>window.restoredPersonal=JSON.parse(t));`, string(backup), string(key)))
			browser.wait(`window.restoredPersonal?.notes.length===1`)
			browser.script(fmt.Sprintf(`const damaged=JSON.parse(%q);damaged.ciphertext=(damaged.ciphertext[0]==='A'?'B':'A')+damaged.ciphertext.slice(1);decryptPersonalBackup(new File([JSON.stringify(damaged)],'damaged.gnotes-backup'),testKey).then(()=>window.tamperRejected=false,()=>window.tamperRejected=true);`, string(backup)))
			browser.wait(`window.tamperRejected===true`)
			browser.script(`if(restoredPersonal.notes[0].title!=='Personal archive'||!restoredPersonal.notes[0].favorited||restoredPersonal.notes[0].tags[0]!=='restore')throw new Error('Personal decrypt lost details');const r=document.getElementById('profile-backup-form').getBoundingClientRect();if(r.left<0||r.right>innerWidth||document.documentElement.scrollWidth>innerWidth)throw new Error('Profile backups overflow');`)
			mustRecoveryExec(t, "DELETE FROM notes WHERE user_id=1")
			browser.script(`document.getElementById('transfer-open').click();`)
			browser.wait(`document.getElementById('transfer-note-list').textContent==='No notes to export.'`)
			browser.script(`const d=new DataTransfer();d.items.add(testBackup);document.getElementById('import-file').files=d.files;document.getElementById('import-file').dispatchEvent(new Event('change'));const k=new DataTransfer();k.items.add(testKey);document.getElementById('import-recovery-key').files=k.files;document.getElementById('import-start').click();`)
			browser.wait(`document.getElementById('transfer-status').textContent.startsWith('Imported 1 note')`)
			var restored int
			if err := db.DB.QueryRow("SELECT COUNT(*) FROM notes WHERE user_id=1 AND title='Personal archive' AND archived_at IS NOT NULL AND favorited=1").Scan(&restored); err != nil || restored != 1 {
				t.Fatal("encrypted backup import failed")
			}
			// Pause decryption, then really switch accounts. The pending import must
			// not send the old account's decrypted notes under the new session.
			browser.script(`window.savedDecrypt=decryptPersonalBackup;decryptPersonalBackup=(...args)=>new Promise(resolve=>{window.releaseDecrypt=async()=>resolve(await savedDecrypt(...args));});const d=new DataTransfer();d.items.add(testBackup);document.getElementById('import-file').files=d.files;document.getElementById('import-file').dispatchEvent(new Event('change'));const k=new DataTransfer();k.items.add(testKey);document.getElementById('import-recovery-key').files=k.files;document.getElementById('import-start').click();`)
			browser.wait(`typeof window.releaseDecrypt==='function'`)
			browser.script(`document.getElementById('logout-button').click();`)
			browser.wait(`!document.getElementById('auth-screen').hidden`)
			browser.script(`document.getElementById('auth-username').value='other';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
			browser.wait(`!document.getElementById('app-shell').hidden&&!notesLoading`)
			browser.script(`decryptPersonalBackup=savedDecrypt;releaseDecrypt();`)
			browser.wait(`document.getElementById('transfer-status').textContent.includes('Your session changed')`)
			var crossed int
			if err := db.DB.QueryRow("SELECT COUNT(*) FROM notes WHERE user_id=2 AND title='Personal archive'").Scan(&crossed); err != nil || crossed != 0 {
				t.Fatal("pending import crossed accounts")
			}
			browser.script(`document.getElementById('account-menu-username').click();`)
			browser.wait(`document.getElementById('profile-heading').textContent==='other'&&document.getElementById('profile-active-notes').textContent==='1'`)
			browser.script(`if(!document.getElementById('profile-site-backup').hidden)throw new Error('Regular user sees whole-site key control');document.getElementById('profile-backups').open=true;`)
			download("personal-key")
			otherKey := readDownload("gnotes-other-recovery-key.json")
			browser.script(fmt.Sprintf(`decryptPersonalBackup(testBackup,new File([%q],'other-key.json')).then(()=>window.wrongKeyRejected=false,e=>window.wrongKeyRejected=e.message.includes('does not match'));`, string(otherKey)))
			browser.wait(`window.wrongKeyRejected===true`)
			var regularKey personalRecoveryKey
			if err := json.Unmarshal(otherKey, &regularKey); err != nil || regularKey.Username != "other" {
				t.Fatal("wrong regular-user recovery key")
			}
		})
	}
}
