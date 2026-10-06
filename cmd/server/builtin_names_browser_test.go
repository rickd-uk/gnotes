package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestBuiltinNamesBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local Chromium check")
	}
	recoveryFixture(t)
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
	server := httptest.NewServer(securityHeaders(mux))
	defer server.Close()
	browser := newRecoveryBrowser(t)
	browser.navigate(server.URL)
	browser.wait(`!document.getElementById('auth-screen').hidden`)
	browser.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
	browser.wait(`!document.getElementById('app-shell').hidden && builtinNamesReady && !spellingBusy`)
	browser.script(`window.namesHost=document.createElement('div');document.body.append(namesHost);window.namesEditor=GnotesRichEditor.mount(namesHost,['Tokyo New York Albert **Einstein** OpenAI Microsoft RickdUnknownName will apple tokyo '+String.fromCharCode(96)+'Tokyo'+String.fromCharCode(96)+'','',String.fromCharCode(96).repeat(3),'Tokyo',String.fromCharCode(96).repeat(3),'','After code'].join('\n'),()=>{},()=>{});`)
	browser.script(`window.namesOriginal=namesEditor.getMarkdown();namesEditor.setSpelling(true,[],[{name:'RickdUnknownName',kind:'person'}]);const spans=[...namesHost.querySelectorAll('[data-spelling-ignored="name"]')];for(const word of ['Tokyo','New','York','Albert','Einstein','OpenAI','Microsoft','RickdUnknownName'])if(!spans.some(s=>s.textContent.includes(word)))throw new Error('Missing automatic name: '+word);if(spans.some(s=>/will|apple|tokyo/.test(s.textContent))||namesHost.querySelector('code [data-spelling-ignored]'))throw new Error('Ordinary lowercase words or code were ignored');if(namesEditor.getMarkdown()!==namesOriginal)throw new Error('Names changed stored Markdown: '+JSON.stringify([namesOriginal,namesEditor.getMarkdown()]));namesEditor.setSpelling(true,[],[{name:'RickdUnknownName',kind:'person'}],false);`)
	browser.script(`const spans=[...namesHost.querySelectorAll('[data-spelling-ignored="name"]')];if(spans.length!==1||spans[0].textContent!=='RickdUnknownName')throw new Error('Disabling built-ins must retain personal names');namesEditor.setSpelling(false,[],[{name:'RickdUnknownName',kind:'person'}]);if(namesHost.querySelector('[data-spelling-ignored]'))throw new Error('Spellcheck off retained decorations');namesEditor.destroy();namesHost.remove();document.getElementById('spelling-builtin-enabled').checked=false;document.getElementById('spelling-builtin-enabled').dispatchEvent(new Event('change'));`)
	browser.navigate(server.URL)
		browser.wait(`!document.getElementById('app-shell').hidden && !spellingBusy && !builtinSpellingEnabled && !document.getElementById('spelling-builtin-enabled').checked`)
	browser.script(`if(spellingNames.length || document.querySelectorAll('#spelling-names-list li').length)throw new Error('Built-in list copied into personal names');`)
}
