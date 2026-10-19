import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import {fileURLToPath} from 'node:url';
import {spawnSync} from 'node:child_process';
const root=fileURLToPath(new URL('../../',import.meta.url));
function fixture(run) {
 const temp=fs.mkdtempSync(path.join(os.tmpdir(),'smartzapyt-builder-'));
 try {
  fs.cpSync(path.join(root,'mini-app'),path.join(temp,'mini-app'),{recursive:true,filter:source=>!source.includes(path.join('qa','results'))});
  fs.cpSync(path.join(root,'internal/web/static'),path.join(temp,'internal/web/static'),{recursive:true});
  const build=(...args)=>spawnSync(process.execPath,[path.join(temp,'mini-app/build-patches.mjs'),...args],{encoding:'utf8'});
  const index=path.join(temp,'internal/web/static/index.html');
  run({temp,build,index});
 } finally {fs.rmSync(temp,{recursive:true,force:true});}
}
test('builder fails closed for modified base and ambiguous anchors',()=>{
 const temp=fs.mkdtempSync(path.join(os.tmpdir(),'smartzapyt-builder-'));
 try {
  fs.cpSync(path.join(root,'mini-app'),path.join(temp,'mini-app'),{recursive:true});
  fs.cpSync(path.join(root,'internal/web/static'),path.join(temp,'internal/web/static'),{recursive:true});
  const manifestPath=path.join(temp,'mini-app/patches/manifest.json');
  const manifest=JSON.parse(fs.readFileSync(manifestPath));
  const base=path.join(temp,manifest.baseAsset), original=fs.readFileSync(base);
  fs.appendFileSync(base,'\n// altered');
  let result=spawnSync(process.execPath,[path.join(temp,'mini-app/build-patches.mjs')],{encoding:'utf8'});
  assert.notEqual(result.status,0);assert.match(result.stderr,/Base asset changed/);
  fs.writeFileSync(base,original);
  manifest.changes[0].before='function';fs.writeFileSync(manifestPath,JSON.stringify(manifest));
  result=spawnSync(process.execPath,[path.join(temp,'mini-app/build-patches.mjs')],{encoding:'utf8'});
  assert.notEqual(result.status,0);assert.match(result.stderr,/exactly once/);
 } finally {fs.rmSync(temp,{recursive:true,force:true});}
});

test('builder rejects a missing patch anchor',()=>fixture(({temp,build})=>{
 const manifestPath=path.join(temp,'mini-app/patches/manifest.json');
 const manifest=JSON.parse(fs.readFileSync(manifestPath));
 manifest.changes[0].before='THIS_ANCHOR_DOES_NOT_EXIST';
 fs.writeFileSync(manifestPath,JSON.stringify(manifest));
 const result=build();
 assert.notEqual(result.status,0);assert.match(result.stderr,/exactly once/);
}));

for(const count of [0,2]) test(`builder rejects ${count} application entrypoints`,()=>fixture(({build,index})=>{
 const html=fs.readFileSync(index,'utf8');
 const entrypoint=html.match(/<script[^>]+src="\/assets\/index-[^"]+\.js"[^>]*><\/script>/)[0];
 fs.writeFileSync(index,html.replace(entrypoint,count===0?'':entrypoint+entrypoint));
 const result=build();
 assert.notEqual(result.status,0);assert.match(result.stderr,/exactly one app bundle entrypoint/);
}));

test('check rejects stale generated JavaScript without silently rebuilding it',()=>fixture(({temp,build,index})=>{
 assert.equal(build().status,0);
 const asset=fs.readFileSync(index,'utf8').match(/src="(\/assets\/index-status-[^"]+\.js)"/)[1];
 const output=path.join(temp,'internal/web/static',asset);
 fs.appendFileSync(output,'\n// stale output');
 const result=build('--check');
 assert.notEqual(result.status,0);assert.match(result.stderr,/stale/);
 assert.ok(fs.readFileSync(output,'utf8').endsWith('// stale output'));
}));

test('check rejects stale HTML entrypoint and leaves it unchanged',()=>fixture(({temp,build,index})=>{
 assert.equal(build().status,0);
 const manifest=JSON.parse(fs.readFileSync(path.join(temp,'mini-app/patches/manifest.json')));
 const html=fs.readFileSync(index,'utf8').replace(/src="\/assets\/index-status-[^"]+\.js"/,`src="/assets/${path.basename(manifest.baseAsset)}"`);
 fs.writeFileSync(index,html);
 const result=build('--check');
 assert.notEqual(result.status,0);assert.match(result.stderr,/stale/);
 assert.equal(fs.readFileSync(index,'utf8'),html);
}));

test('building twice is byte-for-byte reproducible',()=>fixture(({temp,build,index})=>{
 let result=build();assert.equal(result.status,0,result.stderr);
 const firstHTML=fs.readFileSync(index,'utf8');
 const refs=[...firstHTML.matchAll(/(?:src|href)="(\/assets\/[^\"]+)"/g)].map(match=>match[1]);
 const contents=refs.map(ref=>fs.readFileSync(path.join(temp,'internal/web/static',ref)));
 result=build();assert.equal(result.status,0,result.stderr);
 assert.equal(fs.readFileSync(index,'utf8'),firstHTML);
 refs.forEach((ref,i)=>assert.deepEqual(fs.readFileSync(path.join(temp,'internal/web/static',ref)),contents[i]));
 result=build('--check');assert.equal(result.status,0,result.stderr);
}));

test('check rejects stale generated CSS without silently rebuilding it',()=>fixture(({temp,build,index})=>{
 assert.equal(build().status,0);
 const ref=fs.readFileSync(index,'utf8').match(/href="(\/assets\/blue-theme-[^"]+\.css)"/)[1];
 const output=path.join(temp,'internal/web/static',ref);
 fs.appendFileSync(output,'\n/* stale stylesheet */');
 const result=build('--check');
 assert.notEqual(result.status,0);assert.match(result.stderr,/stylesheet is stale/);
 assert.ok(fs.readFileSync(output,'utf8').endsWith('/* stale stylesheet */'));
}));

test('builder rejects duplicate generated CSS entrypoints',()=>fixture(({build,index})=>{
 const html=fs.readFileSync(index,'utf8');
 const link=html.match(/<link[^>]+href="\/assets\/blue-theme-[^"]+\.css"[^>]*>/)[0];
 fs.writeFileSync(index,html.replace(link,link+link));
 const result=build();
 assert.notEqual(result.status,0);assert.match(result.stderr,/at most one generated style entrypoint/);
}));

test('check detects missing blue CSS reference, then explicit build repairs it',()=>fixture(({build,index})=>{
 const html=fs.readFileSync(index,'utf8').replace(/<link[^>]+href="\/assets\/blue-theme-[^"]+\.css"[^>]*>/,'');
 fs.writeFileSync(index,html);
 const result=build('--check');
 assert.notEqual(result.status,0);assert.match(result.stderr,/stale/);
 assert.equal(fs.readFileSync(index,'utf8'),html);
 assert.equal(build().status,0);
 assert.equal(build('--check').status,0);
 assert.equal([...fs.readFileSync(index,'utf8').matchAll(/href="\/assets\/blue-theme-[^"]+\.css"/g)].length,1);
}));
