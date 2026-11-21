<script setup lang="ts">
/**
 * 播放控件（主站渲染，不是插件）。
 *
 * 为什么必须由主站渲染：
 *   插件跑在沙箱 iframe 里，输出的是**描述式 UI**（text/button/card/list/divider…），
 *   白名单组件做不出「可拖动进度条 + 实时进度 + 封面图」这类连续交互。
 *   而播放核心（拉流/解密/解码/缓存）本来就在宿主（musicHost → MusicEngine），
 *   所以控件放主站、逻辑在宿主，插件只负责曲库与搜索这些「内容」。
 *
 * 形态（左上角的「⊞」按钮切换，选择会记住）：
 *   bottom / top       — 贴底 / 贴顶的整条播放条
 *   left / right       — 贴左右边缘的平铺面板（封面/歌名/进度/控制/音量/歌词，全部摊开，不折叠成图标列）
 *   window             — 独立窗口：拖动移动、右下角拉伸改大小，位置尺寸都持久化
 *
 * 可见性由外部控制（ChatLayout）：只有 music-player 插件启用时才挂载本组件，
 * 这样「卸掉插件即回到没有音乐的状态」才真正成立。
 */
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

import { BackwardIcon, ForwardIcon, PauseIcon, PlayIcon, StopIcon } from 'tdesign-icons-vue-next'

import { useAppI18n } from '@/composables/useAppI18n'
import {
  WINDOW_MIN_H,
  WINDOW_MIN_W,
  clampWindow,
  playerLayout,
  setPlayerMode,
  type PlayerLayoutMode,
} from '@/composables/usePlayerLayout'
import * as musicHost from '@/music/musicHost'

defineProps<{ expanded: boolean }>()
const emit = defineEmits<{ expand: [] }>()

const { t } = useAppI18n()

// ===== 播放状态轮询（描述式 UI 拿不到连续事件，控件自己拉）=====
const snap = ref(musicHost.musicState())
const lyric = ref(musicHost.musicLyricView())
let timer: ReturnType<typeof setInterval> | null = null

function refresh() {
  snap.value = musicHost.musicState()
  lyric.value = musicHost.musicLyricView()
}

const playing = computed(() => snap.value.status === 'playing')
const busy = computed(() => ['loading', 'buffering', 'decrypting'].includes(snap.value.status))
const progressPct = computed(() => {
  const d = snap.value.duration
  return d > 0 ? Math.min(100, (snap.value.position / d) * 100) : 0
})

const mode = computed(() => playerLayout.mode)
const isWindow = computed(() => mode.value === 'window')
const isSide = computed(() => mode.value === 'left' || mode.value === 'right')

const rootStyle = computed(() => {
  if (!isWindow.value) return undefined
  return {
    left: `${playerLayout.x}px`,
    top: `${playerLayout.y}px`,
    width: `${playerLayout.w}px`,
    height: `${playerLayout.h}px`,
  }
})

const statusText = computed(() => {
  const s = snap.value
  switch (s.status) {
    case 'loading':
      return t('music.statusLoading')
    case 'buffering':
      return t('music.statusBuffering')
    case 'decrypting':
      return t('music.statusDecrypting')
    case 'error':
      return s.error || t('music.statusError')
    default:
      return s.fromCache ? t('music.fromCache') : ''
  }
})

async function toggle() {
  await musicHost.musicToggle()
  refresh()
}

async function next() {
  await musicHost.musicNext()
  refresh()
}

async function prev() {
  await musicHost.musicPrev()
  refresh()
}

// ===== 播放模式（列表循环 / 列表播放 / 单曲循环）=====
const REPEAT_ICON: Record<string, string> = { all: '🔁', off: '↪️', one: '🔂' }
const repeatMode = ref<'all' | 'off' | 'one'>(musicHost.musicRepeat())
const repeatLabel = computed(() => {
  const m = repeatMode.value || 'all'
  if (m === 'off') return t('music.repeatOff')
  if (m === 'one') return t('music.repeatOne')
  return t('music.repeatAll')
})

function cycleRepeat() {
  const order: Array<'all' | 'off' | 'one'> = ['all', 'off', 'one']
  const cur = repeatMode.value as 'all' | 'off' | 'one'
  const idx = order.indexOf(cur)
  repeatMode.value = order[(idx + 1) % order.length] ?? 'all'
  musicHost.musicSetRepeat(repeatMode.value)
  refresh()
}

async function stop() {
  musicHost.musicStop()
  refresh()
}

function onSeek(e: Event) {
  const v = Number((e.target as HTMLInputElement).value)
  void musicHost.musicSeek(v).then(refresh)
}

function onVolume(e: Event) {
  musicHost.musicVolume(Number((e.target as HTMLInputElement).value))
  refresh()
}

// ===== 形态切换 =====
const MODE_ITEMS: { value: PlayerLayoutMode; key: string; icon: string }[] = [
  { value: 'top', key: 'music.layoutTop', icon: '⤒' },
  { value: 'left', key: 'music.layoutLeft', icon: '⤆' },
  { value: 'right', key: 'music.layoutRight', icon: '⤇' },
  { value: 'bottom', key: 'music.layoutBottom', icon: '⤓' },
  { value: 'window', key: 'music.layoutWindow', icon: '⧉' },
]

const menuOpen = ref(false)
const rootEl = ref<HTMLElement | null>(null)

function pickMode(m: PlayerLayoutMode) {
  menuOpen.value = false
  if (m === 'window' && (playerLayout.x < 0 || playerLayout.y < 0)) {
    // 首次进入独立窗口：贴右下角放，且不盖住正在看的输入框
    playerLayout.x = Math.max(0, window.innerWidth - playerLayout.w - 24)
    playerLayout.y = Math.max(0, window.innerHeight - playerLayout.h - 24)
  }
  setPlayerMode(m)
  if (m === 'window') clampWindow()
}

function onDocPointerDown(e: PointerEvent) {
  if (!menuOpen.value) return
  if (rootEl.value && !rootEl.value.contains(e.target as Node)) menuOpen.value = false
}

// ===== 独立窗口：拖动 =====
let dragFrom: { dx: number; dy: number } | null = null

function onDragStart(e: PointerEvent) {
  if (!isWindow.value) return
  // 点在按钮/输入框上时不要开始拖动
  if ((e.target as HTMLElement).closest('button, input, label')) return
  dragFrom = { dx: e.clientX - playerLayout.x, dy: e.clientY - playerLayout.y }
  ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
}

function onDragMove(e: PointerEvent) {
  if (!dragFrom) return
  playerLayout.x = e.clientX - dragFrom.dx
  playerLayout.y = e.clientY - dragFrom.dy
  clampWindow()
}

function onDragEnd(e: PointerEvent) {
  dragFrom = null
  try {
    ;(e.currentTarget as HTMLElement).releasePointerCapture(e.pointerId)
  } catch {
    /* 指针已释放 */
  }
}

// ===== 独立窗口：右下角缩放 =====
let resizeFrom: { x: number; y: number; w: number; h: number } | null = null

function onResizeStart(e: PointerEvent) {
  e.preventDefault()
  resizeFrom = { x: e.clientX, y: e.clientY, w: playerLayout.w, h: playerLayout.h }
  ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
}

function onResizeMove(e: PointerEvent) {
  if (!resizeFrom) return
  playerLayout.w = Math.max(WINDOW_MIN_W, resizeFrom.w + (e.clientX - resizeFrom.x))
  playerLayout.h = Math.max(WINDOW_MIN_H, resizeFrom.h + (e.clientY - resizeFrom.y))
  clampWindow()
}

function onResizeEnd(e: PointerEvent) {
  resizeFrom = null
  try {
    ;(e.currentTarget as HTMLElement).releasePointerCapture(e.pointerId)
  } catch {
    /* 指针已释放 */
  }
}

function onWindowResize() {
  if (isWindow.value) clampWindow()
}

onMounted(() => {
  refresh()
  timer = setInterval(refresh, 1000)
  document.addEventListener('pointerdown', onDocPointerDown)
  window.addEventListener('resize', onWindowResize)
  if (isWindow.value) clampWindow()
})

onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
  document.removeEventListener('pointerdown', onDocPointerDown)
  window.removeEventListener('resize', onWindowResize)
})

function fmt(sec: number): string {
  if (!Number.isFinite(sec) || sec < 0) sec = 0
  const m = Math.floor(sec / 60)
  const s = Math.floor(sec % 60)
  return `${m}:${s.toString().padStart(2, '0')}`
}
</script>

<template>
  <div
    ref="rootEl"
    class="mc-bbar"
    :class="[`mc-bbar--${mode}`, { 'is-window': isWindow, 'is-side': isSide }]"
    :style="rootStyle"
    role="region"
    :aria-label="t('music.title')"
  >
    <!-- 头部：贴边（平铺）与横向两种排布，内容一致 -->
    <div v-if="isSide" class="mc-bbar__side-head">
      <div class="mc-bbar__cover">
        <img v-if="snap.song?.cover_url" :src="snap.song.cover_url" alt="" decoding="async" />
        <span v-else class="mc-bbar__note">♪</span>
      </div>

      <div class="mc-bbar__meta">
        <p class="mc-bbar__title">{{ snap.song?.title || t('music.noTrack') }}</p>
        <p class="mc-bbar__sub">
          {{ snap.song?.artist || t('music.pickSong') }}
          <span v-if="statusText" class="mc-bbar__status" :class="{ 'is-error': snap.status === 'error' }">
            · {{ statusText }}
          </span>
        </p>
      </div>

      <div class="mc-bbar__side-tools">
        <button
          type="button"
          class="mc-bbar__icon-btn"
          :aria-label="t('music.expand')"
          :title="t('music.expand')"
          @click="emit('expand')"
        >
          ⤢
        </button>
        <button
          type="button"
          class="mc-bbar__icon-btn"
          :aria-label="t('music.layoutTitle')"
          :title="t('music.layoutTitle')"
          :aria-expanded="menuOpen"
          @click="menuOpen = !menuOpen"
        >
          ⊞
        </button>
      </div>
    </div>

    <div
      v-else
      class="mc-bbar__head"
      :class="{ 'is-draggable': isWindow }"
      @pointerdown="onDragStart"
      @pointermove="onDragMove"
      @pointerup="onDragEnd"
      @pointercancel="onDragEnd"
    >
      <div class="mc-bbar__cover">
        <img v-if="snap.song?.cover_url" :src="snap.song.cover_url" alt="" decoding="async" />
        <span v-else class="mc-bbar__note">♪</span>
      </div>

      <div class="mc-bbar__meta">
        <p class="mc-bbar__title">{{ snap.song?.title || t('music.noTrack') }}</p>
        <p class="mc-bbar__sub">
          {{ snap.song?.artist || t('music.pickSong') }}
          <span v-if="statusText" class="mc-bbar__status" :class="{ 'is-error': snap.status === 'error' }">
            · {{ statusText }}
          </span>
        </p>
      </div>

      <!-- 独立窗口是竖向排布，行尾按钮会被裁掉 —— 放进头部，永远可点 -->
      <div v-if="isWindow" class="mc-bbar__head-tools">
        <button
          type="button"
          class="mc-bbar__icon-btn"
          :aria-label="t('music.expand')"
          :title="t('music.expand')"
          @click="emit('expand')"
        >
          ⤢
        </button>
        <button
          type="button"
          class="mc-bbar__icon-btn"
          :aria-label="t('music.layoutTitle')"
          :title="t('music.layoutTitle')"
          :aria-expanded="menuOpen"
          @click="menuOpen = !menuOpen"
        >
          ⊞
        </button>
      </div>
    </div>

    <div class="mc-bbar__ctrl">
      <button type="button" class="mc-bbar__btn" :aria-label="t('music.repeatTitle')" :title="repeatLabel" @click="cycleRepeat">
        {{ REPEAT_ICON[repeatMode] }}
      </button>
      <button type="button" class="mc-bbar__btn" :aria-label="t('music.prev')" @click="prev">
        <BackwardIcon />
      </button>
      <button
        type="button"
        class="mc-bbar__btn mc-bbar__btn--main"
        :disabled="!snap.song"
        :aria-label="playing ? t('music.pause') : t('music.play')"
        @click="toggle"
      >
        <PauseIcon v-if="playing" />
        <PlayIcon v-else />
      </button>
      <button type="button" class="mc-bbar__btn" :aria-label="t('music.next')" @click="next">
        <ForwardIcon />
      </button>
      <button type="button" class="mc-bbar__btn" :aria-label="t('music.stop')" @click="stop">
        <StopIcon />
      </button>
    </div>

    <div class="mc-bbar__timeline">
      <span class="num">{{ fmt(snap.position) }}</span>
      <input
        class="mc-bbar__range"
        type="range"
        min="0"
        :max="Math.max(0.1, snap.duration)"
        step="0.1"
        :value="snap.position"
        :disabled="!snap.song || busy"
        :aria-label="t('music.seek')"
        :style="{ '--pct': progressPct + '%' }"
        @input="onSeek"
      />
      <span class="num">{{ fmt(snap.duration) }}</span>
    </div>

    <p class="mc-bbar__lyric" :title="lyric.lines[lyric.active] ?? ''">
      {{ lyric.lines[lyric.active] || '' }}
    </p>

    <label class="mc-bbar__vol">
      <span aria-hidden="true">🔈</span>
      <input
        class="mc-bbar__range mc-bbar__range--vol"
        type="range"
        min="0"
        max="1"
        step="0.01"
        :value="snap.volume"
        :aria-label="t('music.volume')"
        @input="onVolume"
      />
    </label>

    <!-- 行尾按钮只给贴顶 / 贴底：贴边在头部，独立窗口也在头部（竖向排布会被裁掉） -->
    <div v-if="!isSide && !isWindow" class="mc-bbar__tail">
      <button
        type="button"
        class="mc-bbar__icon-btn"
        :aria-label="t('music.expand')"
        :title="t('music.expand')"
        @click="emit('expand')"
      >
        ⤢
      </button>

      <button
        type="button"
        class="mc-bbar__icon-btn"
        :aria-label="t('music.layoutTitle')"
        :title="t('music.layoutTitle')"
        :aria-expanded="menuOpen"
        @click="menuOpen = !menuOpen"
      >
        ⊞
      </button>
    </div>

    <div v-if="menuOpen" class="mc-bbar__menu" role="menu">
      <button
        v-for="item in MODE_ITEMS"
        :key="item.value"
        type="button"
        class="mc-bbar__menu-item"
        :class="{ 'is-active': mode === item.value }"
        role="menuitemradio"
        :aria-checked="mode === item.value"
        @click="pickMode(item.value)"
      >
        <span class="mc-bbar__menu-icon" aria-hidden="true">{{ item.icon }}</span>
        {{ t(item.key) }}
      </button>
    </div>

    <!-- 独立窗口的缩放手柄 -->
    <span
      v-if="isWindow"
      class="mc-bbar__resize"
      :aria-label="t('music.layoutResize')"
      @pointerdown="onResizeStart"
      @pointermove="onResizeMove"
      @pointerup="onResizeEnd"
      @pointercancel="onResizeEnd"
    />
  </div>
</template>

<style scoped>
/* ===== 基础容器 ===== */
.mc-bbar {
  position: fixed;
  /* 低于 UiDrawer 遮罩(2500)：抽屉打开时播放控件自动被盖住，不挡面板内容 */
  z-index: 2400;
  box-sizing: border-box;
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 0 16px;
  background: var(--td-bg-color-container);
  color: var(--td-text-color-primary);
}

/* 贴顶 / 贴底：整条横条贴住视口上/下边缘。
 * 一行从左到右排「信息 | 控制 | 进度 | 歌词 | 音量 | 按钮」，
 * 歌词与音量允许收缩/隐藏，保证窄窗口下也不换行。 */
.mc-bbar--top,
.mc-bbar--bottom {
  left: 0;
  right: 0;
  height: 64px;
  padding: 0 16px;
}

.mc-bbar--top {
  top: 0;
  border-bottom: 1px solid var(--mc-hairline);
}

.mc-bbar--bottom {
  bottom: 0;
  border-top: 1px solid var(--mc-hairline);
}

.mc-bbar--top .mc-bbar__head,
.mc-bbar--bottom .mc-bbar__head {
  flex: 0 1 240px;
  min-width: 120px;
}

.mc-bbar__tail {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 0 0 auto;
}

/* 平铺（tile）：贴住四边时不要投影、不要圆角、不要半透明，
 * 只在与内容相接的一侧留 1px 细线 —— 视觉上和相邻面板是同一块面，
 * 而不是浮在上面的一个控件卡片。 */
.mc-bbar--top,
.mc-bbar--bottom,
.mc-bbar--left,
.mc-bbar--right {
  border-radius: 0;
  box-shadow: none;
  background: var(--td-bg-color-container);
  backdrop-filter: none;
}

/* 贴顶/贴底时进度条占中间弹性宽度，歌词允许收缩截断 */
.mc-bbar--top .mc-bbar__timeline,
.mc-bbar--bottom .mc-bbar__timeline {
  flex: 1 1 auto;
  min-width: 120px;
}

.mc-bbar--top .mc-bbar__lyric,
.mc-bbar--bottom .mc-bbar__lyric {
  flex: 0 1 200px;
  min-width: 0;
}

/* 贴左右：平铺面板。
 * 宽度必须与 ChatLayout 的 .has-player-left/right 留白一致（264px），
 * 否则面板会盖住聊天区；改这里记得同步改 ChatLayout.vue 的 padding。 */
.mc-bbar--left,
.mc-bbar--right {
  top: 0;
  bottom: 0;
  width: 264px;
  padding: 16px 14px;
  flex-direction: column;
  align-items: stretch;
  justify-content: center;
  gap: 12px;
}

/* 贴边时各块按「信息 → 进度 → 控制 → 歌词 → 音量」从上到下摊开 */
.mc-bbar--left .mc-bbar__timeline,
.mc-bbar--right .mc-bbar__timeline {
  order: 1;
  flex: 0 0 auto;
  min-width: 0;
}

.mc-bbar--left .mc-bbar__ctrl,
.mc-bbar--right .mc-bbar__ctrl {
  order: 2;
  justify-content: center;
}

.mc-bbar--left .mc-bbar__lyric,
.mc-bbar--right .mc-bbar__lyric {
  order: 3;
  flex: 0 0 auto;
  text-align: left;
}

.mc-bbar--left .mc-bbar__vol,
.mc-bbar--right .mc-bbar__vol {
  order: 4;
}

.mc-bbar--left .mc-bbar__range--vol,
.mc-bbar--right .mc-bbar__range--vol {
  flex: 1 1 auto;
  width: auto;
}

.mc-bbar--left {
  left: 0;
  border-right: 1px solid var(--mc-hairline);
}

.mc-bbar--right {
  right: 0;
  border-left: 1px solid var(--mc-hairline);
}

/* 独立窗口 */
/* 独立窗口：竖向摊开。
 * 不写 overflow:hidden —— 形态菜单要在窗口内弹出，裁切会让它整块消失。 */
.mc-bbar--window {
  flex-direction: column;
  align-items: stretch;
  gap: 8px;
  padding: 10px 12px 12px;
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-md);
  background: var(--td-bg-color-container);
  box-shadow: var(--mc-elev-3);
  overflow: visible;
}

.mc-bbar--window .mc-bbar__head {
  flex: 0 0 auto;
  min-width: 0;
}

/* 头部右侧的「展开 / 形态」按钮 */
.mc-bbar__head-tools {
  flex: 0 0 auto;
  display: flex;
  gap: 6px;
  margin-left: auto;
}

/* ===== 顶部信息区（窗口模式下即拖动把手） ===== */
.mc-bbar__head {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
  flex: 0 0 auto;
}

.mc-bbar__head.is-draggable {
  cursor: grab;
  user-select: none;
  touch-action: none;
}

.mc-bbar__head.is-draggable:active {
  cursor: grabbing;
}

.mc-bbar--bottom .mc-bbar__head,
.mc-bbar--top .mc-bbar__head {
  flex: 0 1 240px;
  min-width: 120px;
}

/* 封面 */
.mc-bbar__cover {
  flex: 0 0 auto;
  width: 44px;
  height: 44px;
  border-radius: var(--mc-radius-sm);
  overflow: hidden;
  display: grid;
  place-items: center;
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-text-color-placeholder);
}

.mc-bbar--left .mc-bbar__cover,
.mc-bbar--right .mc-bbar__cover {
  width: 48px;
  height: 48px;
}

/* 贴边形态的头部：封面 + 歌名 + 两个图标按钮（展开 / 形态） */
.mc-bbar__side-head {
  display: flex;
  align-items: center;
  gap: 10px;
  order: 0;
  min-width: 0;
}

.mc-bbar__side-tools {
  flex: 0 0 auto;
  display: flex;
  gap: 6px;
  margin-left: auto;
}

.mc-bbar__cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.mc-bbar__note {
  font-size: 20px;
}

/* 曲名 */
.mc-bbar__meta {
  flex: 1 1 auto;
  min-width: 0;
}

.mc-bbar__title {
  margin: 0;
  font-size: var(--mc-fs-body);
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mc-bbar__sub {
  margin: 2px 0 0;
  font-size: var(--mc-fs-caption);
  color: var(--td-text-color-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mc-bbar__status.is-error {
  color: var(--td-error-color);
}

/* ===== 控制按钮 ===== */
.mc-bbar__ctrl {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: 4px;
}

.mc-bbar__btn {
  display: grid;
  place-items: center;
  width: 32px;
  height: 32px;
  border: 0;
  border-radius: 50%;
  background: transparent;
  color: var(--td-text-color-primary);
  cursor: pointer;
  font-size: 18px;
}

.mc-bbar__btn:hover {
  background: var(--td-bg-color-secondarycontainer);
}

.mc-bbar__btn--main {
  width: 38px;
  height: 38px;
  background: var(--td-brand-color);
  color: #fff;
}

.mc-bbar__btn--main:hover {
  background: var(--td-brand-color-hover);
}

.mc-bbar__btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

/* ===== 进度 ===== */
.mc-bbar__timeline {
  flex: 1 1 auto;
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 120px;
}

.mc-bbar--window .mc-bbar__timeline {
  min-width: 0;
}

.num {
  flex: 0 0 auto;
  font-size: var(--mc-fs-caption);
  font-variant-numeric: tabular-nums;
  color: var(--td-text-color-secondary);
}

.mc-bbar__range {
  flex: 1 1 auto;
  min-width: 60px;
  height: 4px;
  appearance: none;
  border-radius: 999px;
  cursor: pointer;
  background: linear-gradient(
    to right,
    var(--td-brand-color) 0%,
    var(--td-brand-color) var(--pct, 0%),
    var(--td-bg-color-secondarycontainer) var(--pct, 0%),
    var(--td-bg-color-secondarycontainer) 100%
  );
}

.mc-bbar__range::-webkit-slider-thumb {
  appearance: none;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: var(--td-brand-color);
  border: 2px solid var(--td-bg-color-container);
}

/* ===== 当前歌词句 ===== */
.mc-bbar__lyric {
  flex: 0 0 240px;
  margin: 0;
  font-size: var(--mc-fs-sm);
  color: var(--td-brand-color);
  text-align: center;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mc-bbar--window .mc-bbar__lyric {
  flex: 0 0 auto;
  min-width: 0;
  text-align: left;
}

/* ===== 音量 ===== */
.mc-bbar__vol {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--td-text-color-placeholder);
}

.mc-bbar__range--vol {
  width: 72px;
  flex: 0 0 auto;
  background: var(--td-bg-color-secondarycontainer);
}

/* ===== 图标按钮（展开 / 形态） ===== */
.mc-bbar__icon-btn {
  flex: 0 0 auto;
  width: 30px;
  height: 30px;
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-sm);
  background: transparent;
  color: var(--td-text-color-secondary);
  cursor: pointer;
  font-size: 14px;
  line-height: 1;
}

.mc-bbar__icon-btn:hover {
  color: var(--td-brand-color);
  border-color: var(--td-brand-color);
}

/* ===== 形态菜单 ===== */
.mc-bbar__menu {
  position: absolute;
  z-index: 1;
  min-width: 168px;
  padding: 6px;
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-md);
  background: var(--td-bg-color-container);
  box-shadow: 0 12px 32px -12px rgba(0, 0, 0, 0.45);
}

/* 贴顶：菜单往下弹；贴底：菜单往上弹（100% 是横条自身高度 64px，不会出屏） */
.mc-bbar--top .mc-bbar__menu {
  left: 16px;
  top: calc(100% + 8px);
}

.mc-bbar--bottom .mc-bbar__menu {
  left: 16px;
  bottom: calc(100% + 8px);
}

.mc-bbar--left .mc-bbar__menu {
  left: calc(100% + 8px);
  top: 16px;
}

.mc-bbar--right .mc-bbar__menu {
  right: calc(100% + 8px);
  top: 16px;
}

/* 窗口形态的菜单往窗口内部下方弹：窗口贴屏幕顶端时，往外弹会跑到视口外 */
.mc-bbar--window .mc-bbar__menu {
  right: 10px;
  top: 46px;
  z-index: 2;
  min-width: 152px;
}

.mc-bbar__menu-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px 10px;
  border: 0;
  border-radius: var(--mc-radius-sm);
  background: transparent;
  color: var(--td-text-color-primary);
  font-size: var(--mc-fs-sm);
  text-align: left;
  cursor: pointer;
}

.mc-bbar__menu-item:hover {
  background: var(--td-bg-color-secondarycontainer);
}

.mc-bbar__menu-item.is-active {
  color: var(--td-brand-color);
  font-weight: 600;
}

.mc-bbar__menu-icon {
  width: 16px;
  text-align: center;
  color: var(--td-text-color-placeholder);
}

/* ===== 缩放手柄 ===== */
.mc-bbar__resize {
  position: absolute;
  right: 0;
  bottom: 0;
  width: 18px;
  height: 18px;
  cursor: nwse-resize;
  touch-action: none;
  background: repeating-linear-gradient(
    135deg,
    var(--mc-hairline) 0 1px,
    transparent 1px 4px
  );
  border-bottom-right-radius: var(--mc-radius-md);
}

/* ===== 窄屏适配 ===== */
@media (max-width: 900px) {
  /* 窄屏放不下横条里的歌词/音量：只留信息 + 进度 + 控制 */
  .mc-bbar--top .mc-bbar__lyric,
  .mc-bbar--bottom .mc-bbar__lyric,
  .mc-bbar--top .mc-bbar__vol,
  .mc-bbar--bottom .mc-bbar__vol,
  .mc-bbar--left .mc-bbar__lyric,
  .mc-bbar--right .mc-bbar__lyric {
    display: none;
  }

  /* 贴左右收窄到 216px（ChatLayout 的留白也按 216px）；
   * 贴顶/贴底是横条，只需收紧左右内边距 */
  .mc-bbar--left,
  .mc-bbar--right {
    width: 216px;
    padding: 12px 10px;
    gap: 10px;
  }

  .mc-bbar--top,
  .mc-bbar--bottom {
    padding: 0 10px;
    gap: 10px;
  }
}
</style>
