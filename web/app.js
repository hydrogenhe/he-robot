const $ = (id) => document.getElementById(id);
let csrf = "",
  data;
let personaDirty = false,
  permissionsDirty = false;
let qrPending = false;
let serviceClosed = false;
let sessionRefresh = null, reconnectTimer = null, reconnectAttempt = 0;
const reconnectDelays = [1000, 2000, 4000, 8000];
let autoData = null, autoBusy = false;
let browsingData=null, browsingBusy=false, replyPage="settings";
const historyViews=Object.create(null);
let characterLoaded = false, characterBusy = false;
let privateMemoryPage = 1;
let privateData = null, privateBusy = false, privateNonce = null;
let selectedPlatform = null, usageScope = "", diaryData = null, diaryBusy = false;
let activeTab = "home", usageLoaded = false, usagePage = 1, usageFilters = {}, usageBusy = false;
let modelConfigLoaded = false, modelConfig = null, codexModelsLoaded = false;
const titles = {
  home: "今天，从一次对话开始。",
  platforms: "让她在不同的世界里，认识更多人。",
  diary: "把一天，写成自己的故事。",
  "platform-overview": "B 站 · 平台概览",
  "platform-rules": "B 站 · 过滤规则",
  character: "先认识你，再认识世界。",
  models: "为角色，选择一种思考。",
  account: "B 站 · 账号登录",
  permissions: "B 站 · 互动权限",
  replies: "B 站 · 回复工作台",
  memory: "让重要的事情，留得下来。",
  logs: "发生过的事，都在这里。",
  operations: "历史记录",
  auto: "B 站 · 回复策略",
 browsing: "B 站 · 浏览偏好",
 private: "B 站 · 大号私信",
};
function renderModelSelection() {
  for (const button of document.querySelectorAll("[data-provider-target]")) {
    button.disabled = !modelConfigLoaded;
    button.classList.toggle("selected", $(button.dataset.providerTarget).value === button.dataset.providerValue);
  }
  const codex = $("model-local-provider").value === "codex";
  $("model-codex-options").hidden = !codex;
  $("model-local-deepseek-options").hidden = codex;
  const found = $("model-local-provider").value === "codex" ? modelConfig?.codexFound : modelConfig?.deepseekFound;
  $("model-local-found").textContent = found ? "已找到本机程序，可以点击连接。" : "未找到本机程序；连接时将重新检测。";

}
function renderModelConfig(r) {
  modelConfig = r;
  $("model-local-provider").value = r.provider === "codex" ? "codex" : "deepseek";
  $("model-harness-model").value = r.deepseekModel;
  $("model-codex-model").value = r.codexModel || "";
  $("model-codex-effort").value = r.codexEffort || "low";
  $("model-harness-effort").value = r.harnessEffort || "low";
  for(const id of ["model-local-provider","model-connect","model-codex-model","model-codex-effort","model-harness-model","model-harness-effort"])$(id).disabled=false;
  $("model-state").textContent="当前使用：本机 Harness · "+(r.provider==="codex"?"Codex":"DeepSeek")+"；模型："+(r.provider==="codex"?r.codexModel||"尚未选择，请先连接明确模型":r.deepseekModel)+"；思考："+(r.provider==="codex"?r.codexEffort||"low":r.harnessEffort||"low");
  $("model-draft-state").textContent = "已读取当前配置 · 修改后请连接或保存以应用";
  modelConfigLoaded = true;
  renderModelSelection();
}
async function loadCodexModels() {
  const r=await api("model-list");
  const select=$("model-codex-model");select.replaceChildren();
  const blank=el("option","请选择具体模型");blank.value="";select.append(blank);
  for(const choice of r.models || []) {const option=el("option",choice.name || choice.id);option.value=choice.id;select.append(option);}
  select.value=modelConfig?.codexModel || "";codexModelsLoaded=true;
}
async function loadModelConfig() { await loadGenerationSafety(); await loadConversation(); renderModelConfig(await api("model-status")); if(modelConfig.codexFound && !codexModelsLoaded) {try {await loadCodexModels();} catch(e){notice(e.message,true);}} }
task($("model-list-refresh"),loadCodexModels);
$("model-local-provider").addEventListener("change", () => { $("model-connect-result").textContent=""; renderModelSelection(); });
task($("model-refresh"), loadModelConfig);
task($("model-connect"), async () => {
  $("model-connect-result").textContent = "正在检测本机程序并检查协议…";
  try {
    renderModelConfig(await api("model-connect", {provider:$("model-local-provider").value,codexModel:$("model-codex-model").value,codexEffort:$("model-codex-effort").value,deepseekModel:$("model-harness-model").value,harnessEffort:$("model-harness-effort").value}));
    $("model-connect-result").textContent = "连接成功：程序协议握手通过，已选为下一次生成使用；未调用模型测试。";
  } catch(e) { $("model-connect-result").textContent="连接失败："+e.message;throw e; }
});

function el(tag, text, cls) {
  const n = document.createElement(tag);
  if (text !== undefined) n.textContent = text;
  if (cls) n.className = cls;
  return n;
}
let noticeTimer = null, noticeVersion = 0;
function notice(text, error = false) {
  const version = ++noticeVersion;
  if (noticeTimer !== null && typeof clearTimeout === "function") clearTimeout(noticeTimer);
  noticeTimer = null;
  $("notice").hidden = false;
  $("notice").textContent = text;
  $("notice").className = error ? "error" : "";
  if (!error && typeof setTimeout === "function") noticeTimer = setTimeout(() => {
    if (version === noticeVersion) { $("notice").hidden = true; noticeTimer = null; }
  }, 4000);
}
function connectionFailed(message) {
  csrf = "";
  if (data) data.robot = null;
  $("robot-state").textContent = "网页未连接 · 后台状态未确认";
  $("mode").textContent = "正在重新连接";
  for (const id of ["robot-start", "robot-stop", "robot-pause", "robot-resume", "service-shutdown"]) $(id).disabled = true;
  notice(message + "。正在尝试重新连接，恢复后无需更换网址；也可以点击刷新数据。若已退出整个程序，请从本机启动入口重新启动。", true);
  scheduleReconnect();
}
function connectionRecovered() {
  reconnectAttempt = 0;
  if (reconnectTimer !== null) { clearTimeout(reconnectTimer); reconnectTimer = null; }
}
function scheduleReconnect() {
  if (serviceClosed || reconnectTimer !== null || reconnectAttempt >= reconnectDelays.length || typeof setTimeout !== "function") return;
  reconnectTimer = setTimeout(async () => {
    reconnectTimer = null;
    reconnectAttempt++;
    try {
      await initializeSession();
      await refresh();
      notice("工作台已重新连接");
    } catch {
      if (reconnectAttempt >= reconnectDelays.length) {
        $("mode").textContent = "等待服务恢复";
        notice("暂时连接不上后台，请稍后点击刷新数据；若已退出整个程序，请从本机启动入口重新启动。", true);
      } else scheduleReconnect();
    }
  }, reconnectDelays[reconnectAttempt]);
}
async function renewSession() {
  if (!sessionRefresh) sessionRefresh = initializeSession().finally(() => { sessionRefresh = null; });
  return sessionRefresh;
}
async function api(action, input = {}, retryAuth = true) {
 if (serviceClosed) throw new Error("服务已关闭。请双击项目 scripts/start.command 重新启动并打开网页。");
  const localRead = ["overview", "context", "search", "operations", "auto-status", "auto-preview", "private-status", "robot-status", "robot-control", "character-status", "development-status", "private-memories", "diary-status", "diary-config"].includes(action);
  const requestCSRF = csrf;
  try {
    const r = await fetch("/api/" + action, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf },
      body: JSON.stringify(input),
      signal: AbortSignal.timeout(localRead ? 8000 : 120000),
    });
    const j = await r.json();
    if (r.status === 401 && j.error?.code === "AUTH" && retryAuth) {
      // 服务在认证前拒绝了这次动作，未执行业务；只在明确401时刷新CSRF并重试一次。
      if (csrf === requestCSRF) await renewSession();
      return api(action, input, false);
    }
    if (!r.ok) {
      if (r.status === 401 && j.error?.code === "AUTH") connectionFailed("网页会话已失效");
      throw new Error(j.error?.message || "请求失败");
    }
    return j;
  } catch (e) {
    if (e.name === "TimeoutError" || e.name === "AbortError") {
      throw new Error(action === "send" || action === "private-send"
        ? "等待发送结果超时。请先刷新记录并回查 B 站，不要重复发送。"
        : "等待操作结果超时，请稍后刷新。其他页面仍可查看。");
    }
    if (e instanceof TypeError) {
      connectionFailed("本地服务暂时断开");
      throw new Error(action === "send" || action === "private-send"
        ? "连接中断，发送结果不明。请回查记录，不要重复发送。"
        : "本地服务暂时断开，正在重新连接。");
    }
    throw e;
  }
}
function when(s) {
  return new Date(s).toLocaleString();
}
function entry(title, subtitle, text, preview) {
  const n=el("details",undefined,"entry history-record");n.open=false;
  const summary=el("summary",undefined,"history-summary");
  summary.append(el("strong",title),el("span",String(preview??text??subtitle??"").replace(/\s+/g," ").slice(0,100),"history-preview"),el("small",subtitle?String(subtitle).split(" · ")[0]:"","muted"),el("span","详情","history-expand"));
  n.append(summary,el("div",subtitle,"muted history-meta"));if(text)n.append(el("p",text));return n;
}

function link(bvid, rpid) {
  const a = el("a", "在 B 站查看 ↗");
  a.href =
    "https://www.bilibili.com/video/" +
    bvid +
    "/" +
    (rpid ? "#reply" + rpid : "");
  a.target = "_blank";
  a.rel = "noopener noreferrer";
  return a;
}
function task(button, fn) {
  button.addEventListener("click", async () => {
    const label = button.textContent;
    button.disabled = true;
    button.textContent = "处理中…";
    button.ariaBusy = "true";
    try {
      await fn();
    } catch (e) {
      notice(e.message, true);
    } finally {
      button.ariaBusy = "false";
      button.disabled = serviceClosed || (button.id?.startsWith("robot-") ? robotButtonDisabled(button.id) : button.id === "diary-write" ? (diaryData?.todayStatus === "generating" || (diaryData?.todayStatus === "completed" && !diaryData?.testing) || !data?.robot?.enabled) : button.id === "private-resume" ? privateResumeDisabled() : button.id === "auto-stop" ? !autoData?.config.enabled : button.id === "auto-scan" ? (!autoData?.config.enabled || autoData?.paused || (autoData?.config.source==="browsing"&&autoData?.sourceReady===false)) : button.id==="browse-read" ? (!browsingData?.config.enabled || browsingData?.paused || !data?.robot?.enabled || browsingData?.attempts>=browsingData?.config.dailyLimit) : false);
      if (button.id === "diary-write" && diaryData) renderDiary(diaryData);
      else button.textContent = label;
    }
  });
}
function lines(id) {
  return $(id)
    .value.split(/[\s,，、]+/)
    .map((x) => x.trim())
    .filter(Boolean);
}
function render() {
 renderPlatformSummary();
 renderRobot(data.robot || {enabled: false});
  const p = data.permissions;
  if (!personaDirty) $("persona").value = data.persona;
  if (!permissionsDirty) {
    $("enabled").checked = p.enabled;
    $("allow-dynamic").checked = p.allowDynamic;
    $("videos").value = p.allowedVideos.join("\n");
    $("allowed-users").value = p.allowedUsers.join("\n");
    $("blocked-users").value = p.blockedUsers.join("\n");
    $("daily").value = p.dailyLimit;
    $("cooldown").value = p.cooldownSeconds;
  }
  $("mode").textContent = data.automation?.enabled ? (data.automation.paused ? "自动回复已暂停" : "自动回复已开启 · 限授权视频") : p.enabled
    ? "逐条审核 · 发送已启用"
    : "只读模式 · 发送关闭";
  $("event-count").textContent = data.events.length;
  $("draft-count").textContent = data.drafts.filter(
    (d) => d.status === "pending",
  ).length;
  $("note-count").textContent = data.notes.length;
  $("data-path").textContent = data.dataPath;
  $("account-state").textContent = data.account
    ? "已保存账号：" +
      data.account.name +
      " · UID " +
      data.account.uid +
      "（点击检查以核实当前有效性）"
    : "尚未登录";
  const names = {
    pending: "等待审核",
    sending: "正在发送",
    submitted: "已提交 · 等待回查",
    visible: "本账号可查 · 公开性未核验",
    restricted: "展示受限 · 仅自己可见（state=17）",
    pending_review: "待审核／展示受限（state=11）",
    failed: "发送失败",
    unknown: "结果不明 · 请手动回查",
    cancelled: "已取消",
  };
  $("drafts").replaceChildren();
  for (const d of data.drafts) {
    const n = entry(
      names[d.status],
      when(d.createdAt) + " · " + d.videoTitle + " · 账号 " + d.account,
      d.text,
    );
    n.append(el("div", "原评论：" + d.targetText, "quote"));
    n.append(
      el(
        "small",
        "目标 UID " +
          (d.targetUser || "无（一级评论）") +
          " · parent " +
          d.parent +
          " · root " +
          d.root,
      ),
    );
    if (d.rootPlatformState !== undefined) n.append(el("small", "根评论平台状态：" + d.rootPlatformState));
    if (d.platformState !== undefined) n.append(el("small", "平台状态：" + d.platformState));
    if (d.visibilityCheckedAt) n.append(el("small", "最近回查：" + when(d.visibilityCheckedAt)));
    if (d.platformMessage) n.append(el("p", "平台回执：" + d.platformMessage));
    if (d.error) n.append(el("p", d.error));
    const row = el("div", undefined, "row");
    if (d.kind === "dynamic") {
      if (d.rpid) {
        const a = el("a", "查看 B 站动态 ↗");
        a.href = "https://t.bilibili.com/" + d.rpid;
        a.target = "_blank";
        a.rel = "noopener noreferrer";
        row.append(a);
      }
    } else row.append(link(d.bvid, d.rpid));
    if (d.status === "pending") {
      const send = el("button", "审核并发送");
      task(send, async () => {
        if (
          !confirm(
            "请核对账号 " +
              d.account +
              (d.kind === "dynamic" ? "、文字动态 " : "、视频 " + d.bvid) +
              " 和回复正文：\n\n" +
              d.text +
              "\n\n确认后将真实发布到 B 站。",
          )
        )
          return;
        const result = await api("send", { id: d.id });
        notice(names[result.status]);
        await refresh();
      });
      const cancel = el("button", "取消草稿", "quiet");
      task(cancel, async () => {
        updateDraft(await api("cancel", { id: d.id }));
      });
      row.append(cancel, send);
    }
    if (d.rpid && ["submitted", "visible", "restricted", "pending_review"].includes(d.status)) {
      const check = el("button", d.kind === "dynamic" ? "回查动态" : "回查评论", "quiet");
      task(check, async () => {
        updateDraft(await api("recheck", { id: d.id }));
      });
      row.append(check);
    }
    n.append(row);
    appendHistory("drafts",n);
  }
  if (!data.drafts.length)
    appendHistory("drafts",entry(
        "还没有草稿",
        "让 Codex 读取评论、按人设准备回复，或在上方手动填写。",
      ),
    );
  $("notes").replaceChildren();
  for (const n of data.notes) {
    appendHistory("notes",entry(
        (n.kind === "diary" ? "日记 · " : "记忆 · ") + n.title,
        when(n.at) + " · 来源 " + n.sources.join(", "),
        n.text,
      ),
    );
  }
  $("events").replaceChildren();
  for (const e of data.events) {
    const n = entry(e.summary, when(e.at) + " · " + e.kind + " · " + e.id);
    n.append(el("pre",JSON.stringify(e.data,null,2)));
    appendHistory("events",n);
  }
}
async function refresh() {
  data = await api("overview");
  render();
  connectionRecovered();
}
function updateDraft(result) {
  data.drafts = data.drafts.map((d) => d.id === result.id ? result : d);
  render();
}
function renderPlatformSummary() {
 $("platform-summary-account").textContent = data?.account ? `${data.account.name} · UID ${data.account.uid}（已保存）` : "尚未登录 B 站账号";
 $("platform-summary-auto").textContent = autoData ? (!autoData.config.enabled ? "自动回复已关闭" : autoData.paused ? "安全暂停 · "+autoData.reason : "自动回复已开启") : "进入平台后读取状态";
 $("platform-summary-quota").textContent = autoData ? `其他用户每天 ${autoData.config.otherDailyLimit} 次，每人 ${autoData.config.perUserDailyLimit} 次；今天已用 ${autoData.otherUsed} 次。优先账号不占额度。` : "";
 $("platform-summary-private").textContent = privateData ? (!privateData.config.enabled ? "私信功能已关闭" : privateData.paused ? "私信AI回复已暂停" : privateData.config.autoReply ? "仅对绑定大号自动回复" : "仅手动私信") : "进入平台后读取状态";
}
function routeFromHash() {
 const parts=location.hash.replace(/^#\/?/,"").split("/");
 if(parts[0]==="platforms"&&parts[2]==="auto") replyPage=["history","rules"].includes(parts[3])?parts[3]:"settings";

 if(parts[0]==="platforms" && ["bilibili","qq","douyin"].includes(parts[1])) return {tab:parts[2] || "platform-overview",platform:parts[1]};
 return {tab:["home","character","models","platforms","diary","memory","logs","operations"].includes(parts[0]) ? parts[0] : "home",platform:null};
}
async function navigate(tab, platform=null, updateHistory=true) {
 if(tab==="logs")tab="operations";
 if(tab==="operations" && platform) {$("usage-platform").value=platform;usageFilters.platform=platform;platform=null;}
 if(tab==="permissions")tab="replies";
 if(tab==="platform-rules"){tab="auto";replyPage="rules";}
 const allowed=["platform-overview","account","auto","browsing","permissions","platform-rules","replies","private","operations"];
 if(platform && !allowed.includes(tab)) tab="platform-overview";
 selectedPlatform=platform;
 activeTab=platform && platform!=="bilibili" ? "platform-unavailable" : tab;
 $("model-state").hidden=activeTab!=="models";
 $("overview-controls").hidden=activeTab!=="home";


 if(activeTab==="auto")renderReplyPage();
 for(const panel of document.querySelectorAll(".panel")) panel.hidden=panel.id!==activeTab;
 for(const button of document.querySelectorAll("[data-tab]")) button.classList.toggle("active",platform ? button.dataset.tab==="platforms" : button.dataset.tab===activeTab);
 for(const button of document.querySelectorAll("[data-platform-tab]")) {
  const active=button.dataset.platformTab===activeTab;
  button.classList.toggle("active",active);
  button.setAttribute?.("aria-current",active?"page":"false");
 }
 $("platform-workspace").hidden=!platform;
 $("platform-navigation").hidden=platform!=="bilibili";
 if(platform) $("platform-current").textContent=platformNames[platform]+" · 互动平台";
 if(activeTab==="platform-unavailable") $("platform-unavailable-title").textContent=platformNames[platform]+"尚未接入";
 $("page-title").textContent=activeTab==="platform-unavailable" ? platformNames[platform]+" · 等待接入" : selectedPlatform && activeTab==="operations" ? "B 站 · 平台日志与用量" : titles[activeTab];
 if(updateHistory && (history.pushState || history.replaceState)) (history.pushState || history.replaceState).call(history,null,"",location.pathname+(platform ? "#/platforms/"+platform+(tab==="platform-overview"?"":"/"+tab+(tab==="auto"?"/"+replyPage:"")) : "#/"+tab));
 if(activeTab==="operations") {
  const scope=platform || "";
  const changedScope=usageScope!==scope;
  if(changedScope) {
   usageScope=scope;usageFilters=scope?{platform:scope}:{};usageLoaded=false;usagePage=1;
   for(const key of ["account","action","status","reported","from","to"]) $("usage-"+key).value="";
   $("usage-rows").replaceChildren();
   for(const id of ["usage-count","usage-tokens","usage-coverage"]) $(id).textContent="—";
   $("usage-summary").textContent="正在读取当前范围的业务记录…";
  }
  if(scope || changedScope) $("usage-platform").value=scope;
  $("usage-platform").disabled=!!scope;
  $("usage-heading").textContent=scope ? "B 站 · 历史记录" : "历史记录";
  $("usage-scope").textContent=scope ? "仅查询 B 站的读取、回复、发送结果和 Token，其他平台与角色日记不会混入。" : "浏览、回复、写日记等活动统一记录；展开查看内容、来源、耗时和 Token。";
 }
 renderPlatformSummary();
 if(!csrf || !data) return;
 if(activeTab==="platform-overview") await Promise.all([autoData?Promise.resolve():loadAuto(),privateData?Promise.resolve():loadPrivate()]);
 if(activeTab==="character" && !characterLoaded) await loadCharacter();
 if(activeTab==="models" && !modelConfigLoaded) await loadModelConfig();
 if(activeTab==="private" && !privateData && !privateBusy) await loadPrivate();
 if(activeTab==="browsing" && !browsingData&&!browsingBusy) await loadBrowsing();
 if(activeTab==="auto" && !autoData && !autoBusy) await loadAuto();
 if(activeTab==="operations" && !usageLoaded && !usageBusy) await loadOperations(1);
 if(activeTab==="diary" && !diaryData) await loadDiary();
}
function navigationError(e) { notice(e.message,true); }
for(const button of document.querySelectorAll("[data-tab]")) button.addEventListener("click",()=>navigate(button.dataset.tab).catch(navigationError));
for(const button of document.querySelectorAll("[data-platform]")) button.addEventListener("click",()=>navigate("platform-overview",button.dataset.platform).catch(navigationError));
for(const button of document.querySelectorAll("[data-platform-tab]")) button.addEventListener("click",()=>navigate(button.dataset.platformTab,"bilibili").catch(navigationError));
$("platform-back").addEventListener("click",()=>navigate("platforms").catch(navigationError));
window.addEventListener?.("hashchange",()=>{const r=routeFromHash();navigate(r.tab,r.platform,false).catch(navigationError);});
$("persona").addEventListener("input", () => (personaDirty = true));
for (const id of [
  "enabled",
  "allow-dynamic",
  "videos",
  "allowed-users",
  "blocked-users",
  "daily",
  "cooldown",
])
  $(id).addEventListener("input", () => (permissionsDirty = true));
task($("refresh"), async () => {
  if (!csrf) { await initializeSession(); await refresh(); return; }
  if (activeTab === "operations") { await loadOperations(usagePage); return; }
  if (activeTab === "models") { await loadModelConfig(); return; }
  if (activeTab === "browsing") { await loadBrowsing(); return; }
  if (activeTab === "auto") { await loadAuto(); return; }
 if (activeTab === "private") { await loadPrivate(); return; }
 if (activeTab === "character") { await loadCharacter(); return; }
  if (activeTab === "diary") {await loadDiary();return;}
  if (activeTab === "platform-overview") {await Promise.all([loadAuto(),loadPrivate()]);return;}
  await refresh();
  notice(
    personaDirty || permissionsDirty
      ? "已刷新记录；未覆盖尚未保存的设定"
      : "数据已刷新",
  );
});
task($("save-persona"), async () => {
  await api("persona", { persona: $("persona").value });
  personaDirty = false;
  await refresh();
  notice("人设已保存");
});
task($("save-permissions"), async () => {
  await api("permissions", {
    enabled: $("enabled").checked,
    allowDynamic: $("allow-dynamic").checked,
    allowedVideos: lines("videos"),
    allowedUsers: lines("allowed-users"),
    blockedUsers: lines("blocked-users"),
    dailyLimit: Number($("daily").value),
    cooldownSeconds: Number($("cooldown").value),
  });
  permissionsDirty = false;
  await refresh();
  notice("手动发送设置与账号名单已保存");
});
task($("check-account"), async () => {
  const a = await api("account");
  data.account = a.loggedIn ? { uid: a.uid, name: a.name } : null;
  render();
  notice(a.loggedIn ? "登录有效：" + a.name : "未登录或登录已失效");
});
task($("get-qr"), async () => {
  const q = await api("qr");
  qrPending = true;
  $("qr").src = q.image;
  $("qr").hidden = false;
  $("poll-qr").disabled = false;
  $("qr-state").textContent =
    "请用 B 站 App 扫码并确认，再点击检查结果。约三分钟有效。";
});
task($("poll-qr"), async () => {
  if (!qrPending) {
    notice("请先生成新的登录二维码。");
    return;
  }
  const r = await api("poll");
  const messages = {
    0: "登录成功",
    86101: "等待扫码",
    86090: "已扫码，请在手机确认",
    86038: "二维码过期，请重新生成",
  };
  notice(messages[r.code] || "扫码状态：" + r.code);
  if (r.code === 0 || r.code === 86038) qrPending = false;
  if (r.code === 0) {
    $("qr").hidden = true;
    data.account = r.loggedIn ? { uid: r.uid, name: r.name } : null;
    render();
  }
});
task($("load-comments"), async () => {
  const r = await api("comments", {
    bvid: $("video").value.trim(),
    page: Number($("page").value),
    ...($("comment-root").value.trim()
      ? { root: $("comment-root").value.trim() }
      : {}),
  });
  $("comments-meta").textContent =
    r.video.title +
    " · 第 " +
    r.page +
    " 页 · " +
    (r.hasMore ? "仍有后续页" : "当前已到末页") +
    " · 事件 " +
    r.eventId;
  $("comments").replaceChildren();
  for (const c of r.comments) {
    const n = entry(c.name + " · UID " + c.uid, "评论 ID " + c.id, c.text);
    const b = el("button", "回复这条", "quiet");
    b.onclick = () => {
      $("draft-parent").value = c.id;
      $("draft-root").value = c.root === "0" ? c.id : c.root;
      $("draft-text").focus();
    };
    n.append(b);
    appendHistory("comments",n);
  }
  await refresh();
});
task($("prepare-dynamic"), async () => {
  await api("dynamic-draft", { text: $("dynamic-text").value });
  await refresh();
  notice("动态草稿已保存，尚未发布");
});
task($("prepare"), async () => {
  await api("draft", {
    bvid: $("video").value.trim(),
    root: $("draft-root").value.trim(),
    parent: $("draft-parent").value.trim(),
    text: $("draft-text").value,
  });
  await refresh();
  notice("草稿已保存，尚未发送");
});
task($("search-memory"), async () => {
  const r = await api("search", { query: $("search").value });
  $("search-results").replaceChildren();
  for (const e of r.events)
    appendHistory("search-results",entry(e.summary, when(e.at) + " · " + e.id, JSON.stringify(e.data)),
    );
  for (const n of r.notes)
    appendHistory("search-results",entry(n.title, when(n.at), n.text));
  if (!r.events.length && !r.notes.length)
    $("search-results").append(el("p", "没有找到相关记录。"));
});
function renderDiary(r) {
 diaryData=r;
 for(const id of ["diary-enabled","diary-testing","diary-save","diary-topics","diary-owner-weight","diary-interest-weight"]) $(id).disabled=false;
 $("diary-enabled").checked=r.enabled;
 $("diary-testing").checked=!!r.testing;
 $("diary-topics").value=(r.topics||[]).join("、");
 $("diary-owner-weight").value=r.ownerWeight??4;
 $("diary-interest-weight").value=r.interestWeight??2;
 const state=r.todayStatus;
 $("diary-state").textContent=(r.enabled?"每天23:00自动写日记；":"自动日记已暂停；")+(state==="completed"?(r.testing?"测试模式已开放，可以再次生成今天的版本。":"今天已经写过，不重复消耗。"):state==="empty"?"今天暂无可记录的新经历，未调用AI。":state==="generating"?"正在整理今天的心情…":state==="failed"||state==="interrupted"?"上次生成未完成；可以明确点击重试，不会自动重复生成。":r.testing?"测试模式：可空素材写作，也可在同一天反复生成。":"也可以现在写下今天的回顾。");
 $("diary-write").textContent=state==="completed"?(r.testing?"再写今日日记（测试）":"今日日记已完成"):state==="failed"||state==="interrupted"?"重试写今日日记":"写今日日记";
 $("diary-write").disabled=(state==="completed" && !r.testing) || state==="generating" || !data?.robot?.enabled;
 $("diary-entries").replaceChildren();
 for(const d of r.entries.filter(d=>d.status==="completed")) {
  const article=entry(d.title,d.date,d.body);
  article.append(el("div",d.testing && !d.sourceCount ? "测试模式 · 暂无新经历的角色感想" : `依据当天 ${d.sourceCount} 份经历片段 · 仅本机可见的角色回顾${d.testing?" · 测试版本":""}`,"muted"));
  appendHistory("diary-entries",article);
 }
 if(!r.entries.some(d=>d.status==="completed")) appendHistory("diary-entries",entry("日记本的第一页，还在等今天的故事。","有了新经历，再来写下一点心情。"));
}
async function loadDiary() {
 if(diaryBusy)return;
 diaryBusy=true;
 try {renderDiary(await api("diary-status"));} finally {diaryBusy=false;}
}
task($("diary-refresh"),loadDiary);
task($("diary-save"),async()=>{renderDiary(await api("diary-config",{enabled:$("diary-enabled").checked,testing:$("diary-testing").checked,topics:lines("diary-topics"),ownerWeight:Number($("diary-owner-weight").value),interestWeight:Number($("diary-interest-weight").value)}));notice("日记设置已保存");});
task($("diary-write"),async()=>{
 if(diaryData?.todayStatus==="failed" || diaryData?.todayStatus==="interrupted") {
  if(!window.confirm("重新生成会调用一次AI，之前的尝试可能已消耗Token。确定重试今天的日记？"))return;
 }
 try {
  await api("diary-generate",{retry:["failed","interrupted"].includes(diaryData?.todayStatus)});
  await loadDiary();
  notice(diaryData.todayStatus==="empty"?"今天还没有新经历，没有调用AI。":"今日日记已写好");
 } catch(e) {await loadDiary();throw e;}
});

const operationNames = {
 "browsing-read":"浏览视频资料",
  "diary-generate":"写角色日记",
  "auto-reply": "AI自动回复", "private-auto-reply": "大号私信AI回复", "private-send": "发送大号私信", context: "读取角色资料", search: "检索记忆", note: "记录记忆 / 日记",
  comments: "读取视频评论", draft: "准备回复草稿", "dynamic-draft": "准备动态草稿",
  send: "发送", sent: "提交回复", send_result: "发送结果", cancel: "取消草稿",
  recheck: "回查展示状态", account: "检查账号", qr: "生成登录码", poll: "检查扫码结果",
  "import-session": "导入会话", login: "登录",
};
const platformNames = { bilibili: "B站", local: "本地", qq: "QQ", douyin: "抖音" };
const operationStatusNames = {
  sent:"已发送", succeeded: "操作成功", failed: "操作失败", running: "执行中", unknown: "结果未知",
  recorded: "历史事实", visible: "本账号可查 · 公开性未核验", restricted: "展示受限",
  pending_review: "待审核", submitted: "已提交", cancelled: "已取消",
  partial: "已发送部分，后续已停止",
};
function usageFilterValues() {
  const filters = {};
  for (const key of ["platform", "account", "action", "status"]) {
    const value = $("usage-" + key).value.trim();
    if (value) filters[key] = value;
  }
  const reported = $("usage-reported").value;
  if (reported !== "") filters.hasUsage = reported === "true";
  for (const key of ["from", "to"]) {
    const value = $("usage-" + key).value;
    if (value) filters[key] = new Date(value + (key === "from" ? "T00:00:00" : "T23:59:59.999")).toISOString();
  }
  if(selectedPlatform) filters.platform=selectedPlatform;
  return filters;
}
async function loadOperations(page) {
  if (usageBusy) return;
  usageBusy = true;
  const requestScope=usageScope;
  for (const id of ["usage-query", "usage-prev", "usage-next"]) $(id).disabled = true;
  try {
    const r = await api("operations", { ...usageFilters, activitiesOnly:true, page, pageSize: 30 });
    if(usageScope!==requestScope) return;
    usagePage = r.page; usageLoaded = true;
    $("usage-collection").textContent = r.collectionStatus || "";
    $("usage-count").textContent = r.total.toLocaleString();
    $("usage-tokens").textContent = r.reported ? (r.input + r.output).toLocaleString() : "未获取";
    $("usage-coverage").textContent = r.reported + " / " + r.total;
    $("usage-summary").textContent = r.reported
      ? `已记录输入 ${r.input} / 输出 ${r.output}；其中已提供的缓存输入 ${r.cachedInput}（输入子集，不重复计入总量）。${r.total - r.reported} 条操作尚无用量。共享轮次只累计一次；筛选命中共享操作时包含整轮用量。查询仅使用本地数据。`
      : `当前筛选范围的 ${r.total} 条操作尚未获取可归属的真实 Token，暂时无法统计输入、输出和缓存用量。共享轮次只累计一次；筛选命中共享操作时包含整轮用量。查询仅使用本地数据。`;
    const selected = $("usage-action").value;
    $("usage-action").replaceChildren();
    const all = el("option", "全部操作"); all.value = ""; $("usage-action").append(all);
    for (const action of r.actions) { const o = el("option", operationNames[action] || action); o.value = action; $("usage-action").append(o); }
    $("usage-action").value = selected;
    // 新平台只需产生同格式流水，查询页自动出现，无须修改筛选协议。
    const platformSelect = $("usage-platform");
    const known = new Set(Array.from(platformSelect.options || []).map((o) => o.value));
    for (const platform of r.platforms) {
      if (!known.has(platform)) { const o = el("option", platformNames[platform] || platform); o.value = platform; platformSelect.append(o); }
    }
    $("usage-rows").replaceChildren();
    $("usage-summary-list").replaceChildren();
    for (const op of r.items) {
      const title=op.action==="private-auto-reply"?"回复了绑定主人":(operationNames[op.action]||op.action);
      const observed=(data?.events||[]).find(e=>e.id===op.reference);
      const compact=entry(op.activityTitle||observed?.summary||title,when(op.at)+" · "+(platformNames[op.platform]||op.platform)+" · "+(operationStatusNames[op.status]||op.status),op.activityText||operationStatusNames[op.status]||op.status,operationStatusNames[op.status]||op.status);
      compact.append(el("p",op.tokens===null?"Token 未获取":op.tokens+" Token"));
      appendOperationDetails(compact,op,false);
      if(op.action==="private-auto-reply") {const show=el("button","查看私聊内容","quiet");show.addEventListener("click",()=>navigate("private","bilibili").catch(navigationError));compact.append(show);}
      appendHistory("usage-summary-list",compact);
      const tr = el("tr");
      const time = el("td", when(op.at));
      time.append(el("div", ({ mcp: "机器人 MCP", web: "网页", history: "历史补入", automation: "自动回复" })[op.source] || op.source, "muted"));
      const platform = el("td", platformNames[op.platform] || op.platform);
      platform.append(el("div", op.account || "未记录账号", "muted"));
      const action = el("td", operationNames[op.action] || op.action);
      action.append(el("div", operationStatusNames[op.status] || op.status, "muted"));
      if (op.errorCode) action.append(el("div", op.errorCode, "muted"));
      const tokens = el("td", op.tokens === null ? "未获取" : op.tokens.toLocaleString() + " Token");
      if (op.tokens !== null) {
        const rule = op.usage.some((u) => u.source === "local_rule");
        const local = op.usage.some((u) => u.source === "codex_local" || u.source === "codex_app_server");
        const nativeApi = op.usage.some((u) => ["deepseek_api", "deepseek_harness", "openai_api"].includes(u.source));
        const shared = op.usage.some((u) => (u.relatedOperations || []).length > 1);
        tokens.append(el("div", rule ? "本地规则 · 无模型调用" : nativeApi ? "模型供应商原生回执 · 实测用量" : local ? (shared ? "Codex 实测 · 共享轮次总量" : "Codex 实测 · 本业务轮次") : "宿主上报 · 可能为部分用量", "muted"));
      }
      const detailsCell = el("td"); const details = el("details",undefined,"usage-record"); details.open=!!historyViews["usage-summary-list"];details.dataset.historyScope="usage-summary-list"; details.append(el("summary", "查看"));
      appendOperationDetails(details,op);
      detailsCell.append(details); tr.append(time, platform, action, tokens, detailsCell); $("usage-rows").append(tr);
    }
    $("usage-summary-list").hidden=!!historyViews["usage-summary-list"];$("usage-detail-table").hidden=!historyViews["usage-summary-list"];
    if(!r.items.length)$("usage-summary-list").append(el("p","没有符合条件的业务记录。","muted"));
    if (!r.items.length) { const tr = el("tr"); const td = el("td", "没有符合条件的业务记录。"); td.colSpan = 5; tr.append(td); $("usage-rows").append(tr); }
    $("usage-page").textContent = `第 ${r.page} 页 / 共 ${Math.max(1, Math.ceil(r.total / r.pageSize))} 页`;
    $("usage-prev").disabled = r.page <= 1;
    $("usage-next").disabled = r.page * r.pageSize >= r.total;
  } finally {
    usageBusy = false; $("usage-query").disabled = false;
    if(activeTab === "operations" && requestScope!==usageScope) await loadOperations(1);
  }
}
task($("usage-query"), async () => { usageFilters = usageFilterValues(); await loadOperations(1); });
$("usage-prev").addEventListener("click", () => loadOperations(Math.max(1, usagePage - 1)).catch((e) => notice(e.message, true)));
$("usage-next").addEventListener("click", () => loadOperations(usagePage + 1).catch((e) => notice(e.message, true)));


function renderAuto(r) {
  autoData = r;
  renderPlatformSummary();
  const c = r.config;
  $("auto-enabled").checked = c.enabled;
  $("auto-account").value = c.accountUid || data?.account?.uid || "";
  $("auto-priority").value = c.priorityUid || "";
  $("auto-source-state").textContent=c.source==="browsing"?"回复来源：跟随浏览偏好发现的视频。"+(r.sourceReady===false?"当前等待开启浏览并读取视频，尚无可回复范围。":"关闭浏览后不会继续自动回复这些视频。"):"当前保留旧版指定视频任务；保存本页后切换为跟随浏览偏好，不再填写BV号。已有额度和暂停状态不会因打开页面而改变。";
  $("auto-interval").value = c.intervalSeconds;
  $("auto-other-limit").value = c.otherDailyLimit;
  $("auto-user-limit").value = c.perUserDailyLimit;
  $("auto-state").textContent = !c.enabled ? "自动回复已关闭" : r.paused ? "自动回复已暂停：" + r.reason : "自动回复已开启 · 仅处理新评论";
  $("auto-quota").textContent = `今天已对其他用户尝试 ${r.otherUsed} / ${c.otherDailyLimit} 次；每人每天最多 ${c.perUserDailyLimit} 次；优先账号不占额度。额度含失败和未知结果，不会自动重发。`;
  $("auto-users").textContent = Object.entries(r.userUsed).map(([uid, count]) => `UID ${uid}：${count}/${c.perUserDailyLimit}`).join("；") || "今天尚未对其他用户发送自动回复。";
  $("auto-times").textContent = "上次检查：" + (r.lastAt ? when(r.lastAt) : "尚未检查") + "；下次计划：" + (c.enabled && !r.paused && r.nextAt ? when(r.nextAt) : "等待调度或已停止");
  $("auto-scan").disabled = !c.enabled || r.paused || (c.source==="browsing"&&r.sourceReady===false);
  $("auto-stop").disabled = !c.enabled;
  $("auto-entries").replaceChildren();
  const names = { queued: "等待发送", skipped: "已跳过", processing: "处理中", completed: "已处理", interrupted: "中断 · 需人工回查" };
  for (const e of r.entries) {
    const n = entry((names[e.status] || e.status) + " · UID " + e.uid, when(e.at) + " · " + e.bvid + " · 评论 " + e.commentId, e.text);
    n.append(el("p", e.reason, "muted"));
    if (e.reply) n.append(el("p", "回复：" + e.reply));
    if (e.result) n.append(el("p", "发送状态：" + e.result, "muted"));
    appendHistory("auto-entries",n);
  }
  if (!r.entries.length) $("auto-entries").append(el("p", "尚无新评论处理记录。首次扫描不会回复历史评论。", "muted"));
  if (data) { data.automation = { enabled: c.enabled, paused: r.paused }; render(); }
}
const moodNames = {calm:"平静",happy:"开心",curious:"好奇",sad:"低落",tired:"疲惫",excited:"兴奋"};
async function loadCharacter() {
 if (characterBusy) return;
 characterBusy = true;
 try {
  const r = await api("character-status");
  const growth=r.development||{interests:[],relationships:[]};
  $("development-interests").replaceChildren();
  for(const i of growth.interests) appendHistory("development-interests",entry(`${i.topic} · ${i.visibility==="abstract"?"通用兴趣阶段":i.visibility==="private"?"私有":"公开"}`,`熟悉 ${(i.familiarityUnits/100).toLocaleString()} · 喜欢 ${(i.preferenceUnits/100).toLocaleString()} · ${({no_preference:"暂无明确偏好",like_1:"开始感兴趣",like_2:"感兴趣",like_3:"非常喜欢",dislike_1:"有些不喜欢",dislike_2:"不喜欢",dislike_3:"很不喜欢"})[i.stage]||"旧版记录"}`,i.conversation?`当前私聊会话 ${i.conversation}`:"只共享粗略兴趣阶段，私人经历和关系细节不对外共享"));
  if(!growth.interests.length) $("development-interests").append(el("p","兴趣还在萌芽。只有启用后的真实新经历才积累，反复刷新不会增加。","muted"));
  $("development-relationships").replaceChildren();
  for(const r of growth.relationships) appendHistory("development-relationships",entry(`${platformNames[r.platform]||r.platform} · ${r.peer} · ${r.label}`,`${r.visibility==="private"?"私聊限定":"公开"} · 熟悉 ${((r.familiarityUnits??r.score*100)/100).toLocaleString()} · 亲近 ${((r.affinityUnits??0)/100).toLocaleString()} · ${r.days}天、${r.interactions}段有效互动`,"按平台和稳定账号区分；不凭昵称合并，不代表每条都已回复成功。"));
  if(!growth.relationships.length) $("development-relationships").append(el("p","尚无新的互动关系记录。","muted"));
  characterLoaded = true;
  $("character-state").textContent = r.description;
  $("character-traits").replaceChildren();
  for (const [key,name] of [["warmth","亲切"],["curiosity","好奇"],["playfulness","活泼"]]) {
   const n = el("div"); n.append(el("strong",name+"："+r.traits[key]+" / 100"));
   const bar = el("progress"); bar.max = 100; bar.value = r.traits[key]; n.append(bar); $("character-traits").append(n);
  }
  $("character-changes").replaceChildren();
  for (const c of [...r.changes].reverse().slice(0,20)) appendHistory("character-changes",entry((c.channel === "private-message" ? "读到大号新私信" : "读到视频新互动")+" · "+(moodNames[c.mood] || c.mood),when(c.at),`情绪强度 ${c.intensity}；亲切 ${c.traits.warmth} / 好奇 ${c.traits.curiosity} / 活泼 ${c.traits.playfulness}。来源操作 ${c.source}；这里记录理解收到的消息，不代表发送已成功。`));
  if (!r.changes.length) $("character-changes").append(el("p","尚未积累新的AI互动；不会用开发聊天或旧评论伪造成长记录。","muted"));
 } finally { characterBusy = false; }
}
task($("character-refresh"),loadCharacter);
function robotButtonDisabled(id) {
 if (!csrf || !data?.robot) return true;
 const status = data?.robot?.status || (data?.robot?.enabled ? "running" : "paused");
 return id === "robot-start" ? status !== "stopped" : id === "robot-stop" ? status === "stopped" : id === "robot-pause" ? status !== "running" : id === "robot-resume" ? status !== "paused" : false;
}
function renderRobot(r) {
 if (!r.status) r = {...r,status:r.enabled ? "running" : "paused"};
 if (data) data.robot = r;
 $("robot-state").textContent = ({running:"机器人运行中",paused:"机器人已暂停",stopped:"机器人已关闭"})[r.status] || "状态未知";
 for (const id of ["robot-start","robot-stop","robot-pause","robot-resume"]) $(id).disabled = serviceClosed || robotButtonDisabled(id);
 $("service-shutdown").disabled = serviceClosed || !csrf;
}
task($("service-shutdown"), async () => {
 if (!window.confirm("退出整个程序会停止机器人并退出网页后台服务。之后需双击项目 scripts/start.command 重新打开；账号、记忆和记录会保留。确定关闭？")) return;
 await api("shutdown", {confirmed: true});
 serviceClosed = true;
 connectionRecovered();
 if (data) data.robot = {enabled: false};
 $("robot-state").textContent = "服务正在关闭 · 网页已停止操作";
 $("mode").textContent = "服务已关闭";
 for (const n of document.querySelectorAll("button, input, textarea, select")) n.disabled = true;
 notice("已请求彻底关闭服务，网页和自动回复将停止。账号、记忆与记录已保留；下次双击项目 scripts/start.command 重新启动。");
});
task($("robot-start"), async () => { renderRobot(await api("robot-control", {action: "start"})); notice("机器人已启动，将按评论和私信各自设置运行"); });
task($("robot-pause"), async () => { renderRobot(await api("robot-control", {action: "pause"})); notice("机器人已暂停，自动监听与AI生成已停止；已经提交到B站的消息无法撤回。网页仍可使用。"); });
task($("robot-stop"), async () => {renderRobot(await api("robot-control",{action:"stop"})); notice("机器人已关闭，自动任务停止，控制台保留。下次点击启动机器人会重新建立基线，不补发旧消息。");});
task($("robot-resume"), async () => {renderRobot(await api("robot-control",{action:"resume"})); notice("机器人已恢复，继续暂停前的进度与额度，仍受渠道开关和安全检查约束。");});
function privateResumeDisabled() {
 const r=privateData,c=r?.config;
 return !c?.enabled || !c?.autoReply || !c?.allowSend || (!r.paused && !r.readPaused && !r.readFailures);
}
function renderPrivate(r) {
 privateData = r;
 renderPlatformSummary();
 const c = r.config;
 $("private-binding").textContent = `发送账号 UID ${c.senderUid || "尚未绑定"} → 唯一收件人（大号）UID ${c.peerUid || "尚未绑定"}`;
 $("private-enabled").checked = c.enabled;
 $("private-send-enabled").checked = c.allowSend;
 $("private-auto").checked = c.autoReply;
 $("private-memory-enabled").checked = !!c.memoryEnabled;
 $("private-interval").value = c.intervalSeconds;
 $("private-state").textContent = !c.enabled ? "私信已关闭" : r.paused ? "私信AI回复已暂停："+r.reason+(c.memoryEnabled ? "；自动记忆保持开启"+(r.readPaused ? "，读取已暂停" : "") : "") : c.autoReply ? "大号私信AI自动回复已开启" : "仅手动私信";
 if(c.enabled) {
  if(!data?.robot?.enabled || r.readPaused || r.push?.mode === "disabled") $("private-state").textContent+="；通知监听已停止";
  else $("private-state").textContent += r.push?.connected ? (r.push.verified ? "；已接收有效消息通知（触发大号会话核查，低频兜底）" : "；推送已连接，尚待验证有效通知，短周期兜底保持") : r.push?.mode === "connecting" ? "；推送连接中，查询兜底保持" : "；推送未连接，按设置间隔查询兜底";
 }
 $("private-times").textContent = "上次检查："+(r.lastAt ? when(r.lastAt) : "尚未检查")+"；下次计划："+(r.nextAt ? when(r.nextAt) : "等待调度或已停止")+(data?.robot?.enabled ? "" : "；机器人总开关已暂停");
 $("private-resume").disabled=privateResumeDisabled();
 if(r.lastTriggerKey) $("private-times").textContent+="；上条新消息来源："+({push:"大号推送","push-hint":"推送提示后核查",poll:"周期扫描","connect-sync":"连接恢复同步"}[r.lastTriggerKind]||"历史未记录")+" · "+when(r.lastTriggerAt);
 if(r.push?.errorCode) $("private-times").textContent+="；推送状态："+r.push.errorCode;
 if (r.readFailures && !r.readPaused) $("private-times").textContent+=`；只读网络退避中 ${r.readFailures}/5`;
 if (r.lastErrorCode) $("private-times").textContent+="；最近错误："+({read:"会话读取",generation:"AI生成","send-preflight":"发送前核验",send:"私信发送"}[r.lastErrorStage] || "处理")+" / "+r.lastErrorCode;

 $("private-inbox").replaceChildren();
 for (const m of r.inbox) appendHistory("private-inbox",entry(m.sender === c.peerUid ? "大号" : "小龙", when(m.timestamp*1000), m.withdrawn ? "消息已撤回" : m.text || "非文字消息"));
 $("private-outbox").replaceChildren();
 for (const m of [...r.outbox].reverse()) appendHistory("private-outbox",entry((m.automatic ? "自动回复了主人" : "手动发给主人")+(m.groupId?" · 第"+(m.part+1)+"条":"")+" · "+({sent:"已发送",unknown:"结果未知，请人工核对",sending:"发送中",failed:"发送失败"}[m.status] || m.status), when(m.at), m.text));
}
async function loadPrivate() {
 if (privateBusy) return;
 privateBusy = true;
 try { renderPrivate(await api("private-status")); } finally { privateBusy = false; }
}
async function loadPrivateMemories(page = 1) {
 const query = $("private-memory-query").value.trim();
 const r = await api("private-memories",{...(query ? {query} : {}),page,pageSize:10});
 privateMemoryPage = r.page;
 $("private-memory-count").textContent = `本条B站私聊专用记忆：${r.total} 条匹配，当前第 ${r.page} 页。${r.enabled ? "自动记忆已开启" : "自动记忆已关闭，已有记录保留"}。`;
 $("private-memory-prev").disabled = r.page <= 1;
 $("private-memory-next").disabled = r.page*r.pageSize >= r.total;
 $("private-memory-items").replaceChildren();
 for (const m of r.items) {
  const sources = m.sources.map(s => `${when(s.occurredAt)} · 原消息 ${s.messageKey}`).join("；");
  const n = entry("大号陈述 · 非机器人亲眼观察",sources,m.quote);
  const forget = el("button","删除此记忆","quiet");
  forget.addEventListener("click",async () => {
   if (!window.confirm("删除这条私聊记忆？原B站消息和本机会话记录不会因此删除。")) return;
   forget.disabled = true;
   try {await api("private-memory-forget",{id:m.id});await loadPrivateMemories(1);}catch(e){forget.disabled=false;notice(e.message,true);}
  });
  n.append(forget);appendHistory("private-memory-items",n);
 }
 if (!r.items.length) $("private-memory-items").append(el("p","没有匹配的私聊记忆。首次检查只建基线，只有后续有用的新陈述才会记录。","muted"));
}
task($("private-memory-search"),async () => {await loadPrivateMemories(1);});
$("private-memory-prev").addEventListener("click",() => loadPrivateMemories(Math.max(1,privateMemoryPage-1)).catch(e=>notice(e.message,true)));
$("private-memory-next").addEventListener("click",() => loadPrivateMemories(privateMemoryPage+1).catch(e=>notice(e.message,true)));
task($("private-status"), loadPrivate);
task($("private-resume"),async () => {renderPrivate(await api("private-resume"));notice("核验已通过，已跳过现有消息。只会回复之后的新消息，旧消息不重发。");});
task($("private-refresh"), async () => { renderPrivate(await api("private-refresh")); });
task($("private-save"), async () => {
 renderPrivate(await api("private-config", {memoryEnabled:$("private-memory-enabled").checked, enabled: $("private-enabled").checked, allowSend: $("private-send-enabled").checked, autoReply: $("private-auto").checked, intervalSeconds: Number($("private-interval").value)}));
 notice("私信设置已保存，只适用于绑定大号；自动任务同时受机器人总开关控制");
});
task($("private-stop"), async () => {
 if (!privateData) return;
 const c = privateData.config;
 renderPrivate(await api("private-config", {enabled: c.enabled, allowSend: c.allowSend, autoReply: false, intervalSeconds: c.intervalSeconds}));
 notice("大号私信自动回复已关闭");
});
task($("private-send"), async () => {
 const text = $("private-text").value.trim();
 if (!text) throw new Error("请输入私信内容");
 if (!privateNonce || privateNonce.text !== text) privateNonce = {text, requestId: crypto.randomUUID()};
 const out = await api("private-send", privateNonce);
 notice(out.status === "sent" ? "已发给绑定大号" : "发送结果："+out.status+"，请先核对，不要重发");
 await loadPrivate();
});
async function loadAuto() {
  if (autoBusy) return;
  autoBusy = true;
  try { renderAuto(await api("auto-status")); } finally { autoBusy = false; }
}
task($("auto-save"), async () => {
  const r = await api("auto-config", {
    enabled: $("auto-enabled").checked,
    accountUid: $("auto-account").value.trim(), priorityUid: $("auto-priority").value.trim(),
    source:"browsing", videos: [], intervalSeconds: Number($("auto-interval").value),
    otherDailyLimit: Number($("auto-other-limit").value), perUserDailyLimit: Number($("auto-user-limit").value),
  });
  renderAuto(r); notice(r.config.enabled ? "回复策略已保存，将跟随浏览范围处理新评论；浏览开关独立控制" : "自动回复已关闭");
});
task($("auto-stop"), async () => {
  if (!autoData) return;
  renderAuto(await api("auto-config", { ...autoData.config, enabled: false }));
  notice("自动回复已停止；已经开始的发送请按账本回查，不自动重发");
});
task($("auto-refresh"), loadAuto);
task($("auto-scan"), async () => { renderAuto(await api("auto-scan")); });
task($("auto-preview"), async () => {
  const r = await api("auto-preview", { text: $("auto-preview-text").value });
  $("auto-preview-result").textContent = (r.allowed ? "通过筛选，实际回复由AI生成" : "不回复") + "；原因：" + r.reason + "；本次预览模型Token：0";
});

async function initializeSession() {
  const r = await fetch("/session", {signal: AbortSignal.timeout(8000)});
  const j = await r.json();
  if (!r.ok || typeof j.csrf !== "string" || !j.csrf) throw new Error(j.error?.message || "无法恢复本机会话，请刷新页面");
  csrf = j.csrf;
  // 兼容旧书签，只清理旧片段；新入口不再携带登录密钥。
  if (new URLSearchParams(location.hash.slice(1)).has("key")) history.replaceState(null, "", location.pathname);
}
try {
  await initializeSession();
  await refresh();
  const initialRoute=routeFromHash();
  await navigate(initialRoute.tab,initialRoute.platform,false);
} catch (e) {
  const message = e.name === "TimeoutError" || e.name === "AbortError"
    ? "连接工作台超时"
    : e instanceof TypeError ? "无法连接本地工作台" : e.message || "读取机器人状态失败";
  connectionFailed(message);
}

task($("import-session"), async () => {
  const cookie = $("import-cookie").value;
  $("import-cookie").value = "";
  const a = await api("import-session", {
    cookie,
    expectedUid: $("expected-uid").value.trim(),
  });
  await refresh();
  notice("已验证账号：" + a.name + " · " + a.uid);
});

// 修改表单不改变顶部已生效配置；仅提示有待应用的选择。
for (const field of document.querySelectorAll("#models select, #models input")) {
  field.addEventListener("input", () => { $("model-draft-state").textContent = "有待应用的配置 · 当前连接保持生效"; });
  field.addEventListener("change", () => { $("model-draft-state").textContent = "有待应用的配置 · 当前连接保持生效"; });
}

for (const button of document.querySelectorAll("[data-provider-target]")) {
  button.addEventListener("click", () => {
    $(button.dataset.providerTarget).value = button.dataset.providerValue;
    $("model-connect-result").textContent = "";
    $("model-draft-state").textContent = "有待应用的配置 · 当前连接保持生效";
    renderModelSelection();
  });
}

function renderGenerationSafety(r) {
 for(const [id,key] of [["owner-calls","ownerDailyCalls"],["owner-tokens","ownerDailyTokens"],["other-calls","otherDailyCalls"],["other-tokens","otherDailyTokens"]]) { $("safety-"+id).value=r.config[key];$("safety-"+id).disabled=false; }
 for(const group of ["owner","other"]) {const u=r[group];$("safety-"+group+"-usage").textContent=`${r.day} · 已调用 ${u.calls} 次 · 已知 ${u.tokens} Token · ${u.unknown} 次用量未知 · ${u.pending} 次处理中`;}
 $("safety-save").disabled=false;
}
async function loadGenerationSafety() { renderGenerationSafety(await api("generation-safety-status")); }
task($("safety-refresh"),loadGenerationSafety);
task($("safety-save"),async()=>{for(const id of ["safety-owner-calls","safety-owner-tokens","safety-other-calls","safety-other-tokens"]){if(String($(id).value).trim()===""||!Number.isSafeInteger(Number($(id).value)))throw new Error("请填写完整的整数预算；主人填写0才关闭该项限制。");}const config={ownerDailyCalls:Number($("safety-owner-calls").value),ownerDailyTokens:Number($("safety-owner-tokens").value),otherDailyCalls:Number($("safety-other-calls").value),otherDailyTokens:Number($("safety-other-tokens").value)};renderGenerationSafety(await api("generation-safety-config",config));$("safety-state").textContent="预算已保存，今日用量保留。不会自动恢复渠道暂停。";});

function appendHistory(target,...records){
 for(const record of records){record.dataset.historyScope=target;record.open=!!historyViews[target];$(target).append(record);}
}
function setHistoryView(target,detailed){
 historyViews[target]=detailed;
 for(const n of document.querySelectorAll("details[data-history-scope]"))if(n.dataset.historyScope===target)n.open=detailed;
 for(const b of document.querySelectorAll("[data-history-target]"))if(b.dataset.historyTarget===target){const selected=(b.dataset.historyView==="detailed")===detailed;b.classList.toggle("active",selected);b.ariaPressed=String(selected);}
 if(target==="usage-summary-list"){$("usage-summary-list").hidden=detailed;$("usage-detail-table").hidden=!detailed;}
}
for(const b of document.querySelectorAll("[data-history-target]"))b.addEventListener("click",()=>setHistoryView(b.dataset.historyTarget,b.dataset.historyView==="detailed"));
function renderBrowsing(r){
 browsingData=r;const c=r.config;$("browse-enabled").checked=c.enabled;$("browse-mode-persona").checked=c.mode==="persona";$("browse-mode-custom").checked=c.mode==="custom";$("browse-custom-fields").hidden=c.mode!=="custom";
 $("browse-keywords").value=(c.keywords||[]).join("、");$("browse-excluded").value=(c.excluded||[]).join("、");$("browse-interval").value=c.intervalSeconds;$("browse-daily").value=c.dailyLimit;$("browse-max-duration").value=c.maxDurationMinutes??10;
 $("browse-state").textContent=!c.enabled?"自主浏览已关闭":r.paused?"浏览已暂停："+r.reason:"自主浏览已开启 · 只读资料";
 $("browse-effective").textContent="当前兴趣来源："+(c.mode==="persona"?"角色人设与公开兴趣":"自定义关键词")+"；实际主题："+((r.effectiveKeywords||[]).join("、")||"暂无可用主题，请检查排除词或改用自定义");
 $("browse-usage").textContent=`今天已检查 ${r.attempts} / ${c.dailyLimit} 次；下次：${c.enabled&&!r.paused&&r.nextAt?when(r.nextAt):"等待开启或调度"}`;
 $("browse-read").disabled=!c.enabled||r.paused||!data?.robot?.enabled||r.attempts>=c.dailyLimit;
 $("browse-records").replaceChildren();for(const v of r.records){const n=entry("浏览了《"+v.title+"》",when(v.at)+" · 兴趣："+v.keyword,v.description||v.preview);n.append(el("p",v.preview||"未读取到可展示的评论"),el("p",`读取了标题、简介和 ${v.commentCount} 条评论；未看画面/音轨/字幕。来源 ${v.eventId}`),link(v.bvid));appendHistory("browse-records",n);}if(!r.records.length)$("browse-records").append(el("p","还没有浏览记录。保存偏好不会伪造浏览经历。","muted"));
}
async function loadBrowsing(){if(browsingBusy)return;browsingBusy=true;try{renderBrowsing(await api("browsing-status"))}finally{browsingBusy=false}}
for(const id of ["browse-mode-persona","browse-mode-custom"])$(id).addEventListener("change",()=>{$("browse-custom-fields").hidden=!$("browse-mode-custom").checked;});
for(const b of document.querySelectorAll("[data-browse-keyword]"))b.addEventListener("click",()=>{const words=$("browse-keywords").value.split(/[、，,\n]/).map(x=>x.trim()).filter(Boolean);if(!words.includes(b.dataset.browseKeyword))words.push(b.dataset.browseKeyword);$("browse-keywords").value=words.slice(0,10).join("、");});
function browseWords(id){return $(id).value.split(/[、，,\n]/).map(x=>x.trim()).filter(Boolean);}
task($("browse-save"),async()=>{renderBrowsing(await api("browsing-config",{enabled:$("browse-enabled").checked,mode:$("browse-mode-custom").checked?"custom":"persona",keywords:browseWords("browse-keywords"),excluded:browseWords("browse-excluded"),intervalSeconds:Number($("browse-interval").value),dailyLimit:Number($("browse-daily").value),maxDurationMinutes:Number($("browse-max-duration").value)}));notice("浏览偏好已保存，回复开关保持原设置。");});
task($("browse-refresh"),loadBrowsing);
task($("browse-read"),async()=>renderBrowsing(await api("browsing-read")));

function appendOperationDetails(details,op,includeContent=true){
 if(includeContent&&op.activityTitle)details.append(el("h4",op.activityTitle));
 if(includeContent&&op.activityText)details.append(el("p",op.activityText));
      details.append(el("p", "操作 ID：" + op.id));
      if (op.usageThread) details.append(el("p", `业务聊天 ${op.usageThread} · 轮次 ${op.usageTurn} · 汇总仅计一次`));
      if (op.reference) details.append(el("p", "关联记录：" + op.reference));
      details.append(el("p", op.source === "history" ? "历史耗时未记录" : "耗时：" + op.durationMs + " ms"));
      if (op.timings) {
        const t = op.timings;
        details.append(el("p", `分段耗时：生成 ${t.generationMs === null ? "未执行" : t.generationMs + " ms"}；账号核验 ${t.accountCheckMs === null ? "未执行" : t.accountCheckMs + " ms"}（${t.accountAttempts}次，含退避）；发送 ${t.sendMs === null ? "未执行" : t.sendMs + " ms"}`));
      } else if (op.action === "private-auto-reply") {
        details.append(el("p", "分段耗时未记录"));
      }
      for (const u of op.usage) details.append(el("p", `${u.provider} / ${u.model} · 请求 ${u.requestId} · 输入 ${u.input}（缓存 ${u.cachedInput ?? "未获取"}）/ 输出 ${u.output}（推理 ${u.reasoningOutput ?? "未获取"}）`));
}

function renderReplyPage(){for(const name of ["settings","history","rules"])$("reply-"+name+"-page").hidden=replyPage!==name;for(const b of document.querySelectorAll("[data-reply-page]")){const selected=b.dataset.replyPage===replyPage;b.classList.toggle("active",selected);b.ariaCurrent=selected?"page":"false";}}

function renderConversation(r){
 const c=r.config;
 if(!c)throw new Error("会话配置读取失败，未应用默认值");
 $("conversation-enabled").checked=c.enabled;
 for(const [id,value] of [["turns",c.maxTurns],["idle",c.idleSeconds/60],["age",c.maxAgeSeconds/60],["context",c.contextTokens],["total",c.totalTokens]])$("conversation-"+id).value=value;
 for(const id of ["enabled","turns","idle","age","context","total","save"])$("conversation-"+id).disabled=false;
 const windows=r.windows||[];
 $("conversation-state").textContent=(c.enabled?"有限多轮已开启":"每次回复独立会话")+" · 当前驻留 "+windows.length+" 个窗口"+(windows.length?" · "+windows.map(w=>w.pending?"正在生成或等待发送确认":w.turns+"轮 / 已知 "+w.tokens+" Token").join("；"):" · 有新消息时才创建，不会主动调用模型");
}
async function loadConversation(){renderConversation(await api("conversation-status"));}
task($("conversation-refresh"),loadConversation);
task($("conversation-save"),async()=>{renderConversation(await api("conversation-config",{enabled:$("conversation-enabled").checked,maxTurns:Number($("conversation-turns").value),idleSeconds:Number($("conversation-idle").value)*60,maxAgeSeconds:Number($("conversation-age").value)*60,contextTokens:Number($("conversation-context").value),totalTokens:Number($("conversation-total").value)}));notice("会话策略已保存，后续消息使用新窗口；未调用模型。");});
for(const b of document.querySelectorAll("[data-reply-page]"))b.addEventListener("click",()=>{replyPage=b.dataset.replyPage;navigate("auto","bilibili").catch(navigationError);});
