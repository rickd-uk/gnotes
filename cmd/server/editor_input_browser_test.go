package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestEditorInputBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	server := httptest.NewServer(http.FileServer(http.Dir("../../public")))
	defer server.Close()
	browser := newRecoveryBrowser(t)
	browser.navigate(server.URL)
	browser.wait(`Boolean(window.GnotesRichEditor)`)
	browser.script(`const host=document.createElement('div');host.id='input-check';document.body.prepend(host);window.inputCheck=GnotesRichEditor.mount(host,'[First](https://example.com)',()=>{},()=>{},openRichLinkDialog);inputCheck.editor.commands.focus('end');`)
	element := browser.call("POST", "/element", map[string]string{"using": "css selector", "value": "#input-check .tiptap"}).(map[string]any)
	id := element["element-6066-11e4-a52e-4f735466cecf"].(string)
	browser.call("POST", "/element/"+id+"/value", map[string]string{"text": " https://example.org "})
	browser.script(`const links=inputCheck.editor.view.dom.querySelectorAll('a');if(links.length!==2||links[0].textContent!=='First'||links[1].textContent!=='https://example.org')throw new Error('Link formatting continued across the separating space: '+inputCheck.editor.view.dom.innerHTML);`)
	browser.script(`inputCheck.setMarkdown(inputCheck.getMarkdown());const links=inputCheck.editor.view.dom.querySelectorAll('a');if(links.length!==2||links[0].href===links[1].href)throw new Error('Reopening merged URLs');window.openedLinks=[];window.open=(...args)=>openedLinks.push(args);links[0].dispatchEvent(new MouseEvent('click',{bubbles:true,cancelable:true}));if(openedLinks.length)throw new Error('Editing click opened a page');links[0].dispatchEvent(new MouseEvent('click',{bubbles:true,cancelable:true,ctrlKey:true}));if(openedLinks[0]?.[0]!=='https://example.com/')throw new Error('Ctrl-click did not open the URL');inputCheck.editor.commands.setTextSelection(2);inputCheck.format('link');if(!document.getElementById('rich-link-dialog').open||document.getElementById('rich-link-open').hidden)throw new Error('Link actions unavailable');document.getElementById('rich-link-open').click();if(openedLinks.length!==2)throw new Error('Open link action failed');document.getElementById('rich-link-cancel').click();`)
	// Use an actual Tab key to catch focus leaving the writing area.
	browser.script(`inputCheck.setMarkdown('first\n\nsecond');inputCheck.editor.chain().focus().selectAll().run();`)
	browser.call("POST", "/element/"+id+"/value", map[string]string{"text": "\ue004"})
	browser.script(`if(document.activeElement!==inputCheck.editor.view.dom)throw new Error('Tab moved focus');if([...inputCheck.editor.view.dom.querySelectorAll('p')].some(p=>!p.textContent.startsWith('\u00a0'.repeat(4))))throw new Error('Tab did not indent selected paragraphs');window.indented=inputCheck.getMarkdown();inputCheck.setMarkdown(indented);if(!inputCheck.editor.getText().startsWith('\u00a0'.repeat(4)))throw new Error('Indentation lost on reopen');inputCheck.editor.chain().focus().selectAll().run();inputCheck.editor.view.dom.dispatchEvent(new KeyboardEvent('keydown',{key:'Tab',code:'Tab',shiftKey:true,bubbles:true,cancelable:true}));if(inputCheck.getMarkdown()!=='first\n\nsecond')throw new Error('Shift Tab did not restore paragraphs');inputCheck.editor.commands.undo();if(inputCheck.getMarkdown()!==indented)throw new Error('Indent undo failed');`)
	browser.script(`inputCheck.setMarkdown('first\nsecond');inputCheck.editor.chain().focus().selectAll().run();inputCheck.editor.view.dom.dispatchEvent(new KeyboardEvent('keydown',{key:'Tab',bubbles:true,cancelable:true}));if(!inputCheck.editor.view.dom.textContent.includes('\u00a0'.repeat(4)+'second'))throw new Error('Hard-break line not indented');inputCheck.setMarkdown('- first\n- second');inputCheck.editor.state.doc.descendants((node,pos)=>{if(node.isText&&node.text==='second')inputCheck.editor.commands.setTextSelection(pos+2);});inputCheck.editor.view.dom.dispatchEvent(new KeyboardEvent('keydown',{key:'Tab',bubbles:true,cancelable:true}));if(!inputCheck.editor.view.dom.querySelector('li li'))throw new Error('List did not nest: '+JSON.stringify(inputCheck.editor.getJSON())+' selection '+JSON.stringify(inputCheck.editor.state.selection.toJSON()));inputCheck.editor.view.dom.dispatchEvent(new KeyboardEvent('keydown',{key:'Tab',shiftKey:true,bubbles:true,cancelable:true}));if(inputCheck.editor.view.dom.querySelector('li li'))throw new Error('List did not outdent');`)
	browser.script(`const source=document.createElement('textarea');document.body.prepend(source);source.value='first\nsecond';source.setSelectionRange(0,source.value.length);handleTextIndent(new KeyboardEvent('keydown',{key:'Tab',cancelable:true}),source);if(source.value!=='    first\n    second')throw new Error('Source indent failed');handleTextIndent(new KeyboardEvent('keydown',{key:'Tab',shiftKey:true,cancelable:true}),source);if(source.value!=='first\nsecond')throw new Error('Source outdent failed');`)
}

func TestEditorPreviewAndSizeBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	recoveryFixture(t)
	mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at) VALUES(1,'Preview test','[First](https://example.com) [Second](https://example.org)',?)", time.Now().UTC())
	mux := http.NewServeMux()
	registerDictionaryRoutes(mux)
	mux.HandleFunc("/api/auth/config", authConfigHandler)
	mux.HandleFunc("/api/auth/login", loginHandler)
	mux.HandleFunc("/api/auth/me", protect(meHandler, false))
	mux.HandleFunc("/api/draft", protect(getDraftHandler, false))
	mux.HandleFunc("/api/notes/page", protect(pagedListNotesHandler, false))
	mux.HandleFunc("/api/notes/trash-page", protect(pagedTrashNotesHandler, false))
	mux.HandleFunc("/api/notes/update", protect(updateNoteHandler, true))
	mux.HandleFunc("/api/notes/preview", protect(previewNoteHandler, true))
	mux.HandleFunc("/api/tags", protect(tagsHandler, false))
	mux.Handle("/", http.FileServer(http.Dir("../../public")))
	server := httptest.NewServer(securityHeaders(mux))
	defer server.Close()
	browser := newRecoveryBrowser(t)
	browser.navigate(server.URL)
	browser.wait(`!document.getElementById('auth-screen').hidden`)
	browser.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
	browser.wait(`!notesLoading && loadedNotes.size===1 && spellingReady`)
	for _, width := range []int{320, 1024} {
		browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setDeviceMetricsOverride", "params": map[string]any{"width": width, "height": 800, "deviceScaleFactor": 1, "mobile": false}})
		browser.script(`const account=document.getElementById('account-control');account.open=true;const accountMenu=account.querySelector('.account-menu');const entries=[...accountMenu.querySelectorAll('.menu-entry')];if(entries.length<10||entries.some(entry=>!entry.querySelector('.menu-item-icon')))throw new Error('Account menu icons missing');const menuRect=accountMenu.getBoundingClientRect();if(accountMenu.scrollWidth>accountMenu.clientWidth||menuRect.left<0||menuRect.right>innerWidth)throw new Error('Account menu icons cause overflow');account.open=false;document.getElementById('view-toggle').click();const menu=document.getElementById('view-menu'),grid=document.querySelector('.display-options'),fonts=document.getElementById('font-settings');if(fonts.open||grid.children.length!==4)throw new Error('Controls not compact by default');const buttons=[...grid.children].map(b=>b.getBoundingClientRect());if(Math.abs(buttons[0].top-buttons[1].top)>1||Math.abs(buttons[2].top-buttons[3].top)>1||buttons[2].top<=buttons[0].top)throw new Error('Settings not arranged in two rows');if(buttons.some(rect=>rect.height<44)||grid.scrollWidth>grid.clientWidth)throw new Error('Compact settings overflow or too small');window.collapsedControlsHeight=menu.scrollHeight;fonts.querySelector('summary').click();if(!fonts.open||menu.scrollHeight<=collapsedControlsHeight||menu.scrollWidth>menu.clientWidth)throw new Error('Font rollup failed');document.querySelector('[data-font="technical"]').click();if(document.querySelector('[data-font="technical"]').getAttribute('aria-pressed')!=='true')throw new Error('Font choice stopped working');fonts.querySelector('summary').click();document.getElementById('view-menu-close').click();`)
	}
	browser.script(`window.testNoteID=[...loadedNotes.keys()][0];const link=document.querySelector('.note-view a');document.addEventListener('click',function preventNavigation(e){if(e.target===link){window.normalLinkPrevented=e.defaultPrevented;e.preventDefault();document.removeEventListener('click',preventNavigation);}});link.dispatchEvent(new MouseEvent('click',{bubbles:true,cancelable:true}));if(normalLinkPrevented||editingNoteId!==null)throw new Error('Normal saved link click no longer navigates');link.dispatchEvent(new MouseEvent('click',{bubbles:true,cancelable:true,shiftKey:true}));`)
	browser.wait(`editingNoteId===testNoteID && Boolean(richEdit)`)
	for _, width := range []int{320, 1024, 1920} {
		browser.call("POST", "/goog/cdp/execute", map[string]any{"cmd": "Emulation.setDeviceMetricsOverride", "params": map[string]any{"width": width, "height": 1080, "deviceScaleFactor": 1, "mobile": false}})
		browser.script(`syncFocusedEditViewport();const rect=document.querySelector('.note-card.is-editing').getBoundingClientRect();if(rect.width<innerWidth-33||rect.left<0||rect.right>innerWidth||rect.bottom>innerHeight||rect.height<innerHeight-100)throw new Error('Editor does not use available viewport: '+JSON.stringify(rect));if(getComputedStyle(richEdit.editor.view.dom).scrollbarWidth!=='thin')throw new Error('Scrollbar not subtle');const previewButton=document.getElementById('edit-preview-toggle-'+testNoteID).getBoundingClientRect(),formatButton=document.getElementById('edit-rich-format-toggle-'+testNoteID).getBoundingClientRect(),saveButton=document.querySelector('.note-card.is-editing .edit-done').getBoundingClientRect();if(formatButton.width<48||formatButton.height<44||formatButton.left-previewButton.right<15||saveButton.left-formatButton.right<15)throw new Error('Editor buttons crowded or formatting button too small');`)
	}
	// Preview uses the saved-note renderer, including content unsupported by the rich editor.
	browser.script(`applyEditorMode(true);window.previewSource='## Unsaved heading\n\n| A | B |\n| --- | --- |\n| one | two |\n\n<script>window.previewAttack=true</script>\n\n[Unsafe](javascript:alert(1))';document.getElementById('edit-content-'+testNoteID).value=previewSource;document.getElementById('edit-preview-toggle-'+testNoteID).click();`)
	browser.wait(`Boolean(document.querySelector('.note-editor-preview h2'))`)
	browser.script(`const preview=document.getElementById('edit-preview-'+testNoteID);const originalDensity=notesList.className;for(const density of ['density-full','density-compact','density-titles']){notesList.className=density;const rect=preview.getBoundingClientRect(),editor=document.getElementById('note-editor-'+testNoteID).getBoundingClientRect(),style=getComputedStyle(preview);if(Math.abs(rect.left-editor.left)>1||Math.abs(rect.width-editor.width)>1||style.display==='none'||style.overflowY!=='auto'||style.maskImage!=='none')throw new Error('Preview shifts or inherits list restrictions in '+density+': '+JSON.stringify(rect));}notesList.className=originalDensity;if(!preview.querySelector('table')||preview.querySelector('script')||preview.querySelector('a[href^="javascript:"]')||window.previewAttack)throw new Error('Preview rendering or safety failed');if(getComputedStyle(document.getElementById('edit-content-'+testNoteID)).display!=='none')throw new Error('Source remains visible in preview');document.getElementById('edit-preview-toggle-'+testNoteID).click();if(document.getElementById('edit-content-'+testNoteID).value!==previewSource||!preview.hidden)throw new Error('Preview lost unsaved text');window.originalPreviewFetch=fetch.bind(window);window.fetch=(url,options)=>url==='/api/notes/preview'?new Promise(resolve=>window.releasePreview=()=>originalPreviewFetch(url,options).then(resolve)):originalPreviewFetch(url,options);toggleNotePreview(testNoteID);toggleNotePreview(testNoteID);releasePreview();`)
	browser.wait(`document.getElementById('edit-preview-'+testNoteID).hidden`)
	browser.script(`window.fetch=originalPreviewFetch;document.getElementById('edit-content-'+testNoteID).value='**Saved preview edit**';toggleNotePreview(testNoteID);`)
	browser.wait(`Boolean(document.querySelector('.note-editor-preview strong'))`)
	browser.script(`updateNote(testNoteID);`)
	browser.wait(`editingNoteId===null && pendingNoteEdits.size===0 && !notesLoading`)
	browser.script(`openNoteEditor(testNoteID,false);if(!document.getElementById('edit-preview-'+testNoteID).hidden||document.getElementById('edit-preview-toggle-'+testNoteID).getAttribute('aria-pressed')!=='false'||document.getElementById('edit-content-'+testNoteID).value!=='**Saved preview edit**')throw new Error('Reopen did not default to editing with saved text');`)
}
