import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {runInNewContext} from 'node:vm';

for (const failure of ['expired', 'timeout', 'overview']) {
 test(`初始化${failure}明确显示未连接，禁止未知状态控制且不自动重试`, async () => {
  const nodes=new Map();
  const get=(id)=>{
   if (!nodes.has(id)) nodes.set(id,{textContent:'机器人状态读取中',disabled:false,value:'',classList:{toggle(){}},addEventListener(){}});
   return nodes.get(id);
  };
  let calls=0;
  const fetch=async (url,input)=>{
   calls++;
   assert.ok(input.signal,'初始化请求也必须有超时');
   if (failure==='timeout') {const error=new Error('timed out');error.name='TimeoutError';throw error;}
   const ok=failure==='overview' && url==='/session';
   return {ok,status:ok?200:401,json:async()=>ok?{csrf:'test'}:{error:{code:'AUTH',message:'请刷新页面'}}};
  };
  const context={document:{getElementById:get,querySelectorAll:()=>[]},window:{},fetch,AbortSignal,URLSearchParams,location:{hash:''},history:{}};
  const source=await readFile('web/app.js','utf8');
  await runInNewContext('(async()=>{'+source+'})()',context);
  assert.match(get('robot-state').textContent,/未连接/);
  assert.doesNotMatch(get('robot-state').textContent,/读取中/);
  assert.match(get('notice').textContent,/重新连接/);
  for (const id of ['robot-start','robot-stop','robot-pause','robot-resume','service-shutdown']) assert.equal(get(id).disabled,true);
  assert.equal(calls,failure==='overview'?4:1,'禁止无效会话自动轮询');
 });
}

async function frontend(fetch,timers) {
 const nodes=new Map();
 const node=()=>({textContent:'',value:'',disabled:false,children:[],dataset:{},listeners:{},classList:{toggle(){}},addEventListener(name,fn){this.listeners[name]=fn;},append(...items){this.children.push(...items);},replaceChildren(){this.children=[];}});
 const get=id=>{if(!nodes.has(id)) {const value=node();value.id=id;nodes.set(id,value);}return nodes.get(id);};
 const context={document:{getElementById:get,createElement:node,querySelectorAll:()=>[]},window:{},fetch,AbortSignal,URLSearchParams,TypeError,location:{hash:''},history:{}};
 if(timers) Object.assign(context,timers);
 const source=await readFile('web/app.js','utf8');
 const app=await runInNewContext('(async()=>{'+source+';return {api};})()',context);
 return {app,get};
}
const overview={robot:{enabled:true,status:'running'},permissions:{enabled:false,allowedVideos:[],allowedUsers:[],blockedUsers:[]},persona:'人设',events:[],notes:[],drafts:[]};
const response=(value,status=200)=>({ok:status===200,status,json:async()=>JSON.parse(JSON.stringify(value))});

test('旧页面遇到明确401只更新一次CSRF，业务只执行一次',async()=>{
 const calls=[];let sessions=0,executed=0;
 const fetch=async(url,input)=>{
  calls.push(url);
  if(url==='/session') return response({csrf:++sessions===1?'old':'new'});
  if(url==='/api/overview') return response(overview);
  if(url==='/api/send') {
   if(input.headers['X-CSRF-Token']==='old') return response({error:{code:'AUTH',message:'csrf expired'}},401);
   executed++;return response({status:'submitted'});
  }
  throw new Error('unexpected request');
 };
 const {app}=await frontend(fetch);
 const result=await app.api('send',{id:'approved-draft'});
 assert.equal(result.status,'submitted');
 assert.equal(executed,1);
 assert.equal(sessions,2);
 assert.equal(calls.filter(url=>url==='/api/send').length,2);
});

test('发送请求断线结果不明时，禁止自动重发',async()=>{
 let sends=0;
 const fetch=async url=>{
  if(url==='/session') return response({csrf:'valid'});
  if(url==='/api/overview') return response(overview);
  sends++;throw new TypeError('connection lost after submission');
 };
 const {app,get}=await frontend(fetch);
 await assert.rejects(app.api('private-send',{requestId:'approved-request'}),/结果不明/);
 assert.equal(sends,1);
 assert.match(get('robot-state').textContent,/未连接/);
});

for(const recovers of [true,false]) {
 test(`短暂断线${recovers?'恢复':'最多四次退避后停止'}，不轮询业务或重放动作`,async()=>{
  const queue=[];const delays=[];let down=true,calls=0;
  const fetch=async url=>{
   calls++;
   if(down) throw new TypeError('offline');
   return response(url==='/session'?{csrf:'restored'}:overview);
  };
  const timers={setTimeout(fn,delay){queue.push(fn);delays.push(delay);return queue.length;},clearTimeout(){}};
  const {get}=await frontend(fetch,timers);
  if(recovers) down=false;
  while(queue.length) await queue.shift()();
  if(recovers) {
   assert.match(get('robot-state').textContent,/运行中/);
   assert.equal(calls,3);
   assert.deepEqual(delays,[1000,4000]);
   assert.equal(get("notice").hidden,true);
  } else {
   assert.equal(calls,5);
   assert.deepEqual(delays,[1000,2000,4000,8000]);
   assert.match(get('mode').textContent,/等待服务恢复/);
  }
 });
}


test('角色日记在测试模式允许重复，关闭测试模式恢复按钮保护',async()=>{
 let generations=0,testingMode=true;
 const record=()=>({date:'2026-10-31',status:'completed',title:'今天的一点心情',body:'安静地把好奇留给明天。',sourceCount:0,testing:true});
 const fetch=async (url,input)=>{
  if(url==='/session')return response({csrf:'valid'});
  if(url==='/api/overview')return response(overview);
  if(url==='/api/diary-generate'){generations++;return response(record());}
  if(url==='/api/diary-config'){const cfg=JSON.parse(input.body);assert.equal(cfg.ownerWeight,4);assert.equal(cfg.interestWeight,2);assert.deepEqual(cfg.topics,['狗','植物']);testingMode=cfg.testing;}
  return response({enabled:true,testing:testingMode,todayStatus:generations?'completed':'',entries:generations?[record()]:[]});
 };
 const {get}=await frontend(fetch);
 await get('diary-refresh').listeners.click();
 await get('diary-write').listeners.click();
 assert.match(get('diary-write').textContent,/再写/);
 assert.equal(get('diary-write').disabled,false);
 await get('diary-write').listeners.click();
 assert.equal(generations,2);
 get('diary-enabled').checked=true;get('diary-testing').checked=false;get('diary-topics').value='狗、植物';
 await get('diary-save').listeners.click();
 assert.equal(get('diary-write').disabled,true);
});

test('角色成长页面按需显示私聊兴趣和关系，不触发模型或平台动作',async()=>{
 const actions=[];
 const fetch=async(url)=>{
  if(url==='/session')return response({csrf:'valid'});
  if(url==='/api/overview')return response(overview);
  actions.push(url);
  if(url==='/api/character-status')return response({description:'当前平静',traits:{warmth:50,curiosity:50,playfulness:50},changes:[],development:{interests:[{topic:'植物',visibility:'private',platform:'bilibili',conversation:'789',familiarity:2,preference:0}],relationships:[{platform:'bilibili',peer:'789',visibility:'private',label:'已绑定主人',score:1,days:1,interactions:1}]}});
  throw new Error('unexpected '+url);
 };
 const {get}=await frontend(fetch);
 await get('character-refresh').listeners.click();
 assert.deepEqual(actions,['/api/character-status']);
 assert.equal(get('development-interests').children.length,1);
 assert.equal(get('development-relationships').children.length,1);
});
