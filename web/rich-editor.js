import { Editor, Extension, InputRule, PasteRule } from '@tiptap/core';
import StarterKit from '@tiptap/starter-kit';
import { TaskItem, TaskList } from '@tiptap/extension-list';
import { Markdown } from '@tiptap/markdown';
import { Marked } from 'marked';
import { closeHistory } from '@tiptap/pm/history';
import { spellingKey, SpellingPreferences, setBuiltinNames } from './spelling.mjs';

// Keep comparison rendering separate from Tiptap's custom tokenizers.
const comparisonMarkdown = new Marked({ gfm: true, breaks: true });

const markdownLinkPattern = /(?<!\\)\[([^\]\n]+)\]\((https?:\/\/[^\s)]+|mailto:[^\s)]+)\)/g;

function replaceMarkdownLink({ state, range, match }) {
  const [, label, href] = match;
  try {
    if (!['http:', 'https:', 'mailto:'].includes(new URL(href).protocol)) return null;
  } catch { return null; }
  const link = state.schema.marks.link;
  if (!link) return null;
  const marks = state.doc.resolve(range.from).marks().filter((mark) => mark.type !== link);
  state.tr.replaceWith(range.from, range.to,
    state.schema.text(label, [...marks, link.create({ href })])).removeStoredMark(link);
}

const MarkdownLinkShortcut = Extension.create({
  name: 'markdownLinkShortcut',
  addInputRules() {
    return [new InputRule({
      find: /(?<!\\)\[([^\]\n]+)\]\((https?:\/\/[^\s)]+|mailto:[^\s)]+)\)$/,
      handler: replaceMarkdownLink,
    })];
  },
  addPasteRules() {
    return [new PasteRule({ find: markdownLinkPattern, handler: replaceMarkdownLink })];
  },
});

const WritingStarterKit = StarterKit.extend({
  addExtensions() {
    return this.parent().map((extension) => {
      if (extension.name === 'bold') return extension.extend({ keepOnSplit: false });
      // New text at a link boundary must not inherit the previous URL.
      if (extension.name === 'link') return extension.extend({ inclusive: false });
      return extension;
    });
  },
});

const TextIndent = Extension.create({
  name: 'textIndent',
  priority: 1100,
  addKeyboardShortcuts() {
    const indent = (outdent) => {
      const editor = this.editor;
      for (const type of ['listItem', 'taskItem']) {
        if (editor.isActive(type)) {
          editor.commands[outdent ? 'liftListItem' : 'sinkListItem'](type);
          return true;
        }
      }
      const { state } = editor;
      const { from, to } = state.selection;
      const tr = closeHistory(state.tr);
      // Nonbreaking spaces preserve indentation through Markdown without
      // turning ordinary paragraphs into indented code blocks.
      state.doc.nodesBetween(from, to, (node, pos) => {
        if (!node.isTextblock) return;
        let offset = 0;
        for (const line of node.textBetween(0, node.content.size, '\n', '\n').split('\n')) {
          const start = pos + 1 + offset;
          offset += line.length + 1;
          if (start + line.length < from || (from !== to ? start >= to : start > to)) continue;
          if (outdent) {
            const leading = line.match(/^(?:\t|[ \u00a0]{1,4})/);
            if (leading) tr.delete(tr.mapping.map(start), tr.mapping.map(start + leading[0].length));
          } else {
            tr.insertText(node.type.name === 'codeBlock' ? '    ' : '\u00a0\u00a0\u00a0\u00a0', tr.mapping.map(start));
          }
        }
        return false;
      });
      editor.view.dispatch(tr.scrollIntoView());
      return true;
    };
    return { Tab: () => indent(false), 'Shift-Tab': () => indent(true) };
  },
});

const extensions = [
  WritingStarterKit.configure({ heading: { levels: [2, 3] }, link: { openOnClick: false } }),
  TaskList,
  TaskItem.configure({ nested: true }),
  Markdown.configure({ markedOptions: { gfm: true, breaks: true } }),
  MarkdownLinkShortcut,
  SpellingPreferences,
  TextIndent,
];

// onDirty opts into lazy serialization. App read/save boundaries call the
// adapter's cached getMarkdown(); legacy onChange callbacks still receive text.
function mount(host, markdown, onChange, onContext = () => {}, onLink = () => {}, { onDirty } = {}) {
  let serializedDoc = null, serializedMarkdown = '';
  let contentDoc = null, containsContent = false;
  function readMarkdown(current) {
    if (serializedDoc !== current.state.doc) {
      serializedMarkdown = current.getMarkdown();
      serializedDoc = current.state.doc;
    }
    return serializedMarkdown;
  }
  function hasContent() {
    const doc = editor.state.doc;
    if (contentDoc !== doc) {
      containsContent = false;
      let formattingOnly = false;
      // Ordinary text needs no serialization to enable Save. Empty formatted
      // blocks have different Markdown rules, so ask the serializer only when
      // there is formatting but no non-whitespace text anywhere in the doc.
      doc.descendants(node => {
        if (containsContent) return false;
        if (node.isText) {
          containsContent = /\S/.test(node.text);
          formattingOnly ||= node.marks.length > 0;
        } else if (node.type.name !== 'paragraph' && node.type.name !== 'hardBreak') formattingOnly = true;
        return !containsContent;
      });
      if (!containsContent && formattingOnly) containsContent = Boolean(readMarkdown(editor).trim());
      contentDoc = doc;
    }
    return containsContent;
  }
  function updateContext(editor) {
    const selection = editor.state.selection;
    const { from, to, empty, $from } = selection;
    let mode = null;
    if ($from.parent.type.name === 'codeBlock') mode = 'codeBlock';
    else if (!empty) mode = 'selection';
    else if ($from.parent.type.name === 'paragraph' &&
      $from.parent.textBetween(0, $from.parentOffset) === '/') mode = 'slash';
    if (!mode) return onContext(null);
    try {
      const start = editor.view.coordsAtPos(from);
      const end = editor.view.coordsAtPos(to);
      onContext({ mode, x: (start.left + end.right) / 2, y: Math.min(start.top, end.top),
        language: mode === 'codeBlock' ? ($from.parent.attrs.language || '') : '' });
    } catch { onContext(null); }
  }

  const editor = new Editor({
    element: host,
    extensions,
    content: markdown || '',
    contentType: 'markdown',
    editorProps: {
      attributes: { 'aria-label': 'Note content', spellcheck: 'true' },
      handleDOMEvents: {
        click(view, event) {
          const link = event.target.closest?.('a[href]');
          if (!link) return false;
          event.preventDefault();
          if (event.ctrlKey || event.metaKey) {
            window.open(link.href, '_blank', 'noopener,noreferrer');
            return true;
          }
          return false;
        },
      },
      handleTextInput(view, from, to, text) {
        if (from !== to || !/^[ \t]+$/.test(text)) return false;
        const { $from } = view.state.selection;
        const bold = view.state.schema.marks.bold;
        const marks = view.state.storedMarks || $from.marks();
        if (!bold.isInSet(marks) || bold.isInSet($from.nodeAfter?.marks || [])) return false;
        // A space after a bold word starts ordinary text. Spaces inside an
        // existing bold phrase retain its formatting.
        view.dispatch(view.state.tr.insertText(text, from, to)
          .removeMark(from, from + text.length, bold).removeStoredMark(bold));
        return true;
      },
    },
    onUpdate: ({ editor: current }) => {
      current.view.dom.dataset.empty = String(current.isEmpty);
      // App editors serialize at save/read boundaries, not on each keystroke.
      // Keep eager callbacks available for other consumers of this adapter.
      if (onDirty) onDirty();
      else onChange?.(readMarkdown(current));
      updateContext(current);
    },
    onSelectionUpdate: ({ editor: current }) => updateContext(current),
  });
  editor.view.dom.dataset.empty = String(editor.isEmpty);
  return {
    editor,
    hasContent,
    setSpelling(enabled, ignored, nameEntries = [], builtin = true) {
      editor.setOptions({ editorProps: { ...editor.options.editorProps,
        attributes: { ...editor.options.editorProps.attributes, spellcheck: String(enabled) } } });
      const names = new Map();
      for (const entry of nameEntries) {
        const raw = entry.name.toLowerCase().replaceAll('’', "'").replace(/\s+/g, ' ');
        const tokens = [...raw.matchAll(/[\p{L}\p{N}]+(?:['\-][\p{L}\p{N}]+)*/gu)];
        if (!tokens.length) continue;
        const last = tokens[tokens.length - 1];
        const text = raw.slice(tokens[0].index, last.index + last[0].length);
        const key = tokens[0][0];
        if (!names.has(key)) names.set(key, []);
        names.get(key).push({ text, count: tokens.length });
      }
      editor.view.dispatch(editor.state.tr.setMeta(spellingKey, { enabled, ignored: new Set(ignored), names, builtin }));
    },
    preservesContent(markdown) {
      // Compare rendered content, not source spelling: the serializer escapes
      // literal asterisks and writes explicit breaks for ordinary newlines.
      // Unsupported content (for example tables) must still use the fallback.
      const render = (value) => {
        const template = document.createElement('template');
        template.innerHTML = comparisonMarkdown.parse(value);
        function canonical(node, verbatim = false) {
          if (node.nodeType === Node.TEXT_NODE) {
            if (verbatim) return node.textContent;
            let text = node.textContent.replace(/\s+/g, ' ');
            if (!node.previousSibling || node.previousSibling.nodeName === 'BR') text = text.trimStart();
            if (!node.nextSibling || node.nextSibling.nodeName === 'BR') text = text.trimEnd();
            return text || null;
          }
          const attributes = Array.from(node.attributes || [], (attr) => [attr.name, attr.value]).sort();
          return [node.nodeName, attributes, Array.from(node.childNodes,
            (child) => canonical(child, verbatim || node.nodeName === 'PRE' || node.nodeName === 'CODE'))
            .filter((child) => child !== null)];
        }
        return JSON.stringify(canonical(template.content));
      };
      return render(markdown) === render(readMarkdown(editor));
    },
    getMarkdown: () => readMarkdown(editor),
    setMarkdown: (value) => {
      editor.commands.setContent(value || '', { contentType: 'markdown', emitUpdate: false });
      editor.view.dom.dataset.empty = String(editor.isEmpty);
      onContext(null);
    },
    focus: () => editor.commands.focus(),
    focusAtTextOffset(offset) {
      const position = typeof offset === 'object' && offset !== null ? offset : { offset };
      if (!Number.isInteger(position.offset) || position.offset < 0) return editor.commands.focus('end');
      const block = Number.isInteger(position.blockIndex)
        ? editor.view.dom.children[position.blockIndex]
        : null;
      const scope = block || editor.view.dom;
      const walker = document.createTreeWalker(scope, NodeFilter.SHOW_TEXT);
      let remaining = block ? position.offset : (position.globalOffset ?? position.offset);
      let node;
      while ((node = walker.nextNode())) {
        if (remaining <= node.textContent.length) {
          const position = editor.view.posAtDOM(node, remaining);
          return editor.chain().focus().setTextSelection(position).run();
        }
        remaining -= node.textContent.length;
      }
      return editor.commands.focus('end');
    },
    destroy: () => { onContext(null); editor.destroy(); },
    setCodeLanguage(language) {
      if (editor.isDestroyed || editor.state.selection.$from.parent.type.name !== 'codeBlock') return;
      editor.chain().focus().updateAttributes('codeBlock', { language: language || null }).run();
      updateContext(editor);
    },
    format(command, removeSlash = false) {
      if (command === 'link') {
        const href = editor.getAttributes('link').href || '';
        if (href && editor.state.selection.empty) editor.commands.extendMarkRange('link');
        const { from, to } = editor.state.selection;
        const otherMarks = editor.state.selection.$from.marks()
          .filter((mark) => mark.type.name !== 'link')
          .map((mark) => ({ type: mark.type.name, attrs: mark.attrs }));
        onLink({
          text: editor.state.doc.textBetween(from, to, ' '),
          href,
          apply(label, url) {
            if (editor.isDestroyed) return;
            editor.chain().focus().insertContentAt({ from, to }, {
              type: 'text', text: label, marks: [...otherMarks, { type: 'link', attrs: { href: url } }],
            }).run();
          },
          remove() {
            if (editor.isDestroyed || !href) return;
            editor.chain().focus().setTextSelection({ from, to }).unsetLink().run();
          },
        });
        return;
      }
      const chain = editor.chain().focus();
      if (removeSlash) chain.deleteRange({ from: editor.state.selection.from - 1, to: editor.state.selection.from });
      if (command === 'bold') chain.toggleBold().run();
      if (command === 'italic') chain.toggleItalic().run();
      if (command === 'strike') chain.toggleStrike().run();
      if (command === 'code') chain.toggleCode().run();
      if (command === 'heading') chain.toggleHeading({ level: 2 }).run();
      if (command === 'subheading') chain.toggleHeading({ level: 3 }).run();
      if (command === 'bullet') chain.toggleBulletList().run();
      if (command === 'numbered') chain.toggleOrderedList().run();
      if (command === 'task') chain.toggleTaskList().run();
      if (command === 'quote') chain.toggleBlockquote().run();
      if (command === 'codeBlock') chain.toggleCodeBlock().run();
      if (command === 'normalText') chain.setParagraph().run();
      if (command === 'divider') chain.setHorizontalRule().run();
    },
  };
}

window.GnotesRichEditor = { mount, setBuiltinNames };
