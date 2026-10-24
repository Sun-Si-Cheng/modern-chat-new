<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  Form as TForm,
  FormItem as TFormItem,
  Input as TInput,
  Button as TButton,
  MessagePlugin,
} from 'tdesign-vue-next'
import type { FormInstanceFunctions, FormProps } from 'tdesign-vue-next'
import { LockOnIcon, UserIcon } from 'tdesign-icons-vue-next'

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

/** 当前模式的表单数据（模板与校验共用） */
const data = computed(() => (mode.value === 'login' ? loginForm : registerForm))

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

/** TForm 的 submit：Enter 与按钮统一走校验→提交 */
function onSubmit({ validateResult }: { validateResult: boolean | Record<string, unknown> }) {
  if (validateResult === true) void doSubmit()
}

function switchMode(m: 'login' | 'register') {
  mode.value = m
}
</script>

<template>
  <div class="auth-page">
    <!-- 背景层：两段纯色（天 / 水）+ 地平线；放置 web/public/login-bg.jpg 可覆盖为自定义图片 -->
    <div class="auth-scene" aria-hidden="true">
      <div class="auth-scene__water" />
      <div class="auth-scene__img" />
    </div>

    <div class="auth-top">
      <PreferencesMenu />
    </div>

    <main class="auth-card">
      <div class="auth-card__brand">
        <span class="auth-card__mark">M</span>
        <span class="auth-card__product">{{ t('app.name') }}</span>
      </div>

      <h1 class="auth-card__title">
        {{ mode === 'login' ? t('auth.loginTitle') : t('auth.registerTitle') }}
      </h1>
      <p class="auth-card__sub">
        {{ mode === 'login' ? t('auth.loginSub') : t('auth.registerSub') }}
      </p>

      <TForm
        ref="form"
        :key="mode"
        class="auth-card__form"
        :data="data"
        :rules="rules"
        label-width="0"
        @submit="onSubmit"
      >
        <TFormItem name="username">
          <TInput v-model="data.username" :placeholder="t('auth.username')" clearable>
            <template #prefix-icon><UserIcon /></template>
          </TInput>
        </TFormItem>

        <TFormItem v-if="mode === 'register'" name="nickname">
          <TInput v-model="registerForm.nickname" :placeholder="t('auth.nickname')" clearable>
            <template #prefix-icon><UserIcon /></template>
          </TInput>
        </TFormItem>

        <TFormItem name="password" :help="mode === 'register' ? t('auth.passwordHelp') : undefined">
          <TInput v-model="data.password" type="password" :placeholder="t('auth.password')">
            <template #prefix-icon><LockOnIcon /></template>
          </TInput>
        </TFormItem>

        <TFormItem v-if="mode === 'register'" name="confirm">
          <TInput v-model="registerForm.confirm" type="password" :placeholder="t('auth.confirmPassword')">
            <template #prefix-icon><LockOnIcon /></template>
          </TInput>
        </TFormItem>

        <TButton class="auth-card__submit" theme="primary" type="submit" block size="large" :loading="loading">
          {{ mode === 'login' ? t('auth.actionLogin') : t('auth.actionRegister') }}
        </TButton>
      </TForm>

      <div class="auth-card__links">
        <template v-if="mode === 'login'">
          <RouterLink to="/forgot">{{ t('auth.forgot') }}</RouterLink>
          <span class="auth-card__dot" aria-hidden="true">·</span>
          <span class="text-placeholder">{{ t('auth.noAccount') }}</span>
          <button type="button" class="auth-card__link-btn" @click="switchMode('register')">
            {{ t('auth.signupLink') }}
          </button>
        </template>
        <template v-else>
          <span class="text-placeholder">{{ t('auth.haveAccount') }}</span>
          <button type="button" class="auth-card__link-btn" @click="switchMode('login')">
            {{ t('auth.loginLink') }}
          </button>
        </template>
      </div>

      <p class="auth-card__legal">
        <span v-if="mode === 'register'" class="text-placeholder">{{ t('legal.agreePrefix') }}</span>
        <RouterLink to="/terms">{{ t('legal.termsLink') }}</RouterLink>
        <span class="text-placeholder">{{ t('legal.and') }}</span>
        <RouterLink to="/privacy">{{ t('legal.privacyLink') }}</RouterLink>
      </p>
    </main>

    <p class="auth-foot text-placeholder">{{ t('auth.footDb') }} · {{ t('auth.footSingle') }}</p>
  </div>
</template>

<style scoped>
/* ===== 场景层：两段纯色（天 / 水）由一条地平线分开，无渐变、无装饰光斑 ===== */
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

.auth-scene {
  position: fixed;
  inset: 0;
  z-index: 0;
  background: #123a5c; /* 天 */
}

.auth-scene__water {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  height: 34%;
  background: #0b2436; /* 水 */
  border-top: 1px solid rgba(255, 255, 255, 0.1); /* 地平线 */
}

/* 可选自定义背景图：把图片放到 web/public/login-bg.jpg 即自动覆盖纯色（404 时无副作用） */
.auth-scene__img {
  position: absolute;
  inset: 0;
  background: url('/login-bg.jpg') center / cover no-repeat;
}

/* 图片蒙版：无论用户放什么图，都保证卡片周围与页脚文字可读；暗色主题下压得更沉 */
.auth-scene__img::after {
  content: '';
  position: absolute;
  inset: 0;
  background: rgba(8, 19, 31, 0.28);
}

:root[theme-mode='dark'] .auth-scene__img::after {
  background: rgba(3, 8, 14, 0.52);
}

:root[theme-mode='dark'] .auth-scene {
  background: #08131f;
}

:root[theme-mode='dark'] .auth-scene__water {
  background: #050d16;
  border-top-color: rgba(255, 255, 255, 0.06);
}

/* ===== 顶部偏好（语言 / 主题） ===== */
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

/* ===== 卡片 ===== */
.auth-card {
  position: relative;
  z-index: 1;
  width: 400px;
  max-width: 100%;
  padding: 28px 28px 18px;
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-border);
  border-radius: 12px;
  /* 浮于场景之上，允许一层柔和阴影 */
  box-shadow: 0 12px 40px rgba(6, 18, 34, 0.22);
}

.auth-card__brand {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 18px;
}

.auth-card__mark {
  width: 24px;
  height: 24px;
  border-radius: 6px;
  background: var(--td-brand-color);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 13px;
  font-weight: 700;
  flex-shrink: 0;
}

.auth-card__product {
  font-size: 14px;
  font-weight: 600;
  letter-spacing: 0.01em;
  color: var(--td-text-color-secondary);
}

.auth-card__title {
  margin: 0 0 6px;
  font-size: 24px;
  letter-spacing: -0.02em;
}

.auth-card__sub {
  margin: 0 0 20px;
  font-size: 14px;
  line-height: 1.6;
  color: var(--mc-text-faint);
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

.auth-card__submit {
  margin-top: 4px;
}

.auth-card__links {
  margin-top: 18px;
  padding-top: 14px;
  border-top: 1px solid var(--td-component-border);
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 6px;
  font-size: 14px;
}

.auth-card__dot {
  color: var(--mc-text-faint);
}

/* 切换模式是动作而非跳转，用按钮保持语义与键盘可达 */
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

.auth-card__legal {
  margin: 14px 0 0;
  font-size: 12px;
  line-height: 1.6;
  color: var(--mc-text-faint);
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  justify-content: center;
}

.auth-card__legal a {
  color: var(--mc-link);
}

/* ===== 页脚 ===== */
/* 页脚压在场景层上：用半透明深色胶囊保证任意背景图下的对比度 */
.auth-foot {
  position: relative;
  z-index: 1;
  margin: 20px 0 0;
  padding: 4px 12px;
  border-radius: 999px;
  background: rgba(8, 19, 31, 0.5);
  color: rgba(255, 255, 255, 0.86);
  font-size: 13px;
  text-align: center;
}

/* ===== 窄屏：卡片贴边，弱化阴影 ===== */
@media (max-width: 520px) {
  .auth-page {
    padding: 16px 12px 32px;
    justify-content: flex-start;
    padding-top: 64px;
  }

  .auth-card {
    padding: 24px 20px 16px;
    box-shadow: none;
  }

  .auth-card__title {
    font-size: 22px;
  }

  .auth-foot {
    margin-top: 16px;
  }
}
</style>
