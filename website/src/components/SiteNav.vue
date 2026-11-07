<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRoute } from 'vue-router'

import UiIcon from './UiIcon.vue'
import { toggleLang } from '@/i18n'
import { REPO, VERSION } from '@/links'
import { isDark, toggleTheme } from '@/theme'

const { t } = useI18n()
const route = useRoute()

const dark = ref(isDark())
const open = ref(false)

function onTheme() {
  toggleTheme()
  dark.value = isDark()
}

function onLang() {
  toggleLang()
}

/** 导航：首页内区块用锚点，独立页面用路由；顺序＝阅读顺序（先看能力，再看生态） */
const items = computed(() => [
  { key: 'capabilities', to: { path: '/', hash: '#capabilities' }, label: t('nav.capabilities') },
  { key: 'plugins', to: { path: '/', hash: '#plugins' }, label: t('nav.plugins') },
  { key: 'deploy', to: { path: '/', hash: '#deploy' }, label: t('nav.deploy') },
  { key: 'docs', to: { path: '/docs' }, label: t('nav.docs') },
  { key: 'market', to: { path: '/plugins' }, label: t('nav.market') },
])

const isDocs = computed(() => route.name === 'docs')
const isMarket = computed(() => route.name === 'plugins')

function isCurrent(key: string) {
  return (key === 'docs' && isDocs.value) || (key === 'market' && isMarket.value)
}

watch(
  () => route.fullPath,
  () => {
    open.value = false
  },
)

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') open.value = false
}
window.addEventListener('keydown', onKey)
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <header class="nav">
    <div class="container nav__inner">
      <RouterLink class="brand" to="/" :aria-label="t('site.title')">
        <span class="brand__mark">
          <img src="/favicon.ico" alt="" width="22" height="22" />
        </span>
        <span class="brand__name">ModernChat</span>
        <span class="brand__ver mono">v{{ VERSION }}</span>
      </RouterLink>

      <nav class="nav__links" :aria-label="t('nav.primary')">
        <RouterLink
          v-for="item in items"
          :key="item.key"
          :to="item.to"
          class="nav__link"
          :class="{ 'is-current': isCurrent(item.key) }"
        >
          {{ item.label }}
        </RouterLink>
      </nav>

      <div class="nav__actions">
        <a class="nav__repo" :href="REPO" target="_blank" rel="noopener">
          <span>{{ t('nav.repo') }}</span>
          <UiIcon name="arrow-up-right" :size="14" />
        </a>
        <button
          class="icon-btn"
          type="button"
          :aria-label="dark ? t('nav.themeLight') : t('nav.themeDark')"
          :title="dark ? t('nav.themeLight') : t('nav.themeDark')"
          @click="onTheme"
        >
          <UiIcon :name="dark ? 'sun' : 'moon'" :size="17" />
        </button>
        <button class="icon-btn icon-btn--text" type="button" :title="t('nav.lang')" @click="onLang">
          {{ t('langNext') }}
        </button>
        <button
          class="icon-btn icon-btn--menu"
          type="button"
          :aria-expanded="open"
          :aria-label="open ? t('nav.closeMenu') : t('nav.openMenu')"
          @click="open = !open"
        >
          <UiIcon :name="open ? 'close' : 'menu'" :size="18" />
        </button>
      </div>
    </div>

    <transition name="sheet">
      <div v-if="open" class="nav__sheet">
        <div class="container nav__sheet-inner">
          <RouterLink
            v-for="item in items"
            :key="item.key"
            :to="item.to"
            class="nav__sheet-link"
            :class="{ 'is-current': isCurrent(item.key) }"
          >
            {{ item.label }}
            <UiIcon name="arrow-right" :size="15" />
          </RouterLink>
          <a class="nav__sheet-link" :href="REPO" target="_blank" rel="noopener">
            {{ t('nav.repo') }}
            <UiIcon name="arrow-up-right" :size="15" />
          </a>
        </div>
      </div>
    </transition>
  </header>
</template>
