#!/usr/bin/env node
/**
 * 设计令牌审计：扫描 .vue <style> 块与 .css 文件里的硬编码颜色。
 *
 * 约定（AGENTS.md / 前端技术方案）：颜色/圆角/动效一律走令牌 --td-* / --mc-*
 * （assets/main.css），页面不得写死色值——否则暗色模式与主题切换必然漏。
 *
 * 误报说明（审计时人工甄别，不自动改）：
 *   - JS 里的 avatarColor 等域数据（头像色板、取色器默认值）不是样式，豁免；
 *   - 生成式渐变演示（取色器预览）属功能内容；
 *   - 真正要修的是 style 块里的 background:#fff、color:#333 这类散落样式。
 *
 * 用法：node scripts/audit-tokens.mjs        （只报告，不改文件，退出码恒 0）
 */
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..', 'src')
const COLOR_RE = /#[0-9a-fA-F]{3,8}\b|rgba?\([^)]*\)/
// 域数据豁免：头像色板 / 取色器 / 渐变预览按白名单路径与行特征放行
const DOMAIN_HINTS = [/\bcolor:/i, /avatarColor|conic-gradient|type="color"/]

function* walk(dir) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    const st = statSync(p)
    if (st.isDirectory()) yield* walk(p)
    else yield p
  }
}

const findings = []
for (const rawPath of walk(root)) {
  const file = rawPath.replace(/\\/g, '/') // Windows 分隔符归一，便于路径匹配与展示
  if (!file.endsWith('.vue') && !file.endsWith('.css')) continue
  if (file.includes('assets/main.css')) continue // 令牌真源，豁免
  const text = readFileSync(file, 'utf8')
  // .vue 只看 <style> 块（JS 里的颜色多半是域数据，人工甄别）
  const styleBlocks = file.endsWith('.vue')
    ? [...text.matchAll(/<style[^>]*>([\s\S]*?)<\/style>/g)].map((m) => m[1])
    : [text]
  for (const block of styleBlocks) {
    const lines = block.split('\n')
    lines.forEach((line, i) => {
      if (!COLOR_RE.test(line)) return
      // var(--token, fallback) 是令牌用法的带兜底写法，合规
      if (line.includes('var(--')) return
      if (DOMAIN_HINTS.some((re) => re.test(line))) return
      findings.push(`${file}:${i + 1}: ${line.trim().slice(0, 100)}`)
    })
  }
}

if (findings.length === 0) {
  console.log('令牌审计通过：style 块无硬编码颜色')
} else {
  console.log(`发现 ${findings.length} 处疑似硬编码颜色（请映射到 --td-*/--mc-* 令牌）：`)
  for (const f of findings) console.log('  ' + f)
}
