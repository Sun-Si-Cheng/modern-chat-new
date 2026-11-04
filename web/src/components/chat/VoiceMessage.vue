<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'

/**
 * 语音消息气泡。
 *
 * 用自定义播放器而不是原生 <audio controls>：
 *   - 原生控件高度 34px 且样式不可控，和消息气泡的观感完全不搭；
 *   - 气泡宽度随时长变化（微信那种），需要读到 duration；
 *   - 同时只允许一条语音在播。
 *
 * 时长不落库：浏览器加载元数据后自己算，省掉改消息结构和后端校验。
 */

withDefaults(defineProps<{ src: string; mine?: boolean }>(), { mine: false })

const audioEl = ref<HTMLAudioElement | null>(null)
const playing = ref(false)
const duration = ref(0)
const current = ref(0)
const failed = ref(false)

/** 气泡宽度随时长增长：1s → 96px，60s 封顶 220px */
const bubbleWidth = computed(() => {
  if (!duration.value) return 96
  const ratio = Math.min(1, duration.value / 30)
  return Math.round(96 + ratio * 124)
})

const label = computed(() => {
  const d = playing.value ? current.value : duration.value
  const s = Math.max(0, Math.round(d))
  const m = Math.floor(s / 60)
  const sec = String(s % 60).padStart(2, '0')
  return duration.value ? `${m}:${sec}` : '--:--'
})

/** 全局同一时刻只播一条 */
const CURRENT_KEY = 'mc-playing-voice'

function onMeta() {
  const el = audioEl.value
  if (!el) return
  // 录制生成的 webm 有时 duration 是 Infinity，seek 到极大值后能逼出真实值
  if (!Number.isFinite(el.duration)) {
    el.currentTime = 1e101
    const fix = () => {
      el.removeEventListener('timeupdate', fix)
      if (Number.isFinite(el.duration)) {
        duration.value = el.duration
        el.currentTime = 0
      }
    }
    el.addEventListener('timeupdate', fix)
    return
  }
  duration.value = el.duration
  current.value = 0
}

function isCurrent(id: string) {
  return (window as unknown as Record<string, string>)[CURRENT_KEY] === id
}

const myId = Math.random().toString(36).slice(2)

/** 抢占播放权：其它语音在 timeupdate 里发现不是自己就会暂停 */
function claimPlayback() {
  ;(window as unknown as Record<string, string>)[CURRENT_KEY] = myId
}

function toggle() {
  const el = audioEl.value
  if (!el || failed.value) return
  if (playing.value) {
    el.pause()
    playing.value = false
    return
  }
  // 停掉其它正在播的语音
  document.querySelectorAll('audio[data-voice]').forEach((a) => {
    if (a !== el) (a as HTMLAudioElement).pause()
  })
  claimPlayback()
  void el.play().catch(() => {
    failed.value = true
  })
}

function onTime() {
  const el = audioEl.value
  if (!el) return
  current.value = el.currentTime
  if (!isCurrent(myId)) {
    el.pause()
    playing.value = false
  }
}

function onEnd() {
  playing.value = false
  current.value = 0
  if (audioEl.value) audioEl.value.currentTime = 0
}

function onPlay() {
  playing.value = true
}
function onPause() {
  playing.value = false
}

onBeforeUnmount(() => {
  audioEl.value?.pause()
})
</script>

<template>
  <div class="mc-voice" :class="{ 'is-mine': mine, 'is-failed': failed }" :style="{ width: `${bubbleWidth}px` }">
    <button
      type="button"
      class="mc-voice__btn"
      :aria-label="playing ? '暂停' : '播放'"
      @click="toggle"
    >
      <!-- 播放 / 暂停 -->
      <svg v-if="!playing" width="14" height="14" viewBox="0 0 24 24" aria-hidden="true">
        <path d="M8 5.5v13l11-6.5z" fill="currentColor" />
      </svg>
      <svg v-else width="14" height="14" viewBox="0 0 24 24" aria-hidden="true">
        <rect x="7" y="5.5" width="3.6" height="13" rx="1.2" fill="currentColor" />
        <rect x="13.4" y="5.5" width="3.6" height="13" rx="1.2" fill="currentColor" />
      </svg>
    </button>

    <!-- 进度条：用 scaleX 而不是 width，避免每帧触发布局 -->
    <span class="mc-voice__track" aria-hidden="true">
      <i class="mc-voice__bar" :style="{ transform: `scaleX(${duration ? current / duration : 0})` }" />
    </span>

    <span class="mc-voice__time num">{{ failed ? '无法播放' : label }}</span>

    <audio
      ref="audioEl"
      data-voice
      :src="src"
      preload="metadata"
      @loadedmetadata="onMeta"
      @timeupdate="onTime"
      @play="onPlay"
      @pause="onPause"
      @ended="onEnd"
      @error="failed = true"
    />
  </div>
</template>

<style scoped>
.mc-voice {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 36px;
  padding: 0 12px;
  border-radius: var(--mc-radius-lg);
  background: var(--td-bg-color-container);
  color: var(--td-text-color-primary);
  transition: width var(--mc-dur-base) var(--mc-ease-out);
}

.mc-voice.is-mine {
  background: var(--td-brand-color);
  color: #fff;
}

.mc-voice.is-failed {
  opacity: 0.6;
}

.mc-voice__btn {
  flex-shrink: 0;
  width: 20px;
  height: 20px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: 50%;
  background: transparent;
  color: inherit;
  cursor: pointer;
  padding: 0;
}

.mc-voice__btn:hover {
  background: color-mix(in srgb, currentcolor 16%, transparent);
}

.mc-voice__track {
  position: relative;
  flex: 1;
  min-width: 28px;
  height: 3px;
  border-radius: 2px;
  background: color-mix(in srgb, currentcolor 26%, transparent);
  overflow: hidden;
}

.mc-voice__bar {
  position: absolute;
  inset: 0;
  transform-origin: left center;
  border-radius: 2px;
  background: currentcolor;
  will-change: transform;
}

.mc-voice__time {
  flex-shrink: 0;
  font-size: var(--mc-fs-caption);
  opacity: 0.85;
}

/* 原生 audio 只用来解码，不显示 */
audio {
  display: none;
}
</style>
