/* User shortcuts reuse the UI's actions, including their validation/confirmation. */
(() => {
  const dialog = document.createElement('dialog');
  dialog.id = 'shortcuts-dialog';
  dialog.className = 'shortcuts-dialog';
  dialog.setAttribute('aria-labelledby', 'shortcuts-heading');
  dialog.innerHTML = `<header><h2 id="shortcuts-heading">Keyboard shortcuts</h2><button type="button" id="shortcuts-close" aria-label="Close shortcuts">×</button></header>
    <p>Choose an action, then press your keys. Note actions use the focused or pointed-at note. Changes are saved for your account in this browser.</p>
    <input type="search" id="shortcuts-search" placeholder="Find an action…" aria-label="Find a shortcut action">
    <p class="shortcuts-status" id="shortcuts-status" role="status"></p><div id="shortcuts-list"></div>
    <footer><button type="button" id="shortcuts-reset">Reset shortcuts</button><span>Escape closes dialogs. Tab moves or indents text. Browser and text-editing shortcuts remain available.</span></footer>`;
  document.body.append(dialog);
  const list = dialog.querySelector('#shortcuts-list');
  const search = dialog.querySelector('#shortcuts-search');
  const status = dialog.querySelector('#shortcuts-status');
  const actions = new Map();
  let recording = null;
  let owner = '';
  let bindings = {};
  let pointedNote = null;
  const defaults = { 'search': '/', 'save': 'Ctrl+Enter', 'preview': 'Ctrl+Shift+P' };
  const label = el => (el.getAttribute('aria-label') || el.getAttribute('title') || el.labels?.[0]?.textContent || el.textContent || el.id).replace(/\s+/g, ' ').trim();
  function syncOwner() {
    const next = currentUser?.username?.toLowerCase() || '';
    if (owner === next) return;
    owner = next;
    actions.clear();
    recording = null;
    try {
      const saved = JSON.parse(localStorage.getItem(`gnotes-shortcuts:${owner}`) || 'null');
      bindings = saved && typeof saved === 'object' && !Array.isArray(saved) ? saved : { ...defaults };
    } catch { bindings = { ...defaults }; }
    if (dialog.open) { dialog.close(); render(); }
  }
  function persist() {
    try { localStorage.setItem(`gnotes-shortcuts:${owner}`, JSON.stringify(bindings)); }
    catch { status.textContent = 'Could not save shortcuts in this browser.'; }
  }
  function add(id, name, group, run, available = () => true) {
    if (!actions.has(id)) actions.set(id, { id, name, group, run, available: () => GnotesFeatures.actionAllowed(id) && (!actions.get(id)?.element || GnotesFeatures.elementAllowed(actions.get(id).element)) && available() });
  }
  function noteID() {
    const focused = document.activeElement?.closest('.note-card');
    const id = editingNoteId ?? (focused ? Number(focused.id.replace('note-', '')) : pointedNote);
    return id && loadedNotes.has(id) ? id : null;
  }
  function writing() {
    const target = document.activeElement;
    if (target?.matches('input:not([type=checkbox]):not([type=range]), textarea, select') || target?.isContentEditable) return true;
    return false;
  }
  function activeWriter() {
    const target = document.activeElement;
    if (target?.matches('textarea')) return target;
    if (richEdit?.editor.view.dom.contains(target)) return richEdit;
    if (richDraft?.editor.view.dom.contains(target)) return richDraft;
    return null;
  }
  function discover() {
    if (actions.size) return;
    add('search', 'Find notes', 'Navigation', () => {
      if (document.getElementById('search-panel').hidden) document.getElementById('search-toggle').click();
      document.getElementById('search-query').focus();
    }, () => editingNoteId === null);
    add('shortcuts', 'Keyboard shortcuts', 'Navigation', open);
    add('new', 'Write a new note', 'Notes', async () => {
      if (favoritesMode) await toggleFavoritesMode();
      if (archiveMode) await toggleArchiveMode();
      if (!showNewNoteArea) document.getElementById('new-note-area-toggle').click();
      document.getElementById('note-form').scrollIntoView({ block: 'center' });
      if (vimDraft) vimDraft.focus();
      else if (richDraft) richDraft.editor.commands.focus(); else document.getElementById('content').focus();
    }, () => editingNoteId === null);
    add('save', 'Save and close note', 'Editor', () => updateNote(editingNoteId), () => editingNoteId !== null);
    add('copy-edit', 'Copy edited note text', 'Editor', () => copyEditedNoteText(editingNoteId, document.querySelector('.is-editing .edit-copy')), () => editingNoteId !== null);
    add('preview', 'Toggle rendered preview', 'Editor', () => toggleNotePreview(editingNoteId), () => editingNoteId !== null);
    add('vim-mode', 'Toggle Vim editing', 'Editor', toggleVimMode);
    add('edit', 'Edit focused note', 'Notes', () => openNoteEditor(noteID(), true), () => editingNoteId === null && noteID() !== null && !loadedNotes.get(noteID())?.locked);
    add('formatting', 'Show / hide formatting', 'Editor', () => {
      if (activeWriter()?.editor) setRichToolbarVisible(!richToolbarVisible);
      else document.querySelector(`#${CSS.escape(activeWriter().id)}`)?.closest('form, .note-editor')?.querySelector('.format-toggle')?.click();
    }, () => Boolean(activeWriter()));
    for (const [command, name, rich] of [
      ['bold','Bold','bold'],['italic','Italic','italic'],['heading','Heading','heading'],['subheading','Subheading','subheading'],
      ['list','Bulleted list','bullet'],['ordered-list','Numbered list','numbered'],['task-list','Task checkbox','task'],
      ['quote','Quote','quote'],['code','Inline code','code'],['code-block','Code block','codeBlock'],
      ['link','Insert / edit link','link'],['strikethrough','Strikethrough','strike'],['divider','Divider','divider'],
      ['table','Table (Markdown source)',''],['normal','Normal text','normalText']]) {
      add(`format:${command}`, name, 'Formatting', () => {
        const writer = activeWriter();
        if (writer.editor) writer.format(rich);
        else if (command === 'code-block') applyFencedCode(writer.id);
        else if (command === 'subheading') prefixMarkdownLines(writer, '### ');
        else if (command === 'normal') toggleMarkdownLinePattern(writer, /^#{1,6}\s+/, () => '');
        else applyMarkdownFormat(writer.id, command);
      }, () => Boolean(activeWriter()) && !(activeWriter().editor && !rich));
    }
    for (const [color, name] of noteColorOptions) {
      add(`color:${color || 'default'}`, name, 'Note colors', () => setNoteColor(noteID(), color), () => editingNoteId === null && noteID() !== null && !loadedNotes.get(noteID())?.locked);
    }
    for (const language of ['', 'python', 'javascript', 'html', 'css', 'go', 'rust', 'java', 'sql', 'bash']) {
      add(`language:${language || 'plain'}`, `${language || 'Plain'} code block`, 'Code languages', () => {
        const writer = activeWriter();
        if (writer.editor) { if (!writer.editor.isActive('codeBlock')) writer.format('codeBlock'); writer.setCodeLanguage(language); }
        else applyFencedCode(writer.id, language);
      }, () => Boolean(activeWriter()));
    }
    for (const [id, name, backwards] of [['indent', 'Indent / nest list', false], ['outdent', 'Outdent / unnest list', true]]) {
      add(`format:${id}`, name, 'Formatting', () => {
        const writer = activeWriter();
        const event = new KeyboardEvent('keydown', { key: 'Tab', shiftKey: backwards, bubbles: true, cancelable: true });
        if (writer.editor) writer.editor.view.dom.dispatchEvent(event); else handleTextIndent(event, writer);
      }, () => Boolean(activeWriter()));
    }
    for (const [id, name] of [['undo', 'Undo'], ['redo', 'Redo']]) {
      add(`format:${id}`, name, 'Formatting', () => {
        const writer = activeWriter();
        if (writer.editor) writer.editor.commands[id](); else document.execCommand(id);
      }, () => Boolean(activeWriter()));
    }
    for (const [key, name, selector] of [
      ['lock','Toggle read-only mode','[data-note-action="lock"]'],['favorite','Toggle Favorite','[data-note-action="favorite"]'],['pin','Pin / unpin','[data-note-action="pin"]'],
      ['hide','Hide','[data-note-action="hide"]'],['archive','Archive / unarchive','[data-note-action="archive"]'],
      ['copy','Copy note text','[data-note-action="copy"]'],['delete','Move to recycle bin','[data-note-action="delete"]'],
      ['tags','Edit tags','[aria-label="Edit note tags"]'],['color','Change color','.color-btn'],['roll','Expand / roll up','.note-roll-toggle'],
      ['tools','Show / hide note tools','.note-actions-toggle']]) {
      const button = () => document.getElementById(`note-${noteID()}`)?.querySelector(selector);
      add(`note:${key}`, name, 'Focused note', () => button()?.click(), () => editingNoteId === null && Boolean(button()) && !button().disabled);
    }
    const textRoot = () => {
      const active = document.activeElement;
      if (active?.matches('#title, #content, .edit-title, .edit-content, .tiptap')) return active;
      const selection = window.getSelection();
      return selection && !selection.isCollapsed ? selection.anchorNode?.parentElement?.closest('.note-content, .tiptap, .note-view > h2') : null;
    };
    for (const [id, name] of [['cut','Cut selection'],['copy','Copy selection'],['paste','Paste'],['dictionary','Look up selected word'],['ignore','Always ignore selected spelling']]) {
      add(`text:${id}`, name, 'Selected text', () => {
        const root = textRoot();
        if (!root) return;
        const selection = window.getSelection();
        const rect = root.matches('input, textarea') || !selection?.rangeCount ? root.getBoundingClientRect() : selection.getRangeAt(0).getBoundingClientRect();
        const event = new PointerEvent('contextmenu', { pointerType: 'mouse', button: 2, bubbles: true, cancelable: true, clientX: (rect.left + rect.right) / 2, clientY: (rect.top + rect.bottom) / 2 });
        root.dispatchEvent(event);
        const menu = document.getElementById('note-context-menu');
        if (!event.defaultPrevented || menu.hidden) return;
        const button = document.getElementById(`note-context-${id}`);
        if (!button.disabled) button.click();
        menu.hidden = true;
        if (!document.querySelector('dialog[open]')) root.focus({ preventScroll: true });
      }, () => Boolean(textRoot()));
    }
    for (const [id, name, rowSelector, buttonSelector] of [
      ['recover', 'Recover focused recycled note', '.trash-item', '.restore-one'],
      ['unhide', 'Show focused hidden note', '.hidden-note-item', '.show-hidden-note'],
      ['user-active', 'Enable / disable focused user', '.admin-user', '.admin-user-actions button:first-child'],
      ['user-sessions', 'Sign out focused user devices', '.admin-user', '.admin-user-actions button:nth-child(2)'],
      ['user-delete', 'Delete focused user and notes', '.admin-user', '.admin-user-actions .danger-action']]) {
      const button = () => document.activeElement?.closest(rowSelector)?.querySelector(buttonSelector);
      add(`row:${id}`, name, 'Focused item', () => button()?.click(), () => editingNoteId === null && Boolean(button()) && !button().disabled && Boolean(button().getClientRects().length));
    }
    for (const [key, delta] of [['next',1],['previous',-1]]) {
      add(`note:${key}`, `${delta === 1 ? 'Next' : 'Previous'} note`, 'Notes', () => {
        const cards = [...document.querySelectorAll('#notes-list .note-card:not([hidden])')];
        const at = cards.findIndex(el => Number(el.id.slice(5)) === noteID());
        const card = cards[Math.max(0, Math.min(cards.length - 1, at + delta))];
        if (!card) return;
        card.tabIndex = -1; card.focus(); pointedNote = Number(card.id.slice(5)); card.scrollIntoView({ block: 'nearest' });
      }, () => editingNoteId === null && loadedNotes.size > 0);
    }
    // Include every static control: menus, account, search, import/export, admin,
    // spelling, offline reading, display settings and confirmation dialogs.
    document.querySelectorAll('#app-shell button, #app-shell summary, #app-shell input[type=checkbox], #app-shell select, #app-shell input[type=range], dialog:not(#shortcuts-dialog) button, dialog:not(#shortcuts-dialog) input[type=checkbox], dialog:not(#shortcuts-dialog) select').forEach(el => {
      if (el.closest('#notes-list, #trash-list, #hidden-notes-list, #admin-users, .rich-editor-container, .vim-editor, .vim-toggle, .format-control')) return;
      if (['shortcuts-open', 'search-toggle', 'view-backdrop'].includes(el.id)) return;
      const name = label(el);
      if (!name) return;
      const container = el.parentElement.closest('[id]');
      const id = `control:${el.id || `${container?.id || 'app'}:${el.tagName}:${name}`}`;
      const panel = el.closest('dialog, section[aria-label], .info-panel, .account-menu, #search-panel, #view-menu');
      const group = panel?.getAttribute('aria-label') || panel?.querySelector('h2')?.textContent || (el.closest('.account-menu') ? 'Account' : 'Controls');
      const available = () => {
        if (!el.isConnected || el.disabled || el.hidden || el.closest('[hidden]')) return false;
        const modal = el.closest('dialog');
        if (modal && !modal.open) return false;
        return editingNoteId === null || Boolean(modal) || Boolean(el.closest('#note-context-menu, #note-selection-actions'));
      };
      add(id, name, group, () => {
        if (el.matches('select, input[type=range]')) {
          for (const parent of [el.closest('#view-menu'), el.closest('.account-menu')]) {
            if (parent?.id === 'view-menu' && !document.getElementById('view-control').classList.contains('is-open')) document.getElementById('view-toggle').click();
            if (parent?.classList.contains('account-menu')) document.getElementById('account-control').open = true;
          }
          let parent = el.parentElement;
          while (parent) { if (parent.tagName === 'DETAILS') parent.open = true; parent = parent.parentElement; }
          el.focus();
        } else el.click();
      }, available);
      actions.get(id).element = el;
    });
  }
  function keyFor(event) {
    if (['Control','Shift','Alt','Meta','Dead','Unidentified'].includes(event.key)) return '';
    let key = event.key === ' ' ? 'Space' : event.key;
    if (key.length === 1) key = key.toUpperCase();
    // Slash is the existing unmodified search shortcut.
    if (event.key === '/' && !event.ctrlKey && !event.metaKey && !event.altKey && !event.shiftKey) return '/';
    return [...(event.ctrlKey ? ['Ctrl'] : []), ...(event.metaKey ? ['Meta'] : []), ...(event.altKey ? ['Alt'] : []), ...(event.shiftKey ? ['Shift'] : []), key].join('+');
  }
  function render() {
    list.replaceChildren();
    let lastGroup = '';
    const query = search.value.toLowerCase();
    for (const action of [...actions.values()].sort((a,b) => a.group.localeCompare(b.group) || a.name.localeCompare(b.name))) {
      if (!GnotesFeatures.actionAllowed(action.id) || (action.element && !GnotesFeatures.elementAllowed(action.element))) continue;
      if (!`${action.group} ${action.name} ${bindings[action.id] || ''}`.toLowerCase().includes(query)) continue;
      if (action.group !== lastGroup) {
        const heading = document.createElement('h3'); heading.textContent = action.group; list.append(heading); lastGroup = action.group;
      }
      const row = document.createElement('div'); row.className = 'shortcut-row'; row.dataset.action = action.id;
      const name = document.createElement('span'); name.textContent = action.name;
      const record = document.createElement('button'); record.type = 'button'; record.className = 'shortcut-record';
      record.textContent = recording === action.id ? 'Press keys…' : bindings[action.id] || 'Set keys';
      record.setAttribute('aria-label', `${action.name}: ${record.textContent}`);
      record.onclick = () => { recording = action.id; status.textContent = 'Press a key combination. Escape cancels. Delete clears it.'; render(); list.querySelector(`[data-action="${CSS.escape(action.id)}"] .shortcut-record`).focus(); };
      const clear = document.createElement('button'); clear.type = 'button'; clear.textContent = '×'; clear.setAttribute('aria-label', `Clear ${action.name} shortcut`);
      clear.disabled = !bindings[action.id];
      clear.onclick = () => { delete bindings[action.id]; recording = null; persist(); render(); };
      row.append(name, record, clear); list.append(row);
    }
    if (!list.children.length) { const empty = document.createElement('p'); empty.textContent = 'No matching actions.'; list.append(empty); }
  }
  function open() {
    syncOwner(); if (!owner) return;
    discover(); recording = null; search.value = ''; status.textContent = ''; render();
    document.getElementById('account-control').open = false;
    if (!dialog.open) dialog.showModal(); search.focus();
  }
  document.getElementById('shortcuts-open').addEventListener('click', open);
  dialog.querySelector('#shortcuts-close').onclick = () => dialog.close();
  dialog.addEventListener('click', event => {
    if (event.target !== dialog) return;
    const bounds = dialog.getBoundingClientRect();
    if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) dialog.close();
  });
  dialog.addEventListener('close', () => { recording = null; });
  search.oninput = render;
  dialog.querySelector('#shortcuts-reset').onclick = () => { bindings = { ...defaults }; recording = null; persist(); status.textContent = 'Default shortcuts restored.'; render(); };
  document.addEventListener('pointerover', event => {
    const card = event.target.closest?.('.note-card');
    pointedNote = card ? Number(card.id.replace('note-', '')) : null;
  });
  document.addEventListener('keydown', event => {
    syncOwner();
    if (!owner || event.isComposing || event.key === 'Process') return;
    if (dialog.open) {
      if (!recording) return;
      event.preventDefault(); event.stopImmediatePropagation();
      if (event.key === 'Escape') { recording = null; status.textContent = 'Assignment cancelled.'; render(); return; }
      if (event.key === 'Delete' || event.key === 'Backspace') { delete bindings[recording]; recording = null; persist(); status.textContent = 'Shortcut cleared.'; render(); return; }
      const key = keyFor(event); if (!key) return;
      if (event.key === 'Tab' || (!event.ctrlKey && !event.metaKey && !event.altKey && !/^F([2-9]|1[0-2])$/.test(event.key) && event.key !== '/')) {
        status.textContent = 'Use Ctrl, Command or Alt with a key, or F2–F12. Tab and Escape keep their editing roles.'; return;
      }
      if (/^(Ctrl|Meta)\+(?:Shift\+)?[WTRLNQ]$/.test(key) || key === 'Alt+F4' || key === 'F5' || key === 'F11' || key === 'F12') {
        status.textContent = 'That combination belongs to your browser. Choose another.'; return;
      }
      if ((event.ctrlKey || event.metaKey) && ['a','c','x','v','z','y'].includes(event.key.toLowerCase())) {
        status.textContent = 'Keep standard selection, clipboard and undo keys available. Choose another.'; return;
      }
      const conflict = Object.keys(bindings).find(id => id !== recording && bindings[id] === key);
      if (conflict) { status.textContent = `Already assigned to ${actions.get(conflict)?.name || conflict}. Clear that assignment first.`; return; }
      bindings[recording] = key; recording = null; persist(); status.textContent = 'Shortcut saved.'; render(); return;
    }
    if (event.defaultPrevented || event.repeat || event.getModifierState('AltGraph')) return;
    discover();
    const key = keyFor(event);
    const id = Object.keys(bindings).find(id => bindings[id] === key);
    const action = actions.get(id);
    // Preserve Vim's own commands (including Ctrl-R, Ctrl-D and /). Only the
    // app's save, preview and editor-switch bindings take priority here.
    if (event.target.closest?.('.vim-editor') && !['save', 'preview', 'vim-mode'].includes(id)) return;
    if (!action || !action.available() || (!event.ctrlKey && !event.metaKey && !event.altKey && event.key.length === 1 && writing())) return;
    const modal = document.querySelector('dialog[open]');
    if (modal && !id.startsWith('control:')) return;
    if (modal && ![...modal.querySelectorAll('button,input,select')].some(el => `control:${el.id}` === id)) return;
    if (document.getElementById('note-context-menu')?.hidden === false && !id.startsWith('control:note-context-')) return;
    event.preventDefault(); event.stopImmediatePropagation(); action.run();
  }, true);
  window.GnotesShortcuts = { open, keyFor, actions, get bindings() { syncOwner(); return { ...bindings }; } };
})();
