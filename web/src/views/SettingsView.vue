<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Button as TButton, DialogPlugin, Input as TInput, MessagePlugin } from 'tdesign-vue-next'
import { ArrowLeftIcon } from 'tdesign-icons-vue-next'

import { useAppI18n } from '@/composables/useAppI18n'
import { systemApi, type VersionInfo } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { useConversationsStore } from '@/stores/conversations'
import { useFriendsStore } from '@/stores/friends'
import { SCENE_PRESETS, usePrefsStore, type SceneEffect, type Theme } from '@/stores/prefs'
import { useWsStore } from '@/stores/ws'

const router = useRouter()
const prefs = usePrefsStore()
const auth = useAuthStore()
const convStore = useConversationsStore()
const friendsStore = useFriendsStore()
const ws = useWsStore()
const { t, te } = useAppI18n()

// ===== 外观 =====

const themeOptions: Array<{ value: Theme; label: string }> = [
  { value: 'auto', label: 'prefs.themeAuto' },
  { value: 'light', label: 'prefs.themeLight' },
  { value: 'dark', label: 'prefs.themeDark' },
]

const effectOptions: Array<{ value: SceneEffect; label: string }> = [
  { value: 'aurora', label: 'prefs.effectAurora' },
  { value: 'waves', label: 'prefs.effectWaves' },
  { value: 'grid', label: 'prefs.effectGrid' },
  { value: 'none', label: 'prefs.effectNone' },
]

function onColorInput(e: Event) {
  prefs.setSceneColor((e.target as HTMLInputElement).value)
}

const showCustom = computed(() => !SCENE_PRESETS.some((c) => c.toLowerCase() === prefs.sceneColor.toLowerCase()))

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

        <div class="settings-row settings-row--stack">
          <span class="settings-row__label">{{ t('prefs.sceneColor') }}</span>
          <div class="settings-colors">
            <button
              v-for="c in SCENE_PRESETS"
              :key="c"
              type="button"
              class="settings-swatch"
              :class="{ 'is-active': prefs.sceneColor.toLowerCase() === c.toLowerCase() }"
              :style="{ background: c }"
              :aria-label="c"
              :aria-pressed="prefs.sceneColor.toLowerCase() === c.toLowerCase()"
              @click="prefs.setSceneColor(c)"
            />
            <label
              class="settings-swatch settings-swatch--custom"
              :class="{ 'is-active': showCustom }"
              :title="t('prefs.sceneCustom')"
            >
              <input type="color" :value="prefs.sceneColor" @input="onColorInput" />
            </label>
          </div>
        </div>

        <div class="settings-row settings-row--stack">
          <span class="settings-row__label">{{ t('prefs.sceneEffect') }}</span>
          <div class="settings-seg" role="group" :aria-label="t('prefs.sceneEffect')">
            <button
              v-for="opt in effectOptions"
              :key="opt.value"
              type="button"
              :class="{ 'is-active': prefs.sceneEffect === opt.value }"
              @click="prefs.setSceneEffect(opt.value)"
            >
              {{ t(opt.label) }}
            </button>
          </div>
        </div>

        <div class="settings-foot">
          <span class="text-placeholder">{{ t('settings.sceneTitle') }}</span>
          <button type="button" class="settings-link" @click="prefs.resetScene()">
            {{ t('prefs.sceneReset') }}
          </button>
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

.settings-row--stack {
  flex-direction: column;
  align-items: flex-start;
  gap: 10px;
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

/* 颜色块 */
.settings-colors {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.settings-swatch {
  width: 30px;
  height: 30px;
  padding: 0;
  border-radius: 8px;
  border: 1px solid rgba(0, 0, 0, 0.18);
  cursor: pointer;
  transition: box-shadow 0.12s ease-out;
}

.settings-swatch.is-active {
  box-shadow:
    0 0 0 2px var(--td-bg-color-container),
    0 0 0 4px var(--td-brand-color);
}

.settings-swatch--custom {
  position: relative;
  background: conic-gradient(from 210deg, #7ec4ff, #60e2c8, #aa96ff, #ffb37e, #7ec4ff);
  overflow: hidden;
}

.settings-swatch--custom input {
  position: absolute;
  inset: -4px;
  width: calc(100% + 8px);
  height: calc(100% + 8px);
  padding: 0;
  border: none;
  background: transparent;
  cursor: pointer;
  opacity: 0;
}

.settings-swatch:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 2px;
}

.settings-link {
  padding: 0;
  border: none;
  background: none;
  font: inherit;
  font-size: 13px;
  color: var(--mc-link);
  cursor: pointer;
}

.settings-link:hover {
  text-decoration: underline;
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

/* 卡片底部辅助行（说明 + 重置入口） */
.settings-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 0 14px;
  font-size: 12px;
}

@media (max-width: 560px) {
  .settings-body {
    padding: 16px 12px 0;
  }

  .settings-card {
    padding: 4px 16px 6px;
  }

  /* 窄屏：标签与控件上下排列，避免右侧被挤压 */
  .settings-row:not(.settings-row--stack) {
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
  }
}
</style>
