import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
const source=name=>fs.readFileSync(new URL(name,import.meta.url),'utf8');

test('rating uses successful/all with the existing minimum-five scope',()=>{
 const ctx=vm.createContext({});vm.runInContext(source('rating-metric.js'),ctx);
 for(const [data,expected] of [[{requests:100,successful:40},'40%'],[{requests:5,successful:0},'0%'],[{requests:6,successful:1},'16,7%'],[{requests:5,successful:5},'100%']]) assert.equal(ctx.szSuccessRate(data),expected);
 for(const data of [{requests:0},{requests:4,successful:1},{requests:5,successful:6},{requests:5,successful:-1},{requests:'bad'},{requests:6.5,successful:2}]) assert.equal(ctx.szSuccessRate(data),null);
});

test('bot navigation uses Telegram SDK only on a normal user click with initData',()=>{
 const calls=[];const app={initData:'fixture',openTelegramLink:url=>calls.push(url),close:()=>calls.push('close')};
 const ctx=vm.createContext({window:{Telegram:{WebApp:app}}});vm.runInContext(source('bot-navigation.js'),ctx);
 let prevented=0;const event={button:0,preventDefault(){prevented++;}};
 ctx.szOpenBotChat(event);assert.deepEqual(calls,['https://t.me/Infozaputbot','close']);assert.equal(prevented,1);
 for(const props of [{ctrlKey:true},{metaKey:true},{shiftKey:true},{altKey:true},{button:1},{defaultPrevented:true}])ctx.szOpenBotChat({...event,...props});
 assert.equal(calls.length,2);assert.equal(prevented,1);
 app.openTelegramLink=()=>{throw Error('unsupported');};ctx.szOpenBotChat(event);assert.equal(prevented,1);
 app.initData='';ctx.szOpenBotChat(event);assert.equal(prevented,1);
 delete ctx.window.Telegram;ctx.szOpenBotChat(event);assert.equal(prevented,1);
 ctx.window.Telegram={WebApp:{initData:'fixture',openTelegramLink(){},close(){throw Error('unavailable');}}};ctx.szOpenBotChat(event);assert.equal(prevented,2);
});

function renderList({rows,filter='all',ready=true,error=false}) {
 let selected;
 const ctx=vm.createContext({
  go:select=>select({requests:rows}),
  _:{useState:()=>[filter,value=>{selected=value;}],useMemo:fn=>fn(),useContext:()=>({ready,error})},
  SmartSyncContext:{},sz:(type,props,...children)=>({type,props,children}),
  Oo:'Heading',Fs:'Empty',Si:'Icon',xs:'Card',H:(...v)=>v.join(' '),_o:row=>row.status,
  Ls:[{id:'all',label:'Усі'},{id:'pending',label:'Очікують'},{id:'replied',label:'Відповіді'},{id:'overdue',label:'Прострочені'},{id:'refused',label:'Відмови'},{id:'partial',label:'Частково'}],
 });
 vm.runInContext(source('blue-request-list.js'),ctx);
 return {view:ctx.Rs(),selected:()=>selected,ctx};
}
test('empty category offers reset without saying the account has no requests',()=>{
 const result=renderList({rows:[{status:'replied',id:'reply'}],filter:'pending'});
 const empty=result.view.children.find(node=>node?.type==='Empty');
 assert.equal(empty.props.title,'У цій категорії запитів немає');
 assert.ok(!empty.props.hint.includes('Створіть'));
 assert.equal(empty.props.action.children[0],'Показати всі запити');
 empty.props.action.props.onClick();assert.equal(result.selected(),'all');
});
test('initial empty account, pending load, and failed load remain distinct',()=>{
 for(const [settings,title] of [[{},'Запитів немає'],[{ready:false},'Завантаження…'],[{error:true},'Не вдалося завантажити запити']]) {
  const {view}=renderList({rows:[],...settings});const empty=view.children.find(node=>node?.type==='Empty');
  assert.equal(empty.props.title,title);assert.equal(empty.props.action,undefined);
 }
});
test('routine repaired views/helpers are served and analytics typo is gone',()=>{
 const html=fs.readFileSync(new URL('../../internal/web/static/index.html',import.meta.url),'utf8');
 const filename=html.match(/src="\/assets\/(index-status-[a-f0-9]+\.js)"/)[1];
 const bundle=fs.readFileSync(new URL('../../internal/web/static/assets/'+filename,import.meta.url),'utf8');
 for(const name of ['blue-request-list.js','rating-metric.js','bot-navigation.js'])assert.equal(bundle.split(source(name)).length,2);
 assert.ok(bundle.includes('Тільки автовідповідь'));assert.ok(!bundle.includes('автоавтовідповідь'));
 assert.ok(bundle.includes('Успішність: '));assert.ok(bundle.includes('Відкрити чат бота'));
 assert.ok(!bundle.includes('Продовжити у боті'));
});
