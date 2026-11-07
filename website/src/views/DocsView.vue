<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { marked } from 'marked'
import { Button as TButton } from 'tdesign-vue-next'

import { currentLang } from '@/i18n'

type Lang = 'zh' | 'en'

interface DocItem {
  group?: boolean
  file?: string
  zh: string
  en: string
  descZh?: string
  descEn?: string
}

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

/** 文档目录：与 docs/ 下的源文件对应（由 scripts/sync-docs 同步到 public/docs） */
const DOCS: DocItem[] = [
  { group: true, zh: '入门', en: 'Getting started' },
  {
    file: 'getting-started.md',
    zh: '项目总览（README）',
    en: 'Project overview',
    descZh: '定位、快速开始、插件开发与升级部署',
    descEn: 'Positioning, quick start, plugin dev, upgrades',
  },
  {
    file: 'changelog.md',
    zh: '更新日志',
    en: 'Changelog',
    descZh: '各版本新增、修复与性能变更记录',
    descEn: 'Features, fixes and perf changes per release',
  },
  { group: true, zh: '设计与架构', en: 'Design & architecture' },
  {
    file: 'design.md',
    zh: '设计文档',
    en: 'Design doc',
    descZh: '产品定位、架构、数据库、WS 协议、插件系统',
    descEn: 'Product, architecture, DB, WS protocol, plugins',
  },
  {
    file: 'api-spec.md',
    zh: '后端需求规格',
    en: 'Backend API spec',
    descZh: '接口契约、校验规则、错误码、验收标准',
    descEn: 'Contracts, validation, error codes, acceptance',
  },
  {
    file: 'frontend-guide.md',
    zh: '前端技术方案',
    en: 'Frontend guide',
    descZh: '结构、状态管理、WS 链路、性能约定与踩坑',
    descEn: 'Structure, state, WS, performance notes & pitfalls',
  },
  { group: true, zh: '运维与子项目', en: 'Ops & subprojects' },
  {
    file: 'ops-guide.md',
    zh: '运维手册',
    en: 'Ops manual',
    descZh: '环境变量全表、部署、备份升级、排障 FAQ',
    descEn: 'Env vars, deploy, backup & upgrade, troubleshooting',
  },
  {
    file: 'security-migration-guide.md',
    zh: '安全与迁移指南',
    en: 'Security & migration guide',
    descZh: '附件鉴权、认证与登出、实时通道、插件安全、迁移与部署',
    descEn: 'Attachment auth, sessions, realtime, plugin security, migrations',
  },
  {
    file: 'server-readme.md',
    zh: '后端运行手册',
    en: 'Server runbook',
    descZh: '配置、迁移、测试、目录结构与覆盖率',
    descEn: 'Config, migrations, tests, structure & coverage',
  },
  {
    file: 'web-readme.md',
    zh: '前端结构速览',
    en: 'Web structure',
    descZh: '目录、命令、聊天界面与设计约定',
    descEn: 'Layout, commands, chat UI & design conventions',
  },
]

const items = DOCS.filter((d) => !!d.file)
const lang = computed<Lang>(() => currentLang())

function label(item: DocItem) {
  return lang.value === 'zh' ? item.zh : item.en
}
function descOf(item: DocItem) {
  return lang.value === 'zh' ? (item.descZh ?? '') : (item.descEn ?? '')
}

const activeFile = computed(() => {
  const f = route.params.file as string | undefined
  return f && items.some((d) => d.file === f) ? f : items[0].file!
})

const activeIndex = computed(() => items.findIndex((d) => d.file === activeFile.value))
const prevItem = computed(() => (activeIndex.value > 0 ? items[activeIndex.value - 1] : null))
const nextItem = computed(() =>
  activeIndex.value >= 0 && activeIndex.value < items.length - 1 ? items[activeIndex.value + 1] : null,
)

const html = ref('')
const loading = ref(true)
const failed = ref(false)

/** 仓库内相对链接 → 站内文档路由 */
const LINK_MAP: Record<string, string> = {
  'README.md': 'getting-started.md',
  'CHANGELOG.md': 'changelog.md',
  '设计文档.md': 'design.md',
  '后端需求规格.md': 'api-spec.md',
  '前端技术方案.md': 'frontend-guide.md',
  '运维手册.md': 'ops-guide.md',
  '安全与迁移指南.md': 'security-migration-guide.md',
}

async function loadDoc() {
  loading.value = true
  failed.value = false
  html.value = ''

  const file = activeFile.value
  const enPath = `docs/en/${file}`
  const zhPath = `docs/${file}`
  const path = lang.value === 'en' ? enPath : zhPath

  try {
    const res = await fetch(`${import.meta.env.BASE_URL}${path}`)
    if (!res.ok) {
      // 英文缺失时回退中文
      if (lang.value === 'en') {
        const fallback = await fetch(`${import.meta.env.BASE_URL}${zhPath}`)
        if (!fallback.ok) throw new Error(String(fallback.status))
        html.value = renderMd(await fallback.text(), true)
      } else {
        throw new Error(String(res.status))
      }
    } else {
      html.value = renderMd(await res.text(), false)
    }
  } catch {
    failed.value = true
  } finally {
    loading.value = false
  }
}

function renderMd(md: string, fallbackNote: boolean) {
  const body = marked.parse(md, { async: false }) as string
  // 仓库内 md 链接改写为站内路由，避免 404（点击由 onBodyClick 走前端路由）
  const rewritten = body.replace(/href="([^"]+\.md)(#[^"]*)?"/g, (_m, file: string, hash = '') => {
    const base = file.split('/').pop() ?? file
    const mapped = LINK_MAP[base] ?? base
    return `href="/docs/${mapped}${hash ?? ''}"`
  })
  return fallbackNote ? `<p class="docs-fallback">${t('docs.enMissing')}</p>${rewritten}` : rewritten
}

/** 文档内跳转走前端路由，避免整页刷新（静态托管下会 404） */
function onBodyClick(e: MouseEvent) {
  const link = (e.target as HTMLElement | null)?.closest?.('a')
  if (!link) return
  const href = link.getAttribute('href') ?? ''
  if (/^\/docs\/[\w.-]+\.md(#.*)?$/.test(href)) {
    e.preventDefault()
    void router.push(href)
  }
}

watch([activeFile, lang], loadDoc, { immediate: true })

// 兼容旧的 #/docs/xxx.md 锚点写法
watch(
  () => route.hash,
  (hash) => {
    const m = hash.match(/^#\/docs\/(.+\.md)$/)
    if (m) void router.push({ path: `/docs/${m[1]}` })
  },
)
</script>

<template>
  <main class="container docs-layout">
    <aside class="docs-side">
      <nav class="docs-nav" aria-label="Docs">
        <template v-for="(item, i) in DOCS" :key="i">
          <div v-if="item.group" class="docs-group">{{ label(item) }}</div>
          <RouterLink
            v-else
            :to="`/docs/${item.file}`"
            :class="{ 'is-active': item.file === activeFile }"
          >
            {{ label(item) }}
          </RouterLink>
        </template>
      </nav>
      <p class="docs-side-note">{{ t('docs.zhNote') }}</p>
    </aside>

    <article class="docs-main">
      <div class="docs-meta">
        <span class="docs-crumb">{{ t('docs.crumb') }}</span>
      </div>
      <h1 class="docs-title">{{ label(items[activeIndex] ?? items[0]) }}</h1>
      <p class="docs-desc">{{ descOf(items[activeIndex] ?? items[0]) }}</p>

      <div class="md-body" @click="onBodyClick">
        <p v-if="loading" class="docs-loading">{{ t('docs.loading') }}</p>
        <p v-else-if="failed" class="docs-loading">{{ t('docs.failed') }}</p>
        <div v-else v-html="html" />
      </div>

      <nav class="docs-pager">
        <TButton
          v-if="prevItem"
          variant="outline"
          size="small"
          @click="router.push(`/docs/${prevItem.file}`)"
        >
          ← {{ label(prevItem) }}
        </TButton>
        <span v-else />
        <TButton
          v-if="nextItem"
          variant="outline"
          size="small"
          @click="router.push(`/docs/${nextItem.file}`)"
        >
          {{ label(nextItem) }} →
        </TButton>
      </nav>
    </article>
  </main>
</template>
