<script setup lang="ts">
/**
 * 产品界面复刻（纯 HTML/CSS，不是截图）
 * ------------------------------------------------------------
 * 作用：官网最重要的证据。四栏结构与 web/ 一致——图标栏 / 会话列表 /
 * 消息流 / 详情面板，尺寸比例取自真实界面在 1440 宽下的取值。
 *
 * 允许的交互只有三个：切会话、开合详情面板、切暗色。
 * 不做"假装能发消息"的输入框——那是骗人的，点到没反应更糟。
 */
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import UiIcon from './UiIcon.vue'
import { isDark, onThemeChange } from '@/theme'

const { t } = useI18n()

const dark = ref(isDark())
const manualTheme = ref(false)
const panel = ref(true)
const active = ref(0)
const bodyRef = ref<HTMLElement | null>(null)

/** 像真实聊天一样停在最新一条：首屏看到的是插件消息卡片，而不是历史 */
function scrollToEnd() {
  nextTick(() => {
    const el = bodyRef.value
    if (!el) return
    el.scrollTop = el.scrollHeight
  })
}

function toggleDark() {
  manualTheme.value = true
  dark.value = !dark.value
}

// 默认跟随站点亮暗；用户手动点过预览里的主题按钮后就不再自动跟随
const stopTheme = onThemeChange((isDarkNow) => {
  if (!manualTheme.value) dark.value = isDarkNow
})

watch(active, scrollToEnd)
watch(panel, scrollToEnd)
onMounted(() => {
  scrollToEnd()
  // 字体加载完成后行高会变，再校一次
  requestAnimationFrame(() => setTimeout(scrollToEnd, 120))
})
onBeforeUnmount(stopTheme)

interface Conv {
  name: string
  initial: string
  hue: number
  preview: string
  time: string
  unread?: number
  group?: boolean
  members?: string
}

const convs = computed<Conv[]>(() => [
  {
    name: t('pv.c1'),
    initial: t('pv.c1i'),
    hue: 214,
    preview: t('pv.c1p'),
    time: '09:42',
    unread: 2,
    group: true,
    members: t('pv.members'),
  },
  {
    name: t('pv.c2'),
    initial: t('pv.c2i'),
    hue: 152,
    preview: t('pv.c2p'),
    time: '09:18',
  },
  {
    name: t('pv.c3'),
    initial: t('pv.c3i'),
    hue: 28,
    preview: t('pv.c3p'),
    time: t('pv.yesterday'),
    group: true,
    members: t('pv.membersAll'),
  },
  {
    name: t('pv.c4'),
    initial: t('pv.c4i'),
    hue: 340,
    preview: t('pv.c4p'),
    time: t('pv.mon'),
  },
])

type Msg =
  | {
      kind: 'text'
      out?: boolean
      who: string
      initial: string
      hue: number
      text: string
      time: string
      mention?: boolean
      read?: boolean
      edits?: boolean
    }
  | { kind: 'plugin'; who: string; initial: string; hue: number; time: string }
  | { kind: 'sys'; text: string }

const threads = computed<Msg[][]>(() => [
  [
    {
      kind: 'text',
      who: t('pv.n1'),
      initial: t('pv.n1i'),
      hue: 214,
      text: t('pv.m1'),
      time: '09:38',
    },
    {
      kind: 'text',
      out: true,
      who: t('pv.me'),
      initial: t('pv.mei'),
      hue: 214,
      text: t('pv.m2'),
      time: '09:40',
      read: true,
    },
    {
      kind: 'text',
      who: t('pv.n2'),
      initial: t('pv.n2i'),
      hue: 292,
      text: t('pv.m3'),
      time: '09:41',
      mention: true,
    },
    { kind: 'plugin', who: t('pv.n1'), initial: t('pv.n1i'), hue: 214, time: '09:42' },
    { kind: 'sys', text: t('pv.sys1') },
  ],
  [
    {
      kind: 'text',
      who: t('pv.c2'),
      initial: t('pv.c2i'),
      hue: 152,
      text: t('pv.m4'),
      time: '09:12',
    },
    {
      kind: 'text',
      out: true,
      who: t('pv.me'),
      initial: t('pv.mei'),
      hue: 214,
      text: t('pv.m5'),
      time: '09:15',
      read: true,
    },
    {
      kind: 'text',
      who: t('pv.c2'),
      initial: t('pv.c2i'),
      hue: 152,
      text: t('pv.m6'),
      time: '09:18',
      edits: true,
    },
  ],
  [
    { kind: 'sys', text: t('pv.sys2') },
    {
      kind: 'text',
      who: t('pv.n3'),
      initial: t('pv.n3i'),
      hue: 28,
      text: t('pv.m7'),
      time: '08:56',
    },
    {
      kind: 'text',
      out: true,
      who: t('pv.me'),
      initial: t('pv.mei'),
      hue: 214,
      text: t('pv.m8'),
      time: '08:58',
      read: true,
    },
    {
      kind: 'text',
      who: t('pv.n4'),
      initial: t('pv.n4i'),
      hue: 190,
      text: t('pv.m9'),
      time: '09:02',
      mention: true,
    },
  ],
  [
    { kind: 'sys', text: t('pv.sys3') },
    {
      kind: 'text',
      who: t('pv.n5'),
      initial: t('pv.n5i'),
      hue: 336,
      text: t('pv.m10'),
      time: t('pv.mon'),
    },
  ],
])

const current = computed(() => convs.value[active.value] ?? convs.value[0])
const msgs = computed(() => threads.value[active.value] ?? [])
const files = computed(() => [
  { name: t('pv.f1'), size: '2.4 MB' },
  { name: t('pv.f2'), size: '860 KB' },
])
const members = computed(() => [
  { name: t('pv.n1'), initial: t('pv.n1i'), hue: 214, role: t('pv.owner') },
  { name: t('pv.n2'), initial: t('pv.n2i'), hue: 292, role: t('pv.admin') },
  { name: t('pv.me'), initial: t('pv.mei'), hue: 214, role: '' },
])
</script>

<template>
  <div class="pv" :class="{ 'is-dark': dark, 'has-panel': panel }">
    <!-- 窗口顶栏：只放真实存在的东西（地址、主题切换） -->
    <div class="pv__chrome">
      <span class="pv__dots" aria-hidden="true"><i /><i /><i /></span>
      <span class="pv__addr mono">
        <UiIcon name="lock" :size="11" />
        chat.example.com
      </span>
      <span class="pv__chrome-tag">{{ t('pv.live') }}</span>
      <button
        class="pv__icon pv__theme"
        type="button"
        :aria-label="t('pv.theme')"
        :title="t('pv.theme')"
        @click="toggleDark"
      >
        <UiIcon :name="dark ? 'sun' : 'moon'" :size="14" />
      </button>
    </div>

    <div class="pv__app">
      <!-- 图标栏 -->
      <aside class="pv__rail">
        <span class="pv__me" :title="t('pv.me')">{{ t('pv.mei') }}</span>
        <button class="pv__rail-btn is-on" type="button" aria-label="Chats">
          <UiIcon name="chat" :size="19" />
        </button>
        <button class="pv__rail-btn" type="button" aria-label="Contacts">
          <UiIcon name="users" :size="19" />
        </button>
        <button class="pv__rail-btn" type="button" aria-label="Plugins">
          <UiIcon name="puzzle" :size="19" />
        </button>
        <button class="pv__rail-btn pv__rail-btn--last" type="button" aria-label="Settings">
          <UiIcon name="settings" :size="19" />
        </button>
      </aside>

      <!-- 会话列表 -->
      <div class="pv__list">
        <div class="pv__list-head">
          <strong>{{ t('pv.chats') }}</strong>
          <span class="pv__list-tools">
            <UiIcon name="search" :size="14" />
            <UiIcon name="plus" :size="14" />
          </span>
        </div>
        <div class="pv__search">{{ t('pv.search') }}</div>
        <div class="pv__tabs">
          <span class="is-on">{{ t('pv.tabAll') }}</span>
          <span>{{ t('pv.tabUnread') }}</span>
        </div>

        <button
          v-for="(c, i) in convs"
          :key="c.name"
          type="button"
          class="pv__conv"
          :class="{ 'is-on': i === active }"
          @click="active = i"
        >
          <span class="pv__avatar" :style="{ background: `hsl(${c.hue} 58% 52%)` }">
            {{ c.initial }}
            <i v-if="c.group" class="pv__badge-group" />
          </span>
          <span class="pv__conv-main">
            <span class="pv__conv-top">
              <span class="pv__conv-name">{{ c.name }}</span>
              <span class="pv__conv-time mono">{{ c.time }}</span>
            </span>
            <span class="pv__conv-bottom">
              <span class="pv__conv-prev">{{ c.preview }}</span>
              <span v-if="c.unread" class="pv__unread mono">{{ c.unread }}</span>
            </span>
          </span>
        </button>
      </div>

      <!-- 消息流 -->
      <div class="pv__chat">
        <div class="pv__chat-head">
          <div class="pv__chat-title">
            <strong>{{ current.name }}</strong>
            <span v-if="current.members" class="pv__chat-sub">{{ current.members }}</span>
          </div>
          <span class="pv__list-tools">
            <UiIcon name="search" :size="14" />
            <button
              class="pv__icon"
              type="button"
              :aria-label="t('pv.togglePanel')"
              :title="t('pv.togglePanel')"
              @click="panel = !panel"
            >
              <UiIcon name="layers" :size="15" />
            </button>
          </span>
        </div>

        <div ref="bodyRef" class="pv__body">
          <template v-for="(m, i) in msgs" :key="i">
            <p v-if="m.kind === 'sys'" class="pv__sys">{{ m.text }}</p>

            <div
              v-else-if="m.kind === 'text'"
              class="pv__msg"
              :class="{ 'is-out': m.out, 'is-mention': m.mention }"
            >
              <span class="pv__avatar pv__avatar--sm" :style="{ background: `hsl(${m.hue} 58% 52%)` }">
                {{ m.initial }}
              </span>
              <div class="pv__msg-main">
                <div class="pv__msg-meta">
                  <span class="pv__msg-who">{{ m.who }}</span>
                  <span class="pv__msg-time mono">{{ m.time }}</span>
                </div>
                <div class="pv__bubble">{{ m.text }}</div>
                <div v-if="m.edits || m.read" class="pv__msg-foot">
                  <span v-if="m.edits">{{ t('pv.edited') }}</span>
                  <span v-if="m.read">{{ t('pv.read') }}</span>
                </div>
              </div>
            </div>

            <!-- 插件自定义消息：由 registerMessageRenderer 渲染 -->
            <div v-else class="pv__msg">
              <span class="pv__avatar pv__avatar--sm" :style="{ background: `hsl(${m.hue} 58% 52%)` }">
                {{ m.initial }}
              </span>
              <div class="pv__msg-main">
                <div class="pv__msg-meta">
                  <span class="pv__msg-who">{{ m.who }}</span>
                  <span class="pv__msg-time mono">{{ m.time }}</span>
                </div>
                <div class="pv__card">
                  <div class="pv__card-top">
                    <span class="pv__card-label mono">{{ t('pv.pluginMsg') }}</span>
                    <span class="pv__card-name">weather-demo</span>
                  </div>
                  <div class="pv__wx">
                    <div>
                      <div class="pv__wx-city">{{ t('pv.city') }}</div>
                      <div class="pv__wx-sky">{{ t('pv.sky') }}</div>
                    </div>
                    <div class="pv__wx-temp mono">22°</div>
                  </div>
                  <button class="pv__wx-btn" type="button">{{ t('pv.wxSend') }}</button>
                </div>
              </div>
            </div>
          </template>
        </div>

        <div class="pv__composer">
          <span class="pv__comp-icons">
            <UiIcon name="paperclip" :size="15" />
            <UiIcon name="image" :size="15" />
            <UiIcon name="smile" :size="15" />
          </span>
          <span class="pv__comp-input">{{ t('pv.composer') }}</span>
          <span class="pv__comp-send">
            <UiIcon name="send" :size="14" />
          </span>
        </div>
      </div>

      <!-- 详情面板 -->
      <aside v-if="panel" class="pv__panel">
        <div class="pv__panel-head">{{ t('pv.detail') }}</div>

        <div class="pv__block">
          <div class="pv__block-title">{{ t('pv.notice') }}</div>
          <p class="pv__notice">{{ t('pv.noticeText') }}</p>
        </div>

        <div class="pv__block">
          <div class="pv__block-title">
            {{ t('pv.memberTitle') }}
            <span class="pv__more">{{ t('pv.more') }}</span>
          </div>
          <ul class="pv__members">
            <li v-for="m in members" :key="m.name">
              <span class="pv__avatar pv__avatar--xs" :style="{ background: `hsl(${m.hue} 58% 52%)` }">
                {{ m.initial }}
              </span>
              <span class="pv__member-name">{{ m.name }}</span>
              <span v-if="m.role" class="pv__role">{{ m.role }}</span>
            </li>
          </ul>
        </div>

        <div class="pv__block">
          <div class="pv__block-title">{{ t('pv.fileTitle') }}</div>
          <ul class="pv__files">
            <li v-for="f in files" :key="f.name">
              <UiIcon name="file" :size="13" />
              <span class="pv__file-name">{{ f.name }}</span>
              <span class="pv__file-size mono">{{ f.size }}</span>
            </li>
          </ul>
        </div>

        <div class="pv__block pv__block--plugin">
          <div class="pv__block-title">
            {{ t('pv.panelTitle') }}
            <span class="pv__role mono">ui:panel</span>
          </div>
          <div class="pv__wx pv__wx--panel">
            <div>
              <div class="pv__wx-city">{{ t('pv.city') }}</div>
              <div class="pv__wx-sky">{{ t('pv.sky') }}</div>
            </div>
            <div class="pv__wx-temp mono">22°</div>
          </div>
        </div>
      </aside>
    </div>
  </div>
</template>
