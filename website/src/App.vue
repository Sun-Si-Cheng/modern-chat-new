<script setup lang="ts">
import { computed, ref, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, RouterView, useRoute } from 'vue-router'
import { Button as TButton } from 'tdesign-vue-next'

import { toggleLang } from './i18n'
import { isDark, toggleTheme } from './theme'

const { t } = useI18n()
const route = useRoute()

const dark = ref(isDark())

function onToggleTheme() {
  toggleTheme()
  dark.value = isDark()
}

function openRepo() {
  window.open('https://gitee.com/Sun-Si-Cheng/modern-chat-new', '_blank', 'noopener')
}

const isDocs = computed(() => route.name === 'docs')
const isPlugins = computed(() => route.name === 'plugins')

/** 导航项：首页内区块用 hash 锚点，独立页面用路由 */
const navItems = computed(() => [
  { key: 'demo', to: { path: '/', hash: '#playground' }, label: t('nav.demo') },
  { key: 'features', to: { path: '/', hash: '#features' }, label: t('nav.features') },
  { key: 'plugins', to: { path: '/', hash: '#plugins' }, label: t('nav.plugins') },
  { key: 'market', to: { path: '/plugins' }, label: t('nav.market') },
  { key: 'quickstart', to: { path: '/', hash: '#quickstart' }, label: t('nav.quickstart') },
  { key: 'docs', to: { path: '/docs' }, label: t('nav.docs') },
])

// 标题与描述随语言切换（SEO / 分享卡片）
watchEffect(() => {
  document.title = t('site.title')
  const meta = document.querySelector('meta[name="description"]')
  if (meta) meta.setAttribute('content', t('site.desc'))
})
</script>

<template>
  <header class="nav">
    <div class="container nav-inner">
      <RouterLink class="brand" to="/">
        <span class="brand-logo" aria-hidden="true">
          <svg viewBox="0 0 32 32" width="22" height="22">
            <rect x="3" y="5" width="26" height="19" rx="6" fill="#fff" />
            <path d="M11 24l-3 5 7-5z" fill="#fff" />
            <circle cx="11" cy="14.5" r="2" fill="#0052d9" />
            <circle cx="16" cy="14.5" r="2" fill="#0052d9" />
            <circle cx="21" cy="14.5" r="2" fill="#0052d9" />
          </svg>
        </span>
        ModernChat
      </RouterLink>

      <nav class="nav-links">
        <RouterLink
          v-for="item in navItems"
          :key="item.key"
          :to="item.to"
          :class="{
            'is-current': (item.key === 'docs' && isDocs) || (item.key === 'market' && isPlugins),
          }"
        >
          {{ item.label }}
        </RouterLink>
      </nav>

      <div class="nav-actions">
        <TButton variant="text" size="small" @click="openRepo">Gitee</TButton>
        <TButton
          variant="text"
          size="small"
          :aria-label="dark ? t('nav.themeLight') : t('nav.themeDark')"
          @click="onToggleTheme"
        >
          {{ dark ? '☀️' : '🌙' }}
        </TButton>
        <TButton variant="text" size="small" @click="toggleLang">{{ t('langNext') }}</TButton>
      </div>
    </div>
  </header>

  <RouterView />

  <footer class="footer">
    <div class="container footer-inner">
      <div class="footer-brand">ModernChat</div>
      <p>{{ t('footer.line') }}</p>
      <div class="footer-links">
        <RouterLink to="/docs">{{ t('nav.docs') }}</RouterLink>
        <a href="https://gitee.com/Sun-Si-Cheng/modern-chat-new" target="_blank" rel="noopener">Gitee</a>
        <a
          href="https://gitee.com/Sun-Si-Cheng/modern-chat-new/blob/master/LICENSE"
          target="_blank"
          rel="noopener"
        >
          Apache-2.0
        </a>
      </div>
      <p class="footer-copy">© 2026 Sun-Si-Cheng · ModernChat</p>
    </div>
  </footer>
</template>
