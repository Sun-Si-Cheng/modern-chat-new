<script setup lang="ts">
/**
 * 扫码登录确认页（手机端）：桌面二维码内容指向本页 /qr/<token>。
 *
 * - 本机已登录：展示确认卡片，确认后调 qr/confirm 授权桌面登录；
 * - 本机未登录：提示先登录再回来（redirect 记住本页，登录后原路返回）；
 * - 票据无效/过期（400）：提示到桌面端重新生成。
 */
import UiButton from '@/components/ui/UiButton.vue'
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'

import { authApi } from '@/core/network/endpoints'
import { useAppI18n } from '@/composables/useAppI18n'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { t } = useAppI18n()

const token = typeof route.params.token === 'string' ? route.params.token : ''
/** 本机是否已有登录态（扫码确认的前提） */
const logged = computed(() => !!auth.token)

const confirming = ref(false)
const ok = ref(false)
const failed = ref(false)

async function confirm() {
  if (!token) {
    failed.value = true
    return
  }
  confirming.value = true
  try {
    await authApi.qrConfirm(token)
    ok.value = true
    void MessagePlugin.success(t('auth.qrPhoneOk'))
  } catch {
    failed.value = true
  } finally {
    confirming.value = false
  }
}

/** 未登录时先去登录，登录后回本页继续确认 */
function goLogin() {
  void router.replace({ name: 'login', query: { redirect: route.fullPath } })
}
</script>

<template>
  <div class="mc-qr">
    <div class="mc-qr__card">
      <h1 class="mc-qr__title">{{ t('auth.qrPhoneTitle') }}</h1>

      <template v-if="ok">
        <p>{{ t('auth.qrPhoneOk') }}</p>
      </template>
      <template v-else-if="failed || !token">
        <p class="mc-qr__error">{{ t('auth.qrPhoneFailed') }}</p>
        <UiButton v-if="logged" theme="primary" @click="confirm">{{ t('auth.qrPhoneRetry') }}</UiButton>
        <UiButton v-else theme="primary" @click="goLogin">{{ t('auth.qrPhoneGoLogin') }}</UiButton>
      </template>
      <template v-else-if="logged">
        <p>{{ t('auth.qrPhoneDesc', { name: auth.user?.nickname ?? auth.user?.username ?? '' }) }}</p>
        <UiButton theme="primary" size="large" :loading="confirming" @click="confirm">
          {{ t('auth.qrPhoneBtn') }}
        </UiButton>
      </template>
      <template v-else>
        <p class="text-placeholder">{{ t('auth.qrPhoneNeedLogin') }}</p>
        <UiButton theme="primary" @click="goLogin">{{ t('auth.qrPhoneGoLogin') }}</UiButton>
      </template>
    </div>
  </div>
</template>

<style scoped>
.mc-qr {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}
.mc-qr__card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 16px;
  max-width: 360px;
  padding: 32px 40px;
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-lg);
  background: var(--td-bg-color-container);
  color: var(--td-text-color-primary);
  text-align: center;
}
.mc-qr__title {
  font-size: var(--mc-fs-title);
  font-weight: 600;
}
.mc-qr__error {
  color: var(--td-error-color);
}
</style>
