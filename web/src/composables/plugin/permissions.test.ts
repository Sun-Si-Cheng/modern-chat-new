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

  it('on 不进 API_PERMISSION：api.on 是沙箱内本地注册，不走 RPC', () => {
    expect('on' in API_PERMISSION).toBe(false)
  })
})

describe('EVENT_PERMISSION 门槛', () => {
  it('含正文/用户列表的事件必须 message:read', () => {
    expect(EVENT_PERMISSION['message:new']).toBe('message:read')
    expect(EVENT_PERMISSION['message:recall']).toBe('message:read')
    expect(EVENT_PERMISSION['presence']).toBe('message:read')
  })

  it('面板/右键动作按对应 UI 权限收敛，不要求 message:read', () => {
    // 纯面板插件（只有 ui:panel）必须能收到自己的面板动作——
    // 这是不引入 message:read 的前提；右键动作同理归 ui:actions
    expect(EVENT_PERMISSION['ui:action']).toBe('ui:panel')
    expect(EVENT_PERMISSION['message:action']).toBe('ui:actions')
  })

  it('conv:open 故意不设门槛（payload 只有 convId）', () => {
    expect('conv:open' in EVENT_PERMISSION).toBe(false)
  })
})
