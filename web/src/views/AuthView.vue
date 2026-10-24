<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
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

import PreferencesMenu from '@/components/common/PreferencesMenu.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const { t, te } = useAppI18n()

const mode = ref<'login' | 'register'>('login')
const loading = ref(false)
const form = ref<FormInstanceFunctions>()

const loginForm = reactive({ username: '', password: '' })
const registerForm = reactive({ username: '', nickname: '', password: '', confirm: '' })

const rules = computed<FormProps['rules']>(() => ({
  username: [{ required: true, message: t('auth.validation.usernameRequired'), trigger: 'blur' }],
  password: [
    { required: true, message: t('auth.validation.passwordRequired'), trigger: 'blur' },
    { min: 6, message: t('auth.validation.passwordMin'), trigger: 'blur' },
  ],
  nickname: [{ required: true, message: t('auth.validation.nicknameRequired'), trigger: 'blur' }],
  confirm: [
    {
      validator: (val: string) => val === registerForm.password,
      message: t('auth.validation.confirmMismatch'),
      trigger: 'blur',
    },
  ],
}))

async function doSubmit() {
  loading.value = true
  try {
    if (mode.value === 'login') {
      await auth.login(loginForm.username, loginForm.password)
    } else {
      await auth.register(registerForm.username, registerForm.password, registerForm.nickname)
      void MessagePlugin.success(t('auth.registerOk'))
    }
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/chat'
    await router.push(redirect)
  } catch (e) {
    void MessagePlugin.error(te(e) || t('auth.opFailed'))
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
    <div class="auth-page__prefs">
      <PreferencesMenu />
    </div>

    <!-- 品牌侧：产品真实能力，非装饰 -->
    <aside class="auth-brand">
      <div class="auth-brand__mark">M</div>
      <h1 class="auth-brand__title">{{ t('app.name') }}</h1>
      <p class="auth-brand__lead">{{ t('auth.brand.lead') }}</p>
      <ul class="auth-brand__points">
        <li>
          <strong>{{ t('auth.brand.core') }}</strong>
          <span>{{ t('auth.brand.coreDesc') }}</span>
        </li>
        <li>
          <strong>{{ t('auth.brand.plugin') }}</strong>
          <span>{{ t('auth.brand.pluginDesc') }}</span>
        </li>
        <li>
          <strong>{{ t('auth.brand.secure') }}</strong>
          <span>{{ t('auth.brand.secureDesc') }}</span>
        </li>
      </ul>
      <p class="auth-brand__foot text-secondary">{{ t('auth.brand.foot') }}</p>
    </aside>

    <!-- 表单侧 -->
    <main class="auth-panel">
      <div class="auth-panel__foot">
        <span class="text-placeholder">{{ t('auth.footDb') }}</span>
        <span class="text-placeholder">{{ t('auth.footSingle') }}</span>
      </div>
      <div class="auth-card">
        <div class="auth-card__brand">
          <span class="auth-card__mark">M</span>
          <span class="auth-card__name">{{ t('app.name') }}</span>
        </div>
        <h2 class="auth-card__title">{{ mode === 'login' ? t('auth.loginTitle') : t('auth.registerTitle') }}</h2>
        <p class="auth-card__sub">{{ mode === 'login' ? t('auth.loginSub') : t('auth.registerSub') }}</p>

        <TTabss v-model="mode" @change="(v: unknown) => switchMode(v as 'login' | 'register')">
          <TTabPanel value="login" :label="t('auth.tabLogin')" />
          <TTabPanel value="register" :label="t('auth.tabRegister')" />
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
            <TInput
              v-model="(mode === 'login' ? loginForm : registerForm).username"
              :placeholder="t('auth.username')"
              clearable
            />
          </TFormItem>
          <TFormItem v-if="mode === 'register'" name="nickname">
            <TInput v-model="registerForm.nickname" :placeholder="t('auth.nickname')" clearable />
          </TFormItem>
          <TFormItem name="password" :help="mode === 'register' ? t('auth.passwordHelp') : undefined">
            <TInput
              v-model="(mode === 'login' ? loginForm : registerForm).password"
              type="password"
              :placeholder="t('auth.password')"
            />
          </TFormItem>
          <TFormItem v-if="mode === 'register'" name="confirm">
            <TInput v-model="registerForm.confirm" type="password" :placeholder="t('auth.confirmPassword')" />
          </TFormItem>

          <TButton theme="primary" type="submit" block size="large" :loading="loading">
            {{ mode === 'login' ? t('auth.actionLogin') : t('auth.actionRegister') }}
          </TButton>
        </TForm>

        <div class="auth-card__footer">
          <RouterLink v-if="mode === 'login'" to="/forgot">{{ t('auth.forgot') }}</RouterLink>
          <span v-else class="text-placeholder">{{ t('auth.forgotUsername') }}</span>
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
  position: relative;
}

.auth-page__prefs {
  position: absolute;
  top: 12px;
  right: 16px;
  z-index: 10;
  background: var(--td-bg-color-container);
  border-radius: 6px;
  padding: 0 2px;
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
