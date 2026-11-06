<script setup lang="ts">
import UiSlider from '@/components/ui/UiSlider.vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiInput from '@/components/ui/UiInput.vue'
import UiSwitch from '@/components/ui/UiSwitch.vue'
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
import { ArrowLeftIcon } from 'tdesign-icons-vue-next'

import AppBackgroundPicker from '@/components/common/AppBackgroundPicker.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import {
  notificationsEnabled,
  notificationsSupported,
  pushEnabled,
  pushSupported,
  setNotificationsEnabled,
  setPushEnabled,
} from '@/composables/notifications'
import {
  clearStaticCache,
  formatBytes,
  measureLocalCache,
  type LocalCacheUsage,
} from '@/composables/localCache'
import { authApi, systemApi, type VersionInfo } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { useConversationsStore } from '@/stores/conversations'
import { useFriendsStore } from '@/stores/friends'
import { PANE_LIMITS, useLayoutStore, type PaneKey } from '@/stores/layout'
import { usePrefsStore, type Theme } from '@/stores/prefs'
import { useWsStore } from '@/stores/ws'

const router = useRouter()
const prefs = usePrefsStore()
const auth = useAuthStore()
const convStore = useConversationsStore()
const friendsStore = useFriendsStore()
const layout = useLayoutStore()
const ws = useWsStore()
const { t, te } = useAppI18n()

// ===== 桌面通知 =====
const notifyOn = ref(notificationsEnabled())
const notifySupported = notificationsSupported()

// ===== 本地缓存 =====
const cacheUsage = ref<LocalCacheUsage | null>(null)
const cacheBusy = ref(false)

async function refreshCacheUsage() {
  try {
    cacheUsage.value = await measureLocalCache()
  } catch {
    cacheUsage.value = null
  }
}

function confirmClearCache() {
  const dialog = DialogPlugin.confirm({
    header: t('settings.cacheClearConfirmTitle'),
    body: t('settings.cacheClearConfirmBody'),
    theme: 'warning',
    onConfirm: async () => {
      dialog.destroy()
      cacheBusy.value = true
      try {
        await clearStaticCache()
        await refreshCacheUsage()
        void MessagePlugin.success(t('settings.cacheCleared'))
      } catch {
        void MessagePlugin.error(t('settings.cacheClearFailed'))
      } finally {
        cacheBusy.value = false
      }
    },
  })
}

// ===== 离线推送（Web Push）=====
const pushOn = ref(pushEnabled())
const pushIsSupported = pushSupported()

async function onPushToggle(v: unknown) {
  const on = v === true
  if (on && !pushIsSupported) {
    void MessagePlugin.warning(t('settings.pushUnsupported'))
    return
  }
  try {
    const ok = await setPushEnabled(on)
    pushOn.value = ok
    if (on && !ok) {
      void MessagePlugin.error(t('settings.pushUnavailable'))
    } else {
      void MessagePlugin.success(on ? t('settings.pushOn') : t('settings.pushOff'))
    }
  } catch {
    pushOn.value = false
    void MessagePlugin.error(t('settings.pushUnavailable'))
  }
}

async function onNotifyToggle(v: unknown) {
  const on = v === true
  if (on && !notifySupported) {
    void MessagePlugin.warning(t('settings.notifyUnsupported'))
    return
  }
  try {
    const ok = await setNotificationsEnabled(on)
    notifyOn.value = ok
    if (on && !ok) {
      void MessagePlugin.error(t('settings.notifyDenied'))
    } else {
      void MessagePlugin.success(on ? t('settings.notifyOn') : t('settings.notifyOff'))
    }
  } catch {
    // requestPermission 在部分浏览器/隐私模式下会 reject：给出明确失败提示
    notifyOn.value = false
    void MessagePlugin.error(t('settings.notifyDenied'))
  }
}

// 直接打开/刷新设置页时不会经过聊天页的初始化，auth.user 为空 →
// 账号卡片会误显示「登录后可修改昵称与邮箱」。这里自行补一次资料拉取。
onMounted(async () => {
  void refreshCacheUsage()
  if (!auth.token || auth.user) return
  try {
    await auth.fetchMe()
  } catch {
    /* 令牌失效由请求拦截器统一处理 */
  }
})

// ===== 布局（宽度可调 / 区域可改抽屉形态）=====

const layoutPanes = computed(() => [
  { key: 'group' as PaneKey, label: t('settings.layoutGroup') },
  { key: 'conv' as PaneKey, label: t('settings.layoutConv') },
  { key: 'detail' as PaneKey, label: t('settings.layoutDetail') },
])

/** TSlider 单值模式回调 number，区间模式是 number[]，这里统一处理 */
function onWidthChange(key: PaneKey, v: unknown) {
  const n = Array.isArray(v) ? Number(v[0]) : Number(v)
  if (Number.isFinite(n)) layout.setWidth(key, n)
}

// ===== 插件 / 站长（低频入口，按信息架构 L3 收在这里）=====

/** 插件面板属于聊天页，带参数回去由 ChatLayout 自动展开 */
function openPlugins() {
  void router.push({ path: '/chat', query: { panel: 'plugins' } })
}

function openAdmin() {
  void router.push('/admin')
}

// ===== 外观 =====

const themeOptions: Array<{ value: Theme; label: string }> = [
  { value: 'auto', label: 'prefs.themeAuto' },
  { value: 'light', label: 'prefs.themeLight' },
  { value: 'dark', label: 'prefs.themeDark' },
]

// ===== 账号 =====

const profile = reactive({ nickname: '', email: '', emailCode: '' })
const saving = ref(false)

watch(
  () => auth.user,
  (u) => {
    profile.nickname = u?.nickname ?? ''
    profile.email = u?.email ?? ''
    profile.emailCode = ''
  },
  { immediate: true },
)

/** 邮箱被改动 = 换绑：需要先给新邮箱发验证码 */
const emailChanged = computed(() => {
  const current = auth.user?.email ?? ''
  const next = profile.email.trim()
  return next !== '' && next !== current
})

// ===== 换绑邮箱验证码 =====
const codeSending = ref(false)
const codeCooldown = ref(0)
let codeTimer: number | undefined

function stopCodeTimer() {
  if (codeTimer !== undefined) {
    window.clearInterval(codeTimer)
    codeTimer = undefined
  }
}

function startCooldown(sec: number) {
  codeCooldown.value = Math.min(600, Math.max(1, Math.floor(sec) || 60))
  stopCodeTimer()
  codeTimer = window.setInterval(() => {
    codeCooldown.value -= 1
    if (codeCooldown.value <= 0) stopCodeTimer()
  }, 1000)
}

onBeforeUnmount(stopCodeTimer)

async function sendChangeCode() {
  if (codeSending.value || codeCooldown.value > 0) return
  const email = profile.email.trim()
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
    void MessagePlugin.warning(t('auth.validation.emailRequired'))
    return
  }
  codeSending.value = true
  try {
    startCooldown(await auth.sendEmailChangeCode(email))
    void MessagePlugin.success(t('auth.codeSent'))
  } catch (e) {
    void MessagePlugin.error(te(e) || t('auth.codeSendFailed'))
  } finally {
    codeSending.value = false
  }
}

/** 资料是否有未保存的修改（用于禁用保存按钮，避免无意义的提交） */
const profileDirty = computed(() => {
  const u = auth.user
  if (!u) return false
  return profile.nickname.trim() !== u.nickname || profile.email.trim() !== (u.email ?? '')
})

async function saveProfile() {
  const nickname = profile.nickname.trim()
  if (!nickname) {
    void MessagePlugin.warning(t('settings.nicknameRequired'))
    return
  }
  const u = auth.user
  const email = profile.email.trim()
  // 换绑邮箱必须带验证码：服务端会核销通过才落库（防止占用别人的邮箱）
  if (emailChanged.value && !profile.emailCode.trim()) {
    void MessagePlugin.warning(t('settings.emailCodeRequired'))
    return
  }
  // 只提交真正变化的字段：「只改昵称」不该顺手动邮箱与验证状态
  const patch: { nickname?: string; email?: string; emailCode?: string } = {}
  if (!u || nickname !== u.nickname) patch.nickname = nickname
  if (email !== (u?.email ?? '')) {
    patch.email = email
    if (email) patch.emailCode = profile.emailCode.trim()
  }
  if (!u || (!patch.nickname && patch.email === undefined)) {
    void MessagePlugin.info(t('settings.nothingChanged'))
    return
  }
  saving.value = true
  try {
    await auth.updateMe(patch)
    profile.emailCode = ''
    void MessagePlugin.success(t('settings.savedOk'))
  } catch (e) {
    void MessagePlugin.error(te(e) || t('common.saveFailed'))
  } finally {
    saving.value = false
  }
}

function confirmLogout() {
  const dialog = DialogPlugin.confirm({
    header: t('settings.logoutConfirmTitle'),
    body: t('settings.logoutConfirmBody'),
    confirmBtn: { content: t('settings.logout'), theme: 'danger' },
    onConfirm: () => {
      auth.logout()
      ws.disconnect()
      convStore.reset()
      friendsStore.reset()
      void router.push('/login')
      dialog.destroy()
    },
  })
}

/** 退出所有设备：让后端 token_version+1，所有旧令牌失效；当前端也会退出 */
function confirmLogoutOthers() {
  const dialog = DialogPlugin.confirm({
    header: t('settings.logoutOthers'),
    body: t('settings.logoutOthersConfirm'),
    theme: 'warning',
    onConfirm: async () => {
      try {
        await authApi.logoutOthers()
      } catch (e) {
        // 令牌已经失效时继续走本地登出，避免停在半登录状态
        void MessagePlugin.error(te(e))
      }
      auth.logout()
      ws.disconnect()
      convStore.reset()
      friendsStore.reset()
      void router.push('/login')
      dialog.destroy()
    },
  })
}

// ===== 关于 =====

const build = ref<VersionInfo | null>(null)

systemApi
  .version()
  .then((v) => {
    build.value = v
  })
  .catch(() => undefined)

function goBack() {
  if (window.history.length > 1) router.back()
  else void router.push(auth.token ? '/chat' : '/login')
}
</script>

<template>
  <div class="settings-page">
    <header class="settings-top">
      <UiButton variant="text" size="small" @click="goBack">
        <template #icon><ArrowLeftIcon /></template>
        {{ t('settings.back') }}
      </UiButton>
      <h1 class="settings-top__title">{{ t('settings.title') }}</h1>
      <span class="settings-top__spacer" />
    </header>

    <div class="settings-body">
      <!-- 外观 -->
      <section class="settings-card">
        <h2 class="settings-card__title">{{ t('settings.appearance') }}</h2>

        <div class="settings-row">
          <span class="settings-row__label">{{ t('prefs.language') }}</span>
          <div class="settings-seg" role="group" :aria-label="t('prefs.language')">
            <button
              type="button"
              :class="{ 'is-active': prefs.lang === 'zh-CN' }"
              @click="prefs.setLang('zh-CN')"
            >
              {{ t('settings.languageZh') }}
            </button>
            <button
              type="button"
              :class="{ 'is-active': prefs.lang === 'en-US' }"
              @click="prefs.setLang('en-US')"
            >
              {{ t('settings.languageEn') }}
            </button>
          </div>
        </div>

        <div class="settings-row">
          <span class="settings-row__label">{{ t('prefs.theme') }}</span>
          <div class="settings-seg" role="group" :aria-label="t('prefs.theme')">
            <button
              v-for="opt in themeOptions"
              :key="opt.value"
              type="button"
              :class="{ 'is-active': prefs.theme === opt.value }"
              @click="prefs.setTheme(opt.value)"
            >
              {{ t(opt.label) }}
            </button>
          </div>
        </div>

        <div class="settings-row">
          <div class="settings-row__text">
            <span class="settings-row__label">{{ t('settings.appBackground') }}</span>
            <span class="settings-row__hint text-placeholder">{{ t('settings.appBackgroundHint') }}</span>
          </div>
          <AppBackgroundPicker />
        </div>
      </section>

      <!-- 桌面通知 -->
      <section class="settings-card">
        <h2 class="settings-card__title">{{ t('settings.notify') }}</h2>
        <div class="settings-row">
          <div class="settings-row__text">
            <span class="settings-row__label">{{ t('settings.notifyLabel') }}</span>
            <span class="settings-row__hint text-placeholder">{{ t('settings.notifyHint') }}</span>
          </div>
          <UiSwitch :value="notifyOn" @change="onNotifyToggle" />
        </div>
        <!-- 离线推送：应用完全关闭时也能收到消息提醒（需 HTTPS 与浏览器授权） -->
        <div v-if="auth.token" class="settings-row settings-row--last">
          <div class="settings-row__text">
            <span class="settings-row__label">{{ t('settings.pushLabel') }}</span>
            <span class="settings-row__hint text-placeholder">{{ t('settings.pushHint') }}</span>
          </div>
          <UiSwitch :value="pushOn" @change="onPushToggle" />
        </div>
      </section>

      <!-- 账号 -->
      <section class="settings-card">
        <h2 class="settings-card__title">{{ t('settings.account') }}</h2>

        <template v-if="auth.user">
          <div class="settings-field">
            <label class="settings-field__label" for="st-nickname">{{ t('settings.nickname') }}</label>
            <UiInput
              id="st-nickname"
              v-model="profile.nickname"
              :placeholder="t('settings.nicknamePlaceholder')"
              :maxlength="20"
              clearable
            />
          </div>

          <div class="settings-field">
            <label class="settings-field__label" for="st-email">{{ t('settings.email') }}</label>
            <UiInput
              id="st-email"
              v-model="profile.email"
              :placeholder="t('settings.email')"
              clearable
            />
            <p class="settings-field__hint text-placeholder">{{ t('settings.emailHint') }}</p>

            <!-- 换绑邮箱：必须验证新邮箱归属 -->
            <template v-if="emailChanged">
              <div class="settings-code-row">
                <UiInput
                  v-model="profile.emailCode"
                  :maxlength="6"
                  :placeholder="t('auth.emailCode')"
                />
                <UiButton
                  variant="outline"
                  :loading="codeSending"
                  :disabled="codeCooldown > 0"
                  @click="sendChangeCode"
                >
                  {{ codeCooldown > 0 ? t('auth.resendIn', { n: codeCooldown }) : t('auth.sendCode') }}
                </UiButton>
              </div>
              <p class="settings-field__hint text-placeholder">
                {{ t('settings.emailChangeHint') }}
              </p>
            </template>
          </div>

          <div class="settings-actions">
            <UiButton
              theme="primary"
              :loading="saving"
              :disabled="!profileDirty"
              @click="saveProfile"
            >
              {{ t('settings.save') }}
            </UiButton>
          </div>

          <div class="settings-row">
            <div class="settings-row__text">
              <span class="settings-row__label">{{ t('settings.logoutOthers') }}</span>
              <span class="settings-row__hint text-placeholder">
                {{ t('settings.logoutOthersHint') }}
              </span>
            </div>
            <UiButton variant="outline" size="small" @click="confirmLogoutOthers">
              {{ t('settings.logoutOthers') }}
            </UiButton>
          </div>

          <div class="settings-row settings-row--last">
            <span class="settings-row__label">{{ t('settings.logout') }}</span>
            <UiButton variant="outline" theme="danger" size="small" @click="confirmLogout">
              {{ t('settings.logout') }}
            </UiButton>
          </div>
        </template>

        <p v-else class="settings-placeholder text-placeholder">
          {{ t('settings.loginRequired') }}
          <RouterLink to="/login">{{ t('auth.actionLogin') }}</RouterLink>
        </p>
      </section>

      <!-- 布局 -->
      <section class="settings-card">
        <h2 class="settings-card__title">{{ t('settings.layout') }}</h2>
        <p class="settings-card__hint text-placeholder">{{ t('settings.layoutHint') }}</p>
        <div v-for="p in layoutPanes" :key="p.key" class="settings-layout">
          <div class="settings-layout__head">
            <span class="settings-row__label">{{ p.label }}</span>
            <span class="settings-row__value num">{{ layout.panes[p.key].width }}px</span>
          </div>
          <UiSlider
            :model-value="layout.panes[p.key].width"
            :min="PANE_LIMITS[p.key].min"
            :max="PANE_LIMITS[p.key].max"
            @change="onWidthChange(p.key, $event)"
          />
          <div class="settings-seg settings-layout__seg" role="group" :aria-label="p.label">
            <button
              type="button"
              :class="{ 'is-active': !layout.panes[p.key].drawer }"
              @click="layout.setDrawer(p.key, false)"
            >
              {{ t('settings.docked') }}
            </button>
            <button
              type="button"
              :class="{ 'is-active': layout.panes[p.key].drawer }"
              @click="layout.setDrawer(p.key, true)"
            >
              {{ t('settings.drawer') }}
            </button>
          </div>
        </div>
        <UiButton class="settings-layout__reset" variant="outline" size="small" @click="layout.reset()">
          {{ t('settings.resetLayout') }}
        </UiButton>
      </section>

      <!-- 本地缓存 -->
      <section class="settings-card">
        <h2 class="settings-card__title">{{ t('settings.cache') }}</h2>
        <p class="settings-card__hint text-placeholder">{{ t('settings.cacheHint') }}</p>

        <div class="settings-row">
          <div class="settings-row__text">
            <span class="settings-row__label">{{ t('settings.cacheUsage') }}</span>
            <span class="settings-row__hint text-placeholder">{{ t('settings.cacheUsageHint') }}</span>
          </div>
          <span class="settings-row__value num">
            {{ cacheUsage ? formatBytes(cacheUsage.cacheBytes) : '—' }}
          </span>
        </div>

        <div class="settings-row">
          <div class="settings-row__text">
            <span class="settings-row__label">{{ t('settings.cachePrefs') }}</span>
            <span class="settings-row__hint text-placeholder">{{ t('settings.cachePrefsHint') }}</span>
          </div>
          <span class="settings-row__value num">
            {{ cacheUsage ? formatBytes(cacheUsage.localStorageBytes) : '—' }}
          </span>
        </div>

        <div class="settings-row settings-row--last">
          <div class="settings-row__text">
            <span class="settings-row__label">{{ t('settings.cacheClear') }}</span>
            <span class="settings-row__hint text-placeholder">{{ t('settings.cacheClearHint') }}</span>
          </div>
          <UiButton variant="outline" size="small" :loading="cacheBusy" @click="confirmClearCache">
            {{ t('settings.cacheClear') }}
          </UiButton>
        </div>
      </section>

      <!-- 插件 -->
      <section class="settings-card">
        <h2 class="settings-card__title">{{ t('settings.plugins') }}</h2>
        <div class="settings-row">
          <div class="settings-row__text">
            <span class="settings-row__label">{{ t('settings.pluginsLabel') }}</span>
            <span class="settings-row__hint text-placeholder">{{ t('settings.pluginsHint') }}</span>
          </div>
          <UiButton variant="outline" size="small" @click="openPlugins">
            {{ t('settings.openPlugins') }}
          </UiButton>
        </div>
      </section>

      <!-- 站长后台（仅站长可见） -->
      <section v-if="auth.isAdmin" class="settings-card">
        <h2 class="settings-card__title">{{ t('settings.admin') }}</h2>
        <div class="settings-row">
          <div class="settings-row__text">
            <span class="settings-row__label">{{ t('settings.adminLabel') }}</span>
            <span class="settings-row__hint text-placeholder">{{ t('settings.adminHint') }}</span>
          </div>
          <UiButton variant="outline" size="small" @click="openAdmin">
            {{ t('settings.openAdmin') }}
          </UiButton>
        </div>
      </section>

      <!-- 关于 -->
      <section class="settings-card">
        <h2 class="settings-card__title">{{ t('settings.about') }}</h2>
        <div class="settings-row settings-row--last">
          <span class="settings-row__label">{{ t('settings.version') }}</span>
          <span class="settings-row__value text-placeholder num">
            {{ build ? `${build.version} · ${build.commit}` : '—' }}
          </span>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.settings-page {
  min-height: 100%;
  /* 透出 body 上的全站背景 */
  background: transparent;
  padding-bottom: 48px;
}

.settings-top {
  position: sticky;
  top: 0;
  z-index: 5;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 16px;
  background: var(--td-bg-color-container);
  border-bottom: 1px solid var(--td-component-border);
}

.settings-top__title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
}

.settings-top__spacer {
  flex: 1;
}

.settings-body {
  max-width: 880px;
  margin: 0 auto;
  padding: 32px 20px 0;
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.settings-card {
  background: var(--td-bg-color-container);
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-lg);
  padding: 20px 20px 10px;
  box-shadow: var(--mc-shadow-1);
}

.settings-card__title {
  margin: 0 0 4px;
  font-size: 16px;
  font-weight: 600;
  letter-spacing: -0.01em;
}

/* 行：左标签右控件，底部细分隔线 */
.settings-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 16px 0;
  border-bottom: 1px solid var(--mc-hairline);
}

.settings-row--last {
  border-bottom: none;
}

/* 卡片内最后一行不再保留分隔线，避免底部悬挂一条断线 */
.settings-card > :last-child {
  border-bottom: none;
}

.settings-row__label {
  font-size: 14px;
}

.settings-row__value {
  font-size: 13px;
}

/* 左侧「标题 + 说明」两行结构（插件 / 站长分区用） */
.settings-row__text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.settings-row__hint {
  font-size: 12px;
  line-height: 1.4;
}

/* ===== 布局分区 ===== */
.settings-card__hint {
  margin: 0 0 6px;
  font-size: 12px;
  line-height: 1.5;
}

.settings-layout {
  padding: 12px 0;
  border-bottom: 1px solid var(--mc-hairline);
}

.settings-layout__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 2px;
}

.settings-layout__seg {
  margin-top: 10px;
}

.settings-layout__reset {
  margin-top: 14px;
}

/* 分段控件 */
.settings-seg {
  display: inline-flex;
  padding: 4px;
  border-radius: var(--mc-radius-md);
  background: var(--td-bg-color-secondarycontainer);
  flex-wrap: wrap;
  gap: 2px;
}

.settings-seg button {
  padding: 5px 12px;
  border: none;
  border-radius: var(--mc-radius-sm);
  background: transparent;
  font: inherit;
  font-size: 13px;
  color: var(--td-text-color-secondary);
  cursor: pointer;
  white-space: nowrap;
  transition:
    background-color var(--mc-dur-fast) var(--mc-ease-out),
    color var(--mc-dur-fast) var(--mc-ease-out),
    box-shadow var(--mc-dur-fast) var(--mc-ease-out);
}

.settings-seg button.is-active {
  background: var(--td-bg-color-container);
  color: var(--td-text-color-primary);
  font-weight: 500;
  box-shadow: var(--mc-shadow-1);
}

.settings-seg button:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 1px;
}

/* 换绑邮箱的验证码行 */
.settings-code-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
}

.settings-code-row > :first-child {
  flex: 1;
  min-width: 0;
}

.settings-code-row > :last-child {
  flex-shrink: 0;
  white-space: nowrap;
}

/* 表单字段 */
.settings-field {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 16px 0;
  border-bottom: 1px solid var(--mc-hairline);
}

/* UiInput 根元素带本组件的 scope id，可直接命中。
   输入框的 bg-surface 与卡片同色、8% 白的边框在暗色下几乎不可见，
   改成「凹下去」的底色 + 明显一点的描边，输入边界才可辨认。 */
.settings-field > div {
  background: var(--td-bg-color-page);
  border-color: var(--td-component-border);
}

.settings-field__label {
  font-size: 14px;
}

.settings-field__hint {
  margin: 0;
  font-size: 12px;
}

.settings-actions {
  padding: 16px 0;
  display: flex;
  justify-content: flex-end;
  border-bottom: 1px solid var(--mc-hairline);
}

.settings-placeholder {
  padding: 16px 0 20px;
  font-size: 13px;
}

.settings-placeholder a {
  margin-left: 8px;
}

@media (max-width: 560px) {
  .settings-body {
    padding: 16px 12px 0;
  }

  .settings-card {
    padding: 4px 16px 6px;
  }

  /* 窄屏：标签与控件上下排列，避免右侧被挤压 */
  .settings-row {
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
  }
}
</style>
