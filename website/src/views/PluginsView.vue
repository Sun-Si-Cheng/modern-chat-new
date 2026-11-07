<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'

import UiIcon from '@/components/UiIcon.vue'
import { currentLang } from '@/i18n'
import { REPO } from '@/links'

const { t } = useI18n()

interface MarketPlugin {
  file: string
  name: string
  version: string
  cat: 'demo'
  zh: string
  en: string
  perms: string[]
}

/** 插件目录：与 public/plugins/*.js 的 manifest 保持一致 */
const PLUGINS: MarketPlugin[] = [
  {
    file: 'weather.plugin.js',
    name: 'weather-demo',
    version: '1.0.0',
    cat: 'demo',
    zh: '侧边栏天气面板，并把天气作为自定义 weather 消息类型发送到会话。',
    en: 'Weather side panel that also posts weather as a custom message type.',
    perms: ['message:read', 'message:send', 'message:render', 'ui:panel', 'storage'],
  },
  {
    file: 'dice.plugin.js',
    name: 'dice',
    version: '1.0.0',
    cat: 'demo',
    zh: '掷骰子面板，结果以自定义 dice 消息发到当前会话，气泡里直接出点数。',
    en: 'Dice panel that posts rolls as custom dice messages rendered in the bubble.',
    perms: ['message:read', 'message:send', 'message:render', 'ui:panel'],
  },
  {
    file: 'word-count.plugin.js',
    name: 'word-count',
    version: '1.0.0',
    cat: 'demo',
    zh: '监听消息事件，统计消息数与字数，侧边栏实时展示并本地持久化。',
    en: 'Listens to message events, counts messages and characters, persisted locally.',
    perms: ['message:read', 'ui:panel', 'storage'],
  },
]

const keyword = ref('')
const cat = ref<'all' | 'demo'>('all')

const cats = computed(() => [
  { value: 'all' as const, label: t('mkt.cat.all') },
  { value: 'demo' as const, label: t('mkt.cat.demo') },
])

const filtered = computed(() => {
  const k = keyword.value.trim().toLowerCase()
  return PLUGINS.filter((p) => {
    if (cat.value !== 'all' && p.cat !== cat.value) return false
    if (!k) return true
    const desc = currentLang() === 'zh' ? p.zh : p.en
    return [p.name, desc, ...p.perms].some((s) => s.toLowerCase().includes(k))
  })
})

const steps = computed(() => [
  { n: '1', title: t('mkt.step1-t'), desc: t('mkt.step1-d') },
  { n: '2', title: t('mkt.step2-t'), desc: t('mkt.step2-d') },
  { n: '3', title: t('mkt.step3-t'), desc: t('mkt.step3-d') },
])

function descOf(p: MarketPlugin) {
  return currentLang() === 'zh' ? p.zh : p.en
}

// 静态资源放在 content/ 下：/plugins 是前端路由，不能同时是目录
function hrefOf(p: MarketPlugin) {
  return `${import.meta.env.BASE_URL}content/plugins/${p.file}`
}

function sourceHref(p: MarketPlugin) {
  return `${REPO}/blob/main/plugins/examples/${p.file}`
}
</script>

<template>
  <main class="container mkt">
    <header class="mkt__head">
      <p class="eyebrow">{{ t('nav.plugins') }}</p>
      <h1>{{ t('mkt.h1') }}</h1>
      <p>{{ t('mkt.sub') }}</p>
    </header>

    <section class="mkt__tools">
      <label class="field">
        <UiIcon name="search" :size="15" />
        <input v-model="keyword" type="search" :placeholder="t('mkt.search')" />
      </label>

      <div class="mkt__cats">
        <button
          v-for="c in cats"
          :key="c.value"
          class="tabs__btn"
          :class="{ 'is-on': cat === c.value }"
          type="button"
          @click="cat = c.value"
        >
          {{ c.label }}
        </button>
      </div>

      <span class="mkt__count">{{ filtered.length }} / {{ PLUGINS.length }}</span>
    </section>

    <section class="mkt__grid">
      <article v-for="p in filtered" :key="p.file" class="mkt__card">
        <header class="mkt__card-head">
          <span class="mkt__icon"><UiIcon name="puzzle" :size="18" /></span>
          <div>
            <h3>{{ p.name }}</h3>
            <span class="mkt__ver mono">v{{ p.version }}</span>
          </div>
          <span class="pill pill--brand mkt__badge">{{ t('mkt.official') }}</span>
        </header>

        <p>{{ descOf(p) }}</p>

        <div class="mkt__perms">
          <span
            v-for="perm in p.perms"
            :key="perm"
            class="tag-perm"
            :class="{ 'tag-perm--net': perm === 'net' }"
          >
            {{ perm }}
          </span>
        </div>

        <footer class="mkt__card-actions">
          <a class="btn btn--primary btn--sm" :href="hrefOf(p)" download>
            <UiIcon name="download" :size="14" />
            {{ t('mkt.download') }}
          </a>
          <a class="btn btn--quiet btn--sm" :href="sourceHref(p)" target="_blank" rel="noopener">
            {{ t('mkt.source') }}
          </a>
        </footer>
      </article>
    </section>

    <p v-if="!filtered.length" class="mkt__empty">{{ t('mkt.empty') }}</p>

    <section class="mkt__how">
      <h2>{{ t('mkt.how-title') }}</h2>
      <div class="steps">
        <div v-for="s in steps" :key="s.n" class="step">
          <span class="step__n">{{ s.n }}</span>
          <h3>{{ s.title }}</h3>
          <p>{{ s.desc }}</p>
        </div>
      </div>
    </section>

    <section class="mkt__pub">
      <div>
        <h2>{{ t('mkt.pub-title') }}</h2>
        <p>
          {{ t('mkt.pub-short') }}
          <RouterLink to="/docs/design.md">{{ t('nav.docs') }}</RouterLink>
        </p>
      </div>
      <a class="btn btn--ghost" :href="REPO" target="_blank" rel="noopener">
        {{ t('mkt.pub-btn') }}
        <UiIcon name="arrow-up-right" :size="15" />
      </a>
    </section>
  </main>
</template>
