package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gnotes/internal/db"
)

func TestSpellingTransferBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	for _, width := range []int{320, 1024} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			recoveryFixture(t)
			for i := 0; i < 65; i++ {
				mustRecoveryExec(t, "INSERT INTO spelling_entries VALUES(1,'word',?,?,'word','2026-10-06')", fmt.Sprintf("word%03d", i), fmt.Sprintf("word%03d", i))
				mustRecoveryExec(t, "INSERT INTO spelling_entries VALUES(1,'name',?,?,'person','2026-10-06')", fmt.Sprintf("person %03d", i), fmt.Sprintf("Person %03d", i))
			}
			mustRecoveryExec(t, "INSERT INTO spelling_entries VALUES(1,'name','new york','New York','place','2026-10-06'),(2,'word','privateword','privateword','word','2026-10-06')")
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
			var failImport atomic.Bool
			server := httptest.NewServer(securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/spelling/migrate" && failImport.Load() {
					http.Error(w, "Temporary import failure", 503)
					return
				}
				mux.ServeHTTP(w, r)
			})))
			defer server.Close()
			browser := newRecoveryBrowser(t)
			browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setDeviceMetricsOverride", "params": map[string]any{"width": width, "height": 800, "deviceScaleFactor": 1, "mobile": false}})
			click := func(selector string) {
				t.Helper()
				node := browser.call("POST", "/element", map[string]string{"using": "css selector", "value": selector}).(map[string]any)
				browser.call("POST", "/element/"+node["element-6066-11e4-a52e-4f735466cecf"].(string)+"/click", nil)
			}
			browser.navigate(server.URL)
			browser.wait(`!document.getElementById('auth-screen').hidden`)
			browser.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
			browser.wait(`!document.getElementById('app-shell').hidden&&spellingReady&&!spellingBusy`)
			browser.script(`document.getElementById('spelling-open').click();`)
			browser.wait(`document.getElementById('spelling-dialog').open&&!spellingBusy`)
			browser.script(`if(document.querySelectorAll('#spelling-ignored-list li').length!==50||document.querySelectorAll('#spelling-names-list li').length!==50)throw new Error('Large lists should show 50 entries');`)
			click("#spelling-names-more")
			browser.script(`if(document.querySelectorAll('#spelling-names-list li').length!==66)throw new Error('Show more failed');const n=document.getElementById('spelling-names-search');n.value='PLACE';n.dispatchEvent(new Event('input'));const w=document.getElementById('spelling-ignored-search');w.value='word064';w.dispatchEvent(new Event('input'));if(document.querySelectorAll('#spelling-names-list li').length!==1||!document.getElementById('spelling-names-list').textContent.includes('New York')||document.querySelectorAll('#spelling-ignored-list li').length!==1)throw new Error('Search by name type/word failed');document.querySelector('.spelling-transfer').open=true;`)
			// Imports merge atomically; duplicate names preserve their existing type.
			browser.script(`window.importPayload={format:'gnotes-spelling',version:1,words:['Importedword','importedword'],names:[{name:'new york',kind:'company'},{name:'Acme Labs',kind:'company'}]};const d=new DataTransfer();d.items.add(new File([JSON.stringify(importPayload)],'lists.json',{type:'application/json'}));document.getElementById('spelling-import-file').files=d.files;document.getElementById('spelling-import-form').requestSubmit();`)
			browser.wait(`!spellingBusy&&document.getElementById('spelling-transfer-status').textContent.startsWith('Import complete')`)
			browser.script(`if(ignoredSpellingWords.length!==66||spellingNames.length!==67||spellingNames.find(n=>n.name==='New York').kind!=='place'||!ignoredSpellingWords.includes('importedword'))throw new Error('Import replaced or duplicated existing entries');if(document.getElementById('spelling-import-file').files.length)throw new Error('Successful import should clear the file');`)
			// An invalid second entry rejects the whole file without changing lists.
			browser.script(`const d=new DataTransfer();d.items.add(new File([JSON.stringify({words:['unsaved'],names:[{name:'Bad Name',kind:'invalid'}]})],'invalid.json'));document.getElementById('spelling-import-file').files=d.files;document.getElementById('spelling-import-form').requestSubmit();`)
			browser.wait(`!spellingBusy&&document.getElementById('spelling-transfer-status').textContent.startsWith('Could not import')`)
			browser.script(`if(ignoredSpellingWords.includes('unsaved')||!document.getElementById('spelling-import-file').files.length)throw new Error('Invalid import partially saved or discarded the file');`)
			failImport.Store(true)
			browser.script(`const d=new DataTransfer();d.items.add(new File(['Tokyo\nKyoto\nTokyo'],'places.txt'));document.getElementById('spelling-import-type').value='place';document.getElementById('spelling-import-file').files=d.files;document.getElementById('spelling-import-form').requestSubmit();`)
			browser.wait(`!spellingBusy&&document.getElementById('spelling-transfer-status').textContent.includes('Temporary import failure')`)
			browser.script(`if(spellingNames.some(n=>n.name==='Tokyo')||!document.getElementById('spelling-import-file').files.length)throw new Error('Failed import changed names or discarded retry file');`)
			failImport.Store(false)
			browser.script(`document.getElementById('spelling-import-form').requestSubmit();`)
			browser.wait(`!spellingBusy&&spellingNames.length===69`)
			// Export downloads a fresh account snapshot, not only filtered rows.
			mustRecoveryExec(t, "INSERT INTO spelling_entries VALUES(1,'word','remoteaddition','remoteaddition','word','2026-10-06')")
			downloads := t.TempDir()
			browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Browser.setDownloadBehavior", "params": map[string]any{"behavior": "allow", "downloadPath": downloads}})
			click("#spelling-export")
			browser.wait(`!spellingBusy&&document.getElementById('spelling-transfer-status').textContent.startsWith('Exported')`)
			var exported []byte
			for i := 0; i < 100; i++ {
				files, _ := filepath.Glob(filepath.Join(downloads, "*.json"))
				if len(files) == 1 {
					exported, _ = os.ReadFile(files[0])
					if len(exported) > 0 {
						break
					}
				}
				time.Sleep(25 * time.Millisecond)
			}
			var data struct {
				Format  string `json:"format"`
				Version int    `json:"version"`
				spellingLists
			}
			if err := json.Unmarshal(exported, &data); err != nil {
				t.Fatalf("downloaded export: %v", err)
			}
			if data.Format != "gnotes-spelling" || data.Version != 1 || len(data.Words) != 67 || len(data.Names) != 69 || strings.Contains(string(exported), "privateword") {
				t.Fatalf("export incomplete or private: %+v", data)
			}
			// Empty searches do not affect export or account data; exported data round-trips.
			browser.script(fmt.Sprintf(`window.roundTrip=parseSpellingImport(%q,'lists.json','person');if(roundTrip.words.length!==67||roundTrip.names.length!==69)throw new Error('Export cannot round-trip');if(document.documentElement.scrollWidth>innerWidth||document.getElementById('spelling-import-file').getBoundingClientRect().right>document.getElementById('spelling-dialog').getBoundingClientRect().right)throw new Error('Transfer controls overflow');`, string(exported)))
			var foreign int
			db.DB.QueryRow("SELECT COUNT(*) FROM spelling_entries WHERE user_id=2").Scan(&foreign)
			if foreign != 1 {
				t.Fatal("Import changed another account")
			}
		})
	}
}
