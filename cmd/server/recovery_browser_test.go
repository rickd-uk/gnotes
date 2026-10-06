package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Run with a local chromedriver on port 19515 and GNOTES_BROWSER_CHECK=1.
// Brevo calls use the mocked transport from recoveryFixture; no real email is sent.
func TestRecoveryBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	for _, width := range []int{320, 1024} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			s, messages, _, _ := recoveryFixture(t)
			checklist := "Markdown not rendering accurately\n\n## Progress\n\n- [ ] Display Note Title in browser tab as 'gnotes - NOTE_TITLE'\n- [ ] Add favorites feature\n- [ ] Add tags feature\n\n**Status:** going well.\n\n```go\nfunc main() {}\n```\n\n```\nplain block\n```"
			mustRecoveryExec(t, "INSERT INTO notes (user_id,title,content,created_at) VALUES (1,'Body note',?,?)", checklist, time.Now().UTC())
			mustRecoveryExec(t, "INSERT INTO notes (user_id,title,content,created_at) VALUES (1,'Title only','',?)", time.Now().UTC().Add(-time.Minute))
			mux := http.NewServeMux()
			mux.HandleFunc("/api/auth/config", authConfigHandler)
			mux.HandleFunc("/api/auth/login", loginHandler)
			mux.HandleFunc("/api/auth/me", protect(meHandler, false))
			mux.HandleFunc("/api/auth/logout", protect(logoutHandler, true))
			mux.HandleFunc("/api/draft", protect(getDraftHandler, false))
			mux.HandleFunc("/api/notes/page", protect(pagedListNotesHandler, false))
			mux.HandleFunc("/api/notes/search-page", protect(pagedSearchNotesHandler, false))
			mux.HandleFunc("/api/notes/tags", protect(updateNoteTagsHandler, true))
			mux.HandleFunc("/api/tags", protect(tagsHandler, false))
			mux.HandleFunc("/api/notes/update", protect(updateNoteHandler, true))
			mux.HandleFunc("/api/notes/trash-page", protect(pagedTrashNotesHandler, false))
			mux.HandleFunc("/api/auth/recovery/request", s.requestReset)
			mux.HandleFunc("/api/auth/recovery/reset", s.resetPassword)
			mux.HandleFunc("/api/auth/email/request", protect(s.requestVerification, true))
			mux.HandleFunc("/api/auth/email/verify", s.verifyEmail)
			mux.Handle("/", http.FileServer(http.Dir("../../public")))
			server := httptest.NewServer(securityHeaders(mux))
			t.Cleanup(server.Close)
			s.config.PublicURL = server.URL
			t.Setenv("GNOTES_PUBLIC_URL", server.URL)
			ctx, cancel := context.WithCancel(context.Background())
			s.start(ctx)
			t.Cleanup(func() { cancel(); s.wg.Wait() })

			browser := newRecoveryBrowser(t)
			browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setDeviceMetricsOverride", "params": map[string]any{"width": width, "height": 800, "deviceScaleFactor": 1, "mobile": false}})
			browser.navigate(server.URL)
			browser.wait(`!document.getElementById('auth-screen').hidden && !document.getElementById('forgot-password').hidden`)
			brand := browser.script(`const s=getComputedStyle(document.querySelector('.auth-card h1')); return [s.fontFamily,s.color,s.fontWeight,s.textShadow].join('|');`).(string)
			browser.script(`document.getElementById('auth-username').value='rick'; document.getElementById('auth-password').value='original password'; document.getElementById('auth-form').requestSubmit();`)
			browser.wait(`!document.getElementById('app-shell').hidden && document.querySelectorAll('.note-card').length===2`)
			browser.script(`const select=document.getElementById('note-time-zone'); select.value='Asia/Tokyo'; select.dispatchEvent(new Event('change',{bubbles:true}));`)
			browser.wait(`!notesLoading && noteTimeZone()==='Asia/Tokyo' && formatNoteTime('2025-01-01T08:27:00Z')==='17:27'`)
			browser.script(`const select=document.getElementById('note-time-zone'); select.value='UTC'; select.dispatchEvent(new Event('change',{bubbles:true}));`)
			browser.wait(`!notesLoading && noteTimeZone()==='UTC' && formatNoteTime('2025-01-01T08:27:00Z')==='8:27' && formatDateSection('2025-01-01T08:27:00Z',new Date('2026-01-01T00:00:00Z')).includes('25')`)
			browser.navigate(server.URL)
			browser.wait(`!document.getElementById('app-shell').hidden && !notesLoading && document.getElementById('note-time-zone').value==='UTC' && document.querySelectorAll('.note-card').length===2`)
			mainBrand := browser.script(`const s=getComputedStyle(document.querySelector('.site-header h1')); return [s.fontFamily,s.color,s.fontWeight,s.textShadow].join('|');`).(string)
			if brand != mainBrand {
				t.Fatal("sign-in and main-screen branding differ")
			}
			browser.wait(`document.documentElement.scrollWidth<=innerWidth`)
			browser.script(`const bodyHeading=document.querySelector('.note-content h2'), title=document.querySelector('.has-roll-control .note-view > h2'); if(getComputedStyle(bodyHeading).position!=='static' || getComputedStyle(bodyHeading).textAlign!=='left' || bodyHeading.getBoundingClientRect().top<title.getBoundingClientRect().bottom) throw new Error('Markdown heading overlaps the note title');`)
			points := browser.script(`const p=document.querySelector('.note-content p'); p.scrollIntoView({block:'center'}); const text=p.firstChild; const range=document.createRange(); range.setStart(text,1); range.setEnd(text,8); const r=range.getBoundingClientRect(); return [r.left,r.right,r.top+r.height/2];`).([]any)
			browser.call("POST", "/actions", map[string]any{"actions": []any{map[string]any{
				"type": "pointer", "id": "select-note-text", "parameters": map[string]string{"pointerType": "mouse"},
				"actions": []any{
					map[string]any{"type": "pointerMove", "duration": 0, "x": int(points[0].(float64)), "y": int(points[2].(float64))},
					map[string]any{"type": "pointerDown", "button": 0},
					map[string]any{"type": "pointerMove", "duration": 300, "x": int(points[1].(float64)), "y": int(points[2].(float64))},
					map[string]any{"type": "pointerUp", "button": 0},
				},
			}}})
			browser.wait(`window.getSelection().toString().trim().length>0 && !document.documentElement.classList.contains('note-edit-focus')`)
			browser.script(`window.getSelection().removeAllRanges(); window.scrollTo(0,0);`)
			browser.script(`window.tagNoteID=Number(document.querySelector('.has-roll-control').id.slice(5)); document.querySelector('.has-roll-control .note-actions-toggle').click();`)
			browser.script(`const r=document.querySelector('.has-roll-control .note-tools').getBoundingClientRect(); if(r.left<0 || r.right>innerWidth) throw new Error('Note tools overflow with Tags action'); document.querySelector('.has-roll-control button[aria-label="Edit note tags"]').click();`)
			browser.wait(`document.getElementById('tags-dialog').open`)
			browser.script(`const cancel=document.getElementById('tags-cancel'), save=document.querySelector('#tags-form button[type=submit]'); if(save.textContent!=='Save' || getComputedStyle(cancel).backgroundColor===getComputedStyle(save).backgroundColor) throw new Error('Tag actions need distinct styling and the Save label'); document.getElementById('note-tags-input').value='discarded';`)
			browser.call("POST", "/actions", map[string]any{"actions": []any{map[string]any{
				"type": "pointer", "id": "dismiss-tags", "parameters": map[string]string{"pointerType": "mouse"},
				"actions": []any{map[string]any{"type": "pointerMove", "duration": 0, "x": 8, "y": 8}, map[string]any{"type": "pointerDown", "button": 0}, map[string]any{"type": "pointerUp", "button": 0}},
			}}})
			browser.wait(`!document.getElementById('tags-dialog').open && document.querySelectorAll('.note-tag').length===0`)
			browser.script(`openNoteTags(window.tagNoteID);`)
			browser.wait(`document.getElementById('tags-dialog').open && document.getElementById('note-tags-input').value===''`)
			browser.script(`document.getElementById('note-tags-input').value=Array.from({length:10},(_,i)=>'tag-'+i).join(', '); document.getElementById('tags-form').requestSubmit();`)
			browser.wait(`!document.getElementById('tags-dialog').open && !notesLoading && document.querySelectorAll('.note-tag:not([hidden])').length===3 && document.querySelector('.note-tags-toggle')?.textContent==='+7 more'`)
			browser.script(`document.querySelector('.note-tags-toggle').click(); if(document.querySelectorAll('.note-tag:not([hidden])').length!==10 || document.querySelector('.note-tags-toggle').getAttribute('aria-expanded')!=='true' || document.getElementById('search-tag').value!=='' || document.documentElement.classList.contains('note-edit-focus')) throw new Error('Expansion must only reveal tags'); document.querySelector('.note-tags-toggle').click(); if(document.querySelectorAll('.note-tag:not([hidden])').length!==3 || document.querySelector('.note-tags-toggle').getAttribute('aria-expanded')!=='false') throw new Error('Collapse must restore three tags'); document.querySelector('.note-tags-toggle').click(); document.querySelector('.note-tag[data-tag="tag-9"]').click();`)
			browser.wait(`!notesLoading && document.querySelectorAll('.note-card').length===1 && document.getElementById('search-tag').value==='tag-9' && document.documentElement.scrollWidth<=innerWidth`)
			browser.script(`document.getElementById('calendar-clear').click();`)
			browser.wait(`!notesLoading && document.querySelectorAll('.note-card').length===2 && document.getElementById('search-tag').value===''`)
			browser.script(`openNoteTags(window.tagNoteID);`)
			browser.wait(`document.getElementById('tags-dialog').open && document.getElementById('note-tags-input').value.split(',').length===10`)
			browser.script(`document.getElementById('note-tags-input').value='Work, ideas, WORK'; document.getElementById('tags-form').requestSubmit();`)
			browser.wait(`!document.getElementById('tags-dialog').open && !notesLoading && document.querySelectorAll('.note-tag').length===2 && !document.querySelector('.note-tags-toggle') && document.documentElement.scrollWidth<=innerWidth`)
			browser.script(`document.querySelector('.note-tag[data-tag="work"]').click();`)
			browser.wait(`!notesLoading && document.querySelectorAll('.note-card').length===1 && document.getElementById('search-tag').value==='work' && !document.getElementById('calendar-clear').hidden && !document.documentElement.classList.contains('note-edit-focus')`)
			browser.navigate(server.URL)
			browser.wait(`!notesLoading && !document.getElementById('app-shell').hidden && document.querySelectorAll('.note-card').length===1 && document.getElementById('search-tag').value==='work'`)
			browser.script(`window.tagNoteID=Number(document.querySelector('.note-card').id.slice(5));`)
			browser.script(`document.getElementById('search-query').value='no such text'; document.getElementById('search-query').dispatchEvent(new Event('input',{bubbles:true}));`)
			browser.wait(`!notesLoading && document.querySelectorAll('.note-card').length===0 && document.getElementById('search-tag').value==='work'`)
			browser.script(`document.getElementById('search-query').value=''; document.getElementById('search-query').dispatchEvent(new Event('input',{bubbles:true}));`)
			browser.wait(`!notesLoading && document.querySelectorAll('.note-card').length===1 && document.getElementById('search-tag').value==='work'`)
			browser.script(`openNoteTags(window.tagNoteID); document.getElementById('note-tags-input').value='ideas, reading'; document.getElementById('tags-form').requestSubmit();`)
			browser.wait(`!document.getElementById('tags-dialog').open && !notesLoading && document.querySelectorAll('.note-card').length===0`)
			browser.script(`document.getElementById('calendar-clear').click();`)
			browser.wait(`!notesLoading && document.querySelectorAll('.note-card').length===2 && document.getElementById('search-tag').value===''`)
			browser.script(`openNoteTags(window.tagNoteID); document.getElementById('note-tags-input').value=''; document.getElementById('tags-form').requestSubmit();`)
			browser.wait(`!document.getElementById('tags-dialog').open && !notesLoading && document.querySelectorAll('.note-tag').length===0`)
			browser.wait(`document.querySelectorAll('.note-content input[type=checkbox]').length===3`)
			browser.script(`for (const checkbox of document.querySelectorAll('.note-content input[type=checkbox]')) { const li=checkbox.closest('li'); const walker=document.createTreeWalker(li, NodeFilter.SHOW_TEXT); let text; while (text=walker.nextNode()) { if (text.textContent.trim()) break; } const range=document.createRange(); range.setStart(text, text.textContent.search(/\S/)); range.setEnd(text, text.textContent.search(/\S/)+1); const a=checkbox.getBoundingClientRect(), b=range.getBoundingClientRect(); if (a.top>=b.bottom || b.top>=a.bottom || getComputedStyle(li).listStyleType!=='none') throw new Error('Checklist checkbox and text must share a line without a bullet'); }`)
			browser.script(`const id=Number(document.querySelector('.note-card').id.replace('note-','')); openNoteEditor(id, false); window.tabTitleNoteID=id;`)
			browser.wait(`document.title==='gnotes - '+(document.getElementById('edit-title-'+window.tabTitleNoteID).value.trim() || 'Untitled note')`)
			browser.script(`const input=document.getElementById('edit-title-'+window.tabTitleNoteID); input.value='Browser tab title'; input.dispatchEvent(new Event('input', {bubbles:true}));`)
			browser.wait(`document.title==='gnotes - Browser tab title'`)
			browser.script(`updateNote(window.tabTitleNoteID);`)
			browser.wait(`document.title==='gnotes' && !document.documentElement.classList.contains('note-edit-focus')`)
			browser.wait(`document.querySelectorAll('.note-code-language').length===2 && [...document.querySelectorAll('.note-code-language')].map(b=>b.textContent).join(',')==='go,Plain text'`)
			// Exercise source selection explicitly; representable notes now stay
			// in rich text instead of falling back for harmless source changes.
			browser.script(`applyEditorMode(true); document.querySelector('.note-code-language').click();`)
			browser.wait(`document.documentElement.classList.contains('note-edit-focus') && document.getElementById('edit-content-'+window.tabTitleNoteID).value.slice(document.getElementById('edit-content-'+window.tabTitleNoteID).selectionStart, document.getElementById('edit-content-'+window.tabTitleNoteID).selectionEnd)==='func main() {}'`)
			browser.script(`const input=document.getElementById('edit-content-'+window.tabTitleNoteID); input.value=input.value.replace('` + "```go" + `','` + "```python" + `'); input.dispatchEvent(new Event('input',{bubbles:true})); updateNote(window.tabTitleNoteID);`)
			browser.wait(`!document.documentElement.classList.contains('note-edit-focus') && document.querySelector('.note-code-language')?.textContent==='python'`)
			browser.script(`document.querySelector('.note-code-language').click(); const input=document.getElementById('edit-content-'+window.tabTitleNoteID); input.value='` + "```go\\nfunc main() {}\\n```" + `'; updateNote(window.tabTitleNoteID);`)
			browser.wait(`!document.documentElement.classList.contains('note-edit-focus') && document.querySelector('.note-code-language')?.textContent==='go'`)
			browser.script(`applyEditorMode(false);`)
			browser.script(`document.querySelector('.note-code-language').click();`)
			browser.wait(`document.documentElement.classList.contains('note-edit-focus') && !document.querySelector('.rich-context-menu').hidden && document.getElementById('rich-code-language')?.value==='go'`)
			browser.script(`const select=document.getElementById('rich-code-language'); select.value='rust'; select.dispatchEvent(new Event('change', {bubbles:true})); updateNote(window.tabTitleNoteID);`)
			browser.wait(`!document.documentElement.classList.contains('note-edit-focus') && document.querySelector('.note-code-language')?.textContent==='rust'`)
			browser.script(`document.querySelector('.note-code-language').click(); document.getElementById('edit-title-'+window.tabTitleNoteID).value=''; const input=document.getElementById('edit-content-'+window.tabTitleNoteID); input.value='` + "```c\\nvoid main() {\\n   puts(\"Hello from gnotes!\");\\n      return 0;\\n}\\n```" + `'; updateNote(window.tabTitleNoteID);`)
			browser.wait(`!document.documentElement.classList.contains('note-edit-focus') && document.querySelector('.note-code-language')?.textContent==='c' && document.querySelector('#note-'+window.tabTitleNoteID+' .note-view h2')?.textContent==='Untitled note'`)
			if browser.script(`return getDisplayTitle({title:'',content:'~~~python\ncode\n~~~\n\nActual title'});`) != "Actual title" {
				t.Fatal("automatic title should use prose outside code fences")
			}
			browser.wait(`document.querySelectorAll('.note-roll-toggle').length===1`)
			browser.script(`const arrow=document.querySelector('.note-roll-toggle').getBoundingClientRect(), time=document.querySelector('.has-roll-control .date').getBoundingClientRect(); if (arrow.right>time.left) throw new Error('Roll arrow overlaps timestamp'); if ([...document.querySelectorAll('.date')].some(n=>!/\d{1,2}:\d{2}$/.test(n.textContent))) throw new Error('Timestamp must omit AM/PM');`)
			browser.script(`document.querySelector('.note-roll-toggle').click();`)
			browser.wait(`document.querySelector('.note-card.has-roll-control').classList.contains('is-rolled-up') && getComputedStyle(document.querySelector('.has-roll-control .note-content')).display==='none'`)
			browser.navigate(server.URL)
			browser.wait(`!document.getElementById('app-shell').hidden && document.querySelector('.note-card.has-roll-control')?.classList.contains('is-rolled-up')`)
			browser.script(`document.querySelector('.note-roll-toggle').click();`)
			for i := 0; i < 3; i++ {
				browser.script(`document.getElementById('density-toggle').click();`)
				browser.wait(`document.documentElement.scrollWidth<=innerWidth`)
			}
			browser.script(`document.getElementById('account-menu-username').click();`)
			browser.wait(`!document.getElementById('recovery-email-form').hidden`)
			browser.script(`document.getElementById('profile-recovery-email').value='new@example.com'; document.getElementById('profile-recovery-password').value='original password'; document.getElementById('recovery-email-form').requestSubmit();`)
			browser.wait(`document.getElementById('profile-recovery-status').textContent.includes('Check your inbox')`)
			browser.wait(`document.getElementById('profile-recovery-password').value==='' && document.documentElement.scrollWidth<=innerWidth`)
			verificationToken := recoveryBrowserToken(t, messages, "verify-email")
			browser.navigate(server.URL + "/#verify-email=" + verificationToken)
			browser.wait(`!document.getElementById('account-link-screen').hidden && location.hash==='' && document.getElementById('reset-password').disabled`)
			browser.script(`document.getElementById('account-link-submit').click();`)
			browser.wait(`!document.getElementById('auth-screen').hidden && document.getElementById('auth-status').textContent.includes('Recovery email verified')`)
			browser.navigate(server.URL)
			browser.wait(`!document.getElementById('app-shell').hidden`)
			browser.script(`document.getElementById('logout-button').click();`)
			browser.wait(`!document.getElementById('auth-screen').hidden && !document.getElementById('forgot-password').hidden`)
			browser.script(`document.getElementById('forgot-password').click(); document.getElementById('recovery-username').value='rick'; document.getElementById('recovery-email').value='wrong@example.com'; document.getElementById('recovery-request-form').requestSubmit();`)
			browser.wait(`document.getElementById('recovery-request-status').textContent.includes('If those details match')`)
			browser.script(`document.getElementById('recovery-email').value='new@example.com'; document.getElementById('recovery-email').dispatchEvent(new Event('input', {bubbles:true}));`)
			browser.wait(`document.getElementById('recovery-request-status').textContent===''`)
			browser.script(`document.getElementById('recovery-request-form').requestSubmit();`)
			browser.wait(`document.getElementById('recovery-request-status').textContent.includes('If those details match')`)
			token := recoveryBrowserToken(t, messages, "reset-password")
			browser.navigate(server.URL + "/#reset-password=" + token)
			browser.wait(`!document.getElementById('account-link-screen').hidden && location.hash==='' && !document.getElementById('reset-password').disabled`)
			browser.script(`document.getElementById('reset-password').value='replacement password'; document.getElementById('reset-password-confirm').value='different password'; document.getElementById('account-link-form').requestSubmit();`)
			browser.wait(`document.getElementById('account-link-status').textContent==='Passwords do not match.'`)
			browser.script(`document.getElementById('reset-password-confirm').value='replacement password'; document.getElementById('account-link-form').requestSubmit();`)
			browser.wait(`!document.getElementById('auth-screen').hidden && document.getElementById('auth-status').textContent.includes('Password changed')`)
			browser.script(`document.getElementById('auth-username').value='rick'; document.getElementById('auth-password').value='replacement password'; document.getElementById('auth-form').requestSubmit();`)
			browser.wait(`!document.getElementById('app-shell').hidden && document.querySelectorAll('.note-card').length===2 && document.documentElement.scrollWidth<=innerWidth`)
			browser.navigate(server.URL + "/#reset-password=" + token)
			browser.script(`document.getElementById('reset-password').value='another password'; document.getElementById('reset-password-confirm').value='another password'; document.getElementById('account-link-form').requestSubmit();`)
			browser.wait(`document.getElementById('account-link-status').textContent.includes('already used') && document.documentElement.scrollWidth<=innerWidth`)
		})
	}
}

func TestRichLineBreaksBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	server := httptest.NewServer(http.FileServer(http.Dir("../../public")))
	defer server.Close()
	browser := newRecoveryBrowser(t)
	browser.navigate(server.URL)
	browser.wait(`Boolean(window.GnotesRichEditor)`)
	browser.script(`const host=document.createElement('div'); document.body.prepend(host); window.linkLines='[Sriniously](https://www.youtube.com/@sriniously)  *System Engineering\n[Zachary Huang](https://www.youtube.com/@ZacharyLLM/videos)  *AI Dives'; window.lineCheck=GnotesRichEditor.mount(host,linkLines,()=>{});`)
	browser.script(`if(!lineCheck.preservesContent(linkLines)) throw new Error('Linked lines incorrectly require source mode'); const root=lineCheck.editor.view.dom; if(root.querySelectorAll('a').length!==2 || root.querySelectorAll('br').length!==1 || root.textContent.includes('[Sriniously]')) throw new Error('Expected formatted links on separate lines');`)
	serialized := browser.script(`return lineCheck.getMarkdown();`).(string)
	if got := mdToHTML(serialized); strings.Count(got, "<br>") != 1 || strings.Count(got, "<a href=") != 2 {
		t.Fatalf("saved rich text lost its line break or links: %s", got)
	}
	browser.script(`lineCheck.setMarkdown(lineCheck.getMarkdown()); if(lineCheck.editor.view.dom.querySelectorAll('br').length!==1) throw new Error('Reopening lost the break'); const table='| One | Two |\n| --- | --- |\n| A | B |'; lineCheck.setMarkdown(table); if(lineCheck.preservesContent(table)) throw new Error('Unsupported tables must retain source fallback'); lineCheck.setMarkdown('first\n\nsecond'); if(!lineCheck.preservesContent('first\n\nsecond') || lineCheck.editor.view.dom.querySelectorAll('p').length!==2) throw new Error('Paragraphs lost');`)
}

func TestRichBoldBoundariesBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	server := httptest.NewServer(http.FileServer(http.Dir("../../public")))
	defer server.Close()
	browser := newRecoveryBrowser(t)
	browser.navigate(server.URL)
	browser.wait(`Boolean(window.GnotesRichEditor)`)
	browser.script(`const host=document.createElement('div'); host.id='bold-check'; document.body.prepend(host); window.boldCheck=GnotesRichEditor.mount(host,'',()=>{});`)
	for _, separator := range []string{" ", "\ue007"} {
		browser.script(`boldCheck.setMarkdown(''); boldCheck.editor.chain().focus().toggleBold().run();`)
		element := browser.call("POST", "/element", map[string]string{"using": "css selector", "value": "#bold-check .tiptap"}).(map[string]any)
		id := element["element-6066-11e4-a52e-4f735466cecf"].(string)
		browser.call("POST", "/element/"+id+"/value", map[string]string{"text": "Hello" + separator + "world"})
		browser.wait(`document.querySelector('#bold-check strong')?.textContent==='Hello' && document.querySelector('#bold-check .tiptap').textContent==='Helloworld' || document.querySelector('#bold-check strong')?.textContent==='Hello' && document.querySelector('#bold-check .tiptap').textContent==='Hello world'`)
	}
	browser.script(`boldCheck.setMarkdown('**Hello world**'); boldCheck.editor.chain().setTextSelection(6).focus().run();`)
	element := browser.call("POST", "/element", map[string]string{"using": "css selector", "value": "#bold-check .tiptap"}).(map[string]any)
	id := element["element-6066-11e4-a52e-4f735466cecf"].(string)
	browser.call("POST", "/element/"+id+"/value", map[string]string{"text": " "})
	browser.wait(`document.querySelector('#bold-check strong')?.textContent==='Hello  world'`)
}

func recoveryBrowserToken(t *testing.T, messages chan capturedRecoveryMail, action string) string {
	t.Helper()
	select {
	case message := <-messages:
		match := regexp.MustCompile(`#` + action + `=([A-Za-z0-9_-]{43})`).FindStringSubmatch(message.Text)
		if len(match) == 2 {
			return match[1]
		}
		t.Fatal("no account link in test email")
	case <-time.After(2 * time.Second):
		t.Fatal("no test email arrived")
	}
	return ""
}

type recoveryBrowser struct {
	t       *testing.T
	session string
	client  *http.Client
}

func newRecoveryBrowser(t *testing.T) *recoveryBrowser {
	b := &recoveryBrowser{t: t, client: &http.Client{Timeout: 30 * time.Second}}
	result := b.call("POST", "", map[string]any{"capabilities": map[string]any{"alwaysMatch": map[string]any{"browserName": "chrome", "goog:chromeOptions": map[string]any{"args": []string{"--headless=new", "--no-sandbox", "--disable-dev-shm-usage", "--disable-gpu"}}}}})
	b.session = result.(map[string]any)["sessionId"].(string)
	t.Cleanup(func() { b.call("DELETE", "", nil) })
	return b
}

func (b *recoveryBrowser) call(method, path string, body any) any {
	b.t.Helper()
	endpoint := "http://127.0.0.1:19515/session"
	if b.session != "" {
		endpoint += "/" + b.session
	}
	if body == nil {
		body = map[string]any{}
	}
	encoded, _ := json.Marshal(body)
	req, err := http.NewRequest(method, endpoint+path, bytes.NewReader(encoded))
	if err != nil {
		b.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := b.client.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer res.Body.Close()
	var result struct {
		Value any `json:"value"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		b.t.Fatal(err)
	}
	if res.StatusCode >= 400 {
		b.t.Fatalf("browser command %s failed (HTTP %d): %v", path, res.StatusCode, result.Value)
	}
	return result.Value
}

func (b *recoveryBrowser) script(source string) any {
	return b.call("POST", "/execute/sync", map[string]any{"script": source, "args": []any{}})
}
func (b *recoveryBrowser) navigate(target string) {
	b.call("POST", "/url", map[string]string{"url": target})
}
func (b *recoveryBrowser) wait(expression string) {
	b.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ok, _ := b.script("return Boolean(" + expression + ");").(bool); ok {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Only DOM text is reported, avoiding email tokens and session credentials.
	status := b.script(`return [...document.querySelectorAll('.auth-status')].map(n=>n.textContent).filter(Boolean).join('; ');`)
	b.t.Fatalf("browser condition failed: %s; status: %s", strings.TrimSpace(expression), status)
}
