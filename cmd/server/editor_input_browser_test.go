package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"gnotes/internal/db"
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
	browser.script(`const source=document.createElement('textarea');document.body.prepend(source);source.value='first\nsecond';source.setSelectionRange(0,source.value.length);handleTextIndent(new KeyboardEvent('keydown',{key:'Tab',cancelable:true}),source);if(source.value!=='\u00a0'.repeat(4)+'first\n'+'\u00a0'.repeat(4)+'second')throw new Error('Source indent failed');handleTextIndent(new KeyboardEvent('keydown',{key:'Tab',shiftKey:true,cancelable:true}),source);if(source.value!=='first\nsecond')throw new Error('Source outdent failed');`)
	browser.script(`
          const source=document.createElement('textarea');document.body.prepend(source);
          const fence=String.fromCharCode(96).repeat(3),spaces='\u00a0'.repeat(4);
          const cases=[
            {text:fence+'go\ncode\n'+fence,start:fence.length+3,end:fence.length+7,expected:fence+'go\n    code\n'+fence},
            {text:'~~~\ncode\n~~~',start:4,end:8,expected:'~~~\n    code\n~~~'},
            {text:'~~~~\n~~~\ncode',start:9,end:13,expected:'~~~~\n~~~\n    code'},
            {text:fence+'\ncode\n'+fence+'\ntext',start:13,end:17,expected:fence+'\ncode\n'+fence+'\n'+spaces+'text'},
            {text:'- child',start:0,end:0,expected:'    - child'},
            {text:'- parent\n- child',start:9,end:16,expected:'- parent\n    - child'},
            {text:'1. parent\n2. child',start:10,end:18,expected:'1. parent\n    2. child'},
            {text:'first\nsecond\n',start:0,end:13,expected:spaces+'first\n'+spaces+'second\n'}
          ];
          for(const test of cases){
            source.value=test.text;source.setSelectionRange(test.start,test.end);
            handleTextIndent(new KeyboardEvent('keydown',{key:'Tab',cancelable:true}),source);
            if(source.value!==test.expected)throw new Error('Indent context failed: '+JSON.stringify({test,actual:source.value}));
            handleTextIndent(new KeyboardEvent('keydown',{key:'Tab',shiftKey:true,cancelable:true}),source);
            if(source.value!==test.text)throw new Error('Outdent changed literal content: '+JSON.stringify({test,actual:source.value}));
          }
          source.remove();
        `)
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
		browser.script(`syncFocusedEditViewport();const rect=document.querySelector('.note-card.is-editing').getBoundingClientRect();if(rect.width<innerWidth-33||rect.left<0||rect.right>innerWidth||rect.bottom>innerHeight||rect.height>innerHeight/2||Math.abs((rect.top+rect.bottom)/2-(innerHeight-(innerWidth<=560?80:0))/2)>2)throw new Error('Short editor not wide, compact and centered: '+JSON.stringify(rect));if(getComputedStyle(richEdit.editor.view.dom).scrollbarWidth!=='thin')throw new Error('Scrollbar not subtle');const previewButton=document.getElementById('edit-preview-toggle-'+testNoteID).getBoundingClientRect(),formatButton=document.getElementById('edit-rich-format-toggle-'+testNoteID).getBoundingClientRect(),saveButton=document.querySelector('.note-card.is-editing .edit-done').getBoundingClientRect();if(formatButton.width<48||formatButton.height<44||formatButton.left-previewButton.right<15||saveButton.left-formatButton.right<15)throw new Error('Editor buttons crowded or formatting button too small');`)
	}
	browser.script(`window.shortNote=richEdit.editor.getJSON();richEdit.editor.commands.setContent('<p>Long line</p>'.repeat(150));const card=document.querySelector('.note-card.is-editing').getBoundingClientRect(),body=richEdit.editor.view.dom;if(card.height<innerHeight-34||card.bottom>innerHeight||body.scrollHeight<=body.clientHeight)throw new Error('Long editor does not grow and scroll');richEdit.editor.commands.setContent(shortNote);if(document.querySelector('.note-card.is-editing').getBoundingClientRect().height>innerHeight/2)throw new Error('Short editor does not shrink after editing');`)
	// Preview uses the saved-note renderer, including content unsupported by the rich editor.
	browser.script(`applyEditorMode(true);window.previewSource='## Unsaved heading\n\n| A | B |\n| --- | --- |\n| one | two |\n\n<script>window.previewAttack=true</script>\n\n[Unsafe](javascript:alert(1))';document.getElementById('edit-content-'+testNoteID).value=previewSource;document.getElementById('edit-preview-toggle-'+testNoteID).click();`)
	browser.wait(`Boolean(document.querySelector('.note-editor-preview h2'))`)
	browser.script(`const preview=document.getElementById('edit-preview-'+testNoteID);const originalDensity=notesList.className;for(const density of ['density-full','density-compact','density-titles']){notesList.className=density;const rect=preview.getBoundingClientRect(),editor=document.getElementById('note-editor-'+testNoteID).getBoundingClientRect(),style=getComputedStyle(preview);if(Math.abs(rect.left-editor.left)>1||Math.abs(rect.width-editor.width)>1||style.display==='none'||style.overflowY!=='auto'||style.maskImage!=='none')throw new Error('Preview shifts or inherits list restrictions in '+density+': '+JSON.stringify(rect));}notesList.className=originalDensity;if(!preview.querySelector('table')||preview.querySelector('script')||preview.querySelector('a[href^="javascript:"]')||window.previewAttack)throw new Error('Preview rendering or safety failed');if(getComputedStyle(document.getElementById('edit-content-'+testNoteID)).display!=='none')throw new Error('Source remains visible in preview');document.getElementById('edit-preview-toggle-'+testNoteID).click();if(document.getElementById('edit-content-'+testNoteID).value!==previewSource||!preview.hidden)throw new Error('Preview lost unsaved text');window.originalPreviewFetch=fetch.bind(window);window.fetch=(url,options)=>url==='/api/notes/preview'?new Promise(resolve=>window.releasePreview=()=>originalPreviewFetch(url,options).then(resolve)):originalPreviewFetch(url,options);toggleNotePreview(testNoteID);toggleNotePreview(testNoteID);releasePreview();`)
	browser.wait(`document.getElementById('edit-preview-'+testNoteID).hidden`)
	browser.script(`window.fetch=originalPreviewFetch;const source=document.getElementById('edit-content-'+testNoteID);source.value='Long source line\n'.repeat(150);handleEditInput({target:source},testNoteID);let card=document.querySelector('.note-card.is-editing').getBoundingClientRect();if(card.height<innerHeight-34||source.scrollHeight<=source.clientHeight)throw new Error('Long source does not grow and scroll: '+JSON.stringify({card:card.toJSON(),sourceHeight:source.clientHeight,sourceScroll:source.scrollHeight,height:source.style.height,display:getComputedStyle(source).display}));source.value='**Saved preview edit**';handleEditInput({target:source},testNoteID);if(document.querySelector('.note-card.is-editing').getBoundingClientRect().height>innerHeight/2)throw new Error('Short source does not shrink');toggleNotePreview(testNoteID);`)
	browser.wait(`Boolean(document.querySelector('.note-editor-preview strong'))`)
	browser.script(`document.getElementById('edit-preview-toggle-'+testNoteID).focus();document.activeElement.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}));`)
	browser.wait(`editingNoteId===null && pendingNoteEdits.size===0 && !notesLoading`)
	browser.script(`openNoteEditor(testNoteID,false);if(!document.getElementById('edit-preview-'+testNoteID).hidden||document.getElementById('edit-preview-toggle-'+testNoteID).getAttribute('aria-pressed')!=='false'||document.getElementById('edit-content-'+testNoteID).value!=='**Saved preview edit**')throw new Error('Reopen did not default to editing with saved text');`)
	// Source and rich editors share Escape's save behavior; nested dialogs close first.
	browser.script(`document.getElementById('edit-content-'+testNoteID).value='Escape source';document.getElementById('edit-content-'+testNoteID).dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}));`)
	browser.wait(`editingNoteId===null && pendingNoteEdits.size===0 && !notesLoading`)
	browser.script(`openNoteEditor(testNoteID,false);applyEditorMode(false);richEdit.editor.commands.setContent('<p>Escape rich</p>');GnotesShortcuts.open();document.getElementById('shortcuts-dialog').dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}));if(editingNoteId!==testNoteID)throw new Error('Dialog Escape saved underlying editor');document.getElementById('shortcuts-dialog').close();richEdit.editor.view.dom.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}));`)
	browser.wait(`editingNoteId===null && pendingNoteEdits.size===0 && !notesLoading`)
	var saved string
	if err := db.DB.QueryRow("SELECT content FROM notes WHERE id=1").Scan(&saved); err != nil || saved != "Escape rich" {
		t.Fatalf("Escape did not persist rich editor: %q %v", saved, err)
	}
	// Record shortcuts through the UI, reject duplicate assignments and persist reloads.
	browser.script(`GnotesShortcuts.open();document.querySelector('[data-action="new"] .shortcut-record').click();document.dispatchEvent(new KeyboardEvent('keydown',{key:'j',ctrlKey:true,shiftKey:true,bubbles:true,cancelable:true}));if(GnotesShortcuts.bindings.new!=='Ctrl+Shift+J')throw new Error('Shortcut not recorded');window.shortcutOwner=currentUser;currentUser={username:'another-user'};if(GnotesShortcuts.bindings.new)throw new Error('Shortcut leaked between accounts');currentUser=shortcutOwner;GnotesShortcuts.open();document.querySelector('[data-action="search"] .shortcut-record').click();document.dispatchEvent(new KeyboardEvent('keydown',{key:'j',ctrlKey:true,shiftKey:true,bubbles:true,cancelable:true}));if(!document.getElementById('shortcuts-status').textContent.includes('Already assigned')||GnotesShortcuts.bindings.search!=='/')throw new Error('Conflict not rejected');document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}));document.getElementById('shortcuts-dialog').close();document.dispatchEvent(new KeyboardEvent('keydown',{key:'j',ctrlKey:true,shiftKey:true,bubbles:true,cancelable:true}));window.originalShortcutFetch=fetch;`)
	browser.wait(`Boolean(document.activeElement.closest('#note-form'))`)
	browser.navigate(server.URL)
	browser.wait(`!notesLoading && loadedNotes.size===1 && currentUser`)
	browser.script(`window.testNoteID=[...loadedNotes.keys()][0];if(GnotesShortcuts.bindings.new!=='Ctrl+Shift+J')throw new Error('Shortcut lost on reload');GnotesShortcuts.open();document.getElementById('shortcuts-search').value='preview';document.getElementById('shortcuts-search').dispatchEvent(new Event('input'));if(document.querySelectorAll('.shortcut-row').length!==1)throw new Error('Shortcut filtering failed');document.getElementById('shortcuts-search').value='';document.getElementById('shortcuts-search').dispatchEvent(new Event('input'));document.querySelector('[data-action="new"] button[aria-label^="Clear"]').click();if(GnotesShortcuts.bindings.new)throw new Error('Clear shortcut failed');document.getElementById('shortcuts-reset').click();if(GnotesShortcuts.bindings.preview!=='Ctrl+Shift+P')throw new Error('Reset failed');document.getElementById('shortcuts-dialog').close();openNoteEditor(testNoteID,false);applyEditorMode(false);richEdit.editor.view.dom.focus({preventScroll:true});richEdit.format('codeBlock');GnotesShortcuts.actions.get('language:python').run();if(richEdit.editor.getAttributes('codeBlock').language!=='python')throw new Error('Code shortcut failed');richEdit.format('normalText');richEdit.editor.view.dom.dispatchEvent(new KeyboardEvent('keydown',{key:'P',ctrlKey:true,shiftKey:true,bubbles:true,cancelable:true}));`)
	browser.wait(`!document.getElementById('edit-preview-'+testNoteID).hidden`)

}
