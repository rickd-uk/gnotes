import { basicSetup, EditorView } from 'codemirror';
import { Compartment, EditorState } from '@codemirror/state';
import { keymap } from '@codemirror/view';
import { markdown } from '@codemirror/lang-markdown';
import { vim, Vim, getCM } from '@replit/codemirror-vim';

// Ex commands belong to the invoking editor, never a global active note.
const actions = new WeakMap();
function save(cm, params, close) {
  if (params.argString?.trim()) {
    cm.openNotification(document.createTextNode('Use :w to save this note; file names are not supported.'));
    return;
  }
  // Let the Vim command finish before a close destroys its editor view.
  queueMicrotask(() => actions.get(cm)?.(close));
}
Vim.defineEx('write', 'w', (cm, params) => save(cm, params, false));
Vim.defineEx('wq', 'wq', (cm, params) => save(cm, params, true));
Vim.defineEx('xit', 'x', (cm, params) => save(cm, params, true));
// Notes autosave. Quitting keeps those changes, like the app's Done button.
Vim.defineEx('quit', 'q', (cm, params) => save(cm, params, true));

function mount(host, text, onDirty, onSave) {
  let resetting = false, serializedDoc = null, serialized = '';
  let contentDoc = null, hasText = false;
  const editable = new Compartment();
  const view = new EditorView({
    parent: host,
    doc: text,
    extensions: [
      vim({ status: true }),
      keymap.of([
        { key: 'Ctrl-Enter', run: () => { onSave(true); return true; } },
        { key: 'Meta-Enter', run: () => { onSave(true); return true; } },
      ]),
      basicSetup, markdown(), EditorView.lineWrapping,
      EditorState.tabSize.of(4),
      editable.of([EditorState.readOnly.of(false), EditorView.editable.of(true)]),
      EditorView.contentAttributes.of({ 'aria-label': 'Vim Markdown editor', spellcheck: 'false' }),
      EditorView.updateListener.of(update => {
        if (update.docChanged && !resetting) onDirty();
      }),
    ],
  });
  const cm = getCM(view);
  actions.set(cm, onSave);
  // Normal-mode Escape may be unhandled by the Vim library. It must not reach
  // the application's Escape-to-save/close handler or finalize the draft form.
  const stopEscape = event => { if (event.key === 'Escape') event.stopPropagation(); };
  host.addEventListener('keydown', stopEscape);
  return {
    view,
    getMarkdown() {
      if (serializedDoc !== view.state.doc) {
        serializedDoc = view.state.doc;
        serialized = serializedDoc.toString();
      }
      return serialized;
    },
    hasContent() {
      if (contentDoc !== view.state.doc) {
        contentDoc = view.state.doc;
        hasText = false;
        for (let iter = contentDoc.iter(); !iter.next().done;) {
          if (/\S/.test(iter.value)) { hasText = true; break; }
        }
      }
      return hasText;
    },
    setMarkdown(value) {
      resetting = true;
      try { view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value }, selection: { anchor: 0 } }); }
      finally { resetting = false; }
      Vim.exitInsertMode(cm);
    },
    setReadOnly(readOnly) {
      view.dispatch({ effects: editable.reconfigure([
        EditorState.readOnly.of(readOnly), EditorView.editable.of(!readOnly),
      ]) });
    },
    getSelection: () => view.state.sliceDoc(view.state.selection.main.from, view.state.selection.main.to),
    focus: () => view.focus(),
    focusAtTextOffset(offset) {
      const raw = typeof offset === 'number' ? offset : offset?.globalOffset;
      this.selectRange(Number.isFinite(raw) ? raw : view.state.doc.length);
    },
    selectRange(from, to = from) {
      const clamp = value => Math.max(0, Math.min(value, view.state.doc.length));
      view.dispatch({ selection: { anchor: clamp(from), head: clamp(to) }, scrollIntoView: true });
      view.focus();
    },
    destroy() {
      actions.delete(cm);
      host.removeEventListener('keydown', stopEscape);
      view.destroy();
    },
  };
}

function reset() {
  // The engine shares registers, macros, searches and user mappings across
  // views. Keep them within the signed-in session, including after N is off.
  Vim.mapclear();
  Vim.resetVimGlobalState_();
}

window.GnotesVimEditor = { mount, reset };
