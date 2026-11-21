/**
 * 播放控件形态与位置（全局单例）。
 *
 * 五种形态：贴在窗口上/下/左/右四个边缘，或变成可拖动、可缩放的独立窗口。
 * 选择保存在 localStorage，刷新/重开都保持上次的位置与尺寸。
 *
 * 为什么单独抽成 composable：
 *   - MusicFloatBar 要用它决定自己怎么摆；
 *   - ChatLayout 要用它给内容让出被播放条盖住的边距（否则输入框会被压住）。
 * 两处必须共享同一份状态，否则切换形态后布局对不齐。
 */
import { computed, reactive, watch } from 'vue'

export type PlayerLayoutMode = 'top' | 'left' | 'right' | 'bottom' | 'window'

export interface PlayerLayoutState {
  mode: PlayerLayoutMode
  /** 独立窗口模式下的左上角坐标；-1 表示「还没摆过」，首次进入时按右下角自动定位 */
  x: number
  y: number
  /** 独立窗口尺寸 */
  w: number
  h: number
}

const STORAGE_KEY = 'mc_player_layout'
const MODES: PlayerLayoutMode[] = ['top', 'left', 'right', 'bottom', 'window']

/**
 * 独立窗口的最小尺寸，保证控制按钮不会被压得点不到。
 * 高度下限按「摊开一列」算：头部 44 + 控制 38 + 进度 20 + 歌词 18 + 音量 20
 * + 4 段间距 32 + 上下内边距 22 ≈ 196；再小音量条就会被裁掉（用户反馈过）。
 */
export const WINDOW_MIN_W = 300
export const WINDOW_MIN_H = 196

function num(v: unknown, dflt: number): number {
  const n = typeof v === 'number' ? v : Number(v)
  return Number.isFinite(n) ? n : dflt
}

function load(): PlayerLayoutState {
  const fallback: PlayerLayoutState = { mode: 'bottom', x: -1, y: -1, w: 380, h: 208 }
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return fallback
    const p = JSON.parse(raw) as Partial<PlayerLayoutState>
    const mode = MODES.includes(p.mode as PlayerLayoutMode) ? (p.mode as PlayerLayoutMode) : 'bottom'
    return {
      mode,
      x: num(p.x, -1),
      y: num(p.y, -1),
      w: Math.max(WINDOW_MIN_W, num(p.w, fallback.w)),
      h: Math.max(WINDOW_MIN_H, num(p.h, fallback.h)),
    }
  } catch {
    return fallback
  }
}

export const playerLayout = reactive<PlayerLayoutState>(load())

watch(
  playerLayout,
  (v) => {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(v))
    } catch {
      /* 隐私模式下写不进去就算了，不影响播放 */
    }
  },
  { deep: true },
)

export function setPlayerMode(mode: PlayerLayoutMode) {
  playerLayout.mode = mode
}

/** 视口边界夹取：窗口被拖出屏幕就再也抓不回来了，所以每次移动都要夹 */
export function clampWindow() {
  if (playerLayout.w > window.innerWidth) playerLayout.w = Math.max(WINDOW_MIN_W, window.innerWidth - 16)
  if (playerLayout.h > window.innerHeight) playerLayout.h = Math.max(WINDOW_MIN_H, window.innerHeight - 16)
  playerLayout.x = Math.min(Math.max(0, playerLayout.x), Math.max(0, window.innerWidth - playerLayout.w))
  playerLayout.y = Math.min(Math.max(0, playerLayout.y), Math.max(0, window.innerHeight - playerLayout.h))
}

/** 宿主布局要让出的边距类名 */
export const playerDockClass = computed(() => {
  switch (playerLayout.mode) {
    case 'top':
      return 'has-player-top'
    case 'left':
      return 'has-player-left'
    case 'right':
      return 'has-player-right'
    case 'window':
      return 'has-player-window'
    default:
      return 'has-player-bottom'
  }
})
