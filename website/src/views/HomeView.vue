<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'

import CodeCard from '@/components/CodeCard.vue'
import ProductPreview from '@/components/ProductPreview.vue'
import UiIcon from '@/components/UiIcon.vue'
import { REPO } from '@/links'

const { t } = useI18n()

/** 去掉高亮标签得到可复制的纯文本 */
const plain = (html: string) => html.replace(/<[^>]*>/g, '')

/* ---------------- 首屏 ---------------- */
const INSTALL = 'curl -fsSL https://gitee.com/Sun-Si-Cheng/modern-chat-new/raw/main/scripts/install.sh | bash'

const copied = ref(false)
let copyTimer: ReturnType<typeof setTimeout> | undefined

async function copyInstall() {
  try {
    await navigator.clipboard.writeText(INSTALL)
    copied.value = true
    clearTimeout(copyTimer)
    copyTimer = setTimeout(() => (copied.value = false), 1600)
  } catch {
    /* 非安全上下文：忽略，用户可手动选中 */
  }
}

const facts = computed(() => [t('hero.f1'), t('hero.f2'), t('hero.f3'), t('hero.f4')])

/* ---------------- 能力 ---------------- */
const capGroups = computed(() => [
  {
    title: t('cap.g1'),
    items: [
      { icon: 'chat', title: t('cap.1t'), desc: t('cap.1d') },
      { icon: 'users', title: t('cap.2t'), desc: t('cap.2d') },
      { icon: 'layers', title: t('cap.3t'), desc: t('cap.3d') },
      { icon: 'bolt', title: t('cap.4t'), desc: t('cap.4d') },
    ],
  },
  {
    title: t('cap.g2'),
    items: [
      { icon: 'lock', title: t('cap.5t'), desc: t('cap.5d') },
      { icon: 'shield', title: t('cap.6t'), desc: t('cap.6d') },
      { icon: 'settings', title: t('cap.7t'), desc: t('cap.7d') },
    ],
  },
  {
    title: t('cap.g3'),
    items: [
      { icon: 'terminal', title: t('cap.8t'), desc: t('cap.8d') },
      { icon: 'refresh', title: t('cap.9t'), desc: t('cap.9d') },
      { icon: 'cpu', title: t('cap.10t'), desc: t('cap.10d') },
    ],
  },
])

/* ---------------- 插件 ---------------- */
/** 代码块里的注释与示例文案也要跟着语言走，否则英文页里全是中文注释 */
const com = (key: string) => `<span class="tok-com">${t(key)}</span>`
const str = (s: string) => `<span class="tok-str">${s}</span>`

const pluginHtml = computed(() => `${com('plug.codeManifest')}
{
  <span class="tok-key">"name"</span>: ${str('"weather-demo"')},
  <span class="tok-key">"version"</span>: ${str('"1.0.0"')},
  <span class="tok-key">"permissions"</span>: [
    ${str('"ui:panel"')}, ${str('"message:render"')}, ${str('"storage"')}
  ],
  <span class="tok-key">"allowedHosts"</span>: [${str('"*.openweathermap.org"')}]
}
<span class="tok-com">*/</span>

<span class="tok-key">function</span> <span class="tok-fn">__mcActivate</span>(api) {
  ${com('plug.codePanel')}
  api.<span class="tok-fn">addSidePanel</span>({
    title: ${str(`'${t('plug.codeTitle')}'`)},
    body: [
      {
        component: ${str("'text'")},
        props: { content: ${str(`'${t('plug.codeCity')}'`)} },
      },
      {
        component: ${str("'button'")},
        props: { text: ${str(`'${t('plug.codeRefresh')}'`)} },
        action: ${str("'refresh'")},
      },
    ],
  })
}`)

const security = computed(() => [
  { t: t('plug.s1t'), d: t('plug.s1d') },
  { t: t('plug.s2t'), d: t('plug.s2d') },
  { t: t('plug.s3t'), d: t('plug.s3d') },
  { t: t('plug.s4t'), d: t('plug.s4d') },
])

const whitelist = ['text', 'image', 'button', 'input', 'card', 'list', 'divider', 'link']

const perms = computed(() => [
  { name: 'message:read', desc: t('plug.p1') },
  { name: 'message:send', desc: t('plug.p2') },
  { name: 'message:render', desc: t('plug.p3') },
  { name: 'ui:panel', desc: t('plug.p4') },
  { name: 'storage', desc: t('plug.p5') },
  { name: 'net', desc: t('plug.p6') },
])

/* ---------------- 部署 ---------------- */
type TabKey = 'script' | 'docker' | 'manual'
const tab = ref<TabKey>('script')

const tabCode = computed<Record<TabKey, { name: string; html: string }>>(() => ({
  script: {
    name: 'install.sh',
    html: `${com('dep.c1')}
curl -fsSL ${str('https://gitee.com/Sun-Si-Cheng/modern-chat-new/raw/main/scripts/install.sh')} | bash

${com('dep.c2')}
${com('dep.c3')}`,
  },
  docker: {
    name: 'docker-compose.yml',
    html: `docker compose up -d --build           <span class="tok-com">${t('dep.c4')}</span>

cd web && npm install && npm run dev   <span class="tok-com">${t('dep.c5')}</span>

<span class="tok-com">${t('dep.c6')}</span>`,
  },
  manual: {
    name: 'terminal',
    html: `<span class="tok-com">${t('dep.c7')}</span>
cd server
cp .env.example .env       <span class="tok-com"># DB_DSN / JWT_SECRET / FRONTEND_URL</span>
go run ./cmd/server        <span class="tok-com"># :8080</span>

<span class="tok-com">${t('dep.c9')}</span>
cd ../web && npm install && npm run dev`,
  },
}))

const tabs = computed(() => [
  { key: 'script' as const, label: t('dep.tabScript') },
  { key: 'docker' as const, label: t('dep.tabDocker') },
  { key: 'manual' as const, label: t('dep.tabManual') },
])

const ops = computed(() => [
  { icon: 'shield', title: t('dep.o1t'), desc: t('dep.o1d') },
  { icon: 'refresh', title: t('dep.o2t'), desc: t('dep.o2d') },
  { icon: 'cpu', title: t('dep.o3t'), desc: t('dep.o3d') },
  { icon: 'server', title: t('dep.o4t'), desc: t('dep.o4d') },
])

/* ---------------- 生态 ---------------- */
const eco = computed(() => [
  {
    icon: 'book',
    title: t('eco.1t'),
    desc: t('eco.1d'),
    to: '/docs',
    external: false,
  },
  {
    icon: 'puzzle',
    title: t('eco.2t'),
    desc: t('eco.2d'),
    to: '/plugins',
    external: false,
  },
  {
    icon: 'lifetime',
    title: t('eco.3t'),
    desc: t('eco.3d'),
    to: '/docs/changelog.md',
    external: false,
  },
  {
    icon: 'globe',
    title: t('eco.4t'),
    desc: t('eco.4d'),
    to: REPO,
    external: true,
  },
])
</script>

<template>
  <main>
    <!-- ============ 首屏 ============ -->
    <section class="hero">
      <div class="hero__backdrop" aria-hidden="true" />
      <div class="container">
        <div class="hero__inner">
          <p class="hero__eyebrow">
            <span class="pill">{{ t('hero.badge') }}</span>
            {{ t('hero.eyebrow') }}
          </p>

          <h1>{{ t('hero.h1a') }}<span>{{ t('hero.h1b') }}</span></h1>
          <p class="hero__sub">{{ t('hero.sub') }}</p>

          <div class="hero__cta">
            <a class="btn btn--primary btn--lg" href="#deploy">
              {{ t('hero.ctaStart') }}
              <UiIcon name="arrow-right" :size="16" />
            </a>
            <RouterLink class="btn btn--ghost btn--lg" to="/docs">{{ t('hero.ctaDocs') }}</RouterLink>
          </div>

          <div class="hero__install">
            <code class="mono"><span class="tok-prompt">$</span>{{ INSTALL }}</code>
            <button
              class="hero__copy"
              type="button"
              :class="{ 'is-done': copied }"
              :aria-label="t('copy.action')"
              @click="copyInstall"
            >
              <UiIcon :name="copied ? 'check' : 'copy'" :size="14" />
            </button>
          </div>

          <div class="hero__facts">
            <span v-for="f in facts" :key="f">{{ f }}</span>
          </div>
        </div>

        <div class="hero__stage">
          <ProductPreview />
        </div>
      </div>
    </section>

    <!-- ============ 能力 ============ -->
    <section id="capabilities" class="section section--plain">
      <div class="container cap">
        <div class="cap__side">
          <p class="eyebrow">{{ t('cap.eyebrow') }}</p>
          <h2>{{ t('cap.title') }}</h2>
          <p>{{ t('cap.desc') }}</p>
          <RouterLink class="link-arrow" to="/docs/design.md">
            {{ t('cap.more') }}
            <UiIcon name="arrow-right" :size="15" />
          </RouterLink>
        </div>

        <div class="cap__groups">
          <div v-for="g in capGroups" :key="g.title" class="cap__group">
            <p class="cap__group-title">{{ g.title }}</p>
            <ul class="cap__list">
              <li v-for="item in g.items" :key="item.title" class="cap__item">
                <span class="cap__icon"><UiIcon :name="item.icon" :size="17" /></span>
                <div>
                  <h3>{{ item.title }}</h3>
                  <p>{{ item.desc }}</p>
                </div>
              </li>
            </ul>
          </div>
        </div>
      </div>
    </section>

    <!-- ============ 插件系统 ============ -->
    <section id="plugins" class="section section--tint">
      <div class="container">
        <div class="sec-head">
          <p class="eyebrow">{{ t('plug.eyebrow') }}</p>
          <h2>{{ t('plug.title') }}</h2>
          <p>{{ t('plug.sub') }}</p>
        </div>

        <div class="plug">
          <CodeCard name="weather.plugin.js" :html="pluginHtml" :raw="plain(pluginHtml)" />

          <div class="plug__side">
            <div class="plug__block">
              <h3>{{ t('plug.model') }}</h3>
              <ul class="plug__checks">
                <li v-for="s in security" :key="s.t">
                  <UiIcon name="check" :size="15" />
                  <span><b>{{ s.t }}</b> — {{ s.d }}</span>
                </li>
              </ul>
            </div>

            <div class="plug__block">
              <h3>{{ t('plug.uiTitle') }}</h3>
              <p class="plug__hint">{{ t('plug.uiDesc') }}</p>
              <div class="plug__chips">
                <span v-for="w in whitelist" :key="w" class="chip">{{ w }}</span>
              </div>
            </div>

            <div class="plug__block">
              <h3>{{ t('plug.permTitle') }}</h3>
              <div class="table-wrap plug__table">
                <table class="spec">
                  <thead>
                    <tr>
                      <th>{{ t('plug.permCol') }}</th>
                      <th>{{ t('plug.permWhat') }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="p in perms" :key="p.name">
                      <td>{{ p.name }}</td>
                      <td>{{ p.desc }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- ============ 部署 ============ -->
    <section id="deploy" class="section">
      <div class="container">
        <div class="sec-head">
          <p class="eyebrow">{{ t('dep.eyebrow') }}</p>
          <h2>{{ t('dep.title') }}</h2>
          <p>{{ t('dep.sub') }}</p>
        </div>

        <div class="dep">
          <div>
            <div class="tabs" role="tablist">
              <button
                v-for="x in tabs"
                :key="x.key"
                class="tabs__btn"
                :class="{ 'is-on': tab === x.key }"
                type="button"
                role="tab"
                :aria-selected="tab === x.key"
                @click="tab = x.key"
              >
                {{ x.label }}
              </button>
            </div>
            <CodeCard
              :name="tabCode[tab].name"
              :html="tabCode[tab].html"
              :raw="plain(tabCode[tab].html)"
            />
            <p class="dep__note">{{ t('dep.note') }}</p>
          </div>

          <ul class="dep__checks">
            <li v-for="o in ops" :key="o.title">
              <UiIcon :name="o.icon" :size="16" />
              <div>
                <h3>{{ o.title }}</h3>
                <p>{{ o.desc }}</p>
              </div>
            </li>
          </ul>
        </div>
      </div>
    </section>

    <!-- ============ 生态 ============ -->
    <section class="section section--tint">
      <div class="container">
        <div class="sec-head">
          <p class="eyebrow">{{ t('eco.eyebrow') }}</p>
          <h2>{{ t('eco.title') }}</h2>
        </div>

        <div class="eco">
          <component
            :is="e.external ? 'a' : RouterLink"
            v-for="e in eco"
            :key="e.title"
            class="eco__row"
            :to="e.external ? undefined : e.to"
            :href="e.external ? e.to : undefined"
            :target="e.external ? '_blank' : undefined"
            :rel="e.external ? 'noopener' : undefined"
          >
            <span class="eco__icon"><UiIcon :name="e.icon" :size="17" /></span>
            <span class="eco__text">
              <h3>{{ e.title }}</h3>
              <p>{{ e.desc }}</p>
            </span>
            <UiIcon class="eco__arrow" :name="e.external ? 'arrow-up-right' : 'arrow-right'" :size="16" />
          </component>
        </div>
      </div>
    </section>

    <!-- ============ 收尾 ============ -->
    <section class="band">
      <div class="container band__inner">
        <div>
          <h2>{{ t('band.title') }}</h2>
          <p>{{ t('band.sub') }}</p>
        </div>
        <div class="band__cta">
          <a class="btn btn--primary" href="#deploy">{{ t('band.cta') }}</a>
          <RouterLink class="btn btn--ghost" to="/docs/getting-started.md">
            {{ t('hero.ctaDocs') }}
          </RouterLink>
        </div>
      </div>
    </section>
  </main>
</template>
