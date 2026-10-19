import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import {fileURLToPath} from 'node:url';
import {spawnSync} from 'node:child_process';
const root=fileURLToPath(new URL('../../',import.meta.url));
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
