// Dependency-free, fail-closed reconstruction of focused missing-source fixes.
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import {fileURLToPath} from 'node:url';
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const dir = path.join(root, 'mini-app/patches');
const manifest = JSON.parse(fs.readFileSync(path.join(dir, 'manifest.json')));
const hash = data => crypto.createHash('sha256').update(data).digest('hex');
let bundle = fs.readFileSync(path.join(root, manifest.baseAsset), 'utf8');
if (hash(bundle) !== manifest.sha256) throw new Error('Base asset changed; recover/review patches against the new upstream asset.');
for (const patch of manifest.changes) {
  if (bundle.split(patch.before).length !== 2) throw new Error('Patch anchor must occur exactly once: ' + patch.before.slice(0,80));
  const replacement = patch.source
    ? (Array.isArray(patch.source) ? patch.source : [patch.source]).map(source => fs.readFileSync(path.join(dir, source), 'utf8')).join('\n') + (patch.append || '')
    : patch.after;
  bundle = bundle.replace(patch.before, () => replacement);
}
const name = `index-status-${hash(bundle).slice(0,12)}.js`;
const output = path.join(root, 'internal/web/static/assets', name);
const indexPath = path.join(root, 'internal/web/static/index.html');
let html = fs.readFileSync(indexPath, 'utf8');
const refs = [...html.matchAll(/src="\/assets\/index-[^"]+\.js"/g)];
if (refs.length !== 1) throw new Error('Expected exactly one app bundle entrypoint');
html = html.replace(refs[0][0], `src="/assets/${name}"`);
const styles = (manifest.styles || []).map(style => {
  const contents = fs.readFileSync(path.join(dir, style.source), 'utf8');
  const name = `${style.prefix}-${hash(contents).slice(0,12)}.css`;
  const output = path.join(root, 'internal/web/static/assets', name);
  const refs = [...html.matchAll(new RegExp(`<link[^>]+href="/assets/${style.prefix}-[^"]+\\.css"[^>]*>`, 'g'))];
  if (refs.length > 1) throw new Error('Expected at most one generated style entrypoint');
  const link = `<link rel="stylesheet" href="/assets/${name}">`;
  if (refs.length) html = html.replace(refs[0][0], link);
  else {
    if (html.split('</head>').length !== 2) throw new Error('Expected exactly one HTML head');
    html = html.replace('</head>', `  ${link}\n</head>`);
  }
  return {name, output, contents};
});
if (process.argv.includes('--check')) {
  if (!fs.existsSync(output) || fs.readFileSync(output,'utf8') !== bundle || fs.readFileSync(indexPath,'utf8') !== html) throw new Error('Generated asset or entrypoint is stale');
  for (const style of styles) if (!fs.existsSync(style.output) || fs.readFileSync(style.output, 'utf8') !== style.contents) throw new Error('Generated stylesheet is stale');
  console.log('Generated asset and entrypoint verified:', name, ...styles.map(style => style.name));
} else {
  fs.writeFileSync(output, bundle);
  for (const style of styles) fs.writeFileSync(style.output, style.contents);
  fs.writeFileSync(indexPath, html);
  console.log('Generated:', name);
}
