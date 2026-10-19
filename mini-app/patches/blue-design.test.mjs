import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

const source=name=>fs.readFileSync(new URL(name,import.meta.url),'utf8');
const viewFiles=['blue-shell.js','bot-navigation.js','response-preview.js','blue-home.js','blue-request-card.js','blue-request-detail.js'];
const received=new Set(['replied','unrated','refused','partial']);
const example={id:'fixture-id',subject:'Fixture request',recipientName:'Fixture authority',createdAt:'2099-01-01',deadline:'2099-01-08',status:'pending',channel:'dostup'};

function app(requests=[],{id='fixture-id',ready=true,error=false}={}) {
 const state={requests,profile:{firstName:'Тест'}};
 const ctx=vm.createContext({
  URL,Promise,console,
  F:{jsx:(type,props)=>({type,props})},
  _:{createContext:value=>({Provider:'ContextProvider',value}),useContext:()=>({ready,error,loading:!ready,retry(){}}),useState:initial=>[initial,()=>{}],useEffect:()=>{},useRef:initial=>({current:initial})},
  go:selector=>selector(state),pi:({select})=>select({location:{pathname:'/requests/'+id}}),
  Vr:'RouterLink',_s:'StatusBadge',Kr:path=>options=>({path,...options}),
  fo:(date,days)=>new Date(date.getTime()+days*86400000),
  co:date=>date,Ds:date=>date,lo:()=>5,U:()=> 'днів',
  Cs:rows=>rows.find(row=>['pending','overdue'].includes(ctx._o(row))),
  ws:rows=>rows.filter(row=>row.repliedAt&&received.has(ctx._o(row))).sort((a,b)=>new Date(b.repliedAt)-new Date(a.repliedAt))[0],
  po:Object.fromEntries(['replied','unrated','refused','partial','bounced','pending'].map(status=>[status,{label:status}])),
 });
 ctx.go.getState=()=>state;
 for(const name of ['request-status.js','request-summary.js',...viewFiles]) vm.runInContext(source(name),ctx,{filename:name});
 return ctx;
}
function nodes(node) {
 if(node===null||node===undefined||typeof node==='boolean') return [];
 if(Array.isArray(node)) return node.flatMap(nodes);
 if(typeof node!=='object') return [node];
 if(typeof node.type==='function') return nodes(node.type(node.props));
 return [node,...nodes(node.props?.children)];
}
const text=node=>nodes(node).filter(value=>typeof value==='string'||typeof value==='number').join(' ');
const byClass=(node,className)=>nodes(node).filter(value=>value?.props?.className?.split(' ').includes(className));

test('generated bundle includes each reviewed blue view source exactly once and all existing routes',()=>{
 const html=fs.readFileSync(new URL('../../internal/web/static/index.html',import.meta.url),'utf8');
 const asset=html.match(/src="\/assets\/(index-status-[a-f0-9]+\.js)"/)[1];
 const bundle=fs.readFileSync(new URL('../../internal/web/static/assets/'+asset,import.meta.url),'utf8');
 for(const name of viewFiles) assert.equal(bundle.split(source(name)).length,2,`${name} must be generated exactly once`);
 for(const route of ['IndexRoute','AnalyticsRoute','RatingRoute','RequestsIndexRoute']) assert.ok(bundle.includes(route+':'),`${route} missing`);
 assert.ok(bundle.includes('SmartDetailRoute.update('),'request detail route must join the live route tree');
});

test('request cards link to the exact request and preserve derived outcome',()=>{
 for(const [props,status] of [[{lastStatus:'rejected'},'refused'],[{lastStatus:'partially_successful'},'partial'],[{status:'replied'},'unrated'],[{status:'bounced',repliedAt:'2099-01-09'},'bounced']]) {
  const ctx=app(),req={...example,...props},card=ctx.xs({req});
  assert.equal(card.type,'RouterLink');
  assert.equal(card.props.to,'/requests/$requestId');
  assert.equal(card.props.params.requestId,example.id);
  assert.equal(card.props['data-status'],status);
  assert.ok(text(card).includes(example.subject));
  if(status==='bounced') assert.ok(!text(card).includes('2099-01-09'),'bounce timestamp cannot become answer date');
 }
});

test('detail uses authenticated store data, literal excerpts, and only recorded events',()=>{
 const excerpt='<img src=x onerror=alert(1)> Fixture response text';
 const ctx=app([{...example,status:'replied',lastStatus:'rejected',repliedAt:'2099-01-03',responseExcerpt:excerpt,url:'https://dostup.org.ua/request/fixture'}]);
 const detail=ctx.SmartRequestDetail(),all=nodes(detail);
 assert.equal(byClass(detail,'sz-response-excerpt')[0].props.children,excerpt);
 assert.ok(!all.some(node=>node?.props?.dangerouslySetInnerHTML));
 assert.ok(text(detail).includes('Відповідь отримано'));
 assert.ok(!text(detail).includes('Розрахунковий строк'));
 const link=byClass(detail,'sz-source-link')[0];
 assert.equal(link.props.href,'https://dostup.org.ua/request/fixture');
 assert.equal(link.props.rel,'noopener noreferrer');
 assert.ok(text(detail).includes('Це не AI-аналіз'));
});

test('pending and bounced details never manufacture a received answer or excerpt',()=>{
 for(const status of ['pending','bounced']) {
  const ctx=app([{...example,status,repliedAt:status==='bounced'?'2099-01-04':undefined,responseExcerpt:'Must not appear',url:'https://evil.example/request'}]);
  const detail=ctx.SmartRequestDetail(),body=text(detail);
  assert.equal(byClass(detail,'sz-response-excerpt').length,0);
  assert.equal(byClass(detail,'sz-source-link').length,0);
  assert.ok(!body.includes('Відповідь отримано'));
  assert.ok(!body.includes('Must not appear'));
  if(status==='pending') assert.ok(body.includes('Розрахунковий строк'));
  else assert.ok(body.includes('Не доставлено'));
 }
});

test('missing and still-loading request details have honest distinct empty states',()=>{
 assert.ok(text(app([], {ready:true}).SmartRequestDetail()).includes('Запит недоступний'));
 assert.ok(text(app([], {ready:false}).SmartRequestDetail()).includes('Завантаження запиту'));
 const ctx=app([{...example,id:'other'}]);
 assert.ok(!text(ctx.SmartRequestDetail()).includes(example.subject));
 assert.ok(text(app([], {id:'%broken'}).SmartRequestDetail()).includes('Запит недоступний'));
});

test('source URLs allow only HTTPS on the two portal hosts without credentials',()=>{
 const ctx=app();
 for(const url of ['https://dostup.org.ua/request/fixture','https://dostup.pravda.com.ua/request/fixture']) assert.equal(ctx.szPortalURL(url),url);
 for(const url of [undefined,'','/request/fixture','http://dostup.org.ua/request','javascript:alert(1)','https://dostup.org.ua.evil.example/request','https://evil.example/dostup.org.ua','https://user:password@dostup.org.ua/request']) assert.equal(ctx.szPortalURL(url),null,String(url));
});

test('overview counts refusals as answers and excludes bounce from the latest answer',()=>{
 const ctx=app([
  {...example,id:'reply',status:'replied',lastStatus:'rejected',repliedAt:'2099-01-03',subject:'Actual reply'},
  {...example,id:'bounce',status:'bounced',repliedAt:'2099-01-04',subject:'Bounce event'},
 ]);
 const home=ctx.Es();
 assert.ok(text(byClass(home,'sz-summary')[0]).includes('2 Надіслано 1 Відповідей 0 Очікують'));
 assert.ok(text(byClass(home,'sz-latest-event')[0]).includes('Actual reply'));
 assert.ok(!text(byClass(home,'sz-latest-event')[0]).includes('Bounce event'));
 assert.equal(byClass(home,'sz-next-action')[0].props.params.requestId,'reply');
});

test('bounce-only and undated-receipt overviews use real requests instead of empty onboarding',()=>{
 const bounced=app([{...example,status:'bounced',repliedAt:'2099-01-20'}]).Es();
 const bounceHero=byClass(bounced,'sz-next-action')[0];
 assert.ok(text(bounceHero).includes('Перевірити доставку'));
 assert.ok(text(bounceHero).includes(example.createdAt));
 assert.ok(!text(bounceHero).includes('2099-01-20'));
 assert.equal(byClass(bounced,'sz-calculation-note').length,0);
 const reply=app([{...example,status:'replied'}]).Es();
 const replyHero=byClass(reply,'sz-next-action')[0];
 assert.ok(text(replyHero).includes('Перевірити відповідь'));
 assert.ok(text(replyHero).includes('Дату не вказано'));
 assert.ok(!text(reply).includes('Ваша інформація починається із запиту'));
});

test('sync failures are distinct from successful empty data and preserve existing rows',()=>{
 const empty=text(app([]).Es()),failed=text(app([],{error:true}).Es());
 assert.ok(empty.includes('Ваша інформація починається із запиту'));
 assert.ok(failed.includes('Не вдалося завантажити запити'));
 assert.ok(!failed.includes('Ваша інформація починається із запиту'));
 assert.ok(text(app([],{error:true}).SmartRequestDetail()).includes('Не вдалося завантажити запит'));
 assert.ok(text(app([example],{error:true}).Es()).includes(example.subject));
});

test('sync helper recognizes valid empty replacement and an unchanged failed fetch',async()=>{
 const ctx=app([example]);
 ctx.Co=async()=>{};
 assert.equal(await ctx.szSyncRequests(),false);
 assert.equal(ctx.go.getState().requests.length,1);
 ctx.Co=async()=>{ctx.go.getState().requests=[];};
 assert.equal(await ctx.szSyncRequests(),true);
 assert.equal(ctx.go.getState().requests.length,0);
});

test('encoded email-style request IDs resolve to their exact authenticated row',()=>{
 const id='<fixture+tag/part%25@example.org>';
 const ctx=app([{...example,id}],{id:encodeURIComponent(id)});
 assert.ok(text(ctx.SmartRequestDetail()).includes(example.subject));
 assert.equal(ctx.xs({req:{...example,id}}).props.params.requestId,id);
});


test('response preview renders literal CSV cells without guessed headers and preserves raw',()=>{
 const csv='name;value\n"<img src=x onerror=alert(1)>";"2"\ntruncated;…';
 const detail=app([{...example,status:'replied',responseExcerpt:csv}]).SmartRequestDetail();
 const all=nodes(detail);
 assert.equal(all.filter(node=>node?.type==='th').length,0);
 assert.equal(all.filter(node=>node?.type==='td').length,4);
 assert.ok(all.some(node=>node?.type==='td'&&node.props.children==='<img src=x onerror=alert(1)>'));
 assert.ok(!all.some(node=>node?.type==='img'||node?.props?.dangerouslySetInnerHTML));
 assert.ok(text(byClass(detail,'sz-response-tail')[0]).includes('truncated;…'));
 const raw=byClass(detail,'sz-response-raw')[0];
 assert.ok(nodes(raw).some(node=>node?.type==='pre'&&node.props.children===csv));
});
test('flattened CSV remains a sequence of fields with a visible limitation',()=>{
 const raw='date;name;email;2026-10-01;[REDACTED];broken�';
 const detail=app([{...example,status:'replied',responseExcerpt:raw}]).SmartRequestDetail();
 assert.equal(nodes(detail).filter(node=>node?.type==='table').length,0);
 assert.equal(byClass(detail,'sz-response-fields').length,1);
 assert.ok(text(detail).includes('Межі рядків не збережено'));
 assert.ok(text(detail).includes('пошкоджені символи'));
 assert.ok(text(detail).includes('[REDACTED]'));
});
