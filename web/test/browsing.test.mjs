import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {runInNewContext} from 'node:vm';

test('浏览偏好默认关闭且独立保存；记录默认缩略、单条可展开并能切换详细',async()=>{
 const nodes=new Map(),created=[],requests=[];
 const node=(dataset={})=>({value:'',textContent:'',hidden:false,disabled:false,checked:false,dataset,listeners:{},children:[],classList:{toggle(){}},append(...v){this.children.push(...v)},replaceChildren(){this.children=[]},addEventListener(k,f){this.listeners[k]=f}});
 const get=id=>{if(!nodes.has(id))nodes.set(id,node());return nodes.get(id)};
 const views=[node({historyView:'compact',historyTarget:'browse-records'}),node({historyView:'detailed',historyTarget:'browse-records'})];
 let config={enabled:false,mode:'persona',keywords:[],excluded:[],intervalSeconds:900,dailyLimit:5,accountUid:''};
 const result=()=>({config,attempts:0,paused:false,effectiveKeywords:['植物'],records:[{bvid:'BV18VLu6JEit',title:'植物的日常',description:'公开简介',preview:'公开评论',keyword:'植物',at:'2026-11-01T00:00:00Z',commentCount:1,eventId:'event'}]});
 const context={AbortSignal,URLSearchParams,location:{hash:'#/platforms/bilibili/browsing',pathname:'/'},history:{pushState(){}},window:{},document:{getElementById:get,createElement:tag=>{const n=node();n.tag=tag;created.push(n);return n},querySelectorAll:s=>s==='[data-history-target]'?views:s==='details[data-history-scope]'?created.filter(n=>n.tag==='details'&&n.className==='entry history-record'):[]},fetch:async(url,init)=>{
 const action=url.split('/').at(-1),body=JSON.parse(init?.body||'{}');requests.push({action,body});if(action==='browsing-config')config={...config,...body};
 const r=action==='session'?{csrf:'test'}:action==='overview'?{permissions:{allowedVideos:[],allowedUsers:[],blockedUsers:[]},drafts:[],events:[],notes:[],persona:'fixture',robot:{enabled:true,status:'running'}}:result();return {ok:true,json:async()=>r};}};
 await runInNewContext('(async()=>{'+await readFile('web/app.js','utf8')+'})()',context);
 assert.equal(get('browse-enabled').checked,false);assert.equal(get('browse-read').disabled,true);
 const record=get('browse-records').children[0];assert.ok(record);assert.equal(record.open,false);assert.equal(record.children[0].tag,'summary');
 record.open=true;assert.equal(record.open,true);
 const other=node();other.tag='details';other.className='entry history-record';other.dataset.historyScope='private-outbox';other.open=false;created.push(other);
 await views[1].listeners.click();assert.equal(record.open,true);assert.equal(other.open,false);
 await get('browse-refresh').listeners.click();assert.equal(get('browse-records').children[0].open,true,'刷新应保留该列表显示方式');
 await views[0].listeners.click();assert.equal(get('browse-records').children[0].open,false);assert.equal(other.open,false);
 assert.equal(get('browse-max-duration').value,10);get('browse-max-duration').value='7';get('browse-mode-custom').checked=true;get('browse-keywords').value='猫猫、足球比赛';get('browse-excluded').value='惊悚';
 await get('browse-save').listeners.click();
 const saved=requests.find(x=>x.action==='browsing-config');assert.equal(saved.body.maxDurationMinutes,7);assert.equal(saved.body.enabled,false);assert.equal(saved.body.mode,'custom');assert.deepEqual(saved.body.keywords,['猫猫','足球比赛']);
 assert.equal(requests.some(x=>['auto-config','browsing-read','auto-scan','private-send','diary-generate'].includes(x.action)),false);
});

test('回复策略的频率、历史、过滤规则有独立子菜单和可恢复路由',async()=>{
 const nodes=new Map(),requests=[];
 const node=(dataset={})=>({value:'',textContent:'',hidden:false,disabled:false,checked:false,dataset,listeners:{},children:[],classList:{toggle(){}},append(...v){this.children.push(...v)},replaceChildren(){this.children=[]},addEventListener(k,f){this.listeners[k]=f}});
 const get=id=>{if(!nodes.has(id))nodes.set(id,node());return nodes.get(id)};
 const tabs=['settings','history','rules'].map(replyPage=>node({replyPage}));
 const location={hash:'#/platforms/bilibili/auto/history',pathname:'/'};
 const context={AbortSignal,URLSearchParams,location,history:{pushState(a,b,url){location.hash=url.slice(url.indexOf('#'))}},window:{},document:{getElementById:get,createElement:()=>node(),querySelectorAll:s=>s==='[data-reply-page]'?tabs:[]},fetch:async(url,input)=>{
 const action=url.split('/').at(-1);requests.push(action);
 const r=action==='session'?{csrf:'test'}:action==='overview'?{permissions:{allowedVideos:[],allowedUsers:[],blockedUsers:[]},drafts:[],events:[],notes:[],persona:'fixture',robot:{enabled:true,status:'running'}}:{config:{source:'browsing',enabled:false,accountUid:'123',priorityUid:'789',videos:[],intervalSeconds:300,otherDailyLimit:30,perUserDailyLimit:5},entries:[],userUsed:{},otherUsed:0,paused:false,sourceReady:false};
 return {ok:true,json:async()=>r};}};
 await runInNewContext('(async()=>{'+await readFile('web/app.js','utf8')+'})()',context);
 assert.equal(get('reply-history-page').hidden,false);assert.equal(get('reply-settings-page').hidden,true);assert.equal(get('reply-rules-page').hidden,true);
 await tabs[2].listeners.click();assert.equal(get('reply-rules-page').hidden,false);assert.equal(get('reply-history-page').hidden,true);assert.equal(location.hash,'#/platforms/bilibili/auto/rules');
 await tabs[0].listeners.click();assert.equal(get('reply-settings-page').hidden,false);assert.equal(location.hash,'#/platforms/bilibili/auto/settings');
 assert.equal(requests.filter(a=>a==='auto-status').length,1);assert.equal(requests.some(a=>['auto-config','auto-scan','browsing-read'].includes(a)),false);
});
