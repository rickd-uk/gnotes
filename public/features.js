/* Account-owned feature choices. Browser UI state never overwrites these. */
window.GnotesFeatures = (() => {
  const groups = {
    organization: ['Organization', 'Tags, Favorites, pinning and Archive'],
    writing_tools: ['Writing tools', 'Dictionary, spelling and custom shortcuts'],
    note_controls: ['Note controls', 'Read-only, hiding, colors, quick actions and bulk actions'],
    appearance: ['Appearance', 'Fonts, density, folding and display controls'],
    advanced_editing: ['Advanced editing', 'Markdown source and Vim'],
    offline_reading: ['Offline reading', 'Read saved notes without a connection'],
  };
  const selectors = {
    organization: '#favorites-quick-open,#archived-quick-open,#favorites-main-toggle,#unpin-all-notes,[data-note-action="favorite"],[data-note-action="archive"],[data-note-action="pin"],[aria-label="Edit note tags"],.date-section-favorite,#search-tag,.note-tags',
    writing_tools: '.dictionary-ui,#shortcuts-dialog,[data-rich-dictionary],#dictionary-open,#spelling-open,#shortcuts-open,#note-selection-actions,#note-context-dictionary,#note-context-ignore',
    note_controls: '[aria-label="Note actions"].controls-group,.customize-note-buttons,[data-note-action="lock"],[data-note-action="hide"],[data-note-action="color"],[data-note-action="copy"],#hidden-notes-control,.active-section-remove,.active-notes-bulk-actions,#account-maintenance-section,#controls-toggle',
    appearance: '#density-toggle,#date-sections-toggle,#font-settings > summary,.note-roll-toggle,.new-note-collapse,.date-section-chevron,#new-note-area-toggle,#link-style-toggle,.time-zone-control,.menu-opacity-control,#font-choices,.font-control-label,.note-text-preview,#note-max-height',
    advanced_editing: '#account-advanced,#markdown-help,.vim-toggle',
    offline_reading: '.offline-reading-link',
  };
  const style = document.createElement('style');
  style.textContent = Object.entries(selectors).map(([key, list]) => list.split(',').map(s => `body[data-feature-${key}="off"] ${s}`).join(',') + '{display:none!important}').join('\n');
  document.head.append(style);
  let prefs = Object.fromEntries(Object.keys(groups).map(key => [key, true]));
  let busy = false;
  let refreshing = false;
  let revision = 0;
  const enabled = key => prefs[key] !== false;
  const dialog = document.createElement('dialog');
  dialog.id = 'features-dialog';
  dialog.className = 'note-action-dialog features-dialog';
  dialog.setAttribute('aria-labelledby', 'features-heading');
  dialog.innerHTML = `<h2 id="features-heading">More features</h2><p>Keep writing simple, or enable the tools you want. Choices follow your account across devices. Turning tools off keeps your notes and their data.</p>
    <div class="feature-choices">${Object.entries(groups).map(([key, [name, description]]) => `<label><input type="checkbox" data-feature="${key}"><span><strong>${name}</strong><small>${description}</small></span></label>`).join('')}</div>
    <p id="features-status" role="status"></p><footer><button type="button" id="features-full">Full mode</button><button type="button" id="features-simple">Restore Simple defaults</button><button type="button" id="features-close">Done</button></footer>`;
  document.body.append(dialog);
  function syncControls() {
    dialog.querySelectorAll('[data-feature]').forEach(input => { input.checked = enabled(input.dataset.feature); });
    document.getElementById('feature-theme').value = prefs.theme || 'light';
    document.getElementById('features-open').setAttribute('aria-label', `More features: ${Object.keys(groups).every(enabled) ? 'Full mode' : Object.keys(groups).some(enabled) ? 'Custom mode' : 'Simple mode'}`);
  }
  function apply(next) {
    prefs = { ...next };
    for (const key of Object.keys(groups)) document.body.setAttribute(`data-feature-${key}`, enabled(key) ? 'on' : 'off');
    document.documentElement.dataset.theme = prefs.theme || 'light';
    syncControls();
    document.getElementById("font-settings").open = !enabled("appearance");
  }
  async function reconcile() {
    if (!enabled('advanced_editing')) {
      if (vimModeRequested) await setVimMode(false, {focus:false, persist:false});
      if (markdownSource) applyEditorMode(false);
    }
    applySpelling(spellcheckEnabled, ignoredSpellingWords, spellingNames);
    applyDensityMode(densityIndex);
    applyNewNoteArea(showNewNoteArea);
    refreshQuickActions();
    if (!enabled('organization')) {
      if (favoritesMode) await toggleFavoritesMode();
      if (archiveMode) await toggleArchiveMode();
    }
    await renderLoadedNotes();
    window.GnotesDictionary?.refreshSpelling();
  }
  async function save(change) {
    if (busy || !currentUser) return;
    const owner = currentUser;
    busy = true;
    revision++;
    dialog.querySelectorAll('input,button').forEach(el => { el.disabled = true; });
    document.getElementById('feature-theme').disabled = true;
    const status = document.getElementById('features-status');
    status.textContent = 'Saving…';
    try {
      const response = await apiFetch('/api/features', {method:'PUT', headers:{'Content-Type':'application/json'}, body:JSON.stringify(change)});
      if (!response.ok) throw new Error('Could not save. Please try again.');
      const next = await response.json();
      if (currentUser !== owner) return;
      apply(next);
      await reconcile();
      status.textContent = 'Saved for your account.';
    } catch (error) {
      if (currentUser === owner) { syncControls(); status.textContent = error.message; showNoteActionToast(error.message); }
    } finally {
      busy = false;
      dialog.querySelectorAll('input,button').forEach(el => { el.disabled = false; });
      document.getElementById('feature-theme').disabled = false;
    }
  }
  async function refresh() {
    if (!currentUser || busy || refreshing || editingNoteId !== null) return;
    const owner = currentUser;
    refreshing = true;
    const requestedRevision = revision;
    try {
      const response = await apiFetch('/api/features');
      if (!response.ok) return;
      const next = await response.json();
      if (owner !== currentUser || busy || requestedRevision !== revision || editingNoteId !== null) return;
      if (JSON.stringify(next) !== JSON.stringify(prefs)) { apply(next); await reconcile(); }
    } catch {} finally { refreshing = false; }
  }
  function reset() { revision++; dialog.close(); apply({}); }
  function open() {
    document.getElementById('account-control').open = false;
    syncControls();
    document.getElementById('features-status').textContent = '';
    dialog.showModal();
    refresh();
  }
  dialog.addEventListener('change', event => { if (event.target.dataset.feature) save({feature:event.target.dataset.feature, enabled:event.target.checked}); });
  dialog.querySelector('#features-full').onclick = () => save({mode:'full'});
  dialog.querySelector('#features-simple').onclick = () => save({mode:'simple'});
  dialog.querySelector('#features-close').onclick = () => dialog.close();
  dialog.addEventListener('click', event => { if (event.target === dialog && !busy) dialog.close(); });
  window.addEventListener('focus', refresh);
  document.addEventListener('visibilitychange', () => { if (!document.hidden) refresh(); });
  function elementAllowed(element) {
    return Object.entries(selectors).every(([key, list]) => enabled(key) || !element.closest(list));
  }
  function actionAllowed(id) {
    const control = id.startsWith('control:') ? document.getElementById(id.slice(8)) : null;
    if (control) {
      for (const [key, list] of Object.entries(selectors)) if (!enabled(key) && control.closest(list)) return false;
    }
    if (/^(note:(favorite|archive|pin|tags)|control:(favorites|archived|search-tag|unpin))/.test(id)) return enabled('organization');
    if (id === 'note:tools') return enabled('note_controls') || enabled('organization');
    if (/^(note:(lock|hide|color|copy)|color:|row:unhide|control:(active-notes|delete-all|show-all-hidden))/.test(id)) return enabled('note_controls');
    if (/^(note:roll|control:(density|date-sections))/.test(id)) return enabled('appearance');
    if (/^(vim-mode|language:)/.test(id)) return enabled('advanced_editing');
    if (/^(shortcuts|text:(dictionary|ignore))/.test(id)) return enabled('writing_tools');
    return true;
  }
  return {apply, enabled, open, save, refresh, reset, actionAllowed, elementAllowed};
})();
