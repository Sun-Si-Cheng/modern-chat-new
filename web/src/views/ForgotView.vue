<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  Form as TForm,
  FormItem as TFormItem,
  Input as TInput,
  Button as TButton,
  MessagePlugin,
} from 'tdesign-vue-next'
import type { FormInstanceFunctions, FormProps } from 'tdesign-vue-next'
import { KeyIcon, LockOnIcon, MailIcon, SettingIcon } from 'tdesign-icons-vue-next'

import AuthScene from '@/components/common/AuthScene.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import { authApi } from '@/api'

const router = useRouter()
const { t, te } = useAppI18n()

/** 1 = 填写邮箱获取链接；2 = 用邮件中的令牌设置新密码 */
const step = ref<1 | 2>(1)
const loading = ref(false)
const form = ref<FormInstanceFunctions>()

const emailForm = reactive({ email: '' })
const resetForm = reactive({ token: '', newPassword: '', confirm: '' })

const title = computed(() => (step.value === 1 ? t('forgot.title') : t('forgot.titleStep2')))
const sub = computed(() => (step.value === 1 ? t('forgot.sub') : t('forgot.subStep2')))

const emailRules = computed<FormProps['rules']>(() => ({
  email: [{ required: true, message: t('forgot.emailRequired'), trigger: 'blur' }],
}))

const resetRules = computed<FormProps['rules']>(() => ({
  token: [{ required: true, message: t('forgot.tokenRequired'), trigger: 'blur' }],
  newPassword: [
    { required: true, message: t('forgot.newPasswordRequired'), trigger: 'blur' },
    { min: 6, message: t('auth.validation.passwordMin'), trigger: 'blur' },
  ],
  confirm: [
    {
      validator: (val: string) => val === resetForm.newPassword,
      message: t('auth.validation.confirmMismatch'),
      trigger: 'blur',
    },
  ],
}))

async function submitEmail() {
  const valid = await form.value?.validate()
  if (valid !== true) return
  loading.value = true
  try {
    await authApi.forgot({ email: emailForm.email.trim() })
    void MessagePlugin.success(t('forgot.sent'))
    step.value = 2
  } catch (e) {
    void MessagePlugin.error(te(e) || t('forgot.sendFailed'))
  } finally {
    loading.value = false
  }
}

async function submitReset() {
  const valid = await form.value?.validate()
  if (valid !== true) return
  loading.value = true
  try {
    await authApi.reset({ token: resetForm.token.trim(), newPassword: resetForm.newPassword })
    void MessagePlugin.success(t('forgot.resetOk'))
    await router.push('/login')
  } catch (e) {
    void MessagePlugin.error(te(e) || t('forgot.resetFailed'))
  } finally {
    loading.value = false
  }
}

/** 返回第一步：保留已填邮箱，避免重输 */
function backToStep1() {
  step.value = 1
}
</script>

<template>
  <div class="auth-page">
    <AuthScene />

    <div class="auth-top">
      <TButton
        variant="text"
        shape="square"
        :aria-label="t('settings.title')"
        @click="router.push('/settings')"
      >
        <template #icon><SettingIcon /></template>
      </TButton>
    </div>

    <main class="auth-card">
      <div class="auth-card__brand">
        <img class="auth-card__mark" src="/favicon.ico" alt="" width="32" height="32" />
        <span class="auth-card__product">{{ t('app.name') }}</span>
      </div>

      <h1 class="auth-card__title">{{ title }}</h1>
      <p class="auth-card__sub">{{ sub }}</p>

      <!-- 步骤指示：当前步实底，完成步打勾 -->
      <ol class="auth-steps">
        <li class="auth-steps__item" :class="{ 'is-active': step === 1, 'is-done': step === 2 }">
          <span class="auth-steps__num">{{ step === 2 ? '✓' : '1' }}</span>
          {{ t('forgot.step1Label') }}
        </li>
        <li class="auth-steps__item" :class="{ 'is-active': step === 2 }">
          <span class="auth-steps__num">2</span>
          {{ t('forgot.step2Label') }}
        </li>
      </ol>

      <!-- 第一步：邮箱 -->
      <TForm
        v-if="step === 1"
        ref="form"
        class="auth-card__form"
        :data="emailForm"
        :rules="emailRules"
        label-width="0"
        @submit.prevent
      >
        <TFormItem name="email">
          <TInput v-model="emailForm.email" size="large" :placeholder="t('forgot.email')" clearable @enter="submitEmail">
            <template #prefix-icon><MailIcon /></template>
          </TInput>
        </TFormItem>

        <TButton class="auth-card__submit" theme="primary" block size="large" :loading="loading" @click="submitEmail">
          {{ t('forgot.sendLink') }}
        </TButton>
      </TForm>

      <!-- 第二步：令牌 + 新密码 -->
      <TForm
        v-else
        ref="form"
        class="auth-card__form"
        :data="resetForm"
        :rules="resetRules"
        label-width="0"
        @submit.prevent
      >
        <TFormItem name="token">
          <TInput v-model="resetForm.token" size="large" :placeholder="t('forgot.token')" clearable>
            <template #prefix-icon><KeyIcon /></template>
          </TInput>
        </TFormItem>

        <TFormItem name="newPassword">
          <TInput v-model="resetForm.newPassword" size="large" type="password" :placeholder="t('forgot.newPassword')">
            <template #prefix-icon><LockOnIcon /></template>
          </TInput>
        </TFormItem>

        <TFormItem name="confirm">
          <TInput v-model="resetForm.confirm" size="large" type="password" :placeholder="t('forgot.confirm')">
            <template #prefix-icon><LockOnIcon /></template>
          </TInput>
        </TFormItem>

        <TButton class="auth-card__submit" theme="primary" block size="large" :loading="loading" @click="submitReset">
          {{ t('forgot.reset') }}
        </TButton>
      </TForm>

      <div class="auth-card__links">
        <button v-if="step === 2" type="button" class="auth-card__link-btn" @click="backToStep1">
          {{ t('forgot.backStep') }}
        </button>
        <RouterLink to="/login">{{ t('forgot.backToLogin') }}</RouterLink>
      </div>
    </main>
  </div>
</template>

<style scoped>
/* 与登录页保持同一套尺寸与节奏 */
.auth-page {
  position: relative;
  min-height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 32px 20px 56px;
  overflow-y: auto;
}

.auth-top {
  position: fixed;
  top: 14px;
  right: 16px;
  z-index: 2;
  display: flex;
  align-items: center;
  padding: 2px 4px;
  border-radius: 8px;
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-border);
}

.auth-card {
  position: relative;
  z-index: 1;
  width: 420px;
  max-width: 100%;
  padding: 36px 34px 28px;
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-border);
  border-radius: 12px;
  box-shadow: 0 12px 40px rgba(6, 18, 34, 0.22);
}

.auth-card__brand {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  margin-bottom: 26px;
}

/* 产品图标：ico 原图 32px，按 1:1 显示最清晰；1px 描边避免深色卡片上糊成一块 */
.auth-card__mark {
  width: 32px;
  height: 32px;
  border-radius: 8px;
  flex-shrink: 0;
  display: block;
  box-shadow: 0 0 0 1px rgba(0, 0, 0, 0.1);
}

:root[theme-mode='dark'] .auth-card__mark {
  box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.14);
}

.auth-card__product {
  font-size: 16px;
  font-weight: 600;
  letter-spacing: 0.01em;
  color: var(--td-text-color-secondary);
}

.auth-card__title {
  margin: 0 0 10px;
  font-size: 26px;
  letter-spacing: -0.02em;
  text-align: center;
}

.auth-card__sub {
  margin: 0;
  font-size: 14px;
  line-height: 1.6;
  color: var(--mc-text-faint);
  text-align: center;
}

/* ===== 步骤条 ===== */
.auth-steps {
  display: flex;
  gap: 8px;
  margin: 22px 0 0;
  padding: 0;
  list-style: none;
  font-size: 13px;
}

.auth-steps__item {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 7px 10px;
  border-radius: 6px;
  background: var(--td-bg-color-secondarycontainer);
  color: var(--mc-text-faint);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.auth-steps__num {
  width: 18px;
  height: 18px;
  flex-shrink: 0;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  background: var(--td-bg-color-container);
  color: var(--mc-text-faint);
}

.auth-steps__item.is-active {
  background: var(--td-brand-color);
  color: #fff;
}

.auth-steps__item.is-active .auth-steps__num {
  background: rgba(255, 255, 255, 0.22);
  color: #fff;
}

.auth-steps__item.is-done {
  color: var(--td-success-color);
}

.auth-steps__item.is-done .auth-steps__num {
  color: var(--td-success-color);
}

/* ===== 表单 =====
 * 字段间距用 flex gap 统一控制：TDesign 的 FormItem margin 在本布局下不生效，
 * 会导致「最后一个字段紧贴按钮」。gap 对 FormItem 与按钮同样有效。 */
.auth-card__form {
  display: flex;
  flex-direction: column;
  gap: 26px;
  margin-top: 24px;
  animation: auth-form-in 0.16s ease-out;
}

.auth-card__form :deep(.t-form__item) {
  margin-bottom: 0;
}

@keyframes auth-form-in {
  from {
    opacity: 0.4;
    transform: translateY(6px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

@media (prefers-reduced-motion: reduce) {
  .auth-card__form {
    animation: none;
  }
}

.auth-card__submit {
  margin-top: 0;
}

.auth-card__links {
  margin-top: 24px;
  padding-top: 18px;
  border-top: 1px solid var(--td-component-border);
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 16px;
  font-size: 14px;
}

.auth-card__link-btn {
  padding: 0;
  border: none;
  background: none;
  font: inherit;
  color: var(--mc-link);
  cursor: pointer;
}

.auth-card__link-btn:hover {
  text-decoration: underline;
}

.auth-card__link-btn:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 2px;
  border-radius: 2px;
}

/* ===== 窄屏 ===== */
@media (max-width: 520px) {
  .auth-page {
    padding: 64px 12px 32px;
    justify-content: flex-start;
  }

  .auth-card {
    padding: 24px 20px 16px;
    box-shadow: none;
  }

  .auth-card__title {
    font-size: 22px;
  }

  .auth-steps {
    font-size: 12px;
  }
}
</style>
