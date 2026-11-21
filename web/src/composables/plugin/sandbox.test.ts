/**
 * 沙箱引导文档的回归测试：CSP 收敛、插件代码转义、postMessage 目标 origin。
 * bootHtml 生成的 HTML 会被浏览器直接执行，这里的每条断言都是安全边界。
 */
import { describe, expect, it } from 'vitest'

import { bootHtml } from './sandbox'

describe('bootHtml CSP', () => {
  it('connect-src / media-src 只收敛到 allowedHosts', () => {
    const html = bootHtml('', ['example.com', '*.cdn.example'], 'tk', 'https://app.example')
    expect(html).toContain('connect-src example.com *.cdn.example')
    expect(html).toContain('media-src example.com *.cdn.example blob:')
    expect(html).toContain("default-src 'none'")
    expect(html).toContain('script-src \'unsafe-inline\'')
  })

  it('未声明 allowedHosts 时完全禁止出网与媒体', () => {
    const html = bootHtml('', [], 'tk', 'https://app.example')
    expect(html).toContain("connect-src 'none'")
    expect(html).toContain("media-src 'none'")
  })

  it('非法 host 形态（带协议/路径）被过滤，不进 CSP', () => {
    const html = bootHtml('', ['https://evil.com/path', 'ok.example'], 'tk', 'https://app.example')
    expect(html).toContain('connect-src ok.example')
    expect(html).not.toContain('evil.com')
  })
})

describe('bootHtml 转义与注入', () => {
  it('插件代码里的 </script> 被转义，无法提前闭合引导脚本', () => {
    const html = bootHtml('var s = "</script><script>alert(1)</script>";', [], 'tk', 'https://app.example')
    // 逃逸后不会出现裸的 </script>（文档末尾的那个除外）
    expect(html).not.toContain('</script><script>alert(1)')
    expect(html).toContain('<\\/script>')
  })

  it('身份令牌与宿主 origin 被注入', () => {
    const html = bootHtml('', ['ok.example'], 'tok-abc', 'https://app.example')
    expect(html).toContain('var TK = "tok-abc";')
    expect(html).toContain('var HOST = "https://app.example";')
    expect(html).toContain('parent.postMessage(m, HOST)')
    // 沙箱 → 宿主不再使用 "*"
    expect(html).not.toContain('postMessage(m, "*")')
  })

  it('origin 形态非法时回退 "*"（不产生静默失败）', () => {
    const html = bootHtml('', [], 'tk', 'not-a-origin')
    expect(html).toContain('var HOST = "*";')
  })

  it('SDK 暴露 version 与 users.search / removeSidePanel', () => {
    const html = bootHtml('', [], 'tk', 'https://app.example')
    expect(html).toContain('version: "1"')
    expect(html).toContain('removeSidePanel')
    expect(html).toContain('call("users.search", [kw])')
  })
})
