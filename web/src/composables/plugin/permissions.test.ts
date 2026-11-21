/**
 * 权限表不变量：SDK 里每一个 call("...") 都必须在 API_PERMISSION 登记。
 * 漏登记 = 该 API 对任何插件都返回 permission denied（功能性回归）；
 * 这类回归在宿主分发逻辑里不可见，只能在表上验。
 */
import { describe, expect, it } from 'vitest'

import { bootHtml } from './sandbox'
import { API_PERMISSION, EVENT_PERMISSION } from './permissions'

describe('API_PERMISSION 完整性', () => {
  it('bootHtml SDK 中每个 call("...") 都已登记权限', () => {
    const html = bootHtml('', [], 'tk', 'https://app.example')
    const calls = [...html.matchAll(/call\("([a-z.]+)"/g)].map((m) => m[1] ?? '')
    expect(calls.length).toBeGreaterThan(10) // SDK 面非空，防止正则失效导致测试空转
    const missing = [...new Set(calls)].filter((api) => !(api in API_PERMISSION))
    expect(missing).toEqual([])
  })

  it('权限值均为已知权限名（防手滑写出不存在的权限）', () => {
    const known = new Set([
      'message:read',
      'message:send',
      'message:render',
      'ui:panel',
      'ui:actions',
      'ui:host',
      'conv:read',
      'storage',
      'net',
      'ui:theme',
    ])
    const bad = Object.entries(API_PERMISSION)
      .concat(Object.entries(EVENT_PERMISSION))
      .filter(([, perm]) => !known.has(perm))
      .map(([api, perm]) => `${api} -> ${perm}`)
    expect(bad).toEqual([])
  })
})
