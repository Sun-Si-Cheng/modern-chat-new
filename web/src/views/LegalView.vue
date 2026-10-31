<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Button as TButton } from 'tdesign-vue-next'
import { ArrowLeftIcon } from 'tdesign-icons-vue-next'

import QuickPrefs from '@/components/common/QuickPrefs.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import { legalMeta, privacyEn, privacyZh, termsEn, termsZh, type LegalDoc } from '@/content/legal'
import { usePrefsStore } from '@/stores/prefs'

const route = useRoute()
const router = useRouter()
const prefs = usePrefsStore()
const { t } = useAppI18n()

const isTerms = computed(() => route.meta.legal !== 'privacy')

const doc = computed<LegalDoc>(() => {
  if (isTerms.value) return prefs.lang === 'en-US' ? termsEn : termsZh
  return prefs.lang === 'en-US' ? privacyEn : privacyZh
})

/** blocks 中以 "- " 开头的行是列表项 */
function isBullet(line: string): boolean {
  return line.startsWith('- ')
}

function textOf(line: string): string {
  return isBullet(line) ? line.slice(2) : line
}

/** 把连续的 bullet 归并为一个列表，便于渲染 */
function toGroups(blocks: string[]): Array<{ list: boolean; items: string[] }> {
  const groups: Array<{ list: boolean; items: string[] }> = []
  for (const line of blocks) {
    const bullet = isBullet(line)
    const last = groups[groups.length - 1]
    if (bullet && last?.list) last.items.push(textOf(line))
    else groups.push({ list: bullet, items: [textOf(line)] })
  }
  return groups
}

function goBack() {
  if (window.history.length > 1) router.back()
  else void router.push('/login')
}
</script>

<template>
  <div class="legal-page">
    <header class="legal-top">
      <TButton variant="text" size="small" @click="goBack">
        <template #icon><ArrowLeftIcon /></template>
        {{ t('common.back') }}
      </TButton>
      <QuickPrefs />
    </header>

    <article class="legal-doc">
      <h1 class="legal-doc__title">{{ doc.title }}</h1>
      <p class="legal-doc__meta text-placeholder num">
        {{ t('legal.updated') }} {{ doc.updatedAt }} · {{ t('legal.effective') }} {{ legalMeta.effectiveDate }}
      </p>

      <p v-if="prefs.lang === 'en-US'" class="legal-doc__notice">{{ t('legal.zhPrevails') }}</p>

      <p v-for="(p, i) in doc.intro" :key="`intro-${i}`" class="legal-doc__p">{{ p }}</p>

      <section v-for="(sec, i) in doc.sections" :key="`sec-${i}`" class="legal-doc__section">
        <h2 class="legal-doc__h2">{{ sec.heading }}</h2>
        <template v-for="(g, j) in toGroups(sec.blocks)" :key="`g-${i}-${j}`">
          <ul v-if="g.list" class="legal-doc__ul">
            <li v-for="(item, k) in g.items" :key="`li-${i}-${j}-${k}`">{{ item }}</li>
          </ul>
          <p v-else class="legal-doc__p">{{ g.items[0] }}</p>
        </template>
      </section>

      <footer class="legal-doc__footer">
        <RouterLink to="/login">{{ t('forgot.backToLogin') }}</RouterLink>
      </footer>
    </article>
  </div>
</template>

<style scoped>
.legal-page {
  min-height: 100%;
  /* 透出 body 上的全站背景 */
  background: transparent;
  padding-bottom: 64px;
}

.legal-top {
  position: sticky;
  top: 0;
  z-index: 5;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 16px;
  background: var(--td-bg-color-container);
  border-bottom: 1px solid var(--td-component-border);
}

/* 长文放在一块浅色纸面上，背景图 / 纹理不会干扰阅读 */
.legal-doc {
  max-width: 800px;
  margin: 24px auto;
  padding: 36px 40px 28px;
  color: var(--td-text-color-primary);
  font-size: 14px;
  line-height: 1.75;
  background: var(--td-bg-color-container);
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-lg);
  box-shadow: var(--mc-shadow-1);
}

.legal-doc__title {
  margin: 0 0 8px;
  font-size: 26px;
  letter-spacing: -0.01em;
}

.legal-doc__meta {
  margin: 0 0 20px;
  font-size: 12px;
}

.legal-doc__notice {
  margin: 0 0 20px;
  padding: 10px 12px;
  font-size: 12px;
  line-height: 1.6;
  color: var(--td-text-color-secondary);
  background: var(--td-bg-color-secondarycontainer);
  border: 1px solid var(--mc-hairline);
  border-left: 3px solid var(--td-brand-color);
  border-radius: var(--mc-radius-sm);
}

.legal-doc__section {
  margin-top: 26px;
}

.legal-doc__h2 {
  margin: 0 0 8px;
  font-size: 16px;
  font-weight: 600;
}

.legal-doc__p {
  margin: 0 0 10px;
}

.legal-doc__ul {
  margin: 0 0 10px;
  padding-left: 20px;
}

.legal-doc__ul li {
  margin-bottom: 4px;
}

.legal-doc__footer {
  display: flex;
  justify-content: center;
  gap: 16px;
  margin-top: 48px;
  padding-top: 16px;
  border-top: 1px solid var(--mc-hairline);
  font-size: 13px;
  color: var(--td-text-color-secondary);
}

@media (max-width: 720px) {
  .legal-doc {
    padding: 24px 20px 20px;
    margin: 16px 12px;
  }

  .legal-doc__title {
    font-size: 22px;
  }
}
</style>
