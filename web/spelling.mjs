import { Extension } from '@tiptap/core';
import { Plugin, PluginKey } from '@tiptap/pm/state';
import { Decoration, DecorationSet } from '@tiptap/pm/view';

// Decorations suppress native checking without changing stored Markdown.
export const spellingKey = new PluginKey('gnotesSpelling');
let builtinNamesVersion = 0;
let builtinNames = new Map();
export function setBuiltinNames(entries) {
  const index = new Map();
  for (const entry of entries) {
    if (typeof entry !== 'string' || entry.length > 96) continue;
    const text = entry.toLowerCase().replaceAll('’', "'").replace(/\s+/g, ' ');
    const tokens = [...text.matchAll(/[\p{L}\p{N}]+(?:['’\-][\p{L}\p{N}]+)*/gu)];
    if (!tokens.length || tokens.length > 12) continue;
    const first = tokens[0][0];
    if (!index.has(first)) index.set(first, new Map());
    const lengths = index.get(first);
    if (!lengths.has(tokens.length)) lengths.set(tokens.length, new Set());
    lengths.get(tokens.length).add(text);
  }
  builtinNames = index;
  builtinNamesVersion++;
}

function spellingActive(preferences) {
  return preferences.enabled && (preferences.ignored.size || preferences.names.size ||
    (builtinNames.size && preferences.builtin !== false));
}

function decorateTextblock(node, pos, preferences) {
  const decorations = [];
  if (node.type.name === 'codeBlock') return decorations;
  const text = node.textBetween(0, node.content.size, '\n', '\n');
  const tokens = [...text.matchAll(/[\p{L}\p{N}]+(?:['’\-][\p{L}\p{N}]+)*/gu)];
  const normalize = value => value.toLowerCase().replaceAll('’', "'").replace(/\s+/g, ' ');
  const decorate = (from, to, name = false) => {
    let code = false;
    node.nodesBetween(from, to, child => { if (child.marks.some(mark => mark.type.name === 'code')) code = true; });
    if (!code) decorations.push(Decoration.inline(pos + 1 + from, pos + 1 + to,
      { spellcheck: 'false', 'data-spelling-ignored': name ? 'name' : 'word' }));
  };
  for (let i = 0; i < tokens.length; i++) {
    const token = tokens[i], word = normalize(token[0]);
    if (preferences.ignored.has(word)) decorate(token.index, token.index + token[0].length);
    for (const name of preferences.names.get(word) || []) {
      const last = tokens[i + name.count - 1];
      if (last && normalize(text.slice(token.index, last.index + last[0].length)) === name.text)
        decorate(token.index, last.index + last[0].length, true);
    }
    // Shared names require capitalization. Lowercase ordinary words
    // remain checked; per-user names retain their existing matching.
    if (preferences.builtin !== false && /^\p{Lu}/u.test(token[0])) {
      for (const [count, names] of builtinNames.get(word) || []) {
        const last = tokens[i + count - 1];
        if (last && names.has(normalize(text.slice(token.index, last.index + last[0].length))))
          decorate(token.index, last.index + last[0].length, true);
      }
    }
  }
  return decorations;
}

function buildDecorations(doc, preferences) {
  if (!spellingActive(preferences)) return DecorationSet.empty;
  const decorations = [];
  doc.descendants((node, pos) => {
    if (!node.isTextblock) return;
    decorations.push(...decorateTextblock(node, pos, preferences));
    return false;
  });
  return DecorationSet.create(doc, decorations);
}

export const SpellingPreferences = Extension.create({
  name: 'spellingPreferences',
  addProseMirrorPlugins() {
    return [new Plugin({
      key: spellingKey,
      state: {
        init(_, state) {
          const preferences = { enabled: true, ignored: new Set(), names: new Map() };
          return { preferences, version: builtinNamesVersion, decorations: buildDecorations(state.doc, preferences) };
        },
        apply(tr, previous, oldState) {
          const preferences = tr.getMeta(spellingKey);
          if (preferences || previous.version !== builtinNamesVersion) {
            const next = preferences || previous.preferences;
            return { preferences: next, version: builtinNamesVersion, decorations: buildDecorations(tr.doc, next) };
          }
          // Selection, focus and toolbar transactions do not change spelling.
          if (!tr.docChanged || !spellingActive(previous.preferences)) return previous;
          let start = oldState.doc.content.findDiffStart(tr.doc.content);
          if (start === null) return previous;
          let end = oldState.doc.content.findDiffEnd(tr.doc.content)?.b ?? start;
          // Equal adjacent paragraphs can make a content diff place an
          // insertion at the opposite end of the repeated text. Include the
          // actual step ranges, mapped through any later steps in this edit.
          tr.mapping.maps.forEach((map, index) => {
            const rest = tr.mapping.slice(index + 1);
            map.forEach((_from, _to, from, to) => {
              start = Math.min(start, rest.map(from, -1));
              end = Math.max(end, rest.map(to, 1));
            });
          });
          let decorations = previous.decorations.map(tr.mapping, tr.doc);
          const removed = new Set(), added = [];
          // Include either side of a boundary so splits, joins and deletions
          // also rebuild the adjacent textblocks. Unchanged blocks retain their
          // mapped decorations; only the changed envelope is tokenized again.
          tr.doc.nodesBetween(Math.max(0, start - 1), Math.min(tr.doc.content.size, Math.max(start, end) + 1), (node, pos) => {
            if (!node.isTextblock) return;
            for (const decoration of decorations.find(pos, pos + node.nodeSize)) removed.add(decoration);
            added.push(...decorateTextblock(node, pos, previous.preferences));
            return false;
          });
          decorations = decorations.remove([...removed]).add(tr.doc, added);
          return { ...previous, decorations };
        },
      },
      props: { decorations: state => spellingKey.getState(state).decorations },
    })];
  },
});
