/** LRC / YRC / 网易云逐字 歌词解析。 */

export interface LyricLine {
  /** 秒 */
  time: number
  text: string
}

export interface LyricChar {
  /** 该字/词的开始时间（秒） */
  time: number
  text: string
}

export interface LyricDoc {
  kind: 'yrc' | 'lrc' | 'none'
  lines: LyricLine[]
  /** 逐字时间轴；没有时为 []，渲染退化为整行高亮 */
  chars: LyricChar[]
  meta: Record<string, string>
}

const EMPTY: LyricDoc = { kind: 'none', lines: [], chars: [], meta: {} }

/**
 * 网易云逐字歌词（JSONL）。
 * 每行独立一个 JSON：{"t": 毫秒, "c":[{"tx":"字"},{"tx":"词","li":"..","or":".."}]}
 * 某些行会带 "li"/"or"（头像/跳转），与歌词渲染无关，忽略即可。
 */
function parseNetEaseYrc(raw: string): LyricDoc {
  const lines: LyricLine[] = []
  const chars: LyricChar[] = []
  const meta: Record<string, string> = {}

  for (const rawLine of raw.split(/\r?\n/)) {
    const s = rawLine.trim()
    if (!s) continue
    // 只处理形似 JSON 的行；夹杂的普通时间轴文本行直接跳过
    if (!s.startsWith('{')) continue
    let obj: unknown
    try {
      obj = JSON.parse(s)
    } catch {
      continue
    }
    const o = obj as { t?: number; c?: Array<{ tx?: string }> }
    const tMs = Number(o.t ?? 0)
    const segs = Array.isArray(o.c) ? o.c : []
    let text = ''
    const lineStart = tMs / 1000

    for (const seg of segs) {
      const tx = String(seg?.tx ?? '')
      if (!tx) continue
      text += tx
      // 逐字时间：网易云只给行首时间，字级时间在这一层拿不到，
      // 这里按字符长度等比切分，让进度动画有节奏（不是精确对齐，但视觉上自然）。
      chars.push({ time: -1, text: tx })
    }

    lines.push({ time: lineStart, text })
    // 记录"这一行从第几个 char 开始"，供下面按行内比例分配时间
    const from = chars.length - (text.length ? segs.length : 0)
    void from
  }

  if (!lines.length) return EMPTY

  // 按行起始时间给每个字赋时间：行内等分到下一行开始
  const sortedChars: LyricChar[] = []
  for (let i = 0; i < lines.length; i++) {
    const cur = lines[i]
    const next = lines[i + 1]
    if (!cur) continue
    const end = next ? next.time : cur.time + Math.max(2, cur.text.length * 0.22)
    const span = Math.max(0.001, end - cur.time)
    const n = Math.max(1, cur.text.length)
    for (let j = 0; j < n; j++) {
      sortedChars.push({ time: cur.time + (span * j) / n, text: cur.text[j] ?? '' })
    }
  }

  lines.sort((a, b) => a.time - b.time)
  return { kind: 'yrc', lines, chars: sortedChars, meta }
}

/** 普通 LRC：[mm:ss.xxx]，同一行可带多个时间标签（展开成多行） */
function parseLrc(raw: string): LyricDoc {
  const lines: LyricLine[] = []
  const meta: Record<string, string> = {}
  if (!raw) return EMPTY

  const timeTag = /\[(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?\]/g

  for (const rawLine of raw.split(/\r?\n/)) {
    const line = rawLine.trim()
    if (!line) continue

    const metaMatch = /^\[(ti|ar|album|by|ver|offset):(.*)\]$/i.exec(line)
    if (metaMatch) {
      const k = metaMatch[1] ?? ''
      if (k) meta[k.toLowerCase()] = (metaMatch[2] ?? '').trim()
      continue
    }

    // 含 "c": 的行其实是逐字 JSON 混进了 LRC 字段，跳过（由 parseNetEaseYrc 处理）
    if (line.startsWith('{') && line.includes('"c":')) continue

    timeTag.lastIndex = 0
    const times: number[] = []
    let m: RegExpExecArray | null
    let lastEnd = 0
    while ((m = timeTag.exec(line)) !== null) {
      const min = Number(m[1] ?? '0')
      const sec = Number(m[2] ?? '0')
      const fracRaw = m[3] ?? '0'
      const frac = fracRaw.length === 3 ? Number(fracRaw) : Number(fracRaw.padEnd(2, '0')) * 10
      times.push(min * 60 + sec + frac / 1000)
      lastEnd = (m.index ?? 0) + (m[0] ?? '').length
    }
    if (!times.length) continue

    const text = line.slice(lastEnd).trim()
    for (const t of times) lines.push({ time: t, text })
  }

  if (!lines.length) return EMPTY
  lines.sort((a, b) => a.time - b.time)
  return { kind: 'lrc', lines, chars: [], meta }
}

/**
 * 统一入口：**逐字优先于普通 LRC**（按用户要求）。
 * 传入两个字段，哪个更像逐字就用哪个。
 */
export function parseLyric(lrc: string, yrc: string): LyricDoc {
  if (yrc && yrc.includes('"c":')) {
    const d = parseNetEaseYrc(yrc)
    if (d.lines.length) return d
  }
  if (yrc) {
    const d = parseLrc(yrc)
    if (d.lines.length) return d
  }
  if (lrc) {
    const d = parseLrc(lrc)
    if (d.lines.length) return d
  }
  return EMPTY
}

/** 当前时间对应的行下标（-1 = 还没到第一句） */
export function activeLineIndex(lines: LyricLine[], t: number): number {
  if (!lines.length) return -1
  let lo = 0
  let hi = lines.length - 1
  let ans = -1
  while (lo <= hi) {
    const mid = (lo + hi) >> 1
    const item = lines[mid]
    if (item && item.time <= t) {
      ans = mid
      lo = mid + 1
    } else {
      hi = mid - 1
    }
  }
  return ans
}

/** 逐字进度：已唱字数（用于 CSS 渐变填充） */
export function sungCharCount(chars: LyricChar[], line: LyricLine, next: LyricLine | undefined, t: number): number {
  const start = line.time
  const end = next ? next.time : start + Math.max(2, line.text.length * 0.22)
  if (t >= end) return line.text.length

  if (chars.length) {
    // chars 是全曲的连续数组；找到落在本行时间窗内的那些
    let count = 0
    for (const c of chars) {
      if (c.time < start) continue
      if (c.time >= end) break
      if (c.time <= t) count++
      else break
    }
    if (count > 0) return Math.min(count, line.text.length)
  }

  const ratio = Math.min(1, Math.max(0, (t - start) / Math.max(0.001, end - start)))
  return Math.floor(ratio * line.text.length)
}

/** mm:ss */
export function fmtTime(sec: number): string {
  if (!Number.isFinite(sec) || sec < 0) sec = 0
  const m = Math.floor(sec / 60)
  const s = Math.floor(sec % 60)
  return `${m}:${s.toString().padStart(2, '0')}`
}
