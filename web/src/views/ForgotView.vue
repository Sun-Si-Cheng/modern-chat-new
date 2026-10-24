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

import { authApi } from '@/api'

const router = useRouter()

/** step 1: 提交邮箱拿重置链接（开发期服务端打日志）；step 2: 用 token 设新密码 */
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
    void MessagePlugin.success('若邮箱存在，重置链接已发送（开发环境查看服务端日志）')
    step.value = 2
  } catch (e) {
    void MessagePlugin.error((e as Error).message || '发送失败')
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
    void MessagePlugin.success('密码已重置，请用新密码登录')
    await router.push('/login')
  } catch (e) {
    void MessagePlugin.error((e as Error).message || '重置失败')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="forgot-page">
    <div class="forgot-card">
      <div class="forgot-brand">
        <span class="forgot-brand__mark">M</span>
        <span class="forgot-brand__name">ModernChat</span>
      </div>
      <h1 class="forgot-title">找回密码</h1>
      <p class="forgot-sub">通过注册邮箱重置。重置后所有已登录设备将被强制下线。</p>

      <template v-if="step === 1">
        <TForm ref="form" :data="forgotForm" label-width="0" @submit.prevent>
          <TFormItem name="email" :rules="[{ required: true, message: '请输入邮箱', trigger: 'blur' }]">
            <TInput v-model="forgotForm.email" placeholder="注册邮箱" clearable />
          </TFormItem>
          <TButton theme="primary" block size="large" :loading="loading" @click="submitEmail">发送重置链接</TButton>
        </TForm>
      </template>

      <template v-else>
        <TForm ref="form" :data="resetForm" label-width="0" @submit.prevent>
          <TFormItem name="token" :rules="[{ required: true, message: '请输入邮件中的重置令牌', trigger: 'blur' }]">
            <TInput v-model="resetForm.token" placeholder="邮件中的重置令牌" clearable />
          </TFormItem>
          <TFormItem
            name="newPassword"
            :rules="[
              { required: true, message: '请输入新密码', trigger: 'blur' },
              { min: 6, message: '密码至少 6 位', trigger: 'blur' },
            ]"
          >
            <TInput v-model="resetForm.newPassword" type="password" placeholder="新密码" />
          </TFormItem>
          <TFormItem
            name="confirm"
            :rules="[
              {
                validator: (val: string) => val === resetForm.newPassword,
                message: '两次输入的密码不一致',
                trigger: 'blur',
              },
            ]"
          >
            <TInput v-model="resetForm.confirm" type="password" placeholder="确认新密码" />
          </TFormItem>
          <TButton theme="primary" block size="large" :loading="loading" @click="submitReset">重置密码</TButton>
        </TForm>
      </template>

      <div class="forgot-footer">
        <RouterLink to="/login">返回登录</RouterLink>
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
