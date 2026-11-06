/**
 * 插件宿主（§8 方案 A：描述式 UI）
 *
 * 运行模型：每个启用的插件一个 sandbox iframe（allow-scripts，无 same-origin），
 * srcdoc 内注入 SDK + 插件代码。插件通过 postMessage 与本桥接层通信：
 *  - 沙箱 -> 主站：{ __mc: 'call', callId, api, args } / { __mc: 'renderResult', callId, desc }
 *  - 主站 -> 沙箱：{ __mc: 'result', callId, ok, data } / { __mc: 'event', event, payload } / { __mc: 'render', callId, msgType, payload }
 *
 * 插件入口约定：代码中定义 `function __mcActivate(api) { ... }`，由引导器注入 api 后调用。
 * 所有 UI 一律以描述 JSON 表达，主站仅用白名单组件渲染；桥接层按 manifest 权限与
 * allowed_hosts 双重白名单放行，插件拿不到 JWT、碰不到主站 window 与其他插件。
 */
import { reactive, readonly } from 'vue'

import { MessagePlugin } from 'tdesign-vue-next'

import type { Plugin, PanelDesc, ServerFrame, UIDescNode } from '@/types/proto'
import { convApi, pluginApi } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { useConversationsStore } from '@/stores/conversations'
import { usePluginsStore } from '@/stores/plugins'
import { useWsStore } from '@/stores/ws'

// ===== 白名单校验 =====

const ALLOWED_COMPONENTS = new Set([
  'text',
  'image',
  'button',
  'input',
  'card',
  'list',
  'divider',
  'link',
  // 数据展示类：表格 / 进度条 / 徽标，让面板能承载统计与状态，而不只是一段文字
  'table',
  'progress',
  'badge',
])

/** 校验 UI 描述：白名单外组件、javascript: 链接、非 plain props 一律拒绝 */
export function validateUIDesc(node: unknown, depth = 0): UIDescNode | null {
  if (depth > 8 || !node || typeof node !== 'object') return null
  const n = node as Record<string, unknown>
  if (typeof n.component !== 'string' || !ALLOWED_COMPONENTS.has(n.component)) return null
  if (!n.props || typeof n.props !== 'object') return null
  const props: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(n.props as Record<string, unknown>)) {
    // 仅放行原始值，杜绝对象/函数注入面
    if (['string', 'number', 'boolean'].includes(typeof v) || v === null) props[k] = v
  }
  if (n.component === 'link' && typeof props.href === 'string') {
    if (!/^https?:\/\//i.test(props.href)) return null
  }
  if (n.component === 'image' && typeof props.src === 'string') {
    if (!/^https?:\/\//i.test(props.src)) return null
  }
  const children = Array.isArray(n.children)
    ? n.children.map((c) => validateUIDesc(c, depth + 1)).filter((c): c is UIDescNode => !!c)
    : undefined
  return { component: n.component as UIDescNode['component'], props, action: typeof n.action === 'string' ? n.action : undefined, children }
}

/**
 * 校验面板描述：{ title?, body: UIDescNode[] }。
 * 面板对象本身没有 component 字段，不能直接交给 validateUIDesc（会被判 null）；
 * 这里校验外层形状后逐节点校验，任一节点非法则整体拒绝（保持严格的安全边界）。
 */
export function validatePanel(desc: unknown): PanelDesc | null {
  if (!desc || typeof desc !== 'object') return null
  const d = desc as Record<string, unknown>
  if (d.title !== undefined && typeof d.title !== 'string') return null
  if (!Array.isArray(d.body)) return null
  const body: UIDescNode[] = []
  for (const node of d.body) {
    const v = validateUIDesc(node)
    if (!v) return null
    body.push(v)
  }
  return { kind: 'panel', title: typeof d.title === 'string' ? d.title : '', body }
}

/** 渲染结果：面板形状（有 body 数组）走 validatePanel，单节点则包成只含一个节点的面板 */
function validateRenderDesc(desc: unknown): PanelDesc | null {
  if (!desc || typeof desc !== 'object') return null
  if (Array.isArray((desc as Record<string, unknown>).body)) return validatePanel(desc)
  const node = validateUIDesc(desc)
  return node ? { kind: 'panel', title: '', body: [node] } : null
}

// ===== allowed_hosts 匹配 =====

function hostAllowed(hostname: string, patterns: string[]): boolean {
  return patterns.some((p) => {
    const pat = p.trim().toLowerCase()
    if (!pat) return false
    if (pat.startsWith('*.')) {
      const suffix = pat.slice(1) // ".example.com"
      return hostname === pat.slice(2) || hostname.endsWith(suffix)
    }
    return hostname === pat
  })
}

// ===== 权限映射（§8.3） =====

/** 只含定位信息的消息摘要：未声明 message:read 的插件只能拿到这些 */
function messageMeta(message: unknown): Record<string, unknown> | null {
  if (!message || typeof message !== 'object') return null
  const m = message as Record<string, unknown>
  return {
    id: m.id,
    conversationId: m.conversationId,
    senderId: m.senderId,
    msgType: m.msgType,
    createdAt: m.createdAt,
    isRecalled: m.isRecalled,
  }
}

const API_PERMISSION: Record<string, string> = {
  on: 'message:read',
  send: 'message:send',
  registerMessageRenderer: 'message:render',
  addSidePanel: 'ui:panel',
  'storage.get': 'storage',
  'storage.set': 'storage',
  request: 'net',
  'theme.apply': 'ui:theme',
  // 扩展点（§8.3）
  registerMessageAction: 'ui:actions',
  me: 'conv:read',
  'conv.current': 'conv:read',
  'conv.members': 'conv:read',
  'messages.recent': 'conv:read',
  'nav.openConversation': 'ui:host',
  'nav.jumpToMessage': 'ui:host',
  toast: 'ui:host',
}

/** 插件注册的消息动作（右键菜单项） */
export interface PluginMessageAction {
  pluginId: number
  pluginName: string
  id: string
  label: string
}

// ===== 沙箱引导 HTML =====

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
  return [
    "default-src 'none'",
    "script-src 'unsafe-inline'",
    "style-src 'unsafe-inline'",
    'img-src data:',
    `connect-src ${connect}`,
    "base-uri 'none'",
    "form-action 'none'",
    "frame-src 'none'",
    "object-src 'none'",
    "worker-src 'none'",
  ].join('; ')
}

function bootHtml(pluginCode: string, allowedHosts: string[]): string {
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
  function post(m) { parent.postMessage(m, "*"); }
  window.addEventListener("message", function (ev) {
    var m = ev.data;
    if (!m || typeof m !== "object" || !m.__mc) return;
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
  var api = {
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
${safeCode}
  if (typeof __mcActivate === "function") {
    try { __mcActivate(api); post({ __mc: "ready" }); }
    catch (e) { console.error("[plugin] activate error", e); post({ __mc: "ready", error: String((e && e.message) || e) }); }
  } else {
    post({ __mc: "ready", error: "missing __mcActivate" });
  }
})();
</script></body></html>`
}

// ===== 运行时 =====

interface Runtime {
  plugin: Plugin
  iframe: HTMLIFrameElement
  rendererTypes: Set<string>
  renderSeq: number
  pendingRenders: Map<number, { resolve: (d: PanelDesc) => void; reject: (e: Error) => void }>
  /** window message 监听器：卸载运行时必须解绑，否则泄漏 */
  onMessage: (ev: MessageEvent) => void
  /** 插件通过 theme.apply 改过的 --td-* 变量及其原值：停用时要还原 */
  themeVars: Map<string, string>
}

interface PluginHostState {
  started: boolean
  panels: Record<number, PanelDesc> // pluginId -> panel desc
  /** pluginId -> 该插件注册的右键菜单动作 */
  actions: Record<number, Array<{ id: string; label: string }>>
}

function storageKey(pluginId: number, key: string) {
  return `mc_plug_${pluginId}_${key}`
}

function createHost() {
  const state = reactive<PluginHostState>({ started: false, panels: {}, actions: {} })
  const runtimes = new Map<number, Runtime>()
  let offFrame: (() => void) | null = null

  // ----- 消息桥 -----

  function handleSandboxMessage(runtime: Runtime, ev: MessageEvent) {
    if (ev.source !== runtime.iframe.contentWindow) return
    const m = ev.data as Record<string, unknown>
    if (!m || typeof m !== 'object' || !m.__mc) return

    if (m.__mc === 'call') {
      void executeCall(runtime, m.callId as number, m.api as string, (m.args as unknown[]) ?? [])
    } else if (m.__mc === 'renderResult') {
      const p = runtime.pendingRenders.get(m.callId as number)
      if (!p) return
      runtime.pendingRenders.delete(m.callId as number)
      if (m.error) p.reject(new Error(String(m.error)))
      else {
        const panel = validateRenderDesc(m.desc)
        if (panel) p.resolve(panel)
        else p.reject(new Error('invalid ui desc'))
      }
    } else if (m.__mc === 'ready') {
      if (m.error) console.warn(`[plugin:${runtime.plugin.name}] boot warning:`, m.error)
    }
  }

  function reply(runtime: Runtime, callId: number, ok: boolean, data?: unknown, error?: string) {
    runtime.iframe.contentWindow?.postMessage({ __mc: 'result', callId, ok, data, error }, '*')
  }

  async function executeCall(runtime: Runtime, callId: number, apiName: string, args: unknown[]) {
    const perm = API_PERMISSION[apiName]
    if (!perm || !runtime.plugin.permissions.includes(perm)) {
      reply(runtime, callId, false, undefined, `permission denied: ${apiName}`)
      return
    }
    try {
      switch (apiName) {
        case 'on':
          reply(runtime, callId, true) // 事件订阅在沙箱侧，主站无操作
          break
        case 'send': {
          const [convId, payload, msgType] = args as [number, unknown, string | undefined]
          const content = typeof payload === 'string' ? payload : JSON.stringify(payload)
          useWsStore().send({
            type: 'chat',
            convId: Number(convId),
            clientMsgId: crypto.randomUUID(),
            content,
            msgType: msgType || 'text',
          })
          reply(runtime, callId, true)
          break
        }
        case 'registerMessageRenderer': {
          runtime.rendererTypes.add(String(args[0]))
          reply(runtime, callId, true)
          break
        }
        case 'addSidePanel': {
          const panel = validatePanel(args[0])
          if (!panel) {
            reply(runtime, callId, false, undefined, 'invalid panel desc')
            return
          }
          state.panels[runtime.plugin.id] = panel
          reply(runtime, callId, true)
          break
        }
        case 'storage.get': {
          const key = String(args[0])
          reply(runtime, callId, true, localStorage.getItem(storageKey(runtime.plugin.id, key)))
          break
        }
        case 'storage.set': {
          const key = String(args[0])
          const value = String(args[1] ?? '')
          if (value.length > 64 * 1024) {
            reply(runtime, callId, false, undefined, 'storage value too large')
            return
          }
          localStorage.setItem(storageKey(runtime.plugin.id, key), value)
          reply(runtime, callId, true)
          break
        }
        case 'request': {
          const url = String(args[0])
          const opts = (args[1] ?? {}) as { method?: string; headers?: Record<string, string>; body?: string }
          let parsed: URL
          try {
            parsed = new URL(url)
          } catch {
            reply(runtime, callId, false, undefined, 'invalid url')
            return
          }
          if (!['http:', 'https:'].includes(parsed.protocol) || !hostAllowed(parsed.hostname, runtime.plugin.allowedHosts)) {
            reply(runtime, callId, false, undefined, `host not allowed: ${parsed.hostname}`)
            return
          }
          const res = await fetch(url, {
            method: opts.method ?? 'GET',
            headers: opts.headers,
            body: opts.body,
          })
          const text = await res.text()
          let body: unknown = text
          try {
            body = JSON.parse(text)
          } catch {
            /* keep text */
          }
          reply(runtime, callId, true, { status: res.status, ok: res.ok, body })
          break
        }
        case 'theme.apply': {
          const vars = args[0] as Record<string, unknown>
          if (!vars || typeof vars !== 'object') {
            reply(runtime, callId, false, undefined, 'invalid css vars')
            return
          }
          for (const [k, v] of Object.entries(vars)) {
            // 仅放行 TDesign 设计令牌，杜绝任意 CSS 注入
            if (typeof k === 'string' && /^--td-[a-z0-9-]+$/i.test(k) && typeof v === 'string' && v.length <= 100 && !/url\(|;|\{/i.test(v)) {
              // 第一次改某个变量时记下原值：插件停用/卸载后要还原，
              // 否则主站会一直停留在「被这个插件换过肤」的状态，只有刷新才恢复
              if (!runtime.themeVars.has(k)) {
                runtime.themeVars.set(k, document.documentElement.style.getPropertyValue(k))
              }
              document.documentElement.style.setProperty(k, v)
            }
          }
          reply(runtime, callId, true)
          break
        }
        case 'registerMessageAction': {
          const desc = (args[0] ?? {}) as { id?: unknown; label?: unknown }
          const id = String(desc.id ?? '').trim()
          const label = String(desc.label ?? '').trim()
          if (!id || !label || label.length > 16) {
            reply(runtime, callId, false, undefined, 'invalid action desc')
            return
          }
          const list = state.actions[runtime.plugin.id] ?? (state.actions[runtime.plugin.id] = [])
          if (!list.some((a) => a.id === id)) list.push({ id, label })
          reply(runtime, callId, true)
          break
        }
        case 'me': {
          const u = useAuthStore().user
          reply(runtime, callId, true, u ? { id: u.id, nickname: u.nickname } : null)
          break
        }
        case 'conv.current': {
          const c = useConversationsStore().currentConv
          reply(
            runtime,
            callId,
            true,
            c
              ? {
                  id: c.id,
                  type: c.type,
                  name: c.name ?? '',
                  peerNickname: c.peer?.nickname ?? '',
                  memberCount: c.memberCount,
                }
              : null,
          )
          break
        }
        case 'conv.members': {
          const convStore = useConversationsStore()
          const convId = convStore.currentConvId
          if (!convId) {
            reply(runtime, callId, true, [])
            return
          }
          const rows = await convApi.members(convId)
          reply(
            runtime,
            callId,
            true,
            (rows ?? []).map((m) => ({
              userId: m.userId,
              nickname: m.user?.nickname ?? '',
              role: m.role,
            })),
          )
          break
        }
        case 'messages.recent': {
          const want = Math.max(1, Math.min(50, Number(args[0] ?? 20) || 20))
          // 只给「已落库的可见消息」：
          //  - 乐观消息（pending/failed/id<=0）还没确认，不该被插件当历史读走
          //  - 撤回消息只置 isRecalled 不清 content，直接下发等于把撤回原文交给插件
          const msgs = useConversationsStore()
            .currentMessages.filter((m) => m.id > 0 && !m.pending && !m.failed)
            .slice(-want)
          reply(
            runtime,
            callId,
            true,
            msgs.map((m) => ({
              id: m.id,
              senderId: m.senderId,
              msgType: m.msgType,
              content: m.isRecalled ? '' : m.content,
              createdAt: m.createdAt,
              recalled: m.isRecalled,
            })),
          )
          break
        }
        case 'nav.openConversation': {
          const convId = Number(args[0])
          if (!Number.isFinite(convId) || convId <= 0) {
            reply(runtime, callId, false, undefined, 'invalid conversation id')
            return
          }
          await useConversationsStore().openConversation(convId)
          reply(runtime, callId, true)
          break
        }
        case 'nav.jumpToMessage': {
          const convStore = useConversationsStore()
          const convId = convStore.currentConvId
          const messageId = Number(args[0])
          if (!convId || !Number.isFinite(messageId) || messageId <= 0) {
            reply(runtime, callId, false, undefined, 'invalid message id')
            return
          }
          const ok = await convStore.openConversationAtMessage(convId, messageId)
          reply(runtime, callId, true, ok)
          break
        }
        case 'toast': {
          const text = String(args[0] ?? '').slice(0, 200)
          if (!text) {
            reply(runtime, callId, true)
            return
          }
          const kind = String(args[1] ?? 'info')
          if (kind === 'success') void MessagePlugin.success(text)
          else if (kind === 'warning') void MessagePlugin.warning(text)
          else if (kind === 'error') void MessagePlugin.error(text)
          else void MessagePlugin.info(text)
          reply(runtime, callId, true)
          break
        }
        default:
          reply(runtime, callId, false, undefined, `unknown api: ${apiName}`)
      }
    } catch (e) {
      reply(runtime, callId, false, undefined, String((e as Error)?.message ?? e))
    }
  }

  // ----- 事件分发 -----

  /**
   * 事件 -> 接收方必须持有的权限。
   * message:new/recall 的 payload 是整条消息（含正文），只发给声明 message:read 的插件；
   * presence 同样带 userId 列表，按同一权限收敛。
   */
  const EVENT_PERMISSION: Record<string, string> = {
    'message:new': 'message:read',
    'message:recall': 'message:read',
    presence: 'message:read',
  }

  function dispatchEvent(event: string, payload: unknown) {
    const required = EVENT_PERMISSION[event]
    for (const rt of runtimes.values()) {
      if (required && !rt.plugin.permissions.includes(required)) continue
      rt.iframe.contentWindow?.postMessage({ __mc: 'event', event, payload }, '*')
    }
  }

  function handleBroadcastFrame(frame: ServerFrame) {
    if (frame.type === 'message') dispatchEvent('message:new', frame)
    else if (frame.type === 'recall') dispatchEvent('message:recall', frame)
    else if (frame.type === 'presence') dispatchEvent('presence', frame)
  }

  /** 自定义消息类型渲染：向注册了该类型的插件请求 UI 描述 */
  function requestRender(msgType: string, payload: unknown): Promise<PanelDesc | null> {
    for (const rt of runtimes.values()) {
      if (!rt.rendererTypes.has(msgType)) continue
      return new Promise((resolve) => {
        const callId = ++rt.renderSeq
        const timer = setTimeout(() => {
          rt.pendingRenders.delete(callId)
          resolve(null)
        }, 1500)
        rt.pendingRenders.set(callId, {
          resolve: (d) => {
            clearTimeout(timer)
            resolve(d)
          },
          reject: () => {
            clearTimeout(timer)
            resolve(null)
          },
        })
        rt.iframe.contentWindow?.postMessage({ __mc: 'render', callId, msgType, payload }, '*')
      })
    }
    return Promise.resolve(null)
  }

  function hasRenderer(msgType: string): boolean {
    for (const rt of runtimes.values()) if (rt.rendererTypes.has(msgType)) return true
    return false
  }

  // ----- 生命周期 -----

  function mountRuntime(plugin: Plugin, code: string) {
    if (runtimes.has(plugin.id)) return
    const iframe = document.createElement('iframe')
    iframe.setAttribute('sandbox', 'allow-scripts')
    iframe.style.display = 'none'
    // 监听器必须存进 Runtime：卸载时要按同一个引用 removeEventListener
    // 这里必须先声明后赋值：onMessage 回调内部要引用 runtime（互相引用），无法合并为 const
    // eslint-disable-next-line prefer-const
    let runtime: Runtime
    const onMessage = (ev: MessageEvent) => handleSandboxMessage(runtime, ev)
    runtime = {
      plugin,
      iframe,
      rendererTypes: new Set(),
      renderSeq: 0,
      pendingRenders: new Map(),
      onMessage,
      themeVars: new Map(),
    }
    // 用 srcdoc 注入：sandbox 未给 allow-same-origin 时，父页面无权访问
    // iframe.contentWindow.document，document.open/write 会直接抛 SecurityError，
    // 插件永远不会启动；srcdoc 在挂载前写入即可正常执行且仍是 opaque origin
    iframe.srcdoc = bootHtml(code, plugin.allowedHosts ?? [])
    window.addEventListener('message', onMessage)
    document.body.appendChild(iframe)
    runtimes.set(plugin.id, runtime)
  }

  /** 卸载单个运行时：解绑 window message 监听器，再移除 iframe */
  function unmountRuntime(pluginId: number) {
    const rt = runtimes.get(pluginId)
    if (!rt) return
    window.removeEventListener('message', rt.onMessage)
    for (const p of rt.pendingRenders.values()) p.reject(new Error('runtime unmounted'))
    rt.pendingRenders.clear()
    rt.iframe.remove()
    // 还原这个插件改过的主题变量（原值为空串表示原本没设置过，直接移除）
    for (const [key, original] of rt.themeVars) {
      if (original) document.documentElement.style.setProperty(key, original)
      else document.documentElement.style.removeProperty(key)
    }
    rt.themeVars.clear()
    runtimes.delete(pluginId)
    delete state.panels[pluginId]
    delete state.actions[pluginId]
  }

  async function start() {
    if (state.started) return
    const pluginsStore = usePluginsStore()
    await pluginsStore.loadActive()
    // 先订阅广播帧：单个插件代码拉取失败时，其余插件仍要能收到 message/recall/presence
    const ws = useWsStore()
    offFrame = ws.onFrame(handleBroadcastFrame)
    state.started = true

    // 并发拉取：串行 await 会让第 N 个插件等前 N-1 个全部返回
    //（5 个插件 × 200ms 延迟 ≈ 1 秒才挂上最后一个）。挂载仍按顺序做，避免 DOM 顺序抖动。
    const failed: string[] = []
    const active = pluginsStore.plugins.filter((p) => !p.globalDisabled)
    const loaded = await Promise.all(
      active.map(async (p) => {
        try {
          const code = await fetchPluginCode(p.id)
          // 非 200（被全局下架 / 已 purge / 网络抖动）会拿到空串：不要挂一个空 iframe
          if (!code) throw new Error('empty plugin code')
          return { p, code }
        } catch (e) {
          // 一个插件坏掉不能连累其他插件
          failed.push(p.name)
          console.warn(`[plugin:${p.name}] load failed:`, e)
          return null
        }
      }),
    )
    for (const row of loaded) {
      if (!row) continue
      mountRuntime(row.p, row.code)
    }
    if (failed.length) {
      void MessagePlugin.warning(
        failed.length === 1
          ? `插件「${failed[0]}」加载失败，可在插件管理中重新启用`
          : `${failed.length} 个插件加载失败：${failed.join('、')}`,
      )
    }
  }

  async function fetchPluginCode(pluginId: number): Promise<string> {
    return pluginApi.code(pluginId)
  }

  function stop() {
    offFrame?.()
    offFrame = null
    // unmountRuntime 会删除当前项，Map 迭代器按序遍历时这是安全的
    for (const id of runtimes.keys()) unmountRuntime(id)
    state.panels = {}
    state.started = false
  }

  /** 用户启停插件后调用：卸载对应运行时或挂载新运行时 */
  async function reloadPlugin(plugin: Plugin) {
    if (plugin.globalDisabled || !plugin.enabled) {
      unmountRuntime(plugin.id)
      return
    }
    // 先取代码再卸载：拉取失败时保留旧运行时，
    // 否则「服务端已启用、本地面板和菜单项却消失」，只能整页刷新才恢复
    let code = ''
    try {
      code = await fetchPluginCode(plugin.id)
    } catch (e) {
      console.warn(`[plugin:${plugin.name}] reload failed:`, e)
    }
    if (!code) {
      void MessagePlugin.error(`插件「${plugin.name}」加载失败，请稍后重试`)
      return
    }
    unmountRuntime(plugin.id)
    mountRuntime(plugin, code)
  }

  /** 侧边栏 UI 动作回传（§8.2 button/input 的 action） */
  function panelAction(pluginId: number, action: string, value?: unknown) {
    const rt = runtimes.get(pluginId)
    rt?.iframe.contentWindow?.postMessage({ __mc: 'event', event: 'ui:action', payload: { action, value } }, '*')
  }

  /** 所有插件注册的右键菜单动作（供消息菜单渲染） */
  function pluginActions(): PluginMessageAction[] {
    const out: PluginMessageAction[] = []
    for (const [key, list] of Object.entries(state.actions)) {
      const pluginId = Number(key)
      const rt = runtimes.get(pluginId)
      if (!rt) continue
      for (const a of list) out.push({ pluginId, pluginName: rt.plugin.name, id: a.id, label: a.label })
    }
    return out
  }

  /**
   * 触发某个插件的消息动作：只投递给该插件的运行时，不广播给其他插件。
   * 只声明 ui:actions 的插件不能拿到整条消息（含正文），只能拿到定位信息；
   * 需要正文必须同时声明 message:read。
   */
  function runMessageAction(pluginId: number, actionId: string, message: unknown) {
    const rt = runtimes.get(pluginId)
    if (!rt || !rt.plugin.permissions.includes('ui:actions')) return
    const payload = rt.plugin.permissions.includes('message:read')
      ? { actionId, message }
      : { actionId, message: messageMeta(message) }
    rt.iframe.contentWindow?.postMessage({ __mc: 'event', event: 'message:action', payload }, '*')
  }

  return {
    state: readonly(state),
    start,
    stop,
    reloadPlugin,
    requestRender,
    hasRenderer,
    panelAction,
    pluginActions,
    runMessageAction,
    notifyConvOpen: (convId: number) => dispatchEvent('conv:open', { convId }),
  }
}

type PluginHost = ReturnType<typeof createHost>

let host: PluginHost | null = null

/** 全局单例：主应用生命周期内只有一个插件宿主 */
export function usePluginHost(): PluginHost {
  if (!host) host = createHost()
  return host
}
