import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

const manifest=JSON.parse(fs.readFileSync(new URL('manifest.json',import.meta.url)));
const base=fs.readFileSync(new URL('../../'+manifest.baseAsset,import.meta.url),'utf8');
const html=fs.readFileSync(new URL('../../internal/web/static/index.html',import.meta.url),'utf8');
const asset=html.match(/src="\/assets\/(index-status-[a-f0-9]+\.js)"/)[1];
const bundle=fs.readFileSync(new URL('../../internal/web/static/assets/'+asset,import.meta.url),'utf8');
function segment(source,start,end) {
 const begin=source.indexOf(start);assert.ok(begin>=0,`missing ${start}`);
 const finish=source.indexOf(end,begin);assert.ok(finish>begin,`missing ${end}`);
 return source.slice(begin,finish);
}
const transport=segment(bundle,'var vo=','var So=');

test('blue integration preserves original authentication, native base URL and API synchronization',()=>{
 assert.equal(transport,segment(base,'var vo=','var So='));
 assert.equal(segment(bundle,'async function Co()','var To='),segment(base,'async function Co()','var To='));
 assert.ok(bundle.includes('fetch(yo()+`/api/app/auth`,{method:`POST`,headers:{"Content-Type":`application/json`},body:JSON.stringify({code:n})})'));
 assert.ok(bundle.includes('localStorage.setItem(`smartzapyt_init_data`,e.initData)'));
});

test('vendor/runtime prefix remains unchanged',()=>{
 const boundary=base.indexOf('var po={');assert.ok(boundary>0);
 assert.ok(bundle.startsWith(base.slice(0,boundary)),'the original runtime prefix changed');
});

function context(options={}) {
 const events=[],calls=[];
 const ctx=vm.createContext({
  window:{location:{pathname:options.pathname||'/'},Telegram:options.telegram?{WebApp:{initData:options.telegram}}:undefined,Capacitor:{isNativePlatform:()=>Boolean(options.native)}},
  localStorage:{getItem:key=>key==='smartzapyt_init_data'?(options.saved||''):null},
  document:{dispatchEvent:event=>events.push(event.type)},
  CustomEvent:class {constructor(type){this.type=type;}},
  fetch:async(url,init)=>{calls.push({url,init});return {ok:options.status===undefined||options.status<400,status:options.status||200,json:async()=>({ok:true,data:{fixture:true}})};},
 });
 vm.runInContext(transport,ctx);return {ctx,events,calls};
}

test('Telegram credentials take priority over saved native credentials',()=>{
 assert.equal(context({telegram:'telegram-fixture',saved:'saved-fixture'}).ctx.bo(),'telegram-fixture');
 assert.equal(context({saved:'saved-fixture'}).ctx.bo(),'saved-fixture');
 assert.equal(context().ctx.bo(),'');
});

test('API helper keeps relative web, /prozoro and existing native destinations',async()=>{
 for(const [options,prefix] of [[{},''],[{pathname:'/prozoro/requests'},'/prozoro'],[{native:true},'https://35-209-212-217.sslip.io']]) {
  const {ctx,calls}=context({...options,telegram:'synthetic-init-data'});
  await ctx.xo('/api/requests',{headers:{'X-Fixture':'yes'}});
  assert.equal(calls[0].url,prefix+'/api/requests');
  assert.equal(calls[0].init.headers.Authorization,'tma synthetic-init-data');
  assert.equal(calls[0].init.headers['Content-Type'],'application/json');
  assert.equal(calls[0].init.headers['X-Fixture'],'yes');
 }
});

test('anonymous API request sends no authorization; 401 alone requests login',async()=>{
 const anonymous=context();await anonymous.ctx.xo('/api/rating');
 assert.ok(!Object.hasOwn(anonymous.calls[0].init.headers,'Authorization'));
 for(const status of [401,403,500]) {
  const {ctx,events}=context({status});
  await assert.rejects(()=>ctx.xo('/api/requests'),new RegExp(`HTTP ${status}`));
  assert.deepEqual(events,status===401?['require-app-login']:[]);
 }
});
