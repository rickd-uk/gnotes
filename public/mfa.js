/* Authenticator secrets and recovery codes live only in this dialog, never storage. */
window.GnotesMFA = (() => {
  'use strict';
  const dialog = document.createElement('dialog');
  dialog.id = 'mfa-dialog';
  dialog.className = 'note-action-dialog mfa-dialog';
  dialog.setAttribute('aria-labelledby', 'mfa-heading');
  dialog.innerHTML = `
    <h2 id="mfa-heading">Two-step verification</h2>
    <p id="mfa-summary">Loading…</p>
    <form id="mfa-setup-form" class="auth-form" hidden>
      <p>Use an authenticator app for a second sign-in code. Your password is still required. Setup also provides one-time recovery codes.</p>
      <label>Current password<input id="mfa-setup-password" type="password" autocomplete="current-password" maxlength="72" required /></label>
      <button type="submit">Set up authenticator</button>
    </form>
    <form id="mfa-enable-form" class="auth-form" hidden>
      <p>Scan this QR code in your authenticator app, or enter the setup key manually. Setup expires after 10 minutes.</p>
      <img id="mfa-qr" width="240" height="240" alt="Authenticator setup QR code" />
      <label>Setup key<input id="mfa-secret" type="text" readonly autocomplete="off" spellcheck="false" aria-label="Authenticator setup key" /></label>
      <label>6-digit authenticator code<input id="mfa-enable-code" type="text" inputmode="numeric" autocomplete="one-time-code" pattern="[0-9]{6}" maxlength="6" required /></label>
      <p>Enabling two-step verification signs out other devices. This device stays signed in with a new session.</p>
      <button type="submit">Verify and enable</button>
    </form>
    <form id="mfa-manage-form" class="auth-form" hidden>
      <p>Use a new authenticator code or an unused recovery code. Changes sign out other devices and replace this device’s session.</p>
      <label>Current password<input id="mfa-manage-password" type="password" autocomplete="current-password" maxlength="72" required /></label>
      <label>Authenticator or recovery code<input id="mfa-manage-code" type="text" autocomplete="one-time-code" autocapitalize="characters" spellcheck="false" maxlength="64" required /></label>
      <p>New recovery codes replace all your previous codes. Disabling removes the second sign-in step.</p>
      <div class="mfa-buttons"><button type="submit" value="recovery-codes">New recovery codes</button><button type="submit" value="disable" class="mfa-disable">Disable two-step verification</button></div>
    </form>
    <section id="mfa-codes-panel" hidden>
      <h3>Save your recovery codes</h3>
      <p>Each code works once, together with your password, if you lose access to your authenticator. They are shown only now. Keep them somewhere separate from this device.</p>
      <ul id="mfa-recovery-codes"></ul>
      <button type="button" id="mfa-download-codes">Download recovery codes</button>
      <label class="mfa-saved"><input id="mfa-codes-saved" type="checkbox" /> I have saved these recovery codes.</label>
    </section>
    <p id="mfa-status" role="status" aria-live="polite"></p>
    <footer><button type="button" id="mfa-close">Done</button></footer>`;
  document.body.append(dialog);
  const style = document.createElement('style');
  style.textContent = `
    #auth-mfa-label[hidden],.mfa-dialog [hidden] { display:none!important; }
    .mfa-dialog { position:fixed;top:calc(var(--visible-screen-top,0px) + var(--visible-screen-height,100dvh)/2);left:50%;right:auto;bottom:auto;margin:0;transform:translate(-50%,-50%);width:min(520px,calc(100% - 32px));max-height:calc(var(--visible-screen-height,100dvh) - 32px);overflow:auto; }
    .mfa-dialog p { line-height:1.5; overflow-wrap:anywhere; }
    .mfa-dialog .auth-form { margin:12px 0; }
    .mfa-dialog .mfa-buttons { display:flex;flex-wrap:wrap;gap:8px; }
    .mfa-dialog button { margin:0; min-height:40px; }
    .mfa-dialog .mfa-disable { background:var(--danger);color:var(--paper); }
    #mfa-qr { display:block;max-width:100%;height:auto;margin:12px auto;background:white; }
    #mfa-secret { font-family:monospace;font-size:.8rem; }
    #mfa-recovery-codes { padding:12px;list-style:none;border:1px solid var(--line);border-radius:6px; }
    #mfa-recovery-codes li { overflow-wrap:anywhere;font-family:monospace;font-size:.8rem; }
    .mfa-saved { display:flex;align-items:start;gap:8px;margin:14px 0; }
    .mfa-saved input { width:20px;height:20px;flex-shrink:0; }
    #mfa-status[data-error="true"] { color:var(--danger); }
    .mfa-dialog footer { display:flex;justify-content:flex-end; }
    .mfa-dialog button:disabled { opacity:.55;cursor:wait; }`;
  document.head.append(style);
  const el = id => document.getElementById(id);
  let revision = 0, busy = false, setupID = '', codes = [];
  const panels = ['mfa-setup-form', 'mfa-enable-form', 'mfa-manage-form', 'mfa-codes-panel'];
  function status(message = '', error = false) {
    el('mfa-status').textContent = message;
    el('mfa-status').dataset.error = String(error);
  }
  function controls() {
    dialog.querySelectorAll('button').forEach(button => { button.disabled = busy; });
    el('mfa-close').disabled = busy || (!el('mfa-codes-panel').hidden && !el('mfa-codes-saved').checked);
  }
  function panel(id) {
    panels.forEach(name => { el(name).hidden = name !== id; });
    controls();
  }
  function clearSecrets() {
    setupID = '';
    codes = [];
    dialog.querySelectorAll('form').forEach(form => form.reset());
    el('mfa-secret').value = '';
    el('mfa-qr').removeAttribute('src');
    el('mfa-recovery-codes').replaceChildren();
    el('mfa-codes-saved').checked = false;
  }
  function resetLogin() {
    el('auth-mfa-label').hidden = true;
    el('auth-mfa-code').disabled = true;
    el('auth-mfa-code').required = false;
    el('auth-mfa-code').value = '';
    el('auth-submit').textContent = authMode === 'register' ? 'Create account' : 'Sign in';
  }
  function showLogin() {
    el('auth-mfa-label').hidden = false;
    el('auth-mfa-code').disabled = false;
    el('auth-mfa-code').required = true;
    el('auth-mfa-code').value = '';
    el('auth-submit').textContent = 'Verify and sign in';
    el('auth-mfa-code').focus();
  }
  function reset() {
    revision++;
    busy = false;
    clearSecrets();
    if (dialog.open) dialog.close();
    resetLogin();
  }
  async function request(path, input) {
    const owner = currentUser, token = csrfToken;
    const response = await apiFetch(path, input === undefined ? {} : {
      method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(input),
    });
    if (!response.ok) throw new Error((await response.text()).trim() || 'Could not update two-step verification.');
    const result = await response.json();
    if (owner !== currentUser || token !== csrfToken) throw new Error('Your session changed. Open these settings again.');
    // Adopt rotated CSRF immediately, even if the dialog was reset meanwhile.
    if (result.csrf_token) { csrfToken = result.csrf_token; currentUser.csrf_token = csrfToken; }
    return result;
  }
  function summary(data) {
    const text = data.enabled ? `Enabled · ${data.recovery_codes_remaining} recovery codes left` : 'Not enabled';
    el('mfa-account-status').textContent = text;
    el('mfa-summary').textContent = text;
  }
  async function refresh() {
    const owner = currentUser, token = csrfToken;
    if (!owner) return;
    try { const data = await request('/api/auth/mfa'); if (owner === currentUser) summary(data); return data; }
    catch { if (owner === currentUser && token === csrfToken) el('mfa-account-status').textContent = 'Status unavailable. Open settings to retry.'; }
  }
  async function perform(work) {
    if (busy) return;
    const version = revision, owner = currentUser;
    busy = true; controls(); status('Working…');
    try { await work(() => version === revision && currentUser === owner && dialog.open); }
    catch (error) { if (version === revision && currentUser === owner) status(error.message || 'Could not reach the server.',true); }
    finally { if (version === revision) { busy = false; controls(); } }
  }
  async function prepareChange() {
    // Finish pending writes before rotating the browser's authentication tokens.
    if (editingNoteId !== null && !await queueEditAutosave(editingNoteId)) throw new Error('Save your note before changing security settings.');
    if (draftVersion > draftSavedVersion && !await queueDraftSave()) throw new Error('Save your draft before changing security settings.');
    await Promise.all([editSaveChain.catch(() => false),draftSaveChain.catch(() => false)]);
    if (pendingNoteEdits.size) throw new Error('Save your note before changing security settings.');
  }
  function showCodes(data) {
    setupID = '';
    el('mfa-secret').value = '';
    el('mfa-qr').removeAttribute('src');
    codes = data.recovery_codes || [];
    el('mfa-recovery-codes').replaceChildren(...codes.map(code => {
      const item = document.createElement('li'); item.textContent = code; return item;
    }));
    el('mfa-codes-saved').checked = false;
    summary({enabled:true,recovery_codes_remaining:codes.length});
    panel('mfa-codes-panel');
    status('Two-step verification is enabled. Other devices have been signed out.');
    el('mfa-download-codes').focus();
  }
  async function open() {
    if (!currentUser || busy) return;
    revision++; clearSecrets(); status(); panel(null);
    el('mfa-summary').textContent = 'Loading…';
    if (!dialog.open) dialog.showModal();
    await perform(async current => {
      const data = await request('/api/auth/mfa');
      if (!current()) return;
      summary(data); panel(data.enabled ? 'mfa-manage-form' : 'mfa-setup-form'); status();
      el(data.enabled ? 'mfa-manage-password' : 'mfa-setup-password').focus();
    });
  }
  el('mfa-setup-form').addEventListener('submit',event => {
    event.preventDefault();
    const password = el('mfa-setup-password').value;
    perform(async current => {
      try {
        const data = await request('/api/auth/mfa/setup',{password});
        if (!current()) return;
        setupID = data.setup_id;
        el('mfa-secret').value = data.secret;
        el('mfa-qr').src = data.qr_code;
        panel('mfa-enable-form'); status(); el('mfa-enable-code').focus();
      } finally { el('mfa-setup-password').value = ''; }
    });
  });
  el('mfa-enable-form').addEventListener('submit',event => {
    event.preventDefault();
    const code = el('mfa-enable-code').value;
    perform(async current => {
      try {
        await prepareChange();
        if (!current()) return;
        const data = await request('/api/auth/mfa/enable',{setup_id:setupID,code});
        if (current()) showCodes(data);
      } finally { el('mfa-enable-code').value = ''; }
    });
  });
  el('mfa-manage-form').addEventListener('submit',event => {
    event.preventDefault();
    const action = event.submitter?.value || 'recovery-codes';
    const input = {password:el('mfa-manage-password').value,code:el('mfa-manage-code').value};
    perform(async current => {
      try {
        await prepareChange();
        if (!current()) return;
        const data = await request(`/api/auth/mfa/${action}`,input);
        if (!current()) return;
        if (data.enabled) showCodes(data);
        else { summary({enabled:false}); panel('mfa-setup-form'); status('Two-step verification disabled. Other devices have been signed out.'); }
      } finally { el('mfa-manage-password').value = ''; el('mfa-manage-code').value = ''; }
    });
  });
  function close() {
    if (busy) return;
    if (!el('mfa-codes-panel').hidden && !el('mfa-codes-saved').checked) { status('Save your recovery codes before closing.',true); return; }
    dialog.close();
  }
  el('mfa-close').addEventListener('click',close);
  el('mfa-codes-saved').addEventListener('change',controls);
  dialog.addEventListener('cancel',event => { event.preventDefault(); close(); });
  dialog.addEventListener('click',event => { if (event.target === dialog) { const r = dialog.getBoundingClientRect(); if (event.clientX<r.left || event.clientX>r.right || event.clientY<r.top || event.clientY>r.bottom) close(); } });
  dialog.addEventListener('close',() => {
    if (dialog.open) return;
    const id = setupID;
    revision++; clearSecrets(); busy = false;
    if (id && currentUser) request('/api/auth/mfa/cancel',{setup_id:id}).catch(() => {});
  });
  el('mfa-download-codes').addEventListener('click',() => {
    if (!codes.length || !currentUser) return;
    const content = `gnotes recovery codes for ${currentUser.username}\nEach code works once, together with your password.\nKeep this file somewhere safe and separate from your authenticator.\n\n${codes.join('\n')}\n`;
    const url = URL.createObjectURL(new Blob([content],{type:'text/plain'}));
    const link = document.createElement('a'); link.href = url; link.download = 'gnotes-recovery-codes.txt';
    document.body.append(link); link.click(); link.remove(); setTimeout(() => URL.revokeObjectURL(url),1000);
  });
  for (const id of ['auth-username','auth-password']) el(id).addEventListener('input',() => { authRequestVersion++; resetLogin(); });
  window.addEventListener('pagehide',clearSecrets);
  return {open,refresh,reset,resetLogin,showLogin};
})();
