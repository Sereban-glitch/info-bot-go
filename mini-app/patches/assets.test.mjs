import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import crypto from 'node:crypto';
const staticRoot=new URL('../../internal/web/static/',import.meta.url);
const html=fs.readFileSync(new URL('index.html',staticRoot),'utf8');
const hash=content=>crypto.createHash('sha256').update(content).digest('hex').slice(0,12);

test('entrypoint retains Telegram bootstrap and existing stylesheet with one app module',()=>{
 assert.equal([...html.matchAll(/<script[^>]+type="module"[^>]+src="\/assets\/index-status-[a-f0-9]{12}\.js"/g)].length,1);
 assert.ok(html.includes('src="https://telegram.org/js/telegram-web-app.js"'));
 assert.ok(html.includes('href="/assets/index-BlaitVLD.css"'));
 for(const [,ref] of html.matchAll(/(?:src|href)="(\/assets\/[^\"]+)"/g)) {
  assert.ok(fs.statSync(new URL(ref.slice(1),staticRoot)).size>0,`missing or empty ${ref}`);
 }
});

test('generated JavaScript and blue stylesheet are content-addressed and load in override order',()=>{
 const script=html.match(/src="(\/assets\/index-status-([a-f0-9]{12})\.js)"/);
 assert.ok(script,'generated application entrypoint missing');
 assert.equal(hash(fs.readFileSync(new URL(script[1].slice(1),staticRoot))),script[2]);
 const styles=[...html.matchAll(/href="(\/assets\/blue-theme-([a-f0-9]{12})\.css)"/g)];
 assert.equal(styles.length,1,'expected exactly one generated blue stylesheet');
 const css=fs.readFileSync(new URL(styles[0][1].slice(1),staticRoot),'utf8');
 assert.equal(hash(css),styles[0][2]);
 assert.equal(css,fs.readFileSync(new URL('blue-theme.css',import.meta.url),'utf8'));
 assert.ok(html.indexOf(styles[0][1])>html.indexOf('/assets/index-BlaitVLD.css'));
 for(const selector of ['.sz-home','.sz-next-action','.sz-request-card','.sz-detail','.sz-timeline']) assert.ok(css.includes(selector),`${selector} styling missing`);
 assert.ok(!/@import\b|https?:\/\//i.test(css),'blue stylesheet must not add external dependencies');
});
