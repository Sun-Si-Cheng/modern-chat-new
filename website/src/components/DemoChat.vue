<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button as TButton } from 'tdesign-vue-next'

import { currentLang, pickList } from '@/i18n'

interface DemoMsg {
  id: number
  name: string
  hue: number
  html?: string
  card?: boolean
  temps: string[]
  tempIdx: number
}

const { t, locale } = useI18n()

const messages = ref<DemoMsg[]>([])
const typing = ref(false)
const isDark = ref(false)
const playing = ref(false)

const bodyRef = ref<HTMLElement | null>(null)
const rootRef = ref<HTMLElement | null>(null)

let seq = 0
let timers: ReturnType<typeof setTimeout>[] = []

const weatherTemps = ['22', '21', '23', '20', '24']

function clearTimers() {
  timers.forEach(clearTimeout)
  timers = []
}

function after(ms: number, fn: () => void) {
  timers.push(setTimeout(fn, ms))
}

function push(name: string, hue: number, html: string, card = false) {
  messages.value.push({
    id: ++seq,
    name,
    hue,
    html,
    card,
    temps: weatherTemps,
    tempIdx: 0,
  })
  scrollToEnd()
}

function scrollToEnd() {
  requestAnimationFrame(() => {
    const el = bodyRef.value
    if (el) el.scrollTop = el.scrollHeight
  })
}

/** 时间轴演示：纯文本 → 输入中 → 文本 → 插件卡片 → 输入中 → 文本 → 主题插件生效 */
function runDemo() {
  clearTimers()
  messages.value = []
  typing.value = false
  isDark.value = false
  playing.value = true

  const names = pickList(currentLang(), 'demo.names')
  const n = (i: number) => names[i] ?? '?'

  after(500, () => push(n(0), 210, t('demo.m1')))
  after(1600, () => (typing.value = true))
  after(2800, () => {
    typing.value = false
    push(n(2), 150, t('demo.m2'))
  })
  after(3900, () => push(n(2), 150, '', true))
  after(5200, () => (typing.value = true))
  after(6400, () => {
    typing.value = false
    push(n(3), 330, t('demo.m3'))
  })
  after(7800, () => {
    isDark.value = true
    playing.value = false
  })
}

function refreshCard(msg: DemoMsg) {
  msg.tempIdx = (msg.tempIdx + 1) % msg.temps.length
}

const viaLabel = computed(() => t('demo.via'))

// 语言切换后重播，保证气泡文案同步（与原静态站行为一致）
watch(locale, () => runDemo())

onMounted(() => {
  const el = rootRef.value
  if (!el || !('IntersectionObserver' in window)) {
    runDemo()
    return
  }
  const io = new IntersectionObserver(
    (entries) => {
      if (entries.some((e) => e.isIntersecting)) {
        runDemo()
        io.disconnect()
      }
    },
    { threshold: 0.35 },
  )
  io.observe(el)
})

onBeforeUnmount(clearTimers)
</script>

<template>
  <div class="hero-demo">
    <div ref="rootRef" class="demo-chat" :class="{ 'is-dark': isDark }">
      <div class="demo-head">
        <span class="demo-avatar" aria-hidden="true">产</span>
        <div class="demo-head-meta">
          <strong>{{ t('demo.title') }}</strong>
          <span>{{ t('demo.subtitle') }}</span>
        </div>
        <TButton
          variant="text"
          size="small"
          class="demo-replay"
          :aria-label="t('demo.replay')"
          :title="t('demo.replay')"
          @click="runDemo"
        >
          ↻
        </TButton>
      </div>

      <div ref="bodyRef" class="demo-body" aria-live="polite">
        <div v-for="m in messages" :key="m.id" class="demo-msg">
          <span class="demo-msg-avatar" :style="{ background: `hsl(${m.hue}, 62%, 52%)` }">
            {{ m.name.charAt(0) }}
          </span>
          <div v-if="m.card" class="demo-bubble demo-bubble--card">
            <span class="demo-via">{{ viaLabel }}</span>
            <div class="demo-weather">
              <div class="demo-weather-top">
                <span class="demo-weather-icon">🌤️</span>
                <span>
                  <span class="demo-weather-city">{{ t('demo.city') }}</span><br />
                  <span class="demo-weather-sky">{{ t('demo.sky') }}</span>
                </span>
              </div>
              <div class="demo-weather-temp">{{ m.temps[m.tempIdx] }}°C</div>
              <TButton size="small" variant="outline" @click="refreshCard(m)">
                {{ t('demo.refresh') }}
              </TButton>
            </div>
          </div>
          <div v-else class="demo-bubble" v-html="m.html" />
        </div>

        <div v-if="typing" class="demo-typing">
          <span class="demo-msg-avatar" style="background: hsl(150, 62%, 52%)" />
          <div class="demo-typing-bubble"><i /><i /><i /></div>
        </div>
      </div>

      <div class="demo-composer" aria-hidden="true">
        <span>{{ t('demo.composer') }}</span>
        <span class="demo-send">{{ t('demo.send') }}</span>
      </div>
    </div>

    <p class="demo-caption">{{ t('demo.caption') }}</p>
  </div>
</template>
