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

import type { Plugin, PanelDesc, ServerFrame, UIDescNode } from '@/types/proto'
import { pluginApi } from '@/api'
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

const API_PERMISSION: Record<string, string> = {
  on: 'message:read',
  send: 'message:send',
  registerMessageRenderer: 'message:render',
  addSidePanel: 'ui:panel',
  'storage.get': 'storage',
  'storage.set': 'storage',
  request: 'net',
  'theme.apply': 'ui:theme',
}

// ===== 沙箱引导 HTML =====

function bootHtml(pluginCode: string): string {
  const safeCode = pluginCode.replace(/<\/script/gi, '<\\/script')
  return `<!doctype html><html><head><meta charset="utf-8"></head><body><script>
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
}

interface PluginHostState {
  started: boolean
  panels: Record<number, PanelDesc> // pluginId -> panel desc
}

function storageKey(pluginId: number, key: string) {
  return `mc_plug_${pluginId}_${key}`
}

function createHost() {
  const state = reactive<PluginHostState>({ started: false, panels: {} })
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
        const desc = validateUIDesc(m.desc)
        if (desc && (desc.component === 'card' || desc.component === 'list' || desc.component === 'text')) {
          p.resolve({ kind: 'panel', title: '', body: [desc] } as PanelDesc)
        } else {
          p.reject(new Error('invalid ui desc'))
        }
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
          const raw = args[0] as Record<string, unknown>
          const desc = validateUIDesc({ ...raw, kind: undefined })
          if (!desc || typeof raw?.title !== 'string' || !Array.isArray(raw?.body)) {
            reply(runtime, callId, false, undefined, 'invalid panel desc')
            return
          }
          const panel: PanelDesc = { kind: 'panel', title: raw.title, body: [] }
          for (const node of raw.body) {
            const v = validateUIDesc(node)
            if (!v) {
              reply(runtime, callId, false, undefined, 'invalid panel node')
              return
            }
            panel.body.push(v)
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
              document.documentElement.style.setProperty(k, v)
            }
          }
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

  function dispatchEvent(event: string, payload: unknown) {
    for (const rt of runtimes.values()) {
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

  function mountRuntime(plugin: Plugin) {
    if (runtimes.has(plugin.id)) return
    const iframe = document.createElement('iframe')
    iframe.setAttribute('sandbox', 'allow-scripts')
    iframe.style.display = 'none'
    const runtime: Runtime = {
      plugin,
      iframe,
      rendererTypes: new Set(),
      renderSeq: 0,
      pendingRenders: new Map(),
    }
    iframe.addEventListener('load', () => {
      iframe.contentWindow?.document.open()
      iframe.contentWindow?.document.write(bootHtml(pluginCodeCache.get(plugin.id) ?? ''))
      iframe.contentWindow?.document.close()
    })
    window.addEventListener('message', (ev) => handleSandboxMessage(runtime, ev))
    document.body.appendChild(iframe)
    runtimes.set(plugin.id, runtime)
  }

  /** srcdoc 不便直接塞大代码，用 document.write 注入；代码在加载插件清单时缓存 */
  const pluginCodeCache = new Map<number, string>()

  async function start() {
    if (state.started) return
    const pluginsStore = usePluginsStore()
    await pluginsStore.loadActive()
    for (const p of pluginsStore.plugins) {
      if (p.globalDisabled) continue
      pluginCodeCache.set(p.id, await fetchPluginCode(p.id))
      mountRuntime(p)
    }
    const ws = useWsStore()
    offFrame = ws.onFrame(handleBroadcastFrame)
    state.started = true
  }

  async function fetchPluginCode(pluginId: number): Promise<string> {
    return pluginApi.code(pluginId)
  }

  function stop() {
    offFrame?.()
    offFrame = null
    for (const rt of runtimes.values()) rt.iframe.remove()
    runtimes.clear()
    state.panels = {}
    state.started = false
  }

  /** 用户启停插件后调用：卸载对应运行时或挂载新运行时 */
  async function reloadPlugin(plugin: Plugin) {
    if (plugin.globalDisabled || !plugin.enabled) {
      runtimes.get(plugin.id)?.iframe.remove()
      runtimes.delete(plugin.id)
      delete state.panels[plugin.id]
      return
    }
    runtimes.get(plugin.id)?.iframe.remove()
    runtimes.delete(plugin.id)
    pluginCodeCache.set(plugin.id, await fetchPluginCode(plugin.id))
    mountRuntime(plugin)
  }

  /** 侧边栏 UI 动作回传（§8.2 button/input 的 action） */
  function panelAction(pluginId: number, action: string, value?: unknown) {
    const rt = runtimes.get(pluginId)
    rt?.iframe.contentWindow?.postMessage({ __mc: 'event', event: 'ui:action', payload: { action, value } }, '*')
  }

  return {
    state: readonly(state),
    start,
    stop,
    reloadPlugin,
    requestRender,
    hasRenderer,
    panelAction,
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
