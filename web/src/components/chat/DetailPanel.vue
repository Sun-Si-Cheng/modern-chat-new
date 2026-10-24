<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Dialog as TDialog, DialogPlugin, Input as TInput, MessagePlugin, Select as TSelect } from 'tdesign-vue-next'

import AvatarBadge from './AvatarBadge.vue'
import PluginPanel from '@/components/declarative/PluginPanel.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import { convApi } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { useConversationsStore } from '@/stores/conversations'
import { useWsStore } from '@/stores/ws'
import type { ConversationMember } from '@/types/proto'

const auth = useAuthStore()
const convStore = useConversationsStore()
const ws = useWsStore()
const { t, te } = useAppI18n()

const conv = computed(() => convStore.currentConv)
const isGroup = computed(() => conv.value?.type === 2)
const isOwner = computed(() => conv.value?.ownerId === auth.user?.id)

const members = ref<ConversationMember[]>([])

watch(
  () => conv.value?.id,
  async (id) => {
    members.value = []
    if (id === undefined || !isGroup.value) return
    try {
      members.value = await convApi.members(id)
    } catch {
      /* 忽略 */
    }
  },
  { immediate: true },
)

// 群成员变动（他人入群 / 被踢 / 转让）后刷新成员列表
watch(
  () => conv.value?.memberCount,
  async (count, prev) => {
    if (prev === undefined || count === prev || !conv.value || !isGroup.value) return
    await refreshMembers().catch(() => undefined)
  },
)

// ===== 群管理 =====

const renameVisible = ref(false)
const newName = ref('')
const transferVisible = ref(false)
const transferTarget = ref<number | undefined>(undefined)

const memberOptions = computed(() =>
  members.value
    .filter((m) => m.userId !== auth.user?.id)
    .map((m) => ({ label: m.user.nickname, value: m.userId })),
)

async function doRename() {
  if (!conv.value || !newName.value.trim()) return
  try {
    await convApi.rename(conv.value.id, newName.value.trim())
    await convStore.loadConversations()
    renameVisible.value = false
    void MessagePlugin.success(t('group.renameOk'))
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
}

function kick(userId: number, nickname: string) {
  if (!conv.value) return
  const dialog = DialogPlugin.confirm({
    header: t('group.kickTitle'),
    body: t('group.kickBody', { name: nickname }),
    confirmBtn: { content: t('group.kickBtn'), theme: 'danger' },
    onConfirm: async () => {
      try {
        await convApi.kick(conv.value!.id, userId)
        members.value = members.value.filter((m) => m.userId !== userId)
        await convStore.loadConversations()
        void MessagePlugin.success(t('group.kickOk'))
      } catch (e) {
        void MessagePlugin.error(te(e))
      }
      dialog.destroy()
    },
  })
}

async function doTransfer() {
  if (!conv.value || !transferTarget.value) return
  try {
    await convApi.transfer(conv.value.id, transferTarget.value)
    transferVisible.value = false
    await Promise.all([convStore.loadConversations(), refreshMembers()])
    void MessagePlugin.success(t('group.transferOk'))
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
}

async function refreshMembers() {
  if (!conv.value) return
  members.value = await convApi.members(conv.value.id)
}

async function leave() {
  if (!conv.value) return
  const dialog = DialogPlugin.confirm({
    header: t('group.leaveTitle'),
    body: isOwner.value ? t('group.leaveOwnerBody') : t('group.leaveBody'),
    confirmBtn: {
      content: isOwner.value ? t('group.gotIt') : t('group.leaveBtn'),
      theme: isOwner.value ? 'default' : 'danger',
    },
    onConfirm: async () => {
      if (isOwner.value) {
        dialog.destroy()
        return
      }
      try {
        await convApi.leave(conv.value!.id)
        await convStore.loadConversations()
        convStore.currentConvId = convStore.conversations[0]?.id ?? null
        void MessagePlugin.success(t('group.leaveOk'))
      } catch (e) {
        void MessagePlugin.error(te(e))
      }
      dialog.destroy()
    },
  })
}
</script>

<template>
  <div class="mc-detail">
    <!-- 群信息 / 群管理 -->
    <template v-if="conv">
      <div v-if="isGroup" class="mc-detail__section">
        <div class="mc-detail__title">
          {{ t('group.info') }}
          <span v-if="isOwner" class="mc-detail__actions">
            <a @click="newName = conv.name ?? ''; renameVisible = true">{{ t('group.rename') }}</a>
            <a @click="transferVisible = true">{{ t('group.transfer') }}</a>
          </span>
        </div>

        <div class="mc-detail__member" v-for="m in members" :key="m.userId">
          <AvatarBadge
            :name="m.user.nickname"
            :color="m.user.avatarColor"
            :size="30"
            :online="ws.onlineUsers.has(m.userId)"
          />
          <span class="mc-detail__member-name">
            {{ m.user.nickname }}
            <span v-if="m.role === 'owner'" class="text-placeholder">{{ t('group.owner') }}</span>
            <span v-if="m.userId === auth.user?.id" class="text-placeholder">{{ t('group.me') }}</span>
          </span>
          <span v-if="isOwner && m.userId !== auth.user?.id" class="mc-detail__member-ops">
            <a @click="kick(m.userId, m.user.nickname)">{{ t('group.kick') }}</a>
          </span>
        </div>

        <div class="mc-detail__footer">
          <TButton variant="outline" theme="danger" size="small" block @click="leave">
            {{ isOwner ? t('group.leaveOwner') : t('group.leave') }}
          </TButton>
        </div>
      </div>

      <!-- 单聊资料 -->
      <div v-else class="mc-detail__section">
        <div class="mc-detail__title">{{ t('group.peerInfo') }}</div>
        <div class="mc-detail__member">
          <AvatarBadge
            :name="conv.peer?.nickname ?? '?'"
            :color="conv.peer?.avatarColor"
            :size="40"
            :online="conv.peer ? ws.onlineUsers.has(conv.peer.id) : false"
          />
          <div>
            <div>{{ conv.peer?.nickname }}</div>
            <div class="text-placeholder">@{{ conv.peer?.username }}</div>
          </div>
        </div>
      </div>
    </template>

    <!-- 插件侧边栏 -->
    <PluginPanel />
  </div>

  <!-- 改群名 -->
  <TDialog v-model:visible="renameVisible" :header="t('group.renameTitle')" @confirm="doRename">
    <TInput v-model="newName" :placeholder="t('group.newName')" clearable @enter="doRename" />
  </TDialog>

  <!-- 转让群主 -->
  <TDialog v-model:visible="transferVisible" :header="t('group.transferTitle')" @confirm="doTransfer">
    <TSelect v-model="transferTarget" :options="memberOptions" :placeholder="t('group.transferPlaceholder')" clearable />
  </TDialog>
</template>

<style scoped>
.mc-detail {
  width: 280px;
  flex-shrink: 0;
  border-left: 1px solid var(--td-component-border);
  background: var(--td-bg-color-container);
  overflow-y: auto;
  display: flex;
  flex-direction: column;
}

.mc-detail__section {
  padding: 14px;
  border-bottom: 1px solid var(--td-component-border);
}

.mc-detail__title {
  font-weight: 600;
  font-size: 15px;
  margin-bottom: 10px;
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.mc-detail__actions {
  display: flex;
  gap: 8px;
  font-weight: 400;
  font-size: 12px;
}

.mc-detail__actions a {
  cursor: pointer;
}

.mc-detail__member {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 5px 0;
}

.mc-detail__member-name {
  flex: 1;
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mc-detail__member-ops a {
  font-size: 12px;
  cursor: pointer;
}

.mc-detail__footer {
  margin-top: 12px;
}
</style>
