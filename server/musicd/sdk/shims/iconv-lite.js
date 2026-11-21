/**
 * iconv-lite 的最小替代。
 *
 * lx 的酷我模块只在「解码歌词」时用 iconv 处理 gb18030，
 * 而 Node 22 自带 full-icu，`TextDecoder('gb18030')` 可直接胜任，
 * 不必为了这一个函数引入第三方依赖。
 */
const decoderCache = new Map()

function getDecoder(encoding) {
  const key = String(encoding || 'utf8').toLowerCase()
  if (decoderCache.has(key)) return decoderCache.get(key)
  const label = key === 'gb18030' || key === 'gbk' || key === 'gb2312' ? 'gb18030' : key
  let dec
  try {
    dec = new TextDecoder(label)
  } catch {
    dec = new TextDecoder('utf-8')
  }
  decoderCache.set(key, dec)
  return dec
}

const iconv = {
  decode(buf, encoding = 'utf8') {
    const u8 = Buffer.isBuffer(buf) ? buf : Buffer.from(buf)
    return getDecoder(encoding).decode(u8)
  },
  encode(str, encoding = 'utf8') {
    // 只用于 utf8/ascii 场景；gb18030 编码在本项目里用不到
    return Buffer.from(String(str), 'utf8')
  },
  encodingExists(encoding) {
    try {
      getDecoder(encoding)
      return true
    } catch {
      return false
    }
  },
}

export default iconv
export const { decode, encode } = iconv