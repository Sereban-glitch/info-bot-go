import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
const ctx=vm.createContext({fo: (d, days) => new Date(d.getTime()+days*86400000)});
for (const file of ['request-status.js','request-summary.js']) vm.runInContext(fs.readFileSync(new URL(file,import.meta.url),'utf8'),ctx);
const base={status:'pending',createdAt:'2099-01-01'};
const cases=[
 ['pending',{},'pending'], ['expired',{createdAt:'2000-01-01'},'overdue'],
 ['refusal',{lastStatus:'rejected',status:'replied'},'refused'],
 ['partial',{lastStatus:'partially_successful',status:'replied'},'partial'],
 ['success',{lastStatus:'successful',status:'replied'},'replied'],
 ['unclassified reply',{lastStatus:'waiting_classification',repliedAt:'2026-01-01'},'unrated'],
 ['classification flag without reply',{lastStatus:'waiting_classification'},'pending'],
 ['unknown reply',{lastStatus:'new_unknown_status',status:'replied'},'unrated'],
 ['plain email reply',{status:'replied'},'unrated'],
 ['bounced legacy timestamp',{status:'bounced',repliedAt:'2026-01-01'},'bounced'],
 ['portal error with timestamp',{lastStatus:'error_message',repliedAt:'2026-01-01'},'bounced'],
];
for (const [name,props,expected] of cases) test(name,()=>assert.equal(ctx._o({...base,...props}),expected));
test('all reply outcomes count as received answers, not successes',()=>{
 const rows=cases.map(([,props])=>({...base,...props})); const summary=ctx.Ss(rows);
 assert.equal(summary.answered,6); assert.equal(summary.refused,1); assert.equal(summary.partial,1);
 assert.equal(summary.total,11); assert.equal(summary.replied+summary.pending+summary.overdue+summary.refused+summary.partial+summary.bounced,11);
});

test('generated bundle executes the reviewed source, badges, filters and latest answer selector',()=>{
 const html=fs.readFileSync(new URL('../../internal/web/static/index.html',import.meta.url),'utf8');
 const asset=html.match(/src="\/assets\/([^\"]+)"/)[1];
 const bundle=fs.readFileSync(new URL('../../internal/web/static/assets/'+asset,import.meta.url),'utf8');
 const actual=vm.createContext({fo:ctx.fo});
 for (const [start,end] of [['function _o(request)','var vo='],['function Ss(requests)','function Cs('],['function ws(e)','var Ts=']]) {
  const begin=bundle.lastIndexOf(start); assert.ok(begin>=0,start);
  vm.runInContext(bundle.slice(begin,bundle.indexOf(end,begin)),actual);
 }
 const badgeStart=bundle.indexOf('var po={');
 vm.runInContext(bundle.slice(badgeStart,bundle.indexOf(',mo=',badgeStart))+';',actual);
 for (const [,props,expected] of cases) {
  assert.equal(actual._o({...base,...props}),expected);
  assert.ok(actual.po[expected]?.label,expected+' badge missing');
 }
 const rows=cases.map(([,props])=>({...base,...props}));
 assert.equal(actual.Ss(rows).answered,6);
 const newerBounce={status:'bounced',repliedAt:'2026-09-01'};
 const olderReply={status:'replied',lastStatus:'rejected',repliedAt:'2026-08-01'};
 assert.equal(actual.ws([newerBounce,olderReply]),olderReply);
 const filterAnchor='e.filter(e=>t===`replied`?';
 const start=bundle.indexOf(filterAnchor);assert.ok(start>0);
 const expression=bundle.slice(start,bundle.indexOf(',[e,t]',start));
 actual.e=rows;actual.t='replied';
 assert.equal(vm.runInContext(expression,actual).length,6);
 actual.t='refused';assert.equal(vm.runInContext(expression,actual).length,1);
 actual.t='partial';assert.equal(vm.runInContext(expression,actual).length,1);
 actual.t='pending';assert.equal(vm.runInContext(expression,actual).length,2);
 assert.ok(bundle.includes('Відповіді без відмов'));
 assert.ok(bundle.includes('щонайменше 5 запитами'));
 assert.ok(bundle.includes('Успішних: '));
 assert.ok(!bundle.includes('Хто відповідає вчасно'));
});
