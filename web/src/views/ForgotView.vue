<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  Form as TForm,
  FormItem as TFormItem,
  Input as TInput,
  Button as TButton,
  MessagePlugin,
} from 'tdesign-vue-next'
import type { FormInstanceFunctions } from 'tdesign-vue-next'

import PreferencesMenu from '@/components/common/PreferencesMenu.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import { authApi } from '@/api'

const router = useRouter()
const { t, te } = useAppI18n()

/** step 1: 提交邮箱拿重置链接（开发期服务端打日志）；step 2: 用令牌设新密码 */
const step = ref<1 | 2>(1)
const loading = ref(false)
const form = ref<FormInstanceFunctions>()

const forgotForm = reactive({ email: '' })
const resetForm = reactive({ token: '', newPassword: '', confirm: '' })

async function submitEmail() {
  const valid = await form.value?.validate()
  if (valid !== true) return
  loading.value = true
  try {
    await authApi.forgot({ email: forgotForm.email })
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
    await authApi.reset({ token: resetForm.token, newPassword: resetForm.newPassword })
    void MessagePlugin.success(t('forgot.resetOk'))
    await router.push('/login')
  } catch (e) {
    void MessagePlugin.error(te(e) || t('forgot.resetFailed'))
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="forgot-page">
    <div class="forgot-page__prefs">
      <PreferencesMenu />
    </div>

    <div class="forgot-card">
      <div class="forgot-brand">
        <span class="forgot-brand__mark">M</span>
        <span class="forgot-brand__name">{{ t('app.name') }}</span>
      </div>
      <h1 class="forgot-title">{{ t('forgot.title') }}</h1>
      <p class="forgot-sub">{{ t('forgot.sub') }}</p>

      <template v-if="step === 1">
        <TForm ref="form" :data="forgotForm" label-width="0" @submit.prevent>
          <TFormItem name="email" :rules="[{ required: true, message: t('forgot.emailRequired'), trigger: 'blur' }]">
            <TInput v-model="forgotForm.email" :placeholder="t('forgot.email')" clearable />
          </TFormItem>
          <TButton theme="primary" block size="large" :loading="loading" @click="submitEmail">
            {{ t('forgot.sendLink') }}
          </TButton>
        </TForm>
      </template>

      <template v-else>
        <TForm ref="form" :data="resetForm" label-width="0" @submit.prevent>
          <TFormItem name="token" :rules="[{ required: true, message: t('forgot.tokenRequired'), trigger: 'blur' }]">
            <TInput v-model="resetForm.token" :placeholder="t('forgot.token')" clearable />
          </TFormItem>
          <TFormItem
            name="newPassword"
            :rules="[
              { required: true, message: t('forgot.newPasswordRequired'), trigger: 'blur' },
              { min: 6, message: t('auth.validation.passwordMin'), trigger: 'blur' },
            ]"
          >
            <TInput v-model="resetForm.newPassword" type="password" :placeholder="t('forgot.newPassword')" />
          </TFormItem>
          <TFormItem
            name="confirm"
            :rules="[
              {
                validator: (val: string) => val === resetForm.newPassword,
                message: t('auth.validation.confirmMismatch'),
                trigger: 'blur',
              },
            ]"
          >
            <TInput v-model="resetForm.confirm" type="password" :placeholder="t('forgot.confirm')" />
          </TFormItem>
          <TButton theme="primary" block size="large" :loading="loading" @click="submitReset">
            {{ t('forgot.reset') }}
          </TButton>
        </TForm>
      </template>

      <div class="forgot-footer">
        <RouterLink to="/login">{{ t('forgot.backToLogin') }}</RouterLink>
      </div>
    </div>
  </div>
</template>

<style scoped>
.forgot-page {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--td-bg-color-page);
  padding: 24px;
  position: relative;
}

.forgot-page__prefs {
  position: absolute;
  top: 12px;
  right: 16px;
}

.forgot-card {
  width: 420px;
  max-width: 100%;
  padding: 32px;
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-border);
  border-radius: 12px;
}

.forgot-brand {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 18px;
}

.forgot-brand__mark {
  width: 26px;
  height: 26px;
  border-radius: 6px;
  background: var(--td-brand-color);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
  font-weight: 700;
}

.forgot-brand__name {
  font-weight: 600;
}

.forgot-title {
  margin: 0 0 6px;
  font-size: 20px;
}

.forgot-sub {
  margin: 0 0 18px;
  font-size: 13px;
  line-height: 1.6;
}

.forgot-footer {
  margin-top: 16px;
  font-size: 13px;
  text-align: center;
}
</style>
