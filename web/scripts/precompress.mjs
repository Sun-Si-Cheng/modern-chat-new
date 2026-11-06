#!/usr/bin/env node
/**
 * 构建产物预压缩：为 dist 下的文本资源生成 .gz 与 .br 同名文件。
 *
 * 配合 nginx 的 `gzip_static on` / `brotli_static on` 使用：
 * - 有预压缩文件时，nginx 直接发送，**零运行时 CPU**
 * - 没有时回落到 `gzip on` 实时压缩
 *
 * 用 Node 内置 zlib，不引入额外依赖。
 *
 * 用法：npm run compress（在 build 之后执行）；也可传目录参数：
 * node scripts/precompress.mjs dist.new（热更新构建流程压缩备用产物目录）
 */
import fs from 'node:fs'
import path from 'node:path'
import zlib from 'node:zlib'

const DIST = path.resolve(process.argv[2] || 'dist')
/** 只压文本资源：图片/字体本身已是压缩格式，再压反而变大 */
const EXT = new Set(['.js', '.mjs', '.css', '.html', '.json', '.svg', '.txt', '.xml', '.map'])
/** 小于这个体积不值得压（省不到几百字节，反而多一次查找） */
const MIN_BYTES = 1024

function walk(dir, out = []) {
  for (const name of fs.readdirSync(dir)) {
    const p = path.join(dir, name)
    const st = fs.statSync(p)
    if (st.isDirectory()) walk(p, out)
    else if (EXT.has(path.extname(p).toLowerCase()) && st.size >= MIN_BYTES) out.push({ p, size: st.size })
  }
  return out
}

if (!fs.existsSync(DIST)) {
  console.error('dist 不存在，先跑 npm run build')
  process.exit(1)
}

const files = walk(DIST)
let raw = 0
let gzTotal = 0
let brTotal = 0

for (const { p, size } of files) {
  const buf = fs.readFileSync(p)
  const gz = zlib.gzipSync(buf, { level: 9 })
  const br = zlib.brotliCompressSync(buf, {
    params: {
      [zlib.constants.BROTLI_PARAM_QUALITY]: 11,
      [zlib.constants.BROTLI_PARAM_SIZE_HINT]: buf.length,
    },
  })
  fs.writeFileSync(`${p}.gz`, gz)
  fs.writeFileSync(`${p}.br`, br)
  raw += size
  gzTotal += gz.length
  brTotal += br.length
}

const mb = (n) => `${(n / 1024).toFixed(1)} KB`
console.log(`已压缩 ${files.length} 个文件`)
console.log(`  原始   ${mb(raw)}`)
console.log(`  gzip   ${mb(gzTotal)}  (${((gzTotal / raw) * 100).toFixed(0)}%)`)
console.log(`  brotli ${mb(brTotal)}  (${((brTotal / raw) * 100).toFixed(0)}%)`)
