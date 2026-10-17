// Synthetic spelling cost only; excludes DOM layout, serialization and saves.
import { readFileSync } from 'node:fs';
import { Schema } from '@tiptap/pm/model';
import { EditorState, TextSelection } from '@tiptap/pm/state';
import { SpellingPreferences, setBuiltinNames } from '../web/spelling.mjs';

const schema = new Schema({ nodes: {
  doc: { content: 'block+' }, paragraph: { group: 'block', content: 'text*' }, text: { group: 'inline' },
} });
setBuiltinNames(JSON.parse(readFileSync(new URL('../public/builtin-names.json', import.meta.url), 'utf8')).names);
const body = 'Tokyo New York Albert Einstein OpenAI Microsoft and several ordinary words. '.repeat(6);
const doc = schema.node('doc', null, Array.from({ length: 500 }, () => schema.node('paragraph', null, schema.text(body))));
const plugin = SpellingPreferences.config.addProseMirrorPlugins()[0];
let state = EditorState.create({ schema, doc, plugins: [plugin] });

function measure(label, run) {
  for (let i = 0; i < 5; i++) run(i);
  const start = performance.now();
  for (let i = 0; i < 30; i++) run(i);
  console.log(`${label}: ${((performance.now() - start) / 30).toFixed(3)} ms/op`);
}
console.log(`Spelling benchmark: 500 paragraphs, ${doc.textContent.length.toLocaleString()} characters`);
measure('Selection and decoration lookup', i => {
  state = state.apply(state.tr.setSelection(TextSelection.create(state.doc, 1 + i % 20)));
  plugin.props.decorations(state);
});
measure('Single-character edit and decoration lookup', () => {
  state = state.apply(state.tr.insertText('x', 10));
  plugin.props.decorations(state);
});
