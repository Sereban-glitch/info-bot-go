// Execute the full production React/router bundle with synthetic APIs in jsdom.
// This checks runtime/DOM behavior only; it is not a Chromium or visual-layout test.
// Install jsdom into an external temporary package, then set SMARTZAPYT_QA_NODE_MODULES
// to that package's node_modules directory. No application dependencies are changed.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import {createRequire} from 'node:module';
import {fileURLToPath} from 'node:url';
const require=createRequire(process.env.SMARTZAPYT_QA_NODE_MODULES?path.join(process.env.SMARTZAPYT_QA_NODE_MODULES,'../package.json'):import.meta.url);
const {JSDOM,VirtualConsole}=require('jsdom');
const root=fileURLToPath(new URL('../../',import.meta.url));
const out=path.join(root,'mini-app/qa/results');fs.mkdirSync(out,{recursive:true});
const html=fs.readFileSync(path.join(root,'internal/web/static/index.html'),'utf8');
const asset=html.match(/src="\/assets\/(index-status-[a-f0-9]+\.js)"/)[1];
const bundle=fs.readFileSync(path.join(root,'internal/web/static/assets',asset),'utf8');
const rows=[
 ['success','replied','successful',true],['refusal','replied','rejected',true],
 ['partial','replied','partially_successful',true],['unrated','replied','waiting_classification',true],
 ['pending','pending','waiting_classification',false],['bounced','bounced','error_message',true],
 ['unknown','replied','future_status',true],
].map(([label,status,lastStatus,replied],index)=>({
 id:String(index),subject:'Fixture '+label,recipientName:'Authority '+label,date:'2099-01-01T10:00:00Z',status,lastStatus,
 replyReceivedAt:replied?'2099-01-02T10:00:00Z':'',delivered:true,channel:'dostup',
 responseExcerpt:'Fixture response <img src=x onerror=alert(1)>',url:'https://dostup.org.ua/request/fixture_'+index,
}));
const api={
 '/api/requests':rows,'/api/me':{firstName:'Тест'},
 '/api/portal-stats':{requests:100,successful:40,organs:20},
 '/api/rating':{items:[{name:'Fixture authority',requests:100,successful:40,overdue:7,slug:'fixture'}]},
 '/api/analytics':{total:7,answered:5,ackOnly:1,awaiting:1,bounced:1},
};
const report={passed:false,engine:'jsdom',visualValidation:false,bundle:asset,checks:[],errors:[],requests:[],expectedTransportErrors:0};
let requestFailure=false;
const delay=ms=>new Promise(resolve=>setTimeout(resolve,ms));
async function waitFor(predicate,label){for(let i=0;i<500;i++){if(predicate())return;await delay(10);}throw Error('Timed out: '+label);}
const console=new VirtualConsole();console.on('jsdomError',err=>report.errors.push(err.stack||String(err)));console.on('error',(...args)=>{
 if(requestFailure&&String(args[0])==='syncRequests error:') report.expectedTransportErrors++;
 else report.errors.push(args.map(String).join(' '));
});
const dom=new JSDOM(html.replace(/<script\b[^>]*>[\s\S]*?<\/script>/g,'').replace(/<link\b[^>]*>/g,''),{url:'https://smartzapyt.test/',runScripts:'outside-only',pretendToBeVisual:true,virtualConsole:console});
const {window}=dom,document=window.document;
window.scrollTo=()=>{};
window.matchMedia=query=>({matches:false,media:query,addListener(){},removeListener(){},addEventListener(){},removeEventListener(){},dispatchEvent(){return true;}});
window.Request=Request;window.Response=Response;window.Headers=Headers;
window.structuredClone=structuredClone;
window.fetch=async(input,options={})=>{
 const url=new URL(String(input),window.location.href),method=options.method||'GET';
 report.requests.push({path:url.pathname,method});
 assert.equal(url.origin,'https://smartzapyt.test','no external network is permitted');
 assert.equal(method,'GET','fixture test is read-only');
 assert.ok(Object.hasOwn(api,url.pathname),'unexpected API '+url.pathname);
 if(url.pathname==='/api/requests'&&requestFailure) return new Response('{}',{status:500});
 return new Response(JSON.stringify({ok:true,data:api[url.pathname]}),{status:200,headers:{'Content-Type':'application/json'}});
};
window.addEventListener('error',event=>report.errors.push(String(event.error||event.message)));
window.addEventListener('unhandledrejection',event=>report.errors.push(String(event.reason)));
const body=()=>document.querySelector('.sz-main')?.textContent||'';
const click=element=>{assert.ok(element,'click target missing');element.dispatchEvent(new window.MouseEvent('click',{bubbles:true,cancelable:true,button:0}));};
const button=label=>[...document.querySelectorAll('button')].find(el=>el.textContent.trim()===label);
const nav=href=>document.querySelector('.sz-sidebar a[href="'+href+'"]');
async function initialFailure(pathname) {
 let failing=true;
 const errors=[],virtualConsole=new VirtualConsole();
 virtualConsole.on('jsdomError',error=>errors.push(String(error)));
 virtualConsole.on('error',(...args)=>{
  if(failing&&String(args[0])==='syncRequests error:') report.expectedTransportErrors++;
  else errors.push(args.map(String).join(' '));
 });
 const instance=new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>',{
  url:'https://smartzapyt.test'+pathname,runScripts:'outside-only',pretendToBeVisual:true,virtualConsole,
 });
 const w=instance.window,d=w.document;
 w.scrollTo=()=>{};w.matchMedia=window.matchMedia;
 w.Request=Request;w.Response=Response;w.Headers=Headers;w.structuredClone=structuredClone;
 w.fetch=async(input,options={})=>{
  const url=new URL(String(input),w.location.href);
  assert.equal(url.origin,'https://smartzapyt.test');assert.equal(options.method||'GET','GET');
  assert.ok(Object.hasOwn(api,url.pathname),'unexpected initial-state API '+url.pathname);
  if(url.pathname==='/api/requests'&&failing) return new Response('{}',{status:500});
  return new Response(JSON.stringify({ok:true,data:url.pathname==='/api/requests'?[]:api[url.pathname]}),{status:200});
 };
 try {
  w.eval(bundle);
  await waitFor(()=>d.querySelector('.sz-sync-error'),'initial API failure '+pathname);
  if(pathname==='/') {
   assert.ok(d.querySelector('.sz-empty-hero').textContent.includes('Не вдалося завантажити запити'));
   assert.ok(!d.querySelector('.sz-home').textContent.includes('Ваша інформація починається із запиту'));
  } else {
   assert.equal(d.querySelector('.sz-detail h1').textContent,'Не вдалося завантажити запит');
  }
  failing=false;
  d.querySelector('.sz-sync-error button').dispatchEvent(new w.MouseEvent('click',{bubbles:true,cancelable:true,button:0}));
  await waitFor(()=>!d.querySelector('.sz-sync-error'),'initial failure retry '+pathname);
  if(pathname==='/') assert.ok(d.querySelector('.sz-empty-hero').textContent.includes('Ваша інформація починається із запиту'));
  else assert.equal(d.querySelector('.sz-detail h1').textContent,'Запит недоступний');
  assert.deepEqual(errors,[]);
 } finally {instance.window.close();}
}
try {
 window.eval(bundle);
 await waitFor(()=>document.querySelector('.sz-home .sz-latest-event'),'home with synchronized requests');
 assert.ok(document.querySelector('.sz-summary').textContent.includes('5Відповідей'));
 assert.ok(!document.querySelector('.sz-latest-event').textContent.includes('Fixture bounced'));
 report.checks.push('full production React bundle mounts overview and loads API fixtures');
 click(nav('/requests'));
 await waitFor(()=>window.location.pathname==='/requests'&&document.querySelectorAll('.sz-request-card').length===7,'requests navigation');
 for(const [label,expected] of [['Відмови',['refusal']],['Частково',['partial']],['Відповіді',['success','refusal','partial','unrated','unknown']],['Очікують',['pending']]]) {
  click(button(label));await waitFor(()=>document.querySelectorAll('.sz-request-card').length===expected.length,label+' filter');
  const actual=[...document.querySelectorAll('.sz-request-subject')].map(el=>el.textContent.replace('Fixture ',''));
  assert.deepEqual(actual.sort(),expected.sort());
 }
 click(button('Усі'));await waitFor(()=>document.querySelectorAll('.sz-request-card').length===7,'all filter');
 report.checks.push('all status filters execute in mounted React UI');
 click([...document.querySelectorAll('.sz-request-card')].find(el=>el.textContent.includes('Fixture refusal')));
 await waitFor(()=>window.location.pathname==='/requests/1'&&document.querySelector('.sz-response-excerpt'),'request detail navigation');
 assert.equal(document.querySelector('.sz-response-excerpt').textContent,rows[1].responseExcerpt);
 assert.equal(document.querySelector('.sz-response-excerpt img'),null);
 assert.ok(document.querySelector('.sz-timeline').textContent.includes('Відповідь отримано'));
 assert.equal(document.querySelector('.sz-source-link').getAttribute('href'),rows[1].url);
 report.checks.push('live detail route renders literal response excerpt and recorded timeline');
 click(document.querySelector('.sz-back-link'));
 await waitFor(()=>window.location.pathname==='/requests'&&document.querySelectorAll('.sz-request-card').length===7,'detail back link');
 click([...document.querySelectorAll('.sz-request-card')].find(el=>el.textContent.includes('Fixture bounced')));
 await waitFor(()=>window.location.pathname==='/requests/5'&&document.querySelector('.sz-timeline'),'bounce detail');
 assert.equal(document.querySelector('.sz-response-excerpt'),null);
 assert.ok(!document.querySelector('.sz-timeline').textContent.includes('Відповідь отримано'));
 report.checks.push('bounce detail does not manufacture a reply');
 click(nav('/rating'));
 await waitFor(()=>body().includes('Fixture authority'),'rating data');
 assert.ok(body().includes('Успішних: 40'));assert.ok(body().includes('За даними порталу'));
 click(nav('/analytics'));
 await waitFor(()=>body().includes('Зведені дані журналу'),'analytics scope');
 click(nav('/'));
 await waitFor(()=>document.querySelector('.sz-home'),'return to overview');
 report.checks.push('rating, analytics and overview routing remains functional');

 const complex={...rows[0],id:'<fixture+tag/part%25@example.org>',subject:'Fixture complex ID'};
 api['/api/requests']=[complex];
 document.dispatchEvent(new window.Event('visibilitychange'));
 await waitFor(()=>document.querySelector('.sz-request-card')?.textContent.includes(complex.subject),'complex ID refresh');
 click(document.querySelector('.sz-request-card'));
 await waitFor(()=>document.querySelector('.sz-detail h1')?.textContent===complex.subject,'complex email-style ID detail route');
 assert.equal(document.querySelector('.sz-response-excerpt').textContent,complex.responseExcerpt);
 report.checks.push('email-style ID with slash, plus, percent and angle brackets routes to the exact request');

 requestFailure=true;
 document.dispatchEvent(new window.Event('visibilitychange'));
 await waitFor(()=>document.querySelector('.sz-sync-error'),'failed refresh banner');
 assert.equal(document.querySelector('.sz-detail h1').textContent,complex.subject);
 assert.ok(document.querySelector('.sz-sync-error').textContent.includes('Попередні дані'));
 requestFailure=false;api['/api/requests']=[];
 click(document.querySelector('.sz-sync-error button'));
 await waitFor(()=>!document.querySelector('.sz-sync-error')&&document.querySelector('.sz-detail h1')?.textContent==='Запит недоступний','retry with valid empty response');
 click(nav('/'));
 await waitFor(()=>body().includes('Ваша інформація починається із запиту.'),'successful empty overview');
 assert.ok(!body().includes('Не вдалося завантажити запити'));
 report.checks.push('failed refresh preserves authenticated rows; retry accepts valid empty response');
 await initialFailure('/');await initialFailure('/requests/initial-fixture');
 report.checks.push('initial overview and deep-link failures differ from successful empty data and retry correctly');
 assert.deepEqual(report.errors,[]);
 report.passed=true;
 process.stdout.write('Full production bundle DOM fixtures passed ('+report.checks.length+' checks). Visual layout remains unverified.\n');
} catch(error) {
 report.error=error.stack||String(error);throw error;
} finally {
 fs.writeFileSync(path.join(out,'dom-report.json'),JSON.stringify(report,null,2));
 dom.window.close();
}
