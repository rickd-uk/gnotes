(() => {
  const dialog = document.getElementById('dictionary-dialog');
  const wordInput = document.getElementById('dictionary-word');
  const result = document.getElementById('dictionary-result');
  const saveButton = document.getElementById('dictionary-save');
  const ignoreButton = document.getElementById('dictionary-ignore');
  const status = document.getElementById('dictionary-status');
  const wordList = document.getElementById('dictionary-word-list');
  const filter = document.getElementById('dictionary-filter');
  const listStatus = document.getElementById('dictionary-list-status');
  const moreButton = document.getElementById('dictionary-load-more');
  const selectionButton = document.getElementById('dictionary-selection');
  const selectionActions = document.getElementById('note-selection-actions');
  const selectionIgnore = document.getElementById('selection-ignore');
  const contextMenu = document.getElementById('note-context-menu');
  const noteTextSelector = '.note-content, .tiptap, .note-view > h2';
  let contextWord = '', contextText = '', contextEditor = null;
  let lastPointerType = '';
  document.addEventListener('pointerdown', event => { lastPointerType = event.pointerType; }, { capture: true, passive: true });

  function closeContext() { contextMenu.hidden = true; }
  contextMenu.addEventListener('pointerdown', event => event.preventDefault());
  document.addEventListener('pointerdown', event => { if (!contextMenu.contains(event.target)) closeContext(); });
  document.getElementById('note-context-dictionary').addEventListener('click', () => { closeContext(); open(contextWord); });
  document.getElementById('note-context-ignore').addEventListener('click', async () => {
    const owner = currentUser, editor = contextEditor;
    closeContext();
    if (await ignoreSpellingWord(contextWord) && owner === currentUser) showNoteActionToast('Word saved to your account and ignored in rich note text');
    if (owner === currentUser && editor?.isConnected) editor.focus({ preventScroll: true });
  });
  document.getElementById('note-context-copy').addEventListener('click', async () => {
    const text = contextText, owner = currentUser;
    closeContext();
    try {
      await navigator.clipboard.writeText(text);
      if (owner === currentUser) showNoteActionToast('Copied');
    } catch {
      if (owner === currentUser) showNoteActionDialog('Could not copy', 'Use Ctrl+C or Command+C to copy selected text, or Shift + right-click for the browser menu.');
    }
  });
  contextMenu.addEventListener('keydown', event => {
    if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); closeContext(); contextEditor?.focus({ preventScroll: true }); }
    if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
      event.preventDefault();
      const buttons = [...contextMenu.querySelectorAll('button:not(:disabled)')];
      const index = buttons.indexOf(document.activeElement);
      buttons[event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1 : (index + (event.key === 'ArrowDown' ? 1 : -1) + buttons.length) % buttons.length].focus();
    }
    if (event.key === 'Tab') closeContext();
  });
  let currentEntry = null;
  let nextCursor = '';
  let lookupController, listController, filterTimer;
  let selectionWord = '';
  let generation = 0;

  function element(tag, text, className) {
    const node = document.createElement(tag);
    if (text !== undefined) node.textContent = text;
    if (className) node.className = className;
    return node;
  }

  function message(text = '', error = false) {
    status.textContent = text;
    status.classList.toggle('is-error', error);
  }

  async function request(url, options = {}) {
    const owner = currentUser;
    const version = generation;
    const response = await apiFetch(url, options);
    if (!owner || owner !== currentUser || version !== generation) throw new DOMException('Cancelled', 'AbortError');
    if (!response.ok) throw new Error((await response.text()).trim() || 'Could not reach the dictionary.');
    const data = response.status === 204 ? null : await response.json();
    if (owner !== currentUser || version !== generation) throw new DOMException('Cancelled', 'AbortError');
    return data;
  }

  function showLookup() {
    document.getElementById('dictionary-lookup-panel').hidden = false;
    document.getElementById('dictionary-saved-panel').hidden = true;
    document.getElementById('dictionary-lookup-tab').setAttribute('aria-selected', 'true');
    document.getElementById('dictionary-saved-tab').setAttribute('aria-selected', 'false');
    document.getElementById('dictionary-lookup-tab').tabIndex = 0;
    document.getElementById('dictionary-saved-tab').tabIndex = -1;
  }

  function updateSaveButton() {
    saveButton.hidden = !currentEntry;
    saveButton.disabled = false;
    saveButton.textContent = currentEntry?.saved ? 'Remove saved word' : 'Save word';
    ignoreButton.hidden = !currentEntry || !spellingWord(currentEntry.word);
    ignoreButton.textContent = ignoredSpellingWords.includes(spellingWord(currentEntry?.word)) ? 'Spelling always ignored' : 'Always ignore spelling';
    ignoreButton.disabled = !spellingReady || spellingBusy || ignoredSpellingWords.includes(spellingWord(currentEntry?.word));
  }

  ignoreButton.addEventListener('click', async () => {
    const entry = currentEntry, owner = currentUser;
    if (entry && await ignoreSpellingWord(entry.word) && currentEntry === entry && owner === currentUser) {
      updateSaveButton(); message('Spelling ignored in rich note text. This word was not added to saved words.');
    }
  });

  async function lookup(word) {
    lookupController?.abort();
    lookupController = new AbortController();
    const controller = lookupController;
    currentEntry = null;
    result.replaceChildren();
    updateSaveButton();
    message('Looking up…');
    try {
      const entry = await request(`/api/dictionary/lookup?word=${encodeURIComponent(word.trim())}`, { signal: controller.signal });
      if (controller !== lookupController || controller.signal.aborted) return;
      currentEntry = entry;
      wordInput.value = entry.word;
      result.append(element('h3', entry.word));
      if (entry.matched_word !== entry.word) result.append(element('p', `Definitions for ${entry.matched_word}`));
      if (!entry.senses.length) result.append(element('p', 'No definition found in the English dictionary. You can still save this word.'));
      for (const part of ['noun', 'verb', 'adjective', 'adverb']) {
        const senses = entry.senses.filter((sense) => sense.part_of_speech === part);
        if (!senses.length) continue;
        result.append(element('h4', part));
        const list = element('ol');
        for (const sense of senses) {
          const item = element('li', sense.definition);
          const synonyms = sense.synonyms.filter((word) => word.toLowerCase() !== entry.matched_word);
          if (synonyms.length) item.append(element('span', `Related words: ${synonyms.join(', ')}`, 'dictionary-synonyms'));
          list.append(item);
        }
        result.append(list);
      }
      updateSaveButton();
      message(entry.saved ? 'In your saved words.' : '');
    } catch (error) {
      if (error.name !== 'AbortError' && controller === lookupController) message(error.message, true);
    }
  }

  function open(word = '') {
    if (!currentUser) return;
    closeContext();
    document.getElementById('account-control').open = false;
    closeViewMenu();
    hideRichContext();
    hideSelectionActions();
    showLookup();
    if (!dialog.open) dialog.showModal();
    wordInput.value = word.trim();
    wordInput.focus();
    if (word.trim()) lookup(word);
    else {
      lookupController?.abort();
      currentEntry = null;
      updateSaveButton();
      result.replaceChildren(element('p', 'Look up a word here, or select a word in a note and choose Dictionary. On a computer, you can also right-click a word.'));
      message();
    }
  }

  async function loadWords(append = false) {
    listController?.abort();
    listController = new AbortController();
    const controller = listController;
    if (!append) { wordList.replaceChildren(); nextCursor = ''; }
    moreButton.hidden = true;
    listStatus.textContent = 'Loading saved words…';
    try {
      const params = new URLSearchParams({ q: filter.value.trim(), cursor: append ? nextCursor : '' });
      const page = await request(`/api/dictionary/words?${params}`, { signal: controller.signal });
      if (controller !== listController || controller.signal.aborted) return;
      for (const word of page.words) {
        const row = element('li');
        const define = element('button', word.word);
        define.type = 'button';
        define.dataset.lookupWord = word.word;
        const remove = element('button', '×');
        remove.type = 'button';
        remove.dataset.removeWord = word.word;
        remove.setAttribute('aria-label', `Remove ${word.word} from saved words`);
        row.append(define, remove);
        wordList.append(row);
      }
      nextCursor = page.next_cursor;
      moreButton.hidden = !nextCursor;
      listStatus.textContent = page.total ? `${wordList.children.length} of ${page.total} saved words` : filter.value.trim() ? 'No saved words match.' : 'No saved words yet.';
    } catch (error) {
      if (error.name !== 'AbortError' && controller === listController) listStatus.textContent = error.message;
    }
  }

  function showSaved() {
    document.getElementById('dictionary-lookup-panel').hidden = true;
    document.getElementById('dictionary-saved-panel').hidden = false;
    document.getElementById('dictionary-lookup-tab').setAttribute('aria-selected', 'false');
    document.getElementById('dictionary-saved-tab').setAttribute('aria-selected', 'true');
    document.getElementById('dictionary-lookup-tab').tabIndex = -1;
    document.getElementById('dictionary-saved-tab').tabIndex = 0;
    message();
    loadWords();
  }

  document.getElementById('dictionary-open').addEventListener('click', () => open());
  document.getElementById('dictionary-close').addEventListener('click', () => dialog.close());
  document.getElementById('dictionary-lookup-form').addEventListener('submit', (event) => { event.preventDefault(); lookup(wordInput.value); });
  document.getElementById('dictionary-lookup-tab').addEventListener('click', showLookup);
  document.getElementById('dictionary-saved-tab').addEventListener('click', showSaved);
  document.querySelector('.dictionary-tabs').addEventListener('keydown', (event) => {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
    event.preventDefault();
    const saved = event.key === 'End' || (event.key !== 'Home' && document.getElementById('dictionary-saved-tab').getAttribute('aria-selected') !== 'true');
    if (saved) showSaved(); else showLookup();
    document.getElementById(saved ? 'dictionary-saved-tab' : 'dictionary-lookup-tab').focus();
  });
  saveButton.addEventListener('click', async () => {
    const entry = currentEntry;
    if (!entry) return;
    saveButton.disabled = true;
    try {
      if (entry.saved) await request(`/api/dictionary/remove?word=${encodeURIComponent(entry.word)}`, { method: 'DELETE' });
      else await request('/api/dictionary/save', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ word: entry.word }) });
      if (currentEntry !== entry) return;
      entry.saved = !entry.saved;
      updateSaveButton();
      message(entry.saved ? 'Saved to your word list.' : 'Removed from your word list.');
    } catch (error) { if (error.name !== 'AbortError' && currentEntry === entry) { message(error.message, true); saveButton.disabled = false; } }
  });
  wordList.addEventListener('click', async (event) => {
    const define = event.target.closest('[data-lookup-word]');
    if (define) { open(define.dataset.lookupWord); return; }
    const remove = event.target.closest('[data-remove-word]');
    if (!remove) return;
    remove.disabled = true;
    try {
      await request(`/api/dictionary/remove?word=${encodeURIComponent(remove.dataset.removeWord)}`, { method: 'DELETE' });
      if (currentEntry?.word === remove.dataset.removeWord) { currentEntry.saved = false; updateSaveButton(); }
      message('Removed from your word list.');
      loadWords();
    } catch (error) { if (error.name !== 'AbortError') { message(error.message, true); remove.disabled = false; } }
  });
  filter.addEventListener('input', () => { clearTimeout(filterTimer); listController?.abort(); filterTimer = setTimeout(() => loadWords(), 200); });
  moreButton.addEventListener('click', () => loadWords(true));
  document.getElementById('dictionary-export').addEventListener('click', async () => {
    try {
      const data = await request('/api/dictionary/export');
      const url = URL.createObjectURL(new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' }));
      const link = element('a');
      link.href = url;
      link.download = 'gnotes-dictionary.json';
      link.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
      message('Word list exported.');
    } catch (error) { if (error.name !== 'AbortError') message(error.message, true); }
  });
  dialog.addEventListener('close', () => { generation++; lookupController?.abort(); listController?.abort(); clearTimeout(filterTimer); });

  function selectableWord(text) {
    text = text.trim();
    return text.length <= 96 && text.split(/\s+/).length <= 6 && /\p{L}/u.test(text) && /^[\p{L}\p{N}'’ -]+$/u.test(text) ? text : '';
  }

  function selectedNoteWord() {
    const input = document.activeElement;
    if (input?.matches('#title, .edit-title') && input.selectionStart !== input.selectionEnd) {
      return selectableWord(input.value.slice(input.selectionStart ?? 0, input.selectionEnd ?? 0));
    }
    const selection = window.getSelection();
    if (!selection || selection.isCollapsed || !selection.rangeCount) return '';
    const anchor = selection.anchorNode?.nodeType === Node.ELEMENT_NODE ? selection.anchorNode : selection.anchorNode?.parentElement;
    const root = anchor?.closest(noteTextSelector);
    if (!root || !root.contains(selection.focusNode) || anchor.closest('pre, code')) return '';
    return selectableWord(selection.toString());
  }

  function hideSelectionActions() {
    selectionActions.hidden = true;
    selectionButton.hidden = true;
  }

  function refreshSelectionActions() {
    hideSelectionActions();
    if (!currentUser || document.querySelector('dialog[open]')) return;
    selectionWord = selectedNoteWord();
    const selection = window.getSelection();
    const docked = useDockedSelectionTools();
    // Desktop rich selections already have their formatting/Dictionary toolbar.
    if (!selectionWord || (!docked && selection.anchorNode?.parentElement?.closest('.tiptap'))) return;
    const input = document.activeElement;
    const rect = input?.matches('#title, .edit-title') && input.selectionStart !== input.selectionEnd ? input.getBoundingClientRect() : selection.getRangeAt(0).getBoundingClientRect();
    const viewport = window.visualViewport;
    const viewportTop = viewport?.offsetTop || 0;
    const viewportBottom = viewportTop + (viewport?.height || innerHeight);
    if (!rect.width || rect.bottom < viewportTop || rect.top > viewportBottom) return;
    selectionActions.classList.toggle('is-docked', docked);
    selectionActions.hidden = false;
    selectionButton.hidden = false;
    selectionIgnore.hidden = !docked;
    selectionIgnore.disabled = !spellingReady || spellingBusy || !spellingWord(selectionWord) || ignoredSpellingWords.includes(spellingWord(selectionWord));
    selectionIgnore.title = spellingWord(selectionWord) ? 'Always ignore spelling for this word' : 'Select one word to always ignore its spelling';
    const viewportLeft = viewport?.offsetLeft || 0;
    const viewportWidth = viewport?.width || innerWidth;
    selectionActions.style.width = docked ? `${Math.min(440, viewportWidth - 16)}px` : '';
    const width = selectionActions.offsetWidth, height = selectionActions.offsetHeight;
    selectionActions.style.left = `${docked ? viewportLeft + Math.max(8, (viewportWidth - width) / 2) : Math.max(8, Math.min(rect.left, innerWidth - width - 8))}px`;
    let bottom = viewportBottom - 12;
    if (docked) {
      const footer = document.querySelector('.note-card.is-editing .editor-actions')?.getBoundingClientRect();
      if (footer && footer.top > viewportTop && footer.top < viewportBottom) bottom = Math.min(bottom, footer.top - 8);
    }
    selectionActions.style.top = `${Math.max(viewportTop + 8, docked ? bottom - height : Math.min(rect.bottom + 6, viewportBottom - height - 8))}px`;
  }
  document.addEventListener('selectionchange', refreshSelectionActions);
  selectionActions.addEventListener('pointerdown', event => { if (event.target.closest('button')) event.preventDefault(); });
  selectionButton.addEventListener('click', () => open(selectionWord));
  selectionIgnore.addEventListener('click', async () => {
    const word = selectionWord, owner = currentUser;
    selectionIgnore.disabled = true;
    if (await ignoreSpellingWord(word) && owner === currentUser) showNoteActionToast(`“${word}” saved and always ignored in rich note text`);
    refreshSelectionActions();
  });
  window.addEventListener('scroll', () => { if (useDockedSelectionTools()) refreshSelectionActions(); else hideSelectionActions(); closeContext(); }, true);
  window.addEventListener('resize', () => { refreshSelectionActions(); closeContext(); });
  window.visualViewport?.addEventListener('resize', refreshSelectionActions);
  window.visualViewport?.addEventListener('scroll', refreshSelectionActions);
  document.addEventListener('contextmenu', (event) => {
    closeContext();
    // Long presses and selection handles belong to the phone's native text
    // controls. Some mobile browsers report long presses as MouseEvents.
    if (event.shiftKey || event.pointerType === 'touch' || event.pointerType === 'pen' ||
        lastPointerType === 'touch' || lastPointerType === 'pen' ||
        matchMedia('(hover: none) and (pointer: coarse)').matches) return;
    const titleInput = event.target.closest('#title, .edit-title');
    const root = titleInput || event.target.closest(noteTextSelector);
    if (!currentUser || !root || event.target.closest('a, button, pre, code') || (!titleInput && event.target.closest('input'))) return;
    let word = '', selectedText = '';
    const selection = window.getSelection();
    if (titleInput) {
      const from = titleInput.selectionStart ?? 0, to = titleInput.selectionEnd ?? from;
      selectedText = titleInput.value.slice(from, to);
      word = selectableWord(selectedText);
      if (!word && !selectedText) {
        for (const match of titleInput.value.matchAll(/[\p{L}\p{N}]+(?:['’\-][\p{L}\p{N}]+)*/gu)) {
          if (from >= match.index && from <= match.index + match[0].length) { word = selectableWord(match[0]); break; }
        }
      }
    } else {
      if (selection?.rangeCount && root.contains(selection.anchorNode) && root.contains(selection.focusNode) && !selection.isCollapsed) selectedText = selection.toString();
      if (selectedNoteWord() && [...selection.getRangeAt(0).getClientRects()].some((rect) => event.clientX >= rect.left && event.clientX <= rect.right && event.clientY >= rect.top && event.clientY <= rect.bottom)) word = selectedNoteWord();
    }
    if (!word && !titleInput) {
      const position = document.caretPositionFromPoint?.(event.clientX, event.clientY);
      const range = !position ? document.caretRangeFromPoint?.(event.clientX, event.clientY) : null;
      const node = position?.offsetNode || range?.startContainer;
      const offset = position?.offset ?? range?.startOffset;
      if (node?.nodeType === Node.TEXT_NODE && root.contains(node)) {
        for (const match of node.textContent.matchAll(/[\p{L}\p{N}]+(?:['’\-][\p{L}\p{N}]+)*/gu)) {
          if (offset >= match.index && offset <= match.index + match[0].length) { word = selectableWord(match[0]); break; }
        }
      }
    }
    if (!word && !selectedText) return;
    event.preventDefault(); hideRichContext(); hideSelectionActions();
    contextWord = word; contextText = selectedText || word;
    contextEditor = root.matches('.tiptap, #title, .edit-title') ? root : null;
    document.getElementById('note-context-dictionary').disabled = !word;
    document.getElementById('note-context-ignore').disabled = !spellingReady || spellingBusy || !spellingWord(word) || ignoredSpellingWords.includes(spellingWord(word));
    contextMenu.hidden = false;
    const rect = root.getBoundingClientRect();
    const x = event.clientX || rect.left, y = event.clientY || rect.top;
    contextMenu.style.left = `${Math.max(8, Math.min(x, innerWidth - contextMenu.offsetWidth - 8))}px`;
    contextMenu.style.top = `${Math.max(8, Math.min(y, innerHeight - contextMenu.offsetHeight - 8))}px`;
    document.getElementById('note-context-copy').focus({ preventScroll: true });
  });

  function reset() {
    closeContext(); contextWord = ''; contextText = ''; contextEditor = null;
    generation++;
    lookupController?.abort(); listController?.abort(); clearTimeout(filterTimer);
    if (dialog.open) dialog.close();
    currentEntry = null; selectionWord = ''; nextCursor = '';
    wordInput.value = ''; filter.value = '';
    result.replaceChildren(); wordList.replaceChildren();
    listStatus.textContent = ''; message(); updateSaveButton();
    hideSelectionActions();
  }
  window.GnotesDictionary = { open, reset, refreshSpelling: () => { updateSaveButton(); refreshSelectionActions(); } };
})();
