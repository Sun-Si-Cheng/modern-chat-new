<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'

import { REPO, RELEASES, VERSION } from '@/links'

const { t } = useI18n()

const columns = [
  {
    title: 'footer.product',
    links: [
      { label: 'nav.capabilities', to: { path: '/', hash: '#capabilities' } },
      { label: 'nav.plugins', to: { path: '/', hash: '#plugins' } },
      { label: 'nav.deploy', to: { path: '/', hash: '#deploy' } },
      { label: 'nav.market', to: '/plugins' },
    ],
  },
  {
    title: 'footer.docs',
    links: [
      { label: 'footer.docs.index', to: '/docs' },
      { label: 'footer.docs.start', to: '/docs/getting-started.md' },
      { label: 'footer.docs.api', to: '/docs/api-spec.md' },
      { label: 'footer.docs.changelog', to: '/docs/changelog.md' },
    ],
  },
  {
    title: 'footer.dev',
    links: [
      { label: 'footer.dev.design', to: '/docs/design.md' },
      { label: 'footer.dev.frontend', to: '/docs/frontend-guide.md' },
      { label: 'footer.dev.ops', to: '/docs/ops-guide.md' },
      { label: 'footer.dev.plugin', to: { path: '/', hash: '#plugins' } },
    ],
  },
] as const
</script>

<template>
  <footer class="foot">
    <div class="container foot__inner">
      <div class="foot__brand">
        <div class="foot__mark">
          <span class="brand__mark"><img src="/favicon.ico" alt="" width="22" height="22" /></span>
          <span>ModernChat</span>
        </div>
        <p class="foot__line">{{ t('footer.line') }}</p>
        <div class="foot__meta">
          <span class="pill pill--mono">Apache-2.0</span>
          <span class="pill pill--mono">Go · Vue 3</span>
        </div>
      </div>

      <div v-for="col in columns" :key="col.title" class="foot__col">
        <h3>{{ t(col.title) }}</h3>
        <RouterLink v-for="l in col.links" :key="l.label" :to="l.to">
          {{ t(l.label) }}
        </RouterLink>
      </div>

      <div class="foot__col">
        <h3>{{ t('footer.repo') }}</h3>
        <a :href="REPO" target="_blank" rel="noopener">Gitee</a>
        <a :href="RELEASES" target="_blank" rel="noopener">{{ t('footer.releases') }}</a>
        <a :href="`${REPO}/blob/main/LICENSE`" target="_blank" rel="noopener">LICENSE</a>
        <a :href="`${REPO}/blob/main/CHANGELOG.md`" target="_blank" rel="noopener">CHANGELOG</a>
      </div>
    </div>

    <div class="container foot__bottom">
      <span>© 2026 Sun-Si-Cheng · ModernChat</span>
      <span class="mono">v{{ VERSION }}</span>
    </div>
  </footer>
</template>
