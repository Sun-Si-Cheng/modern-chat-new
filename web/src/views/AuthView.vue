<script setup lang="ts">
import UiCheckbox from '@/components/ui/UiCheckbox.vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiInput from '@/components/ui/UiInput.vue'
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Form as TForm, FormItem as TFormItem, MessagePlugin } from 'tdesign-vue-next'
import type { FormInstanceFunctions, FormProps } from 'tdesign-vue-next'
import { LockOnIcon, MailIcon, UserIcon } from 'tdesign-icons-vue-next'

import AuthScene from '@/components/common/AuthScene.vue'
import QuickPrefs from '@/components/common/QuickPrefs.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import { useAuthStore } from '@/stores/auth'
import { useSiteStore } from '@/stores/site'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const site = useSiteStore()
const { t, te } = useAppI18n()

/** 站点名：后台改了这里跟着变；接口没回来时用语言包兜底 */
const siteName = computed(() => (site.loaded ? site.config.name : t('app.name')))

const mode = ref<'login' | 'register'>('login')
const loading = ref(false)
const form = ref<FormInstanceFunctions>()

const loginForm = reactive({ username: '', password: '' })
const registerForm = reactive({
  username: '',
  nickname: '',
  password: '',
  confirm: '',
  inviteCode: '',
  email: '',
  emailCode: '',
})

/** 站点开启「注册需邮箱验证」时才出现邮箱相关字段 */
const needEmail = computed(() => site.config.requireEmailVerify)

// ===== 邮箱验证码 =====

const sendingCode = ref(false)
const codeCooldown = ref(0)
let codeTimer: number | undefined

function stopCodeTimer() {
  if (codeTimer !== undefined) {
    window.clearInterval(codeTimer)
    codeTimer = undefined
  }
}

function startCooldown(seconds: number) {
  codeCooldown.value = Math.min(600, Math.max(1, Math.floor(seconds) || 60))
  stopCodeTimer()
  codeTimer = window.setInterval(() => {
    codeCooldown.value -= 1
    if (codeCooldown.value <= 0) stopCodeTimer()
  }, 1000)
}

onBeforeUnmount(stopCodeTimer)

async function sendEmailCode() {
  if (sendingCode.value || codeCooldown.value > 0) return
  const email = registerForm.email.trim()
  // 前端先挡一次明显错误的邮箱：省一次请求，也避免「验证码发到不存在的地址」
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
    void MessagePlugin.warning(t('auth.validation.emailRequired'))
    return
  }
  sendingCode.value = true
  try {
    startCooldown(await auth.sendRegisterCode(email))
    void MessagePlugin.success(t('auth.codeSent'))
  } catch (e) {
    void MessagePlugin.error(te(e) || t('auth.codeSendFailed'))
  } finally {
    sendingCode.value = false
  }
}

/**
 * 协议同意状态：
 * - 登录态默认勾选（已有账号视为此前已同意，避免每次重复勾选）；
 * - 注册态优先沿用本机记录，否则要求用户主动勾选；
 * - 勾选结果写入 localStorage，后续访问自动沿用。
 */
const AGREED_KEY = 'mc_terms_agreed'
const storedAgreed = () => localStorage.getItem(AGREED_KEY) === '1'
const agreed = ref(mode.value === 'login' || storedAgreed())

watch(agreed, (v) => {
  if (v) localStorage.setItem(AGREED_KEY, '1')
  else localStorage.removeItem(AGREED_KEY)
})

watch(mode, (m) => {
  agreed.value = m === 'login' || storedAgreed()
})

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
  // 邮箱验证只在站点开启时校验（关闭时字段不渲染，规则也不该拦提交）
  ...(needEmail.value
    ? {
        email: [{ required: true, message: t('auth.validation.emailRequired'), trigger: 'blur' }],
        emailCode: [{ required: true, message: t('auth.validation.codeRequired'), trigger: 'blur' }],
      }
    : {}),
}))

async function doSubmit() {
  if (!agreed.value) {
    void MessagePlugin.warning(t('legal.agreeRequired'))
    return
  }
  loading.value = true
  try {
    if (mode.value === 'login') {
      await auth.login(loginForm.username, loginForm.password)
    } else {
      await auth.register(
        registerForm.username,
        registerForm.password,
        registerForm.nickname,
        site.config.requireInviteCode ? registerForm.inviteCode.trim() : undefined,
        needEmail.value ? registerForm.email.trim() : undefined,
        needEmail.value ? registerForm.emailCode.trim() : undefined,
      )
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
    <AuthScene />

    <div class="auth-top">
      <QuickPrefs />
    </div>

    <main class="auth-card">
      <div class="auth-card__brand">
        <img class="auth-card__mark" src="/favicon.ico" alt="" width="32" height="32" />
        <span class="auth-card__product">{{ siteName }}</span>
      </div>

      <h1 class="auth-card__title">
        {{ mode === 'login' ? t('auth.loginTitle') : t('auth.registerTitle') }}
      </h1>

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
          <UiInput v-model="data.username" size="large" :placeholder="t('auth.username')" clearable>
            <template #prefix-icon><UserIcon /></template>
          </UiInput>
        </TFormItem>

        <TFormItem v-if="mode === 'register'" name="nickname">
          <UiInput v-model="registerForm.nickname" size="large" :placeholder="t('auth.nickname')" clearable>
            <template #prefix-icon><UserIcon /></template>
          </UiInput>
        </TFormItem>

        <!-- 站点开启「注册需邮箱验证」时才出现：邮箱 + 验证码 -->
        <TFormItem v-if="mode === 'register' && needEmail" name="email">
          <UiInput
            v-model="registerForm.email"
            size="large"
            type="email"
            :placeholder="t('auth.email')"
            clearable
          >
            <template #prefix-icon><MailIcon /></template>
          </UiInput>
        </TFormItem>

        <TFormItem v-if="mode === 'register' && needEmail" name="emailCode">
          <div class="auth-card__code-row">
            <UiInput
              v-model="registerForm.emailCode"
              size="large"
              :maxlength="6"
              :placeholder="t('auth.emailCode')"
            />
            <UiButton
              variant="outline"
              size="large"
              :loading="sendingCode"
              :disabled="codeCooldown > 0"
              @click="sendEmailCode"
            >
              {{ codeCooldown > 0 ? t('auth.resendIn', { n: codeCooldown }) : t('auth.sendCode') }}
            </UiButton>
          </div>
        </TFormItem>

        <!-- 站点开启邀请码注册时才出现 -->
        <TFormItem v-if="mode === 'register' && site.config.requireInviteCode" name="inviteCode">
          <UiInput
            v-model="registerForm.inviteCode"
            size="large"
            :placeholder="t('auth.inviteCode')"
            clearable
          >
            <template #prefix-icon><LockOnIcon /></template>
          </UiInput>
        </TFormItem>

        <TFormItem name="password" :help="mode === 'register' ? t('auth.passwordHelp') : undefined">
          <UiInput v-model="data.password" size="large" type="password" :placeholder="t('auth.password')">
            <template #prefix-icon><LockOnIcon /></template>
          </UiInput>
        </TFormItem>

        <TFormItem v-if="mode === 'register'" name="confirm">
          <UiInput v-model="registerForm.confirm" size="large" type="password" :placeholder="t('auth.confirmPassword')">
            <template #prefix-icon><LockOnIcon /></template>
          </UiInput>
        </TFormItem>

        <!-- 协议同意：紧邻密码与提交按钮之间 -->
        <div class="auth-card__agree">
          <UiCheckbox v-model="agreed" size="small">
            <span class="auth-card__agree-text">
              {{ t('legal.agreePrefix') }}
              <!-- 新标签打开，避免打断登录流程 -->
              <RouterLink :to="'/terms'" target="_blank" @click.stop>{{ t('legal.termsLink') }}</RouterLink>
              {{ t('legal.and') }}
              <RouterLink :to="'/privacy'" target="_blank" @click.stop>{{ t('legal.privacyLink') }}</RouterLink>
            </span>
          </UiCheckbox>
        </div>

        <UiButton class="auth-card__submit" theme="primary" type="submit" block size="large" :loading="loading">
          {{ mode === 'login' ? t('auth.actionLogin') : t('auth.actionRegister') }}
        </UiButton>
      </TForm>

      <div class="auth-card__links">
        <template v-if="mode === 'login'">
          <RouterLink to="/forgot">{{ t('auth.forgot') }}</RouterLink>
          <!-- 站点关闭注册时不再露出入口（服务端也会拒，这里只是不误导用户） -->
          <button
            v-if="site.config.allowRegister"
            type="button"
            class="auth-card__link-btn"
            @click="switchMode('register')"
          >
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
    </main>
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

/* ===== 顶部偏好（语言 / 主题） ===== */
.auth-top {
  position: fixed;
  top: 14px;
  right: 16px;
  z-index: 2;
  display: flex;
  align-items: center;
  padding: 4px 6px;
  border-radius: var(--mc-radius-sm);
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-border);
  box-shadow: var(--mc-shadow-1);
}

/* ===== 卡片 ===== */
.auth-card {
  position: relative;
  z-index: 1;
  width: 420px;
  max-width: 100%;
  padding: 34px 32px 26px;
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-border);
  border-radius: var(--mc-radius-lg);
  /* 浮于场景之上，允许一层柔和阴影 */
  box-shadow: 0 12px 40px rgba(6, 18, 34, 0.22);
}

/* 品牌块居中，与居中的标题、表单形成同一条轴 */
.auth-card__brand {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  margin-bottom: 22px;
}

/* 产品图标：ico 原图 32px，按 1:1 显示最清晰；1px 描边避免深色卡片上糊成一块 */
.auth-card__mark {
  width: 32px;
  height: 32px;
  border-radius: var(--mc-radius-sm);
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
  margin: 0;
  font-size: 26px;
  letter-spacing: -0.02em;
  text-align: center;
}

/* 表单与标题之间留白；字段间距用 flex gap 统一控制
 * （TDesign 的 FormItem margin 在此布局下不生效，会导致字段贴死） */
.auth-card__form {
  display: flex;
  flex-direction: column;
  gap: 18px;
  margin-top: 24px;
  animation: auth-form-in var(--mc-dur-base) var(--mc-ease-out);
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

/* 验证码行：输入框占满剩余宽度，按钮固定不换行 */
.auth-card__code-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.auth-card__code-row > :first-child {
  flex: 1;
  min-width: 0;
}

.auth-card__code-row > :last-child {
  flex-shrink: 0;
  white-space: nowrap;
}

/* 协议同意行：位于密码之后、提交按钮之前 */
.auth-card__agree {
  margin: 0;
  font-size: 13px;
  line-height: 1.6;
}

.auth-card__agree-text {
  margin-left: 4px;
  color: var(--mc-text-faint);
}

.auth-card__agree-text a {
  color: var(--mc-link);
}

.auth-card__links {
  margin-top: 20px;
  padding-top: 16px;
  border-top: 1px solid var(--mc-hairline);
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 16px;
  font-size: 14px;
  color: var(--td-text-color-secondary);
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

/* ===== 窄屏：卡片贴边，弱化阴影 ===== */
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
    font-size: var(--mc-fs-heading);
  }
}
</style>
