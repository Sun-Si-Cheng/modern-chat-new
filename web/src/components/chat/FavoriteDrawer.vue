<script setup lang="ts">
import UiButton from '@/components/ui/UiButton.vue'
import UiDrawer from '@/components/ui/UiDrawer.vue'
import AvatarBadge from './AvatarBadge.vue'
import { computed, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'

import { useAppI18n } from '@/composables/useAppI18n'
import { useConversationsStore } from '@/stores/conversations'
import { useFavoritesStore } from '@/stores/favorites'
import type { FavoriteItem } from '@/types/proto'

const visible = defineModel<boolean>({ default: false })
const favorites = useFavoritesStore()
const convStore = useConversationsStore()
const { t, te } = useAppI18n()

const loading = ref(false)
const busyId = ref<number | null>(null)

watch(
  visible,
  async (v) => {
    if (!v) return
    loading.value = true
    try {
      await favorites.load(true)
    } catch (e) {
      void MessagePlugin.error(te(e))
    } finally {
      loading.value = false
    }
  },
  { immediate: true },
)

/** 会话名：服务端已按群名 / 单聊对方昵称填充，极端情况下回退到本地会话列表 */
function convLabel(item: FavoriteItem): string {
  if (item.convName) return item.convName
  const c = convStore.conversations.find((x) => x.id === item.conversationId)
  if (!c) return t('favorites.unknownConv')
  return c.type === 1 ? (c.peer?.nickname ?? t('favorites.unknownConv')) : (c.name ?? t('favorites.unknownConv'))
}

function previewText(item: FavoriteItem): string {
  if (item.isRecalled) return t('message.recalledPreview')
  if (item.msgType === 'image') return t('chat.previewImage')
  if (item.msgType === 'audio') return t('chat.previewVoice')
  return item.content
}

function fmtTime(ts: string): string {
  const d = new Date(ts)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}/${d.getMonth() + 1}/${d.getDate()} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

const count = computed(() => favorites.items.length)

async function openItem(item: FavoriteItem) {
  visible.value = false
  if (!convStore.conversations.some((c) => c.id === item.conversationId)) {
    await convStore.loadConversations().catch(() => undefined)
  }
  await convStore.openConversation(item.conversationId)
}

async function unfavorite(item: FavoriteItem) {
  if (busyId.value) return
  busyId.value = item.messageId
  try {
    await favorites.remove(item.messageId)
    void MessagePlugin.success(t('favorites.removed'))
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    busyId.value = null
  }
}
</script>

<template>
  <UiDrawer v-model:visible="visible" :header="`${t('favorites.title')}（${count}）`" size="440px">
    <div class="mc-fav">
      <div v-if="loading" class="text-placeholder mc-fav__empty">{{ t('common.loading') }}</div>
      <template v-else>
        <div v-for="item in favorites.items" :key="item.messageId" class="mc-fav__item">
          <div class="mc-fav__head">
            <AvatarBadge :name="item.senderNickname || '?'" color="#7b8290" :size="28" />
            <span class="mc-fav__sender">{{ item.senderNickname }}</span>
            <span class="mc-fav__conv text-placeholder">{{ convLabel(item) }}</span>
            <span class="mc-fav__time text-placeholder num">{{ fmtTime(item.createdAt) }}</span>
          </div>

          <div class="mc-fav__content">
            <template v-if="!item.isRecalled && item.msgType === 'image'">
              <img class="mc-fav__image" :src="item.content" alt="" decoding="async" />
            </template>
            <p v-else class="mc-fav__text" :class="{ 'text-placeholder': item.isRecalled }">
              {{ previewText(item) }}
            </p>
          </div>

          <div class="mc-fav__ops">
            <UiButton size="small" variant="text" @click="openItem(item)">
              {{ t('favorites.open') }}
            </UiButton>
            <UiButton
              size="small"
              variant="text"
              theme="danger"
              :loading="busyId === item.messageId"
              :disabled="busyId !== null && busyId !== item.messageId"
              @click="unfavorite(item)"
            >
              {{ t('favorites.remove') }}
            </UiButton>
          </div>
        </div>

        <div v-if="!favorites.items.length" class="mc-fav__empty">
          <p class="text-placeholder">{{ t('favorites.empty') }}</p>
        </div>
      </template>
    </div>
  </UiDrawer>
</template>

<style scoped>
.mc-fav {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.mc-fav__item {
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-md);
  padding: 12px 14px;
  background: var(--td-bg-color-container);
  transition:
    border-color var(--mc-dur-fast) var(--mc-ease-out),
    box-shadow var(--mc-dur-fast) var(--mc-ease-out);
}

.mc-fav__item:hover {
  border-color: var(--td-component-border);
  box-shadow: var(--mc-shadow-1);
}

.mc-fav__head {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.mc-fav__sender {
  font-size: 13px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 40%;
}

.mc-fav__conv {
  flex: 1;
  min-width: 0;
  font-size: 12px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mc-fav__time {
  font-size: 11px;
  flex-shrink: 0;
}

.mc-fav__content {
  margin: 8px 0 6px;
}

.mc-fav__text {
  margin: 0;
  font-size: 13px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
}

.mc-fav__image {
  display: block;
  max-width: 100%;
  max-height: 180px;
  border-radius: var(--mc-radius-sm);
  object-fit: cover;
}

.mc-fav__ops {
  display: flex;
  justify-content: flex-end;
  gap: 4px;
}

.mc-fav__empty {
  text-align: center;
  padding: 48px 0;
  font-size: 13px;
}
</style>
