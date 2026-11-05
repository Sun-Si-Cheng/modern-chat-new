<script setup lang="ts">
import UiButton from '@/components/ui/UiButton.vue'
import AvatarBadge from '@/components/chat/AvatarBadge.vue'
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'

import { inviteApi, type InviteInfo } from '@/api'
import { useAppI18n } from '@/composables/useAppI18n'
import { useConversationsStore } from '@/stores/conversations'

const route = useRoute()
const router = useRouter()
const convStore = useConversationsStore()
const { t, te } = useAppI18n()

const token = computed(() => String(route.params.token ?? ''))
const info = ref<InviteInfo | null>(null)
const loading = ref(true)
const joining = ref(false)
const error = ref('')

async function loadInfo() {
  const tk = token.value
  loading.value = true
  error.value = ''
  info.value = null
  try {
    const r = await inviteApi.info(tk)
    // 同一路由复用（/join/A → /join/B）时旧请求可能后到：只接受与当前 token 匹配的结果
    if (token.value !== tk) return
    info.value = r
  } catch (e) {
    if (token.value === tk) error.value = te(e)
  } finally {
    if (token.value === tk) loading.value = false
  }
}

// 路由参数变化时重新拉取：只在 onMounted 拉一次的话，
// 从一条邀请链接跳到另一条会继续显示上一个群的名称与人数
watch(token, () => void loadInfo(), { immediate: true })

async function join() {
  if (joining.value) return
  joining.value = true
  try {
    const r = await inviteApi.join(token.value)
    await convStore.loadConversations()
    await convStore.openConversation(r.convId)
    void MessagePlugin.success(r.alreadyMember ? t('join.alreadyMember') : t('join.ok'))
    await router.push('/chat')
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    joining.value = false
  }
}

function goChat() {
  void router.push('/chat')
}
</script>

<template>
  <div class="mc-join">
    <div class="mc-join__card">
      <h1 class="mc-join__title">{{ t('join.title') }}</h1>

      <div v-if="loading" class="text-placeholder">{{ t('common.loading') }}</div>

      <div v-else-if="info" class="mc-join__group">
        <AvatarBadge :name="info.name ?? '#'" :src="info.avatar" color="#2ba471" :size="56" />
        <div class="mc-join__meta">
          <div class="mc-join__name">{{ info.name || t('join.untitled') }}</div>
          <div class="text-placeholder">
            {{ t('join.members', { n: info.memberCount }) }}
          </div>
        </div>
      </div>

      <p v-if="error" class="mc-join__error">{{ error }}</p>

      <div class="mc-join__actions">
        <UiButton v-if="info" theme="primary" block :loading="joining" @click="join">
          {{ t('join.action') }}
        </UiButton>
        <UiButton variant="text" block @click="goChat">{{ t('join.goChat') }}</UiButton>
      </div>
    </div>
  </div>
</template>

<style scoped>
.mc-join {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: var(--td-bg-color-page);
}

.mc-join__card {
  width: 360px;
  max-width: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 18px;
  padding: 28px 24px;
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-lg);
  background: var(--td-bg-color-container);
  box-shadow: var(--mc-elev-2);
}

.mc-join__title {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
}

.mc-join__group {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px;
  border-radius: var(--mc-radius-md);
  background: var(--td-bg-color-secondarycontainer);
}

.mc-join__meta {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.mc-join__name {
  font-size: 15px;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mc-join__error {
  margin: 0;
  font-size: 13px;
  color: var(--td-error-color);
  text-align: center;
}

.mc-join__actions {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
</style>
