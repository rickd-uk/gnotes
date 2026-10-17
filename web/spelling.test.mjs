import assert from 'node:assert/strict';
import test from 'node:test';
import { Schema } from '@tiptap/pm/model';
import { EditorState, TextSelection } from '@tiptap/pm/state';
import { history, undo, redo } from '@tiptap/pm/history';
import { SpellingPreferences, spellingKey, setBuiltinNames } from './spelling.mjs';

const schema = new Schema({
  nodes: {
    doc: { content: 'block+' },
    paragraph: { group: 'block', content: 'inline*' },
    blockquote: { group: 'block', content: 'block+' },
    codeBlock: { group: 'block', content: 'text*', marks: '', code: true },
    text: { group: 'inline' },
    hardBreak: { group: 'inline', inline: true, selectable: false },
  },
  marks: { code: {}, bold: {} },
});
const paragraph = text => schema.node('paragraph', null, text ? schema.text(text) : null);
const preferences = {
  enabled: true, ignored: new Set(['specialword']),
  names: new Map([['private', [{ text: 'private company', count: 2 }]]]), builtin: true,
};
function create(doc, settings = preferences) {
  const plugin = SpellingPreferences.config.addProseMirrorPlugins()[0];
  let state = EditorState.create({ schema, doc, plugins: [plugin, history()] });
  return state.apply(state.tr.setMeta(spellingKey, settings));
}
function spans(state) {
  return spellingKey.getState(state).decorations.find().map(decoration => ({
    from: decoration.from, to: decoration.to, attrs: decoration.type.attrs,
  })).sort((a, b) => a.from - b.from || a.to - b.to || JSON.stringify(a.attrs).localeCompare(JSON.stringify(b.attrs)));
}
function matchesFullRebuild(state) {
  assert.deepEqual(spans(state), spans(create(state.doc, spellingKey.getState(state).preferences)));
}

test('selection changes reuse decorations and edits update names without changing content', () => {
  setBuiltinNames(['Tokyo', 'New York']);
  let state = create(schema.node('doc', null, [paragraph('Tokyo New York specialword'), paragraph('Private Company tokyo')]));
  const before = spellingKey.getState(state).decorations;
  const text = state.doc.textContent;
  state = state.apply(state.tr.setSelection(TextSelection.create(state.doc, 4)));
  assert.equal(spellingKey.getState(state).decorations, before);
  assert.equal(state.doc.textContent, text);
  assert.deepEqual(spans(state).map(span => state.doc.textBetween(span.from, span.to)), ['Tokyo', 'New York', 'specialword', 'Private Company']);
  state = state.apply(state.tr.insertText('x', 3));
  matchesFullRebuild(state);
  assert.equal(spans(state).some(span => state.doc.textBetween(span.from, span.to).includes('Txokyo')), false);
});

test('splits, joins, multi-step replacements, code marks and undo retain correct decorations', () => {
  setBuiltinNames(['Tokyo', 'New York']);
  let state = create(schema.node('doc', null, [
    paragraph('Tokyo New York specialword'),
    schema.node('blockquote', null, [paragraph('Private Company in Tokyo'), paragraph('New York')]),
    paragraph('Tokyo'), paragraph('Tokyo'),
  ]));
  const apply = tr => { state = state.apply(tr); matchesFullRebuild(state); };
  apply(state.tr.split(10));
  apply(state.tr.join(11));
  apply(state.tr.addMark(1, 6, schema.marks.code.create()));
  assert.equal(spans(state).some(span => span.from === 1), false);
  apply(state.tr.removeMark(1, 6, schema.marks.code));
  apply(state.tr.insertText('New York ', 1).insertText('Tokyo', state.doc.content.size - 1 + 9));
  apply(state.tr.setBlockType(0, 1, schema.nodes.codeBlock));
  assert.equal(spans(state).some(span => span.from < state.doc.firstChild.nodeSize), false);
  apply(state.tr.setBlockType(0, 1, schema.nodes.paragraph));
  apply(state.tr.delete(0, state.doc.firstChild.nodeSize));
  assert.equal(undo(state, apply), true);
  assert.equal(redo(state, apply), true);
  apply(state.tr.replaceWith(0, state.doc.content.size, paragraph('New York Tokyo')));
  apply(state.tr.delete(1, state.doc.content.size - 1));
  assert.deepEqual(spans(state), []);
});

test('incremental decorations equal a full rebuild through repeated boundary edits', () => {
  setBuiltinNames(['Tokyo', 'New York']);
  let state = create(schema.node('doc', null, Array.from({ length: 30 }, () => paragraph('Tokyo New York specialword'))));
  let seed = 731;
  const random = max => { seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0; return seed % max; };
  for (let i = 0; i < 180; i++) {
    const positions = [];
    state.doc.descendants((node, pos) => { if (node.isTextblock) { positions.push({ node, pos }); return false; } });
    const { node, pos } = positions[random(positions.length)];
    const at = pos + 1 + random(node.content.size + 1);
    let tr = state.tr;
    if (i % 4 === 0) tr = tr.split(at);
    else if (i % 4 === 1 && at < pos + 1 + node.content.size) tr = tr.delete(at, at + 1);
    else tr = tr.insertText(['Tokyo', ' ', 'New York', 'specialword'][random(4)], at);
    state = state.apply(tr);
    matchesFullRebuild(state);
  }
});

test('identical paragraph insertions and moves rebuild the actual changed range', () => {
  setBuiltinNames(['Tokyo']);
  const repeated = paragraph('Tokyo');
  let state = create(schema.node('doc', null, [repeated, repeated, repeated]));
  state = state.apply(state.tr.insert(0, repeated));
  matchesFullRebuild(state);
  state = state.apply(state.tr.delete(0, repeated.nodeSize));
  matchesFullRebuild(state);
  state = state.apply(state.tr.insert(0, repeated).insert(state.doc.content.size + repeated.nodeSize, repeated));
  matchesFullRebuild(state);
});

test('preference and built-in list changes invalidate cached decorations', () => {
  setBuiltinNames(['Tokyo']);
  let state = create(schema.node('doc', null, paragraph('Tokyo Paris specialword')));
  state = state.apply(state.tr.setMeta(spellingKey, { ...preferences, enabled: false }));
  assert.deepEqual(spans(state), []);
  state = state.apply(state.tr.insertText('Tokyo ', 1));
  assert.deepEqual(spans(state), []);
  state = state.apply(state.tr.setMeta(spellingKey, preferences));
  matchesFullRebuild(state);
  setBuiltinNames(['Paris']);
  state = state.apply(state.tr.setSelection(TextSelection.create(state.doc, 2)));
  matchesFullRebuild(state);
  assert.deepEqual(spans(state).map(span => state.doc.textBetween(span.from, span.to)), ['Paris', 'specialword']);
});
