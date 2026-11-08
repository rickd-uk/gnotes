/* Passwords stay in this dialog/request only; never browser storage or logs. */
window.GnotesPassword = (() => {
  'use strict';
  const dialog = document.createElement('dialog');
  dialog.id = 'password-dialog';
  dialog.className = 'note-action-dialog';
  dialog.setAttribute('aria-labelledby', 'password-heading');
  dialog.innerHTML = `
    <h2 id="password-heading">Change password</h2>
    <p>Other devices will be signed out. This device stays signed in.</p>
    <form id="password-form" class="auth-form">
      <label>Current password<input id="password-current" type="password" autocomplete="current-password" maxlength="72" required /></label>
      <label>New password<input id="password-new" type="password" autocomplete="new-password" minlength="12" maxlength="72" required aria-describedby="password-help" /></label>
      <label>Confirm new password<input id="password-confirm" type="password" autocomplete="new-password" minlength="12" maxlength="72" required /></label>
      <p id="password-help">Use 12–72 bytes. Your database recovery key stays the same.</p>
      <label id="password-code-label" hidden>Authenticator or recovery code<input id="password-code" type="text" autocomplete="one-time-code" maxlength="64" spellcheck="false" autocapitalize="characters" /></label>
      <p id="password-code-help" hidden>Use a fresh authenticator code or an unused recovery code.</p>
      <p id="password-status" role="status" aria-live="polite"></p>
      <footer><button id="password-close" type="button">Cancel</button><button id="password-submit" type="submit">Change password</button></footer>
    </form>`;
  document.body.append(dialog);
  const style = document.createElement('style');
  style.textContent = `
    #password-dialog { position:fixed;top:calc(var(--visible-screen-top,0px) + var(--visible-screen-height,100dvh)/2);left:50%;right:auto;bottom:auto;margin:0;transform:translate(-50%,-50%);width:min(480px,calc(100% - 32px));max-height:calc(var(--visible-screen-height,100dvh) - 32px);overflow:auto; }
    #password-dialog [hidden] { display:none!important; }
    #password-dialog .auth-form { margin-top:12px; }
    #password-dialog p { line-height:1.5;overflow-wrap:anywhere; }
    #password-dialog footer { display:flex;justify-content:flex-end;flex-wrap:wrap;gap:12px; }
    #password-dialog button { margin:0;min-height:40px; }
    #password-close { border:1px solid var(--line);background:transparent;color:var(--ink); }
    #password-status[data-error="true"] { color:var(--danger); }
    #password-dialog button:disabled { opacity:.55;cursor:wait; }`;
  document.head.append(style);
  const el = id => document.getElementById(id);
  let revision = 0, busy = false;
  function status(message = '', error = false) {
    el('password-status').textContent = message;
    el('password-status').dataset.error = String(error);
  }
  function controls() {
    el('password-submit').disabled = busy;
    el('password-close').disabled = busy;
    dialog.querySelectorAll('input').forEach(input => { input.disabled = busy; });
  }
  function clearSecrets() {
    for (const id of ['password-current', 'password-new', 'password-confirm', 'password-code']) el(id).value = '';
  }
  function reset() {
    revision++;
    busy = false;
    clearSecrets();
    status();
    controls();
    if (dialog.open) dialog.close();
  }
  async function open() {
    if (!currentUser || dialog.open) return;
    reset();
    const version = revision, owner = currentUser, token = csrfToken;
    busy = true;
    controls();
    el('password-code-label').hidden = el('password-code-help').hidden = true;
    el('password-code').required = false;
    el('password-close').textContent = 'Cancel';
    dialog.showModal();
    status('Loading security settings…');
    try {
      const response = await apiFetch('/api/auth/mfa');
      if (!response.ok) throw new Error('Could not load security settings. Close and try again.');
      const data = await response.json();
      if (version !== revision || owner !== currentUser) return;
      if (token !== csrfToken) throw new Error('Your session changed. Close and try again.');
      el('password-code-label').hidden = el('password-code-help').hidden = !data.enabled;
      el('password-code').required = !!data.enabled;
      status();
      busy = false;
      controls();
      el('password-current').focus();
    } catch (error) {
      if (version !== revision || owner !== currentUser) return;
      status(error.message, true);
      busy = false;
      controls();
      el('password-submit').disabled = true;
    }
  }
  el('password-close').addEventListener('click', () => { if (!busy) reset(); });
  dialog.addEventListener('cancel', event => { event.preventDefault(); if (!busy) reset(); });
  dialog.addEventListener('close', clearSecrets);
  el('password-form').addEventListener('submit', async event => {
    event.preventDefault();
    if (busy || !currentUser) return;
    if (el('password-new').value !== el('password-confirm').value) {
      status('New passwords do not match.', true);
      el('password-confirm').focus();
      return;
    }
    const bytes = new TextEncoder().encode(el('password-new').value).byteLength;
    if (bytes < 12 || bytes > 72) { status('New password must be 12–72 bytes.', true); return; }
    const owner = currentUser, token = csrfToken, version = revision;
    const input = {current_password: el('password-current').value, new_password: el('password-new').value, code: el('password-code').value.trim()};
    busy = true;
    controls();
    status('Saving outstanding notes…');
    try {
      // Finish saves before rotating this tab's cookie/CSRF pair. The modal
      // prevents further note typing while account security is changing.
      flushPendingSaves();
      await Promise.all([editSaveChain, draftSaveChain]);
      if (owner !== currentUser || token !== csrfToken || version !== revision) return;
      const edit = editingNoteId === null ? null : currentEdit(editingNoteId);
      if (pendingNoteEdits.size || draftVersion > draftSavedVersion || (edit && !sameNoteText(edit, noteSaveStates.get(editingNoteId)?.saved))) {
        throw new Error('Save your outstanding note changes before changing your password. Your text is still available.');
      }
      status('Changing password…');
      const response = await apiFetch('/api/auth/password', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(input)});
      if (!response.ok) throw new Error((await response.text()).trim() || 'Could not change password.');
      const result = await response.json();
      if (owner !== currentUser || token !== csrfToken) return;
      csrfToken = result.csrf_token;
      currentUser.csrf_token = csrfToken;
      clearSecrets();
      // Always adopt a successful session rotation, even if the dialog closed.
      if (version !== revision) return;
      status(result.message);
      el('password-close').textContent = 'Done';
      window.GnotesMFA?.refresh();
    } catch (error) {
      if (owner === currentUser && version === revision) {
        status(error instanceof TypeError
          ? 'Could not confirm the change. Check your connection; try signing in with the new password in another tab. Keep any unsaved text available.'
          : error.message || 'Could not confirm the password change.', true);
      }
    } finally {
      input.current_password = input.new_password = input.code = '';
      if (owner === currentUser && version === revision) {
        busy = false;
        controls();
      }
    }
  });
  return {open, reset};
})();
