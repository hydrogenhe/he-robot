import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { runInNewContext } from "node:vm";
// 执行实际页面脚本，模拟 DOM 和 HTTP；不启动浏览器或访问平台。
test("网页复用动作结果，扫码完成后不再重复轮询", async () => {
    const nodes = new Map();
    const created = [];
    const requests = [];
    function node(text = "") {
        const n = {
            textContent: text, value: "", hidden: false, dataset: {}, children: [],
            classList: { toggle() { } }, listeners: {},
            append(...items) { this.children.push(...items); },
            replaceChildren() { this.children = []; },
            addEventListener(name, fn) { this.listeners[name] = fn; },
        };
        created.push(n);
        return n;
    }
    function get(id) {
        if (!nodes.has(id))
            nodes.set(id, node());
        return nodes.get(id);
    }
    const pending = {
        id: "pending", status: "pending", createdAt: new Date().toISOString(),
        bvid: "BV161nZ6JE6b", account: "123", parent: "0", root: "0", text: "待审",
    };
    const visible = { ...pending, id: "visible", status: "restricted", rpid: "999", platformState: 17 };
    const overview = {
        permissions: { enabled: false, allowedVideos: [], allowedUsers: [], blockedUsers: [] },
        persona: "人设", account: null, drafts: [pending, visible], events: [], notes: [],
    };
    const fetch = async (url, input) => {
        const action = url.split("/").at(-1);
        requests.push(action);
        const result = action === "shutdown" ? {shuttingDown:true} : action === "character-status" ? {description: "当前情绪：开心",traits:{warmth:51,curiosity:50,playfulness:49},changes:[]}
          : action === "robot-control" ? {enabled: ["start","resume"].includes(JSON.parse(input.body).action),status:({start:"running",resume:"running",pause:"paused",stop:"stopped"})[JSON.parse(input.body).action]}
          : action === "private-memories" ? {enabled:true,total:1,page:1,pageSize:10,items:[{id:"memory-id",kind:"user_statement",quote:"我养了一盆测试植物",sources:[{messageKey:"101",occurredAt:new Date().toISOString()}]}]}
          : action === "private-status" || action === "private-config" || action === "private-resume" ? {config: {enabled: true,allowSend: true,autoReply: true,memoryEnabled:true,senderUid: "123",peerUid: "789",intervalSeconds:120},paused:false,inbox:[],outbox:[]}
          : action === "auto-status" || action === "auto-config" || action === "auto-scan" ? {
            config: { enabled: false, accountUid: "123", priorityUid: "789", videos: ["BV161nZ6JE6b"], intervalSeconds: 300, otherDailyLimit: 20, perUserDailyLimit: 3 },
            paused: false, otherUsed: 0, userUsed: {}, entries: [],
        } : action === "auto-preview" ? { reply: "", reason: "数学或复杂任务，跳过", modelTokens: 0 }
          : action === "operations" ? {
            items: [
                { id: "op1", at: new Date().toISOString(), platform: "bilibili", account: "123", action: "private-auto-reply", source: "automation", status: "succeeded", durationMs: 10, tokens: null, usage: [] },
                { id: "op2", at: new Date().toISOString(), platform: "qq", account: "456", action: "reply", source: "mcp", status: "succeeded", durationMs: 15, timings: {generationMs: 0, accountCheckMs: 12, sendMs: null, accountAttempts: 1}, tokens: 120, usage: [{ provider: "test", model: "test", requestId: "test", input: 100, output: 20, cachedInput: 40, reasoningOutput: 5, source: "codex_local", relatedOperations: ["op1", "op2"] }] },
            ], total: 2, reported: 1, input: 100, output: 20, cachedInput: 40, page: 1, pageSize: 30,
            platforms: ["bilibili", "qq"], actions: ["comments", "reply"],
        } : action === "session" ? { csrf: "test" }
            : action === "overview" ? overview
                : action === "account" || action === "poll" ? { code: 0, loggedIn: true, uid: "123", name: "测试" }
                    : action === "qr" ? { image: "test" }
                        : { ...(JSON.parse(input.body).id === "pending" ? pending : visible), status: action === "cancel" ? "cancelled" : "visible" };
        return { ok: true, json: async () => result };
    };
    const source = await readFile("web/app.js", "utf8");
    let confirmExit = false;
 const context = {
 window: {confirm: () => confirmExit},
        document: { getElementById: get, querySelectorAll: () => [], createElement: () => node() },
        fetch, AbortSignal, URLSearchParams, location: { hash: "" }, history: {},
    };
    await runInNewContext("(async () => {" + source + "})()", context);
    assert.ok(created.some((n) => n.textContent === "展示受限 · 仅自己可见（state=17）"));
    await get("check-account").listeners.click();
    const cancel = created.findLast((n) => n.textContent === "取消草稿");
    await cancel.listeners.click();
    const check = created.findLast((n) => n.textContent === "回查评论");
    await check.listeners.click();
    await get("get-qr").listeners.click();
    await get("poll-qr").listeners.click();
    await get("poll-qr").listeners.click();
    assert.equal(requests.filter((a) => a === "overview").length, 1);
    assert.equal(requests.filter((a) => a === "poll").length, 1);
    assert.equal(requests.filter((a) => a === "cancel").length, 1);
    assert.equal(requests.filter((a) => a === "recheck").length, 1);
    assert.match(get("account-state").textContent, /123/);
    assert.equal(requests.filter((a) => a === "operations").length, 0, "用量页不预取或轮询");
    get("usage-platform").value = "qq";
    get("usage-reported").value = "false";
    await get("usage-query").listeners.click();
    assert.equal(requests.filter((a) => a === "operations").length, 1);
    assert.equal(get("usage-tokens").textContent, "120");
    assert.equal(get("usage-coverage").textContent, "1 / 2");
    assert.ok(created.some((n) => n.textContent === "未获取"));
    assert.ok(created.some((n) => n.textContent === "QQ"));
    assert.ok(created.some((n) => n.textContent === "Codex 实测 · 共享轮次总量"));
    assert.ok(created.some((n) => n.textContent === "分段耗时未记录"));
    assert.ok(created.some((n) => n.textContent === "分段耗时：生成 0 ms；账号核验 12 ms（1次，含退避）；发送 未执行"));
    assert.equal(get("usage-next").disabled, true);
    await get("auto-refresh").listeners.click();
    assert.equal(get("auto-other-limit").value, 20);
    assert.equal(get("auto-user-limit").value, 3);
    get("auto-preview-text").value = "算2+2";
    await get("auto-preview").listeners.click();
    assert.match(get("auto-preview-result").textContent, /不回复.*Token：0/);
    assert.equal(requests.filter((a) => a === "auto-status").length, 1);
    assert.equal(requests.filter((a) => a === "auto-preview").length, 1);
    assert.equal(requests.filter((a) => a === "auto-scan").length, 0, "不隐式触发扫描或发送");
    await get("robot-start").listeners.click();
    assert.match(get("robot-state").textContent,/运行中/);
    await get("robot-pause").listeners.click();
    assert.match(get("robot-state").textContent,/已暂停/);
    assert.equal(get("robot-resume").disabled,false);
    await get("robot-resume").listeners.click();
    assert.match(get("robot-state").textContent,/运行中/);
    await get("robot-stop").listeners.click();
    assert.match(get("robot-state").textContent,/已关闭/);
    assert.equal(get("robot-start").disabled,false);
    await get("robot-start").listeners.click();
    await get("character-refresh").listeners.click();
    assert.match(get("character-state").textContent,/开心/);
    assert.equal(requests.filter(a => a === "character-status").length,1,"成长查询只按需执行");
    await get("private-status").listeners.click();
    assert.match(get("private-binding").textContent,/789/);
    assert.equal(get("private-auto").checked,true);
    assert.equal(get("private-resume").disabled,true,"健康状态不显示可恢复按钮");

    assert.equal(get("private-memory-enabled").checked,true);
    assert.equal(requests.filter(a=>a==="private-memories").length,0,"打开私信页不自动查询全部记忆");
    await get("private-memory-search").listeners.click();
    assert.equal(requests.filter(a=>a==="private-memories").length,1);
    assert.match(get("private-memory-count").textContent,/1 条匹配/);

    assert.equal(requests.filter(a => a === "private-refresh" || a === "private-send").length,0,"仅打开状态不读取平台或发送私信");
    await get("service-shutdown").listeners.click();
    assert.equal(requests.filter(a => a === "shutdown").length,0,"取消退出不能关闭服务");
    confirmExit = true;
    await get("service-shutdown").listeners.click();
    assert.equal(requests.filter(a => a === "shutdown").length,1);
    assert.equal(get("service-shutdown").disabled,true);
    assert.match(get("notice").textContent,/start.command/);

});
