/* One account-owned privacy code. Never retain codes or passwords in storage. */
window.GnotesPrivacy = (() => {
  'use strict';
  const dialog = document.createElement('dialog');
  dialog.id = 'privacy-dialog';
  dialog.className = 'note-action-dialog';
  dialog.setAttribute('aria-labelledby', 'privacy-heading');
  dialog.innerHTML = `
    <h2 id="privacy-heading">Note privacy</h2>
    <p id="privacy-help"></p>
    <div id="privacy-manage">
      <button type="button" data-privacy-mode="setup">Set privacy code</button>
      <button type="button" data-privacy-mode="change">Change code</button>
      <button type="button" data-privacy-mode="unlock">Unlock all notes</button>
      <button type="button" data-privacy-mode="reset">Forgot code?</button>
    </div>
    <form id="privacy-form" class="auth-form">
      <label id="privacy-code-label">Privacy code<input id="privacy-code" type="password" inputmode="numeric" pattern="[0-9]{4,12}" minlength="4" maxlength="12" autocomplete="off" /></label>
      <label id="privacy-new-label">New code<input id="privacy-new" type="password" inputmode="numeric" pattern="[0-9]{4,12}" minlength="4" maxlength="12" autocomplete="new-password" /></label>
      <label id="privacy-confirm-label">Confirm new code<input id="privacy-confirm" type="password" inputmode="numeric" pattern="[0-9]{4,12}" minlength="4" maxlength="12" autocomplete="new-password" /></label>
      <label id="privacy-password-label">Account password<input id="privacy-password" type="password" maxlength="72" autocomplete="current-password" /></label>
      <label id="privacy-mfa-label">Authenticator or recovery code<input id="privacy-mfa" type="text" maxlength="64" autocomplete="one-time-code" /></label>
      <label id="privacy-titles-label" class="privacy-checkbox"><input id="privacy-titles" type="checkbox" /> Hide titles too</label>
      <button type="button" id="privacy-forgot">Forgot code?</button>
      <button type="submit" id="privacy-submit">Save code</button>
    </form>
    <p id="privacy-status" role="status" aria-live="polite"></p>
    <footer><button type="button" id="privacy-close">Close</button></footer>`;
  document.body.append(dialog);
  const el = id => document.getElementById(id);
  let revision = 0, busy = false, mode = 'manage', target = null, settings = null;
  function secrets() {
    for (const id of ['privacy-code','privacy-new','privacy-confirm','privacy-password','privacy-mfa']) el(id).value = '';
  }
  function status(message = '', error = false) {
    el('privacy-status').textContent = message;
    el('privacy-status').dataset.error = String(error);
  }
  function controls() {
    dialog.querySelectorAll('input,button').forEach(node => { node.disabled = busy; });
  }
  function reset() {
    revision++; busy = false; target = null; settings = null;
    secrets(); status(); controls();
    if (dialog.open) dialog.close();
  }
  function choose(next) {
    secrets(); status(); mode = next;
    const manage = mode === 'manage', configure = ['setup','change','reset'].includes(mode);
    el('privacy-manage').hidden = !manage;
    el('privacy-form').hidden = manage;
    el('privacy-heading').textContent = {manage:'Note privacy',setup:'Set privacy code',change:'Change privacy code',reset:'Recover privacy code',unlock:target === null ? 'Unlock all notes' : 'Unlock note'}[mode];
    el('privacy-help').textContent = manage
      ? (settings.configured ? `${settings.locked_count} privacy-locked notes. Use the padlock beside any note to hide it with one click.` : 'Set one code for all your private notes. Locking needs no code; unlocking does. Recover a forgotten code with your account password.')
      : mode === 'unlock' ? 'Enter your shared privacy code.'
      : mode === 'change' ? 'Enter your current code and choose a new code of 4–12 digits. Locked notes stay locked.'
      : mode === 'reset' ? 'Verify your account password and choose a new code of 4–12 digits. No old privacy code is needed. Your notes are kept and stay locked.'
      : 'Choose a code of 4–12 digits and verify your account password. You can recover a forgotten code with that password.';
    const fields = {
      'privacy-code': mode === 'unlock' || mode === 'change',
      'privacy-new': configure, 'privacy-confirm': configure,
      'privacy-password': mode === 'setup' || mode === 'reset',
      'privacy-mfa': (mode === 'setup' || mode === 'reset') && settings.mfa_enabled,
    };
    for (const [id, visible] of Object.entries(fields)) {
      el(id+'-label').hidden = !visible;
      el(id).required = visible;
    }
    el('privacy-titles-label').hidden = !configure;
    el('privacy-titles').checked = settings.hide_titles;
    el('privacy-forgot').hidden = mode !== 'unlock' && mode !== 'change';
    el('privacy-submit').textContent = mode === 'unlock' ? (target === null ? 'Unlock all notes' : 'Unlock note') : mode === 'reset' ? 'Reset code' : 'Save code';
    dialog.querySelector('[data-privacy-mode="setup"]').hidden = settings.configured;
    for (const next of ['change','unlock','reset']) dialog.querySelector(`[data-privacy-mode="${next}"]`).hidden = !settings.configured;
    const first = Object.keys(fields).find(id => fields[id]);
    if (first) el(first).focus();
  }
  async function open(next = 'manage', id = null) {
    if (!currentUser) return;
    reset(); target = id;
    const version = revision, owner = currentUser, token = csrfToken;
    mode = next; busy = true; controls();
    el('privacy-manage').hidden = el('privacy-form').hidden = true;
    dialog.showModal(); status('Loading privacy settings…');
    try {
      const response = await apiFetch('/api/privacy');
      if (!response.ok) throw new Error((await response.text()).trim() || 'Could not load privacy settings.');
      const data = await response.json();
      if (owner !== currentUser || token !== csrfToken || version !== revision) return;
      settings = data; busy = false; controls();
      choose(!data.configured && next !== 'manage' ? 'setup' : next);
    } catch (error) {
      if (owner === currentUser && version === revision) { busy = false; controls(); status(error.message, true); }
    }
  }
  async function refreshViews() {
    cancelNotesLoad(); noteViewRevision++;
    await Promise.all([loadNotes(), loadTrash()]);
    window.GnotesOfflineSync?.schedule();
  }
  function eraseCachedNote(id, hideTitle) {
    const note = loadedNotes.get(id);
    if (note) {
      note.privacy_locked = true; note.privacy_hide_title = hideTitle;
      note.content = note.html_content = ''; note.tags = [];
      if (hideTitle) note.title = '';
    }
    noteSaveStates.delete(id); hiddenNotes.delete(id);
    renderLoadedNotes();
  }
  async function lock(id) {
    const owner = currentUser, token = csrfToken;
    if (!owner || pendingNoteActions.has(id)) return;
    if (editingNoteId === id && !await saveBeforeNoteAction(id,true)) return;
    if (owner !== currentUser || editingNoteId !== null) return;
    await editSaveChain.catch(() => false);
    if (owner !== currentUser || token !== csrfToken) return;
    if (pendingNoteEdits.has(id)) { showNoteActionToast('Save this note before making it private.'); return; }
    pendingNoteActions.add(id); cancelNotesLoad(); noteViewRevision++;
    document.querySelectorAll(`#note-${id} .note-privacy-toggle`).forEach(button => { button.disabled = true; });
    let setup = false;
    try {
      const response = await apiFetch('/api/privacy/notes',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id,locked:true})});
      if (owner !== currentUser || token !== csrfToken) return;
      if (response.status === 409) {
        const message = (await response.text()).trim();
        if (message === 'Set a privacy code first.') { setup = true; return; }
        throw new Error(message || 'Privacy settings changed. Try again.');
      }
      if (!response.ok) throw new Error((await response.text()).trim() || 'Could not lock note.');
      // The server already redacts all subsequent reads. Remove this tab's copy
      // immediately, before refreshing its list and offline snapshot.
      const data = await response.json();
      if (owner !== currentUser || token !== csrfToken) return;
      eraseCachedNote(id,data.hide_titles);
      showNoteActionToast('Note privacy-locked');
    } catch (error) {
      if (owner === currentUser) showNoteActionToast(error.message || 'Could not confirm the privacy lock. Refresh to check.');
    } finally {
      if (owner === currentUser && token === csrfToken) {
        pendingNoteActions.delete(id);
        document.querySelectorAll(`#note-${id} .note-privacy-toggle`).forEach(button => { button.disabled = false; });
        if (setup) open('setup',id);
        else refreshViews();
      }
    }
  }
  el('privacy-close').onclick = () => { if (!busy) reset(); };
  dialog.addEventListener('cancel',event => { event.preventDefault(); if (!busy) reset(); });
  dialog.addEventListener('close',secrets);
  dialog.querySelectorAll('[data-privacy-mode]').forEach(button => { button.onclick = () => { target = null; choose(button.dataset.privacyMode); }; });
  el('privacy-forgot').onclick = () => choose('reset');
  el('privacy-form').onsubmit = async event => {
    event.preventDefault();
    if (busy || !currentUser || !settings) return;
    const unlocking = mode === 'unlock';
    if (!unlocking && el('privacy-new').value !== el('privacy-confirm').value) { status('New codes do not match.',true); return; }
    const owner = currentUser, token = csrfToken, version = revision, action = mode, noteID = target;
    const body = unlocking
      ? {id:noteID || 0,all:noteID === null,locked:false,code:el('privacy-code').value}
      : {action,code:el('privacy-code').value,new_code:el('privacy-new').value,password:el('privacy-password').value,mfa_code:el('privacy-mfa').value.trim(),hide_titles:el('privacy-titles').checked};
    busy = true; controls(); status(unlocking ? 'Unlocking…' : 'Saving privacy code…');
    try {
      const response = await apiFetch(unlocking ? '/api/privacy/notes' : '/api/privacy/code',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
      if (!response.ok) throw new Error((await response.text()).trim() || 'Could not save privacy settings.');
      if (owner !== currentUser || token !== csrfToken) return;
      if (!unlocking) {
        for (const note of loadedNotes.values()) {
          if (!note.privacy_locked) continue;
          note.privacy_hide_title = body.hide_titles;
          if (body.hide_titles) note.title = '';
        }
        renderLoadedNotes();
      }
      secrets(); await refreshViews();
      if (version !== revision || owner !== currentUser) return;
      reset();
      showNoteActionToast(unlocking ? (noteID === null ? 'All notes unlocked' : 'Note unlocked') : 'Privacy code saved');
      if (action === 'setup' && noteID !== null) await lock(noteID);
    } catch (error) {
      if (owner === currentUser && version === revision) status(error.message || 'Could not confirm the change. Reopen this dialog and try again.',true);
    } finally {
      for (const key of ['code','new_code','password','mfa_code']) if (key in body) body[key] = '';
      if (owner === currentUser && version === revision) { busy = false; controls(); }
    }
  };
  return {open,reset,lock};
})();
