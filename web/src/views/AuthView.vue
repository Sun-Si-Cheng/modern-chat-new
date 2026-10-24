<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  Form as TForm,
  FormItem as TFormItem,
  Input as TInput,
  Button as TButton,
  Tabs as TTabss,
  TabPanel as TTabPanel,
  MessagePlugin,
} from 'tdesign-vue-next'
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

async function doSubmit() {
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

/** TForm 的 submit 事件：Enter 键与点击按钮统一走这里 */
function onSubmit({ validateResult }: { validateResult: boolean | Record<string, unknown> }) {
  if (validateResult === true) void doSubmit()
}

function switchMode(m: 'login' | 'register') {
  mode.value = m
}
</script>

<template>
  <div class="auth-page">
    <!-- 品牌侧：产品真实能力，非装饰 -->
    <aside class="auth-brand">
      <div class="auth-brand__mark">M</div>
      <h1 class="auth-brand__title">ModernChat</h1>
      <p class="auth-brand__lead">
        开箱即用的纯文本聊天内核，装上插件，它就是你要的样子。
      </p>
      <ul class="auth-brand__points">
        <li>
          <strong>聊天内核</strong>
          <span>好友、单聊群聊、已读回执、撤回，全部开箱即用</span>
        </li>
        <li>
          <strong>插件扩展</strong>
          <span>自己写 JS 插件：图片、机器人、主题，装上即生效</span>
        </li>
        <li>
          <strong>安全边界</strong>
          <span>沙箱运行 + 权限白名单，插件碰不到你的登录态</span>
        </li>
      </ul>
      <p class="auth-brand__foot text-secondary">前后端分离 · 自托管</p>
    </aside>

    <!-- 表单侧 -->
    <main class="auth-panel">
      <div class="auth-panel__foot">
        <span class="text-placeholder">数据存储在您自己的 MySQL</span>
        <span class="text-placeholder">单实例自托管</span>
      </div>
      <div class="auth-card">
        <div class="auth-card__brand">
          <span class="auth-card__mark">M</span>
          <span class="auth-card__name">ModernChat</span>
        </div>
        <h2 class="auth-card__title">{{ mode === 'login' ? '登录你的账号' : '创建你的账号' }}</h2>
        <p class="auth-card__sub">
          {{
            mode === 'login'
              ? '使用用户名与密码继续'
              : '首个注册的用户将成为站长，之后注册的为普通用户'
          }}
        </p>

        <TTabss v-model="mode" @change="(v: unknown) => switchMode(v as 'login' | 'register')">
          <TTabPanel value="login" label="登录" />
          <TTabPanel value="register" label="注册" />
        </TTabss>

        <TForm
          ref="form"
          :key="mode"
          class="auth-card__form"
          :data="mode === 'login' ? loginForm : registerForm"
          :rules="rules"
          label-width="0"
          @submit="onSubmit"
        >
          <TFormItem name="username">
            <TInput v-model="(mode === 'login' ? loginForm : registerForm).username" placeholder="用户名" clearable />
          </TFormItem>
          <TFormItem v-if="mode === 'register'" name="nickname">
            <TInput v-model="registerForm.nickname" placeholder="昵称（别人看到的名字）" clearable />
          </TFormItem>
          <TFormItem
            name="password"
            :help="mode === 'register' ? '至少 6 位，建议混合字母与数字' : undefined"
          >
            <TInput
              v-model="(mode === 'login' ? loginForm : registerForm).password"
              type="password"
              placeholder="密码"
            />
          </TFormItem>
          <TFormItem v-if="mode === 'register'" name="confirm">
            <TInput v-model="registerForm.confirm" type="password" placeholder="确认密码" />
          </TFormItem>

          <TButton theme="primary" type="submit" block size="large" :loading="loading">
            {{ mode === 'login' ? '登录' : '创建账号并进入' }}
          </TButton>
        </TForm>

        <div class="auth-card__footer">
          <RouterLink v-if="mode === 'login'" to="/forgot">忘记密码？</RouterLink>
          <span v-else class="text-placeholder">忘记用户名？站长可代为重置</span>
        </div>
      </div>
    </main>
  </div>
</template>

<style scoped>
.auth-page {
  height: 100%;
  display: flex;
  background: var(--td-bg-color-container);
}

/* 品牌侧：近黑 ink 面板，品牌蓝只做点缀（mark 与关键词） */
.auth-brand {
  flex: 1 1 46%;
  background: #17191c;
  color: #fff;
  padding: 56px 64px;
  display: flex;
  flex-direction: column;
  justify-content: center;
  min-width: 0;
  position: relative;
}

.auth-brand__mark {
  width: 44px;
  height: 44px;
  border-radius: 10px;
  background: var(--td-brand-color);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 24px;
  font-weight: 700;
  margin-bottom: 20px;
}

.auth-brand__title {
  margin: 0 0 10px;
  font-size: 30px;
  letter-spacing: -0.02em;
}

.auth-brand__lead {
  margin: 0 0 32px;
  font-size: 16px;
  line-height: 1.6;
  color: rgba(255, 255, 255, 0.92);
  max-width: 34em;
}

.auth-brand__points {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 20px;
  max-width: 26em;
}

.auth-brand__points strong {
  display: block;
  font-size: 15px;
  margin-bottom: 2px;
  color: #7cb8ff;
}

.auth-brand__points span {
  font-size: 13px;
  color: rgba(255, 255, 255, 0.78);
  line-height: 1.55;
}

.auth-brand__foot {
  margin-top: 40px;
  font-size: 12px;
  color: rgba(255, 255, 255, 0.6);
}

/* 表单侧：与卡片同色，靠 1px 边框与留白区分 */
.auth-panel {
  flex: 1 1 54%;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 40px 24px 64px;
  background: var(--td-bg-color-container);
  position: relative;
}

.auth-card {
  width: 440px;
  max-width: 100%;
  padding: 32px;
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-border);
  border-radius: 12px;
}

.auth-panel__foot {
  position: absolute;
  left: 32px;
  right: 32px;
  bottom: 20px;
  display: flex;
  justify-content: space-between;
  font-size: 12px;
}

.auth-card__brand {
  display: none;
  align-items: center;
  gap: 8px;
  margin-bottom: 18px;
}

.auth-card__mark {
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

.auth-card__name {
  font-weight: 600;
}

.auth-card__title {
  margin: 0 0 6px;
  font-size: 20px;
}

.auth-card__sub {
  margin: 0 0 10px;
  font-size: 13px;
  line-height: 1.55;
}

.auth-card__form {
  animation: auth-form-in 0.16s ease-out;
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

.auth-card__footer {
  margin-top: 14px;
  min-height: 20px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 13px;
}

/* 窄屏：品牌侧折叠为顶部条 */
@media (max-width: 900px) {
  .auth-page {
    flex-direction: column;
  }

  .auth-brand {
    flex: none;
    padding: 24px;
  }

  .auth-brand__points,
  .auth-brand__foot {
    display: none;
  }

  .auth-panel__foot {
    display: none;
  }

  .auth-brand__mark {
    display: none;
  }

  .auth-brand__title {
    margin: 0 0 4px;
    font-size: 22px;
  }

  .auth-brand__lead {
    margin: 0;
    font-size: 13px;
  }

  .auth-panel {
    flex: 1;
  }

  .auth-card {
    border: none;
    padding: 8px 0;
    background: transparent;
  }

  .auth-card__brand {
    display: flex;
  }
}
</style>
