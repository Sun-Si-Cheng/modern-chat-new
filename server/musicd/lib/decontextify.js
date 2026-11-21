/**
 * decontextify —— 把沙箱里创建的对象「纯净」成宿主对象。
 *
 * 为什么需要：`vm.createContext` 里的对象有自己的 Object/Array 原型，
 * 直接返回给宿主会有两类问题：
 *   1) 宿主侧 `obj instanceof Object` 之类的判断失效（跨 realm）；
 *   2) 恶意脚本可以用 Proxy 的 getter 在宿主读取时执行任意代码。
 *
 * 做法是深拷贝成纯数据：只保留 string/number/boolean/null/数组/普通对象，
 * Buffer/Uint8Array 统一转成宿主 Buffer，Error 转成宿主 Error。
 */
export function decontextify(obj, depth = 0) {
  if (obj === null || obj === undefined) return obj
  if (depth > 12) return '[deep]'
  const t = typeof obj
  if (t === 'string' || t === 'number' || t === 'boolean') return obj
  if (t === 'function') return undefined
  if (t !== 'object') return obj

  try {
    if (Buffer.isBuffer(obj) || obj instanceof Uint8Array) {
      return Buffer.from(Uint8Array.prototype.slice.call(obj))
    }
  } catch {
    /* ignore */
  }

  if (Array.isArray(obj)) {
    const out = []
    for (const item of obj) out.push(decontextify(item, depth + 1))
    return out
  }

  if (obj instanceof Error || (obj && obj.constructor && obj.constructor.name === 'Error')) {
    const err = new Error(String(obj.message || 'error'))
    if (obj.stack) err.stack = String(obj.stack)
    return err
  }

  const out = {}
  let keys = []
  try {
    keys = Object.keys(obj)
  } catch {
    return String(obj)
  }
  for (const k of keys) {
    try {
      out[k] = decontextify(obj[k], depth + 1)
    } catch {
      /* 单个属性读取失败不影响整体 */
    }
  }
  return out
}