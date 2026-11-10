package main

import (
	"fmt"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestPrivacyBrowser(t *testing.T) {
	if os.Getenv("GNOTES_BROWSER_CHECK") != "1" {
		t.Skip("optional local browser check")
	}
	widths := []int{320, 560, 1024}
	if os.Getenv("GNOTES_COLLECTION_BROWSER") == "firefox" {
		widths = []int{560, 1024}
	}
	for _, width := range widths {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			recoveryFixture(t)
			mustRecoveryExec(t, "INSERT INTO notes(id,user_id,title,content,created_at) VALUES(1,1,'Private title','PrivateOrchid body',?)", time.Now().UTC())
			mustRecoveryExec(t, "INSERT INTO notes(id,user_id,title,content,created_at,archived_at,favorited,locked) VALUES(2,1,'Archived secret','ArchivedOrchid body',?,?,1,1)", time.Now().UTC(), time.Now().UTC())
			mux := vimBrowserMux()
			mux.HandleFunc("/api/privacy", protect(privacyStatusHandler, false))
			mux.HandleFunc("/api/privacy/code", protect(privacyCodeHandler, true))
			mux.HandleFunc("/api/privacy/notes", protect(privacyNoteHandler, true))
			mux.HandleFunc("/api/features", protect(featurePreferencesHandler, false))
			server := httptest.NewServer(securityHeaders(mux))
			defer server.Close()
			browser := newCollectionBrowser(t, width)
			browser.navigate(server.URL)
			browser.wait(`!document.getElementById('auth-screen').hidden`)
			browser.script(`document.getElementById('auth-username').value='rick';document.getElementById('auth-password').value='original password';document.getElementById('auth-form').requestSubmit();`)
			browser.wait(`!notesLoading && currentUser?.username==='rick' && loadedNotes.size===1 && Boolean(window.GnotesPrivacy)`)
			browser.script(`window.privacyErrors=[];window.addEventListener('error',event=>privacyErrors.push(event.message));`)
			collectionClick(browser, "#note-1 .note-privacy-toggle")
			browser.wait(`document.getElementById('privacy-dialog').open && !document.getElementById('privacy-new').disabled && !document.getElementById('privacy-new-label').hidden`)
			browser.script(`document.getElementById('privacy-new').value='0012';document.getElementById('privacy-confirm').value='0013';document.getElementById('privacy-password').value='original password';document.getElementById('privacy-form').requestSubmit();`)
			browser.wait(`document.getElementById('privacy-status').textContent.includes('do not match')`)
			browser.script(`document.getElementById('privacy-confirm').value='0012';document.getElementById('privacy-form').requestSubmit();`)
			browser.wait(`!notesLoading && !document.getElementById('privacy-dialog').open && loadedNotes.get(1)?.privacy_locked`)
			browser.script(`if(loadedNotes.get(1).content||loadedNotes.get(1).html_content||document.getElementById('notes-list').textContent.includes('PrivateOrchid'))throw new Error('Locked body remains visible');if(!document.getElementById('note-1').textContent.includes('Private title'))throw new Error('Visible-title preference lost');toggleFavoritesMode();`)
			browser.wait(`favoritesMode && !notesLoading && loadedNotes.has(2)`)
			collectionClick(browser, "#note-2 .note-privacy-toggle")
			browser.wait(`!notesLoading && loadedNotes.get(2)?.privacy_locked`)
			browser.script(`if(document.getElementById('privacy-dialog').open)throw new Error('Second lock asked for code');showFavoritesInMain=true;toggleArchiveMode();`)
			browser.wait(`archiveMode && !notesLoading && loadedNotes.get(2)?.privacy_locked`)
			browser.script(`if(document.getElementById('notes-list').textContent.includes('ArchivedOrchid'))throw new Error('Archive exposed private text');toggleArchiveMode();`)
			browser.wait(`!archiveMode && !notesLoading && loadedNotes.has(1)`)
			collectionClick(browser, "#note-1 .privacy-placeholder button")
			browser.wait(`document.getElementById('privacy-dialog').open && !document.getElementById('privacy-code').disabled`)
			browser.script(`document.getElementById('privacy-code').value='9999';document.getElementById('privacy-form').requestSubmit();`)
			browser.wait(`document.getElementById('privacy-status').textContent.includes('incorrect')`)
			browser.script(`document.getElementById('privacy-code').value='0012';document.getElementById('privacy-form').requestSubmit();`)
			browser.wait(`!document.getElementById('privacy-dialog').open && !notesLoading && !loadedNotes.get(1)?.privacy_locked`)
			browser.script(`if(loadedNotes.get(1).content!=='PrivateOrchid body')throw new Error('Unlock lost text');
              for(const theme of ['light','dark']){
                document.documentElement.dataset.theme=theme;
                for(const density of [0,1,2]){
                  applyDensityMode(density);
                  const button=document.querySelector('#note-1 .note-privacy-toggle'),rect=button.getBoundingClientRect();
                  if(!button.getClientRects().length||rect.left<0||rect.right>innerWidth||rect.width<32||rect.height<32||document.documentElement.scrollWidth>innerWidth)throw new Error('Privacy padlock unavailable or overflows in '+theme+' density '+density);
                }
              }
              applyDensityMode(0);document.documentElement.dataset.theme='light';GnotesPrivacy.open();`)
			browser.wait(`document.getElementById('privacy-dialog').open && !document.querySelector('[data-privacy-mode="change"]').disabled`)
			browser.script(`document.querySelector('[data-privacy-mode="change"]').click();document.getElementById('privacy-code').value='0012';document.getElementById('privacy-new').value=document.getElementById('privacy-confirm').value='4567';document.getElementById('privacy-titles').checked=true;document.getElementById('privacy-form').requestSubmit();`)
			browser.wait(`!document.getElementById('privacy-dialog').open`)
			browser.script(`toggleFavoritesMode();`)
			browser.wait(`favoritesMode && !notesLoading && loadedNotes.get(2)?.privacy_locked`)
			browser.script(`if(loadedNotes.get(2).title||document.getElementById('note-2').textContent.includes('Archived secret'))throw new Error('Hidden title escaped');`)
			collectionClick(browser, "#note-2 .privacy-placeholder button")
			browser.wait(`!document.getElementById('privacy-code').disabled && document.getElementById('privacy-dialog').open`)
			browser.script(`document.getElementById('privacy-forgot').click();document.getElementById('privacy-new').value=document.getElementById('privacy-confirm').value='8888';document.getElementById('privacy-password').value='wrong password';document.getElementById('privacy-form').requestSubmit();`)
			browser.wait(`document.getElementById('privacy-status').textContent.includes('incorrect')`)
			browser.script(`document.getElementById('privacy-password').value='original password';document.getElementById('privacy-form').requestSubmit();`)
			browser.wait(`!document.getElementById('privacy-dialog').open && !notesLoading`)
			browser.script(`if(!loadedNotes.get(2).privacy_locked)throw new Error('Recovery unexpectedly unlocked note');GnotesPrivacy.open();`)
			browser.wait(`document.getElementById('privacy-dialog').open && !document.querySelector('[data-privacy-mode="unlock"]').disabled`)
			browser.script(`document.querySelector('[data-privacy-mode="unlock"]').click();document.getElementById('privacy-code').value='8888';document.getElementById('privacy-form').requestSubmit();`)
			browser.wait(`!document.getElementById('privacy-dialog').open && !notesLoading && !loadedNotes.get(2)?.privacy_locked`)
			browser.script(`if(loadedNotes.get(2).content!=='ArchivedOrchid body'||!loadedNotes.get(2).locked||!loadedNotes.get(2).archived_at||!loadedNotes.get(2).favorited)throw new Error('Unlock all changed text/read-only/archive/favorite state');if([...document.querySelectorAll('#privacy-dialog input:not([type=checkbox])')].some(input=>input.value))throw new Error('Dialog retained secrets');if(document.documentElement.scrollWidth>innerWidth)throw new Error('Privacy controls overflow');if(privacyErrors.length)throw new Error(privacyErrors.join('; '));`)
			browser.navigate(server.URL)
			browser.wait(`!notesLoading && currentUser?.username==='rick' && Boolean(window.GnotesPrivacy)`)
			browser.script(`loadedNotes.get(1).title='';renderLoadedNotes();`)
			collectionClick(browser, "#note-1 .note-privacy-toggle")
			browser.wait(`!notesLoading && loadedNotes.get(1)?.privacy_locked`)
			browser.script(`if(document.getElementById('privacy-dialog').open)throw new Error('Lock after reload asked for code');`)
		})
	}
}
