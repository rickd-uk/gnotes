(() => {
  'use strict';
  const store = window.GnotesOfflineStore;
  const $ = id => document.getElementById(id);
  let notes = [], record = null, session = null, busy = false, generation = 0, visibleCount = 50, lockTimer;
  const supported = isSecureContext && !!crypto.subtle && !!window.indexedDB && 'serviceWorker' in navigator;
  function status(text, error = false) { $('offline-status').textContent = text; $('offline-status').classList.toggle('is-error', error); }
  function lock() {
    generation++;
    notes = [];
    clearTimeout(lockTimer);
    $('offline-notes').replaceChildren();
    $('offline-copy-details').textContent = '';
    $('offline-count').textContent = '';
    $('offline-collection').value = 'all';
    $('offline-search').value = '';
    $('offline-reading').hidden = true;
    $('offline-lock').hidden = true;
    $('offline-passphrase').value = '';
    $('offline-unlock-form').hidden = !record;
  }
  function idle() { clearTimeout(lockTimer); lockTimer = setTimeout(() => { lock(); status('Saved notes locked.'); }, 5 * 60 * 1000); }
  function updateControls() {
    $('offline-forget').hidden = !record;
    $('offline-cancel-setup').hidden = true;
    $('offline-refresh').hidden = !record || !session;
    $('offline-unlock-form').hidden = !record || !$('offline-reading').hidden;
    $('offline-setup').hidden = !!record || !session;
    $('offline-account').textContent = session ? `Save a copy for ${session.username}. Notes are displayed as text; recycled notes and unfinished drafts are excluded.` : '';
  }
  async function account() {
    try {
      const response = await fetch('/api/auth/me', { cache: 'no-store' });
      return response.ok ? await response.json() : null;
    } catch { return null; }
  }
  function render() {
    const query = $('offline-search').value.trim().toLocaleLowerCase();
    const collection = $('offline-collection').value;
    const matches = notes.filter(note => (collection === 'all' || collection === 'active' && !note.archived_at || collection === 'archived' && note.archived_at || collection === 'favorites' && note.favorited)
      && (!query || [note.title, note.content, ...(note.tags || [])].join('\n').toLocaleLowerCase().includes(query)));
    const fragment = document.createDocumentFragment();
    for (const note of matches.slice(0, visibleCount)) {
      const card = document.createElement('article');
      const title = document.createElement('h3'); title.textContent = note.title || 'Untitled note';
      const meta = document.createElement('p'); meta.className = 'note-meta';
      meta.textContent = [new Date(note.created_at).toLocaleString(), note.archived_at ? 'Archived' : '', note.favorited ? 'Favorite' : '', ...(note.tags || []).map(tag => '#'+tag)].filter(Boolean).join(' · ');
      const body = document.createElement('div'); body.className = 'note-body'; body.textContent = note.content;
      card.append(title, meta, body); fragment.append(card);
    }
    $('offline-notes').replaceChildren(fragment);
    $('offline-count').textContent = `${matches.length} ${matches.length === 1 ? 'note' : 'notes'} · Read only`;
    $('offline-more').hidden = matches.length <= visibleCount;
  }
  async function prepareReader() {
    await navigator.serviceWorker.register('/offline-sw.js', { scope: '/' });
    let timeout;
    const ready = await Promise.race([
      navigator.serviceWorker.ready,
      new Promise((_, reject) => { timeout = setTimeout(() => reject(new Error('Offline reader could not finish installing. Reconnect and try again.')), 15000); }),
    ]).finally(() => clearTimeout(timeout));
    if (!ready.active) throw new Error('Offline reader could not be installed. Try again while online.');
    const cache = await caches.open('gnotes-offline-reader-v1');
    for (const path of ['/offline.html', '/offline.css', '/offline-store.js', '/offline-reader.js']) {
      if (!await cache.match(path)) throw new Error('Offline reader is incomplete. Reconnect and try again.');
    }
  }
  async function run(action) {
    if (busy) return;
    busy = true;
    for (const button of document.querySelectorAll('button')) button.disabled = true;
    try { await action(); } catch (error) { status(error.message || 'Could not complete that action. Try again.', true); }
    finally { busy = false; for (const button of document.querySelectorAll('button')) button.disabled = false; }
  }
  $('offline-save-form').addEventListener('submit', event => {
    event.preventDefault();
    run(async () => {
      if (!supported) throw new Error('Offline reading needs a browser with secure device storage.');
      const passphrase = $('offline-new-passphrase').value;
      if (passphrase.length < 12 || passphrase !== $('offline-confirm-passphrase').value || !$('offline-consent').checked) throw new Error('Confirm your passphrase and choose to save on this device.');
      const revision = store.revision();
      const expectedUser = session?.username;
      status('Preparing your offline copy…');
      const response = await fetch('/api/notes/offline', { cache: 'no-store' });
      if (!response.ok) throw new Error(response.status === 401 ? 'Sign in online before saving a copy.' : 'Could not prepare the copy. Your previous copy is unchanged.');
      const snapshot = await response.json();
      if (!expectedUser || snapshot.username !== expectedUser) throw new Error('The signed-in account changed. Return to online notes and try again.');
      const previous = await store.get();
      if (previous) {
        let old;
        try { old = await store.decrypt(previous, passphrase); } catch { throw new Error('Use the existing offline passphrase to refresh, or remove the saved copy first.'); }
        if (old.owner !== snapshot.owner) throw new Error('Remove the other account’s saved copy before saving this account.');
      }
      await prepareReader();
      const encrypted = await store.encrypt(snapshot, passphrase);
      const latestSession = await account();
      if (latestSession?.username !== expectedUser) throw new Error('Your session changed. Sign in online and try again.');
      await store.save(encrypted, revision);
      record = encrypted;
      $('offline-save-form').reset();
      lock(); updateControls();
      navigator.storage?.persist?.().catch(() => {});
      status(`${snapshot.notes.length} ${snapshot.notes.length === 1 ? 'note' : 'notes'} saved for offline reading. Unlock them below. Refresh this copy after changing notes online.`);
    }).finally(() => { $('offline-new-passphrase').value = ''; $('offline-confirm-passphrase').value = ''; });
  });
  $('offline-unlock-form').addEventListener('submit', event => {
    event.preventDefault();
    run(async () => {
      const attempt = generation;
      const revision = store.revision();
      status('Unlocking saved notes…');
      let snapshot;
      try { snapshot = await store.decrypt(record, $('offline-passphrase').value); }
      catch { throw new Error('Could not unlock. Check your offline passphrase; if the copy is damaged, save a new one while online.'); }
      finally { $('offline-passphrase').value = ''; }
      if (generation !== attempt || revision !== store.revision() || document.hidden) return;
      notes = snapshot.notes.filter(note => !note.deleted_at);
      $('offline-unlock-form').hidden = true; $('offline-setup').hidden = true;
      $('offline-reading').hidden = false; $('offline-lock').hidden = false;
      $('offline-copy-details').textContent = `${snapshot.username} · Copy saved ${new Date(snapshot.saved_at).toLocaleString()}`;
      visibleCount = 50; render(); idle(); status('Reading saved notes. This copy may be older than your online notes.');
    });
  });
  $('offline-refresh').addEventListener('click', () => {
    lock(); $('offline-setup').hidden = false; $('offline-unlock-form').hidden = true; $('offline-cancel-setup').hidden = false;
    $('offline-account').textContent = `Refresh the copy for ${session.username} using its existing offline passphrase.`;
    status('Save again while online to replace the copy with your latest active and archived notes.');
  });
  $('offline-cancel-setup').addEventListener('click', () => { $('offline-save-form').reset(); updateControls(); status('Unlock your existing saved copy below.'); });
  $('offline-lock').addEventListener('click', () => { lock(); status('Saved notes locked.'); });
  $('offline-forget').addEventListener('click', () => {
    if (!confirm('Remove the offline copy from this browser? Your online notes will remain available.')) return;
    run(async () => { await store.clear(); record = null; lock(); updateControls(); status('Saved copy removed from this browser.'); });
  });
  for (const id of ['offline-search', 'offline-collection']) $(id).addEventListener('input', () => { visibleCount = 50; render(); idle(); });
  $('offline-more').addEventListener('click', () => { visibleCount += 50; render(); idle(); });
  for (const event of ['pointerdown', 'keydown', 'scroll']) document.addEventListener(event, () => { if (!$('offline-reading').hidden) idle(); }, { passive: true });
  document.addEventListener('visibilitychange', () => { if (document.hidden) { lock(); status('Saved notes locked.'); } });
  window.addEventListener('pagehide', lock);
  async function externalChange() { lock(); record = await store.get(); updateControls(); status(record ? 'Saved copy changed in another tab. Unlock it again.' : 'Saved copy removed.'); }
  store.channel?.addEventListener('message', externalChange);
  window.addEventListener('storage', event => { if (event.key === 'gnotes-offline-revision') externalChange(); });
  window.addEventListener('online', async () => { session = await account(); updateControls(); });
  (async () => {
    if (!supported) { status('Offline reading is unavailable in this browser. Use an HTTPS browser with device storage enabled.', true); return; }
    try {
      [record, session] = await Promise.all([store.get(), account()]);
      updateControls();
      status(record ? 'An encrypted copy is saved on this device. Enter your offline passphrase to read it.'
        : session ? 'Offline reading is off. Choose a passphrase to save a copy on this device.' : 'No offline copy is saved. Sign in online first, then choose Offline reading from your account menu.');
    } catch { status('Device storage is unavailable. Check your browser’s storage settings.', true); }
  })();
})();
