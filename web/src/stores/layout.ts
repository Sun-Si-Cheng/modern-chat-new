import { computed, reactive } from 'vue'
import { defineStore } from 'pinia'

/** 布局中可调节的三个区域 */
export type PaneKey = 'group' | 'conv' | 'detail'

export interface PaneState {
  /** 宽度（px），拖动分隔条或设置页滑块调整 */
  width: number
  /** 形态：false = 常驻占位；true = 抽屉（浮层，可收起、不占位） */
  drawer: boolean
}

const STORAGE_KEY = 'mc_layout'

/** 默认宽度与可调范围（与各组件原硬编码值一致，保证首屏不跳变） */
export const PANE_DEFAULTS: Record<PaneKey, PaneState> = {
  group: { width: 148, drawer: false },
  conv: { width: 300, drawer: false },
  detail: { width: 280, drawer: false },
}

export const PANE_LIMITS: Record<PaneKey, { min: number; max: number }> = {
  group: { min: 120, max: 260 },
  conv: { min: 220, max: 460 },
  detail: { min: 240, max: 420 },
}

function clamp(key: PaneKey, w: number): number {
  const l = PANE_LIMITS[key]
  return Math.min(l.max, Math.max(l.min, Math.round(w)))
}

function load(): { panes: Record<PaneKey, PaneState>; opened: Record<PaneKey, boolean> } {
  const panes: Record<PaneKey, PaneState> = {
    group: { ...PANE_DEFAULTS.group },
    conv: { ...PANE_DEFAULTS.conv },
    detail: { ...PANE_DEFAULTS.detail },
  }
  const opened: Record<PaneKey, boolean> = { group: false, conv: false, detail: false }
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return { panes, opened }
    const saved = JSON.parse(raw) as {
      panes?: Partial<Record<PaneKey, Partial<PaneState>>>
      opened?: Partial<Record<PaneKey, boolean>>
    }
    for (const key of Object.keys(panes) as PaneKey[]) {
      const v = saved.panes?.[key]
      if (!v) continue
      if (typeof v.width === 'number') panes[key].width = clamp(key, v.width)
      if (typeof v.drawer === 'boolean') panes[key].drawer = v.drawer
      // 抽屉的展开态也要记住：否则刷新后抽屉区域「凭空消失」，
      // 用户会以为功能丢了
      if (panes[key].drawer && saved.opened?.[key] === true) opened[key] = true
    }
  } catch {
    /* 隐私模式 / 脏数据：用默认值 */
  }
  return { panes, opened }
}

export const useLayoutStore = defineStore('layout', () => {
  const initial = load()
  const panes = reactive<Record<PaneKey, PaneState>>(initial.panes)

  /**
   * 抽屉展开状态。只有 panes[key].drawer 为 true 时才有意义：
   * 抽屉形态下区域默认收起，点对应入口才浮出。
   * 抽屉互斥 —— 同时展开多个会互相遮挡，没有实际意义。
   */
  const opened = reactive<Record<PaneKey, boolean>>(initial.opened)

  function persist() {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify({ panes, opened }))
    } catch {
      /* 忽略：隐私模式 */
    }
  }

  function setWidth(key: PaneKey, w: number) {
    panes[key].width = clamp(key, w)
    persist()
  }

  /** 常驻 ⇄ 抽屉（切换时关掉抽屉展开态，避免残留） */
  function setDrawer(key: PaneKey, drawer: boolean) {
    panes[key].drawer = drawer
    opened[key] = false
    persist()
  }

  function toggleDrawer(key: PaneKey) {
    setDrawer(key, !panes[key].drawer)
  }

  /** 打开抽屉；已打开则收起（再点一次入口即关闭） */
  function open(key: PaneKey) {
    if (!panes[key].drawer) return
    if (opened[key]) {
      opened[key] = false
      persist()
      return
    }
    // 互斥：先关其它
    for (const k of Object.keys(opened) as PaneKey[]) opened[k] = false
    opened[key] = true
    persist()
  }

  function close(key: PaneKey) {
    opened[key] = false
    persist()
  }

  function closeAll() {
    for (const k of Object.keys(opened) as PaneKey[]) opened[k] = false
    persist()
  }

  /** 抽屉形态且已收起：该区域不应渲染 */
  function hidden(key: PaneKey): boolean {
    return panes[key].drawer && !opened[key]
  }

  /**
   * 「聚焦详情面板搜索」信号（Ctrl/Cmd+F 用）。
   * 详情面板在 ChatLayout 里与 MessagePane 是同级组件，跨组件传指令走 store：
   * 自增一个 tick，DetailPanel watch 到后聚焦搜索框。
   */
  const detailSearchTick = reactive({ n: 0 })

  function requestDetailSearch() {
    detailSearchTick.n++
  }

  function reset() {
    for (const k of Object.keys(panes) as PaneKey[]) {
      panes[k].width = PANE_DEFAULTS[k].width
      panes[k].drawer = PANE_DEFAULTS[k].drawer
      opened[k] = false
    }
    persist()
  }

  /** 供 .mc-layout 使用的 CSS 变量，各区域 width 直接读 var(--mc-w-*) */
  const vars = computed(() => ({
    '--mc-w-group': `${panes.group.width}px`,
    '--mc-w-conv': `${panes.conv.width}px`,
    '--mc-w-detail': `${panes.detail.width}px`,
  }))

  return {
    panes,
    opened,
    vars,
    detailSearchTick,
    requestDetailSearch,
    setWidth,
    setDrawer,
    toggleDrawer,
    open,
    close,
    closeAll,
    hidden,
    reset,
  }
})
