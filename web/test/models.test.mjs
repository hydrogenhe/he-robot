import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {runInNewContext} from 'node:vm';

test('模型页只保留本机Harness；精确档位、会话设置与失败保留选择', async()=>{
 const nodes=new Map(),requests=[];
 const node=(dataset={})=>({value:'',textContent:'',hidden:false,disabled:false,checked:false,dataset,listeners:{},classList:{toggle(){}},children:[],append(...items){this.children.push(...items)},replaceChildren(){this.children=[]},addEventListener(k,f){this.listeners[k]=f}});
 const get=id=>{if(!nodes.has(id))nodes.set(id,node());return nodes.get(id)};
 let config={mode:'local',provider:'codex',codexModel:'gpt-6.1-sol',codexEffort:'low',harnessEffort:'low',deepseekModel:'deepseek-flash',codexFound:true,deepseekFound:false};
 const location={hash:'#/models',pathname:'/'};
 const context={AbortSignal,URLSearchParams,location,history:{pushState(a,b,url){location.hash=url.slice(url.indexOf('#'))}},window:{},document:{getElementById:get,createElement:()=>node(),querySelectorAll:()=>[]},fetch:async(url,input)=>{
  const action=url.split('/').at(-1),body=input?.body?JSON.parse(input.body):{};requests.push({action,body});
  if(action==='model-connect'&&body.provider==='deepseek')return {ok:false,status:400,json:async()=>({error:{code:'HARNESS_NOT_FOUND',message:'未找到DeepSeek Harness'}})};
  if(action==='model-connect')config={...config,provider:body.provider,codexModel:body.codexModel,codexEffort:body.codexEffort};
  const result=action.startsWith('conversation-')?{config:action==='conversation-config'?body:{enabled:true,maxTurns:10,idleSeconds:600,maxAgeSeconds:1800,contextTokens:12000,totalTokens:60000},windows:[]}:action.startsWith('generation-safety-')?{config:{ownerDailyCalls:body.ownerDailyCalls??1000,otherDailyCalls:100,ownerDailyTokens:body.ownerDailyTokens??2000000,otherDailyTokens:200000},day:'2026-11-02',owner:{calls:0,tokens:0,unknown:0,pending:0},other:{calls:0,tokens:0,unknown:0,pending:0}}:action==='session'?{csrf:'test'}:action==='model-list'?{models:[{id:'gpt-6.1-sol',name:'Sol'}]}:action==='overview'?{permissions:{allowedVideos:[],allowedUsers:[],blockedUsers:[]},drafts:[],events:[],notes:[],persona:'test',robot:{enabled:true,status:'running'}}:config;
  return {ok:true,json:async()=>result};
 }};
 await runInNewContext('(async()=>{'+await readFile('web/app.js','utf8')+'})()',context);
 assert.match(get('model-state').textContent,/本机 Harness.*Codex.*gpt-6.1-sol.*low/);
 get('model-local-provider').value='deepseek';await get('model-connect').listeners.click();
 assert.match(get('model-connect-result').textContent,/连接失败.*未找到/);assert.match(get('model-state').textContent,/Codex/);
 get('model-local-provider').value='codex';await get('model-connect').listeners.click();
 const selection=requests.findLast(x=>x.action==='model-connect');assert.equal(selection.body.codexModel,'gpt-6.1-sol');assert.equal(selection.body.codexEffort,'low');
 get('conversation-turns').value='5';get('conversation-idle').value='5';await get('conversation-save').listeners.click();
 const saved=requests.findLast(x=>x.action==='conversation-config');assert.equal(saved.body.maxTurns,5);assert.equal(saved.body.idleSeconds,300);
 assert.equal(requests.some(x=>['model-config','private-send','diary-generate','auto-scan'].includes(x.action)),false);
 const html=await readFile('web/index.html','utf8');assert.equal(/id="model-api|data-model-tab="api"|id="model-openai/.test(html),false);
});
