<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import { Button as TButton, Input as TInput, Tag as TTag } from 'tdesign-vue-next'

import { currentLang } from '@/i18n'

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
    zh: '示例插件：侧边栏天气面板 + 自定义 weather 消息类型',
    en: 'Demo plugin: weather side panel plus a custom weather message type',
    perms: ['message:read', 'message:send', 'message:render', 'ui:panel', 'storage'],
  },
  {
    file: 'dice.plugin.js',
    name: 'dice',
    version: '1.0.0',
    cat: 'demo',
    zh: '示例插件：掷骰子面板，把结果以自定义 dice 消息发到当前会话',
    en: 'Demo plugin: a dice panel that posts results as custom dice messages',
    perms: ['message:read', 'message:send', 'message:render', 'ui:panel'],
  },
  {
    file: 'word-count.plugin.js',
    name: 'word-count',
    version: '1.0.0',
    cat: 'demo',
    zh: '示例插件：统计监听到的消息数与字数，侧边栏实时展示',
    en: 'Demo plugin: counts messages and characters, shown live in a side panel',
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
  { n: 1, title: t('mkt.step1-t'), desc: t('mkt.step1-d') },
  { n: 2, title: t('mkt.step2-t'), desc: t('mkt.step2-d') },
  { n: 3, title: t('mkt.step3-t'), desc: t('mkt.step3-d') },
])

function descOf(p: MarketPlugin) {
  return currentLang() === 'zh' ? p.zh : p.en
}

function hrefOf(p: MarketPlugin) {
  return `${import.meta.env.BASE_URL}plugins/${p.file}`
}

function sourceHref(p: MarketPlugin) {
  return `https://gitee.com/Sun-Si-Cheng/modern-chat-new/blob/master/website/public/plugins/${p.file}`
}
</script>

<template>
  <main class="container mkt-page">
    <section class="mkt-head">
      <h1>{{ t('mkt.h1') }}</h1>
      <p>{{ t('mkt.sub') }}</p>
    </section>

    <section class="mkt-tools">
      <TInput v-model="keyword" :placeholder="t('mkt.search')" clearable />
      <div class="mkt-cats">
        <TButton
          v-for="c in cats"
          :key="c.value"
          size="small"
          :variant="cat === c.value ? 'base' : 'outline'"
          :theme="cat === c.value ? 'primary' : 'default'"
          @click="cat = c.value"
        >
          {{ c.label }}
        </TButton>
      </div>
    </section>

    <section class="mkt-grid">
      <article v-for="p in filtered" :key="p.file" class="mkt-card">
        <header class="mkt-card-head">
          <span class="mkt-card-icon">🧩</span>
          <div>
            <h3>{{ p.name }}</h3>
            <span class="text-placeholder num">v{{ p.version }}</span>
          </div>
          <TTag theme="primary" variant="light" size="small">{{ t('mkt.official') }}</TTag>
        </header>

        <p class="mkt-card-desc">{{ descOf(p) }}</p>

        <div class="mkt-card-perms">
          <span class="text-placeholder">{{ t('mkt.perms') }}</span>
          <TTag v-for="perm in p.perms" :key="perm" variant="outline" size="small">{{ perm }}</TTag>
        </div>

        <footer class="mkt-card-actions">
          <TButton theme="primary" size="small" tag="a" :href="hrefOf(p)" download>
            {{ t('mkt.download') }}
          </TButton>
          <TButton
            variant="text"
            size="small"
            tag="a"
            :href="sourceHref(p)"
            target="_blank"
            rel="noopener"
          >
            {{ t('mkt.source') }}
          </TButton>
        </footer>
      </article>
    </section>

    <p v-if="!filtered.length" class="mkt-empty">{{ t('mkt.empty') }}</p>

    <section class="mkt-how">
      <h2>{{ t('mkt.how-title') }}</h2>
      <div class="mkt-steps">
        <div v-for="s in steps" :key="s.n" class="mkt-step">
          <span class="mkt-step-num">{{ s.n }}</span>
          <h3>{{ s.title }}</h3>
          <p>{{ s.desc }}</p>
        </div>
      </div>
    </section>

    <section class="mkt-publish">
      <div>
        <h2>{{ t('mkt.pub-title') }}</h2>
        <p>
          {{ t('mkt.pub-short') }}
          <RouterLink to="/docs/frontend-guide.md">{{ t('nav.docs') }}</RouterLink>
        </p>
      </div>
      <TButton
        variant="outline"
        tag="a"
        href="https://gitee.com/Sun-Si-Cheng/modern-chat-new"
        target="_blank"
        rel="noopener"
      >
        {{ t('mkt.pub-btn') }}
      </TButton>
    </section>
  </main>
</template>
