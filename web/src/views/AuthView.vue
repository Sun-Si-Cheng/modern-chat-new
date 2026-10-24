<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { MessagePlugin, Form as TForm, FormItem as TFormItem, Input as TInput, Button as TButton, Tabs as TTabss, TabPanel as TTabPanel } from 'tdesign-vue-next'
import type { FormInstanceFunctions, FormProps } from 'tdesign-vue-next'

import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()

const mode = ref<'login' | 'register'>('login')
const loading = ref(false)
const form = ref<FormInstanceFunctions>()

const loginForm = reactive({ username: '', password: '' })
const registerForm = reactive({ username: '', nickname: '', password: '', confirm: '' })

const rules: FormProps['rules'] = {
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' },
    { min: 6, message: '密码至少 6 位', trigger: 'blur' },
  ],
  nickname: [{ required: true, message: '请输入昵称', trigger: 'blur' }],
  confirm: [
    {
      validator: (val: string) => val === registerForm.password,
      message: '两次输入的密码不一致',
      trigger: 'blur',
    },
  ],
}

async function submit() {
  const valid = await form.value?.validate()
  if (valid !== true) return
  loading.value = true
  try {
    if (mode.value === 'login') {
      await auth.login(loginForm.username, loginForm.password)
    } else {
      await auth.register(registerForm.username, registerForm.password, registerForm.nickname)
      void MessagePlugin.success('注册成功，欢迎！')
    }
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/chat'
    await router.push(redirect)
  } catch (e) {
    void MessagePlugin.error((e as Error).message || '操作失败')
  } finally {
    loading.value = false
  }
}

function switchMode(m: 'login' | 'register') {
  mode.value = m
}
</script>

<template>
  <div class="auth-page">
    <div class="auth-card">
      <h1 class="auth-title">ModernChat</h1>
      <p class="auth-sub text-secondary">插件化网页聊天平台</p>

      <TTabss v-model="mode" @change="(v: unknown) => switchMode(v as 'login' | 'register')">
        <TTabPanel value="login" label="登录" />
        <TTabPanel value="register" label="注册" />
      </TTabss>

      <TForm
        ref="form"
        :data="mode === 'login' ? loginForm : registerForm"
        :rules="rules"
        label-width="0"
        @submit.prevent
      >
        <TFormItem name="username">
          <TInput v-model="(mode === 'login' ? loginForm : registerForm).username" placeholder="用户名" clearable />
        </TFormItem>
        <TFormItem v-if="mode === 'register'" name="nickname">
          <TInput v-model="registerForm.nickname" placeholder="昵称" clearable />
        </TFormItem>
        <TFormItem name="password">
          <TInput
            v-model="(mode === 'login' ? loginForm : registerForm).password"
            type="password"
            placeholder="密码（至少 6 位）"
          />
        </TFormItem>
        <TFormItem v-if="mode === 'register'" name="confirm">
          <TInput v-model="registerForm.confirm" type="password" placeholder="确认密码" />
        </TFormItem>

        <TButton theme="primary" type="submit" block :loading="loading" @click="submit">
          {{ mode === 'login' ? '登录' : '注册' }}
        </TButton>
      </TForm>

      <div class="auth-footer">
        <RouterLink to="/forgot">忘记密码？</RouterLink>
        <span v-if="mode === 'login'" class="text-placeholder">首个注册的用户将成为站长</span>
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
  display: flex;
  justify-content: space-between;
  font-size: 13px;
}
</style>
