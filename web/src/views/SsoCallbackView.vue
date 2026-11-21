<script setup lang="ts">
/**
 * 统一登录回跳页（/sso/callback，站内 SPA 路由）。
 *
 * 聚合登录中心授权完成后，后端 /api/auth/sso/callback 换到一次性票据，
 * 302 回本页并带 ?ticket=..&redirect=..。这里用票据换本站 JWT：
 *   - 成功：applyAuth 后跳 redirect（站内相对路径，后端已校验防开放重定向）；
 *   - 中心返回 error=..（用户拒绝授权）：提示后回登录页；
 *   - 票据缺失 / 无效 / 已用过：提示后回登录页重新发起。
 * 无论成败都 replace 清掉地址栏里的票据，防止刷新/复制链接重复消费。
 */
import UiButton from '@/components/ui/UiButton.vue'
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'

import { authApi } from '@/api'
import { useAppI18n } from '@/composables/useAppI18n'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const { t } = useAppI18n()

/** 正在用一次性票据换本站登录态 */
const exchanging = ref(true)
/** 票据失效 / 授权被拒：提示后回登录页 */
const failed = ref(false)

onMounted(async () => {
  const error = typeof route.query.error === 'string' ? route.query.error : ''
  const ticket = typeof route.query.ticket === 'string' ? route.query.ticket : ''
  // 后端 ssoSafeNext 只放行站内相对路径；空值按默认落地页处理
  const redirect =
    typeof route.query.redirect === 'string' && route.query.redirect.startsWith('/')
      ? route.query.redirect
      : '/chat'
  if (!ticket || error) {
    failed.value = true
    exchanging.value = false
    return
  }
  try {
    const payload = await authApi.ssoExchange({ ticket })
    auth.applyAuth(payload)
    if (!payload.user) await auth.fetchMe()
    void MessagePlugin.info({ content: t('auth.ssoLoginOk'), duration: 8_000 })
    await router.replace(redirect)
  } catch {
    failed.value = true
    await router.replace({ name: 'login', query: { redirect } })
  } finally {
    exchanging.value = false
  }
})
</script>

<template>
  <div class="mc-sso">
    <div v-if="exchanging" class="mc-sso__card">
      <p>{{ t('auth.ssoExchanging') }}</p>
    </div>
    <div v-else class="mc-sso__card is-error">
      <p>{{ failed ? t('auth.ssoFailed') : t('auth.ssoFailed') }}</p>
      <UiButton theme="primary" @click="router.replace({ name: 'login' })">
        {{ t('auth.ssoBackLogin') }}
      </UiButton>
    </div>
  </div>
</template>

<style scoped>
.mc-sso {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}
.mc-sso__card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 16px;
  padding: 32px 40px;
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-lg);
  background: var(--td-bg-color-container);
  color: var(--td-text-color-primary);
}
.mc-sso__card.is-error p {
  color: var(--td-error-color);
}
</style>
