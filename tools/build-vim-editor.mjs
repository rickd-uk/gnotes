import { build } from 'esbuild';
import { readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';

const result = await build({
  entryPoints: ['web/vim-editor.js'],
  bundle: true, minify: true, format: 'iife',
  outfile: 'public/vim-editor.js', metafile: true,
  banner: { js: '/*! Third-party notices: /vim-editor.LICENSE.txt */' },
  logLevel: 'info',
});

// Ship dependency notices alongside the standalone bundle in release packages.
const packages = new Set();
for (const input of Object.keys(result.metafile.inputs)) {
  const match = input.match(/^(.*node_modules\/(?:@[^/]+\/)?[^/]+)\//);
  if (match) packages.add(match[1]);
}
const notices = [];
for (const root of [...packages].sort()) {
  const pkg = JSON.parse(await readFile(path.join(root, 'package.json'), 'utf8'));
  let license;
  for (const name of ['LICENSE', 'LICENSE.md', 'LICENSE.txt', 'license', 'license.md', 'COPYING']) {
    try { license = await readFile(path.join(root, name), 'utf8'); break; }
    catch (error) { if (error.code !== 'ENOENT') throw error; }
  }
  if (!license) throw new Error(`Missing license for ${pkg.name}`);
  notices.push(`${pkg.name} ${pkg.version}\n${'='.repeat(72)}\n${license.trim()}\n`);
}
await writeFile('public/vim-editor.LICENSE.txt', notices.join('\n'));
