<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Button as TButton, DialogPlugin, Input as TInput, MessagePlugin } from 'tdesign-vue-next'
import { ArrowLeftIcon } from 'tdesign-icons-vue-next'

import { useAppI18n } from '@/composables/useAppI18n'
import { systemApi, type VersionInfo } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { useConversationsStore } from '@/stores/conversations'
import { useFriendsStore } from '@/stores/friends'
import { usePrefsStore, type Theme } from '@/stores/prefs'
import { useWsStore } from '@/stores/ws'

const router = useRouter()
const prefs = usePrefsStore()
const auth = useAuthStore()
const convStore = useConversationsStore()
const friendsStore = useFriendsStore()
const ws = useWsStore()
const { t, te } = useAppI18n()

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

const profile = reactive({ nickname: '', email: '' })
const saving = ref(false)

watch(
  () => auth.user,
  (u) => {
    profile.nickname = u?.nickname ?? ''
    profile.email = u?.email ?? ''
  },
  { immediate: true },
)

async function saveProfile() {
  const nickname = profile.nickname.trim()
  if (!nickname) {
    void MessagePlugin.warning(t('settings.nicknameRequired'))
    return
  }
  saving.value = true
  try {
    await auth.updateMe({ nickname, email: profile.email.trim() })
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
      <TButton variant="text" size="small" @click="goBack">
        <template #icon><ArrowLeftIcon /></template>
        {{ t('settings.back') }}
      </TButton>
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

      </section>

      <!-- 账号 -->
      <section class="settings-card">
        <h2 class="settings-card__title">{{ t('settings.account') }}</h2>

        <template v-if="auth.user">
          <div class="settings-field">
            <label class="settings-field__label" for="st-nickname">{{ t('settings.nickname') }}</label>
            <TInput
              id="st-nickname"
              v-model="profile.nickname"
              :placeholder="t('settings.nicknamePlaceholder')"
              :maxlength="20"
              clearable
            />
          </div>

          <div class="settings-field">
            <label class="settings-field__label" for="st-email">{{ t('settings.email') }}</label>
            <TInput
              id="st-email"
              v-model="profile.email"
              :placeholder="t('settings.email')"
              clearable
            />
            <p class="settings-field__hint text-placeholder">{{ t('settings.emailHint') }}</p>
          </div>

          <div class="settings-actions">
            <TButton theme="primary" :loading="saving" @click="saveProfile">{{ t('settings.save') }}</TButton>
          </div>

          <div class="settings-row settings-row--last">
            <span class="settings-row__label">{{ t('settings.logout') }}</span>
            <TButton variant="outline" theme="danger" size="small" @click="confirmLogout">
              {{ t('settings.logout') }}
            </TButton>
          </div>
        </template>

        <p v-else class="settings-placeholder text-placeholder">
          {{ t('settings.loginRequired') }}
          <RouterLink to="/login">{{ t('auth.actionLogin') }}</RouterLink>
        </p>
      </section>

      <!-- 插件 -->
      <section class="settings-card">
        <h2 class="settings-card__title">{{ t('settings.plugins') }}</h2>
        <div class="settings-row">
          <div class="settings-row__text">
            <span class="settings-row__label">{{ t('settings.pluginsLabel') }}</span>
            <span class="settings-row__hint text-placeholder">{{ t('settings.pluginsHint') }}</span>
          </div>
          <TButton variant="outline" size="small" @click="openPlugins">
            {{ t('settings.openPlugins') }}
          </TButton>
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
          <TButton variant="outline" size="small" @click="openAdmin">
            {{ t('settings.openAdmin') }}
          </TButton>
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
  background: var(--td-bg-color-page);
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
  max-width: 860px;
  margin: 0 auto;
  padding: 24px 20px 0;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.settings-card {
  background: var(--td-bg-color-container);
  border: 1px solid var(--mc-hairline);
  border-radius: 12px;
  padding: 6px 20px 8px;
}

.settings-card__title {
  margin: 14px 0 4px;
  font-size: 15px;
  font-weight: 600;
}

/* 行：左标签右控件，底部细分隔线 */
.settings-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 13px 0;
  border-bottom: 1px solid var(--mc-hairline);
}

.settings-row--last {
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

/* 分段控件 */
.settings-seg {
  display: inline-flex;
  padding: 2px;
  border-radius: 8px;
  background: var(--td-bg-color-secondarycontainer);
  flex-wrap: wrap;
}

.settings-seg button {
  padding: 5px 12px;
  border: none;
  border-radius: 6px;
  background: transparent;
  font: inherit;
  font-size: 13px;
  color: var(--td-text-color-secondary);
  cursor: pointer;
  white-space: nowrap;
}

.settings-seg button.is-active {
  background: var(--td-bg-color-container);
  color: var(--td-text-color-primary);
  font-weight: 500;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.08);
}

.settings-seg button:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 1px;
}

/* 表单字段 */
.settings-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 12px 0;
  border-bottom: 1px solid var(--mc-hairline);
}

.settings-field__label {
  font-size: 14px;
}

.settings-field__hint {
  margin: 0;
  font-size: 12px;
}

.settings-actions {
  padding: 14px 0;
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
