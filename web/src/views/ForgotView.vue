<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { MessagePlugin, Form as TForm, FormItem as TFormItem, Input as TInput, Button as TButton } from 'tdesign-vue-next'
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
  <div class="auth-page">
    <div class="auth-card">
      <h1 class="auth-title">找回密码</h1>
      <p class="auth-sub text-secondary">通过注册邮箱重置密码</p>

      <template v-if="step === 1">
        <TForm ref="form" :data="forgotForm" label-width="0" @submit.prevent>
          <TFormItem name="email" :rules="[{ required: true, message: '请输入邮箱', trigger: 'blur' }]">
            <TInput v-model="forgotForm.email" placeholder="注册邮箱" clearable />
          </TFormItem>
          <TButton theme="primary" block :loading="loading" @click="submitEmail">发送重置链接</TButton>
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
            <TInput v-model="resetForm.newPassword" type="password" placeholder="新密码（至少 6 位）" />
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
          <TButton theme="primary" block :loading="loading" @click="submitReset">重置密码</TButton>
        </TForm>
      </template>

      <div class="auth-footer">
        <RouterLink to="/login">返回登录</RouterLink>
      </div>
    </div>
  </div>
</template>

<style scoped>
.auth-page {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--td-bg-color-page);
}

.auth-card {
  width: 380px;
  padding: 32px;
  background: var(--td-bg-color-container);
  border-radius: 12px;
  box-shadow: var(--td-shadow-1);
}

.auth-title {
  margin: 0 0 4px;
  font-size: 24px;
  text-align: center;
}

.auth-sub {
  text-align: center;
  margin: 0 0 16px;
  font-size: 13px;
}

.auth-footer {
  margin-top: 16px;
  font-size: 13px;
  text-align: center;
}
</style>
