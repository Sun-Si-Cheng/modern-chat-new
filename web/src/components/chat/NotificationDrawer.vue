<script setup lang="ts">
import UiButton from '@/components/ui/UiButton.vue'
import UiDrawer from '@/components/ui/UiDrawer.vue'
import AvatarBadge from './AvatarBadge.vue'
import { computed, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'

import { useAppI18n } from '@/composables/useAppI18n'
import { useConversationsStore } from '@/stores/conversations'
import { useFriendsStore } from '@/stores/friends'
import type { Conversation } from '@/types/proto'

/** 通知中心：聚合「有人@我」的会话与好友申请，避免用户逐个会话翻找 */
const visible = defineModel<boolean>({ default: false })
const convStore = useConversationsStore()
const friendsStore = useFriendsStore()
const { t, te } = useAppI18n()

const loading = ref(false)
const busyKey = ref('')

const mentions = computed(() => convStore.conversations.filter((c) => c.type !== 3 && c.mentionUnread))
const requestCount = computed(() => friendsStore.requestCount)
const total = computed(() => mentions.value.length + requestCount.value)

watch(
  visible,
  async (v) => {
    if (!v) return
    loading.value = true
    try {
      await Promise.all([convStore.loadConversations(), friendsStore.loadRequests()])
    } catch (e) {
      void MessagePlugin.error(te(e))
    } finally {
      loading.value = false
    }
  },
  { immediate: true },
)

function convName(c: Conversation): string {
  return c.type === 1 ? (c.peer?.nickname ?? '') : (c.name ?? '')
}

function preview(c: Conversation): string {
  return c.lastMessagePreview || t('chat.noMessage')
}

async function openMention(c: Conversation) {
  visible.value = false
  // 打开会话后 markRead 会清掉 mentionUnread（本地与服务端）
  await convStore.openConversation(c.id)
}

async function handleRequest(id: number, accept: boolean) {
  if (busyKey.value) return
  busyKey.value = `req-${id}`
  try {
    await friendsStore.handle(id, accept)
    void MessagePlugin.success(accept ? t('friends.acceptOk') : t('friends.rejectOk'))
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    busyKey.value = ''
  }
}
</script>

<template>
  <UiDrawer v-model:visible="visible" :header="`${t('notify.title')}（${total}）`" size="420px">
    <div class="mc-notify">
      <div v-if="loading" class="text-placeholder mc-notify__empty">{{ t('common.loading') }}</div>

      <template v-else>
        <!-- 好友申请 -->
        <section v-if="requestCount" class="mc-notify__section">
          <div class="mc-notify__section-title">{{ t('notify.friendRequests') }}</div>
          <div v-for="r in friendsStore.pendingRequests" :key="r.id" class="mc-notify__item">
            <AvatarBadge :name="r.from.nickname" :src="r.from.avatar" :color="r.from.avatarColor" :size="30" />
            <div class="mc-notify__body">
              <div class="mc-notify__text">{{ t('notify.friendRequestBody', { name: r.from.nickname }) }}</div>
              <div class="text-placeholder mc-notify__sub">@{{ r.from.username }}</div>
            </div>
            <div class="mc-notify__ops">
              <UiButton
                size="small"
                theme="primary"
                :loading="busyKey === `req-${r.id}`"
                @click="handleRequest(r.id, true)"
              >
                {{ t('friends.accept') }}
              </UiButton>
              <UiButton
                size="small"
                variant="text"
                :disabled="!!busyKey"
                @click="handleRequest(r.id, false)"
              >
                {{ t('friends.reject') }}
              </UiButton>
            </div>
          </div>
        </section>

        <!-- 有人@我 -->
        <section v-if="mentions.length" class="mc-notify__section">
          <div class="mc-notify__section-title">{{ t('notify.mentions') }}</div>
          <button
            v-for="c in mentions"
            :key="c.id"
            type="button"
            class="mc-notify__item mc-notify__item--btn"
            @click="openMention(c)"
          >
            <AvatarBadge
              :name="convName(c) || '#'"
              :src="c.type === 1 ? c.peer?.avatar : c.avatar"
              :color="c.type === 1 ? (c.peer?.avatarColor ?? '#0052d9') : '#2ba471'"
              :size="30"
            />
            <div class="mc-notify__body">
              <div class="mc-notify__text">{{ convName(c) }}</div>
              <div class="mc-notify__sub text-placeholder">{{ preview(c) }}</div>
            </div>
            <span class="mc-notify__at">@</span>
          </button>
        </section>

        <div v-if="!total" class="mc-notify__empty">
          <p class="mc-notify__empty-title">{{ t('notify.emptyTitle') }}</p>
          <p class="text-placeholder">{{ t('notify.empty') }}</p>
        </div>
      </template>
    </div>
  </UiDrawer>
</template>

<style scoped>
.mc-notify {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.mc-notify__section {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.mc-notify__section-title {
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.02em;
  color: var(--mc-text-faint);
}

.mc-notify__item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-md);
  background: var(--td-bg-color-container);
  text-align: left;
}

.mc-notify__item--btn {
  width: 100%;
  font: inherit;
  cursor: pointer;
  transition: background-color var(--mc-dur-fast) var(--mc-ease-out);
}

.mc-notify__item--btn:hover {
  background: var(--td-bg-color-container-hover);
}

.mc-notify__body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.mc-notify__text {
  font-size: 13px;
  color: var(--td-text-color-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mc-notify__sub {
  font-size: 12px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mc-notify__ops {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}

.mc-notify__at {
  flex-shrink: 0;
  width: 20px;
  height: 20px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: var(--td-brand-color-1, rgba(0, 82, 217, 0.12));
  color: var(--td-brand-color);
  font-size: 12px;
  font-weight: 600;
}

.mc-notify__empty {
  padding: 32px 0;
  text-align: center;
}

.mc-notify__empty-title {
  margin: 0 0 6px;
  font-size: 14px;
  font-weight: 600;
}
</style>
