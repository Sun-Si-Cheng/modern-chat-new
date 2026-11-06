<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import {
  Button as TButton,
  Input as TInput,
  Table as TTable,
  TabPanel as TTabPanel,
  Tabs as TTabs,
  Tag as TTag,
} from 'tdesign-vue-next'

import DemoChat from '@/components/DemoChat.vue'
import { currentLang, pickList } from '@/i18n'

const { t } = useI18n()

const qsTab = ref('docker')

const qsDocker = `git clone https://gitee.com/Sun-Si-Cheng/modern-chat-new.git
cd modern-chat-new
docker compose up -d --build        # MySQL 8 + backend :8080，首次启动自动建表
cd web && npm install && npm run dev  # frontend :5173`

const qsManual = `# backend：自备 MySQL 8.0
cd server
cp .env.example .env      # 填 DB_DSN / JWT_SECRET / FRONTEND_URL
go run ./cmd/server       # :8080

# frontend
cd ../web
npm install
npm run dev               # :5173，已代理 /api 与 /ws`

const qsVerify = `curl http://127.0.0.1:8080/api/health    # {"status":"ok"}
curl http://127.0.0.1:8080/api/version   # {version, commit, buildTime}

# 打开 http://localhost:5173 → 注册首个账号（自动成为站长）
# 上传 plugins/examples/weather.plugin.js 体验插件系统`

const pluginSample = `/* {
  "name": "weather-demo",
  "version": "1.0.0",
  "permissions": ["message:read", "message:send",
                  "message:render", "ui:panel", "storage"],
  "allowedHosts": ["*.openweathermap.org"]
} */
function __mcActivate(api) {
  // 注册自定义消息类型：主站在气泡内渲染
  api.registerMessageRenderer('weather', (p) => ({
    body: [{ component: 'text',
             props: { content: p.city + ' ' + p.sky + ' ' + p.temp + '°C' } }],
  }));

  // 侧边栏面板（描述式 UI）
  api.addSidePanel({
    title: 'Weather',
    body: [
      { component: 'text', props: { content: 'Hangzhou · Sunny 22°C' } },
      { component: 'button', props: { text: 'Refresh' }, action: 'refresh' },
    ],
  });
}`

/** SDK 表（用 TDesign Table 渲染，列定义见 sdkColumns） */
const sdkRows = computed(() => [
  { api: 'api.on(event, fn)', role: t('sdk.r1'), perm: 'message:read' },
  { api: 'api.send(convId, payload, msgType?)', role: t('sdk.r2'), perm: 'message:send' },
  { api: 'api.registerMessageRenderer(type, fn)', role: t('sdk.r3'), perm: 'message:render' },
  { api: 'api.addSidePanel(desc)', role: t('sdk.r4'), perm: 'ui:panel' },
  { api: 'api.storage.get / set', role: t('sdk.r5'), perm: 'storage' },
  { api: 'api.request(url, opts?)', role: t('sdk.r6'), perm: 'net' },
  { api: 'api.theme.apply(cssVars)', role: t('sdk.r7'), perm: 'ui:theme' },
])

const sdkColumns = computed(() => [
  { colKey: 'api', title: t('sdk.api'), width: 300 },
  { colKey: 'role', title: t('sdk.role') },
  { colKey: 'perm', title: t('sdk.perm'), width: 160 },
])

// ===== 在线演示（纯前端模拟，数据不出浏览器） =====
interface PgMsg {
  id: number
  me: boolean
  text: string
}

const pgDark = ref(false)
const pgActive = ref(0)
const pgText = ref('')
const pgPanel = ref(false)
const pgMsgs = ref<Record<number, PgMsg[]>>({})
const pgBody = ref<HTMLElement | null>(null)
let pgSeq = 0
let botTimer: ReturnType<typeof setTimeout> | null = null

const pgConvs = computed(() => {
  const names = pickList(currentLang(), 'pg.names')
  const hues = [210, 150, 330]
  return names.map((name, i) => ({ id: i, name, hue: hues[i] ?? 210 }))
})

const pgCurrent = computed(() => pgConvs.value[pgActive.value] ?? { id: 0, name: '?', hue: 210 })
const pgMessages = computed(() => pgMsgs.value[pgActive.value] ?? [])

function pgScroll() {
  nextTick(() => {
    const el = pgBody.value
    if (el) el.scrollTop = el.scrollHeight
  })
}

function pgSend() {
  const text = pgText.value.trim()
  if (!text) return
  const id = pgActive.value
  ;(pgMsgs.value[id] ??= []).push({ id: ++pgSeq, me: true, text })
  pgText.value = ''
  pgScroll()

  // 模拟对方在 0.8s 后回复（取本地化语料）
  if (botTimer) clearTimeout(botTimer)
  botTimer = setTimeout(() => {
    const pool = pickList(currentLang(), 'pg.bot')
    const reply = pool[Math.floor(Math.random() * pool.length)] ?? '…'
    ;(pgMsgs.value[id] ??= []).push({ id: ++pgSeq, me: false, text: reply })
    pgScroll()
  }, 800)
}

function pgSwitch(id: number) {
  pgActive.value = id
  pgScroll()
}

function pgReset() {
  pgMsgs.value = {}
  pgText.value = ''
  pgDark.value = false
  pgPanel.value = false
  pgActive.value = 0
}

// ===== 功能特性 =====
const features = computed(() => {
  const icons = ['🔐', '⚡', '💬', '🧩', '🛠️', '📦']
  return icons.map((icon, i) => ({
    icon,
    title: t(`f${i + 1}.t`),
    desc: t(`f${i + 1}.d`),
  }))
})

const whitelist = ['text', 'image', 'button', 'input', 'card', 'list', 'divider', 'link']
const stackBadges = [
  'Vue 3', 'Vite', 'TypeScript', 'Pinia', 'TDesign', 'vue-i18n',
  'Go', 'Gin', 'GORM', 'gorilla/websocket', 'MySQL 8.0', 'JWT HS256',
]
</script>

<template>
  <main>
    <!-- ============ Hero ============ -->
    <section class="hero">
      <div class="container hero-grid">
        <div class="hero-copy">
          <div class="hero-badges">
            <TTag theme="primary" variant="light" size="small">v0.10.3</TTag>
            <TTag variant="outline" size="small">Apache-2.0</TTag>
            <TTag variant="outline" size="small">{{ t('hero.badge-open') }}</TTag>
          </div>
          <h1 v-html="t('hero.h1')" />
          <p class="hero-sub">{{ t('hero.sub') }}</p>
          <div class="hero-cta">
            <a class="btn btn-primary" href="#quickstart">{{ t('hero.cta-start') }}</a>
            <RouterLink class="btn btn-outline" to="/docs">{{ t('hero.cta-docs') }}</RouterLink>
          </div>
          <ul class="hero-points">
            <li>{{ t('hero.p1') }}</li>
            <li>{{ t('hero.p2') }}</li>
            <li>{{ t('hero.p3') }}</li>
          </ul>
        </div>

        <DemoChat />
      </div>
    </section>

    <!-- ============ 在线演示 ============ -->
    <section id="playground" class="section section-alt">
      <div class="container">
        <h2 class="sec-title">{{ t('pg.title') }}</h2>
        <p class="sec-sub">{{ t('pg.sub') }}</p>

        <div class="pg-toolbar">
          <TButton size="small" variant="outline" @click="pgDark = !pgDark">
            {{ pgDark ? '☀️' : '🌙' }} {{ t('pg.dark') }}
          </TButton>
          <TButton size="small" variant="outline" @click="pgReset">↻ {{ t('pg.reset') }}</TButton>
          <span class="pg-note">{{ t('pg.note') }}</span>
        </div>

        <div class="pg-app" :class="{ 'is-dark': pgDark }">
          <div class="pg-rail">
            <span class="pg-me">我</span>
            <button class="pg-rail-btn" type="button" title="Chats">💬</button>
            <button class="pg-rail-btn" type="button" title="Plugins" @click="pgPanel = !pgPanel">
              🧩
            </button>
          </div>

          <div class="pg-list">
            <button
              v-for="c in pgConvs"
              :key="c.id"
              type="button"
              class="pg-conv"
              :class="{ 'is-active': c.id === pgActive }"
              @click="pgSwitch(c.id)"
            >
              <span class="pg-avatar" :style="{ background: `hsl(${c.hue}, 62%, 52%)` }">
                {{ c.name.charAt(0) }}
              </span>
              <span class="pg-conv-name">{{ c.name }}</span>
            </button>
          </div>

          <div class="pg-chat">
            <div class="pg-chat-head">
              <strong>{{ pgCurrent.name }}</strong>
              <span class="text-placeholder">{{ t('pg.online') }}</span>
            </div>

            <div ref="pgBody" class="pg-msgs">
              <div v-for="m in pgMessages" :key="m.id" class="pg-msg" :class="{ 'is-me': m.me }">
                {{ m.text }}
              </div>
              <p v-if="!pgMessages.length" class="pg-hint">{{ t('pg.hint') }}</p>
            </div>

            <div class="pg-inputrow">
              <TInput v-model="pgText" :placeholder="t('pg.placeholder')" @enter="pgSend" />
              <TButton theme="primary" @click="pgSend">{{ t('pg.send') }}</TButton>
            </div>
          </div>

          <aside v-if="pgPanel" class="pg-panel">
            <div class="pg-panel-head">
              <strong>{{ t('pg.pluginTitle') }}</strong>
              <button class="pg-panel-close" type="button" aria-label="Close" @click="pgPanel = false">
                ✕
              </button>
            </div>
            <div class="pg-panel-body">
              <div class="demo-weather">
                <div class="demo-weather-top">
                  <span class="demo-weather-icon">🌤️</span>
                  <span>
                    <span class="demo-weather-city">{{ t('demo.city') }}</span><br />
                    <span class="demo-weather-sky">{{ t('demo.sky') }}</span>
                  </span>
                </div>
                <div class="demo-weather-temp">22°C</div>
              </div>
            </div>
            <p class="pg-panel-note">{{ t('pg.pluginBy') }}</p>
          </aside>
        </div>
      </div>
    </section>

    <!-- ============ 功能特性 ============ -->
    <section id="features" class="section">
      <div class="container">
        <h2 class="sec-title">{{ t('features.title') }}</h2>
        <p class="sec-sub">{{ t('features.sub') }}</p>
        <div class="feature-grid">
          <div v-for="f in features" :key="f.title" class="feature-card">
            <div class="feature-icon">{{ f.icon }}</div>
            <h3>{{ f.title }}</h3>
            <p>{{ f.desc }}</p>
          </div>
        </div>
      </div>
    </section>

    <!-- ============ 插件系统 ============ -->
    <section id="plugins" class="section section-alt">
      <div class="container">
        <h2 class="sec-title">{{ t('plugin.title') }}</h2>
        <p class="sec-sub">{{ t('plugin.sub') }}</p>

        <div class="plugin-grid">
          <div class="plugin-copy">
            <h3>{{ t('plugin.security-t') }}</h3>
            <ul class="check-list">
              <li>{{ t('plugin.s1') }}</li>
              <li>{{ t('plugin.s2') }}</li>
              <li>{{ t('plugin.s3') }}</li>
              <li>{{ t('plugin.s4') }}</li>
            </ul>

            <h3 class="mt">{{ t('plugin.ui-t') }}</h3>
            <div class="chips">
              <TTag v-for="w in whitelist" :key="w" variant="outline" size="small">{{ w }}</TTag>
            </div>
            <p class="muted">{{ t('plugin.ui-note') }}</p>
          </div>

          <div class="plugin-code">
            <div class="code-head">
              <span class="dot r" /><span class="dot y" /><span class="dot g" />
              <span class="code-name">weather.plugin.js</span>
            </div>
            <pre><code>{{ pluginSample }}</code></pre>
          </div>
        </div>

        <div class="table-wrap">
          <TTable
            row-key="api"
            :data="sdkRows"
            :columns="sdkColumns"
            size="small"
            :hover="true"
            table-layout="auto"
          />
        </div>
      </div>
    </section>

    <!-- ============ 快速开始 ============ -->
    <section id="quickstart" class="section">
      <div class="container">
        <h2 class="sec-title">{{ t('qs.title') }}</h2>
        <p class="sec-sub">{{ t('qs.sub') }}</p>

        <TTabs v-model="qsTab" class="qs-tabs-td">
          <TTabPanel value="docker" :label="t('qs.docker')">
            <pre class="qs-pre"><code>{{ qsDocker }}</code></pre>
          </TTabPanel>
          <TTabPanel value="manual" :label="t('qs.manual')">
            <pre class="qs-pre"><code>{{ qsManual }}</code></pre>
          </TTabPanel>
          <TTabPanel value="verify" :label="t('qs.verify')">
            <pre class="qs-pre"><code>{{ qsVerify }}</code></pre>
          </TTabPanel>
        </TTabs>

        <div class="stack">
          <h3 class="stack-title">{{ t('qs.stack') }}</h3>
          <div class="stack-badges">
            <TTag v-for="s in stackBadges" :key="s" variant="outline" size="small">{{ s }}</TTag>
          </div>
        </div>
      </div>
    </section>
  </main>
</template>
