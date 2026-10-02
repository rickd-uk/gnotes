import { Editor } from '@tiptap/core';
import StarterKit from '@tiptap/starter-kit';
import { TaskItem, TaskList } from '@tiptap/extension-list';
import { Markdown } from '@tiptap/markdown';

const extensions = [
  StarterKit.configure({ heading: { levels: [2, 3] } }),
  TaskList,
  TaskItem.configure({ nested: true }),
  Markdown.configure({ markedOptions: { gfm: true, breaks: false } }),
];

function mount(host, markdown, onChange, onContext = () => {}) {
  function updateContext(editor) {
    const selection = editor.state.selection;
    const { from, to, empty, $from } = selection;
    let mode = null;
    if (!empty) mode = 'selection';
    else if ($from.parent.type.name === 'paragraph' &&
      $from.parent.textBetween(0, $from.parentOffset) === '/') mode = 'slash';
    if (!mode) return onContext(null);
    try {
      const start = editor.view.coordsAtPos(from);
      const end = editor.view.coordsAtPos(to);
      onContext({ mode, x: (start.left + end.right) / 2, y: Math.min(start.top, end.top) });
    } catch { onContext(null); }
  }

  const editor = new Editor({
    element: host,
    extensions,
    content: markdown || '',
    contentType: 'markdown',
    editorProps: { attributes: { 'aria-label': 'Note content', spellcheck: 'true' } },
    onUpdate: ({ editor: current }) => {
      current.view.dom.dataset.empty = String(current.isEmpty);
      onChange(current.getMarkdown());
      updateContext(current);
    },
    onSelectionUpdate: ({ editor: current }) => updateContext(current),
  });
  editor.view.dom.dataset.empty = String(editor.isEmpty);
  return {
    editor,
    getMarkdown: () => editor.getMarkdown(),
    setMarkdown: (value) => {
      editor.commands.setContent(value || '', { contentType: 'markdown', emitUpdate: false });
      editor.view.dom.dataset.empty = String(editor.isEmpty);
      onContext(null);
    },
    focus: () => editor.commands.focus(),
    destroy: () => { onContext(null); editor.destroy(); },
    format(command, removeSlash = false) {
      const chain = editor.chain().focus();
      if (removeSlash) chain.deleteRange({ from: editor.state.selection.from - 1, to: editor.state.selection.from });
      if (command === 'bold') chain.toggleBold().run();
      if (command === 'italic') chain.toggleItalic().run();
      if (command === 'heading') chain.toggleHeading({ level: 2 }).run();
      if (command === 'bullet') chain.toggleBulletList().run();
      if (command === 'numbered') chain.toggleOrderedList().run();
      if (command === 'task') chain.toggleTaskList().run();
      if (command === 'link') {
        const old = editor.getAttributes('link').href || 'https://';
        const href = window.prompt('Link URL', old);
        if (href === null) return;
        if (!href.trim()) return chain.unsetLink().run();
        try {
          const parsed = new URL(href);
          if (!['http:', 'https:', 'mailto:'].includes(parsed.protocol)) return;
        } catch { return; }
        chain.extendMarkRange('link').setLink({ href }).run();
      }
    },
  };
}

window.GnotesRichEditor = { mount };
