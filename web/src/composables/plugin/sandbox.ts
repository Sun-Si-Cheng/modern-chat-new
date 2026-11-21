/**
 * 插件沙箱引导文档（内核模块）。
 *
 * 每个启用的插件一个 sandbox iframe（allow-scripts，无 same-origin），srcdoc
 * 内注入引导脚本 + 插件代码。这里只负责"沙箱长什么样"：CSP 收敛、SDK 注入、
 * 插件代码的执行作用域；消息协议与运行时编排在 usePluginHost。
 */

/**
 * 构造沙箱 CSP：把插件自身网络出口收敛到 manifest 的 allowed_hosts。
 * 之前插件可以直接在自己的 iframe 里 fetch()，完全绕过宿主 request() 的白名单；
 * 有了 connect-src 后 fetch/XHR/WebSocket/beacon 只能到达这些主机，未声明则全部禁止。
 * 注意不能加 allow-same-origin：插件拿不到父页面 localStorage/JWT 是硬约束。
 */
function buildSandboxCsp(allowedHosts: string[]): string {
  const hosts = allowedHosts
    .map((h) => h.trim().toLowerCase())
    // 只接受主机名或 *.子域形式，避免把端口/路径/引号拼进 CSP 指令
    .filter((h) => /^(\*\.)?[a-z0-9][a-z0-9.-]*$/.test(h))
  const connect = hosts.length ? hosts.join(' ') : "'none'"
  // 媒体能力（通用，无音乐等业务语义）：插件可在自己的沙箱文档里用 <audio>/<video>
  // 播放其 allowedHosts 内的媒体（音频/播客等），blob: 支持先 fetch 缓冲再播。
  // 与 connect-src 同一信任边界——都来自插件安装时声明的 allowedHosts，不新增决策点。
  // 沙箱无 same-origin，读不到跨域媒体字节（不可缓存/解析波形），只能顺序播放。
  const media = hosts.length ? `${connect} blob:` : "'none'"
  return [
    "default-src 'none'",
    "script-src 'unsafe-inline'",
    "style-src 'unsafe-inline'",
    'img-src data:',
    `connect-src ${connect}`,
    `media-src ${media}`,
    "base-uri 'none'",
    "form-action 'none'",
    "frame-src 'none'",
    "object-src 'none'",
    "worker-src 'none'",
  ].join('; ')
}

export function bootHtml(pluginCode: string, allowedHosts: string[], token: string): string {
  // `</script>` 与 `<script` 都必须转义：否则插件代码里的字符串可在 HTML 解析阶段
  // 提前闭合引导脚本，或进入 script-data double-escaped 状态
  const safeCode = pluginCode
    .replace(/<\/script/gi, '<\\/script')
    .replace(/<script/gi, '<\\script')
  const csp = buildSandboxCsp(allowedHosts)
  return `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${csp}"></head><body><script>
"use strict";
(function () {
  var handlers = {}, callSeq = 0, pending = new Map(), renderers = new Map();
  // 文档身份令牌：只有本引导文档知道（跨文档不可读）。沙箱每条出站消息都带回 tk，
  // 宿主据此拒收「自导航后的陌生文档」发的消息（详见 mountRuntime）
  var TK = "${token}";
  function post(m) { m.tk = TK; parent.postMessage(m, "*"); }
  window.addEventListener("message", function (ev) {
    var m = ev.data;
    if (!m || typeof m !== "object" || !m.__mc) return;
    // 宿主在 iframe 每次 load 后都会 ping；带 token 的 pong 证明原文档仍在位
    if (m.__mc === "ping") { post({ __mc: "pong" }); return; }
    if (m.__mc === "event") {
      (handlers[m.event] || []).forEach(function (fn) {
        try { fn(m.payload); } catch (e) { console.error("[plugin] handler error", e); }
      });
    } else if (m.__mc === "render") {
      var r = renderers.get(m.msgType);
      if (!r) { post({ __mc: "renderResult", callId: m.callId, error: "no renderer for " + m.msgType }); return; }
      Promise.resolve().then(function () { return r(m.payload); }).then(
        function (desc) { post({ __mc: "renderResult", callId: m.callId, desc: desc }); },
        function (e) { post({ __mc: "renderResult", callId: m.callId, error: String((e && e.message) || e) }) }
      );
    } else if (m.__mc === "result") {
      var p = pending.get(m.callId);
      if (p) { pending.delete(m.callId); if (m.ok) p.resolve(m.data); else p.reject(new Error(m.error || "plugin api error")); }
    }
  });
  function call(name, args) {
    return new Promise(function (resolve, reject) {
      var callId = ++callSeq;
      pending.set(callId, { resolve: resolve, reject: reject });
      post({ __mc: "call", callId: callId, api: name, args: args });
    });
  }
  var sdk = {
    on: function (ev, fn) { (handlers[ev] || (handlers[ev] = [])).push(fn); },
    send: function (convId, payload, msgType) { return call("send", [convId, payload, msgType]); },
    registerMessageRenderer: function (type, fn) { renderers.set(type, fn); return call("registerMessageRenderer", [type]); },
    addSidePanel: function (desc) { return call("addSidePanel", [desc]); },
    // 右键菜单动作：desc = { id, label }，触发时收到 on('message:action', { actionId, message })
    registerMessageAction: function (desc) { return call("registerMessageAction", [desc]); },
    // 当前用户身份（需 conv:read）：只含 id / 昵称
    me: function () { return call("me", []); },
    // 当前会话只读数据（需 conv:read）
    conv: {
      current: function () { return call("conv.current", []); },
      members: function () { return call("conv.members", []); }
    },
    messages: {
      recent: function (n) { return call("messages.recent", [n]); }
    },
    // 宿主侧动作（需 ui:host）
    nav: {
      openConversation: function (id) { return call("nav.openConversation", [id]); },
      jumpToMessage: function (id) { return call("nav.jumpToMessage", [id]); }
    },
    toast: function (text, kind) { return call("toast", [text, kind]); },
    storage: {
      get: function (k) { return call("storage.get", [k]); },
      set: function (k, v) { return call("storage.set", [k, v]); }
    },
    request: function (url, opts) { return call("request", [url, opts || {}]); },
    theme: { apply: function (vars) { return call("theme.apply", [vars]); } }
  };
  window.__mcActivate = null;
  // 插件代码放进独立函数作用域执行。
  // 关键：不能与引导器同一个作用域 —— 插件顶层的 var/function 声明会与引导器
  // 同名变量互相覆盖（插件写 var api = null 就把引导器的 sdk 顶掉，
  // __mcActivate 收到 null 并抛 "Cannot read properties of null (reading 'on')"）。
  // 也不能用 new Function：沙箱 CSP 只开 script-src 'unsafe-inline'，没有 'unsafe-eval'。
  (function () {
${safeCode}
    // 两种入口写法都支持：顶层 function __mcActivate，或挂到 window 上
    var entry = typeof __mcActivate === "function" ? __mcActivate : null;
    if (!entry && typeof window.__mcActivate === "function") entry = window.__mcActivate;
    if (!entry) { post({ __mc: "ready", error: "missing __mcActivate" }); return; }
    try { entry(sdk); post({ __mc: "ready" }); }
    catch (e) { console.error("[plugin] activate error", e); post({ __mc: "ready", error: String((e && e.message) || e) }); }
  })();
})();
</script></body></html>`
}
