<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Dialog as TDialog, DialogPlugin, Input as TInput, MessagePlugin, Select as TSelect } from 'tdesign-vue-next'

import AvatarBadge from './AvatarBadge.vue'
import PluginPanel from '@/components/declarative/PluginPanel.vue'
import { convApi } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { useConversationsStore } from '@/stores/conversations'
import { useWsStore } from '@/stores/ws'
import type { ConversationMember } from '@/types/proto'

const auth = useAuthStore()
const convStore = useConversationsStore()
const ws = useWsStore()

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
    void MessagePlugin.success('群名已更新')
  } catch (e) {
    void MessagePlugin.error((e as Error).message)
  }
}

function kick(userId: number, nickname: string) {
  if (!conv.value) return
  const dialog = DialogPlugin.confirm({
    header: '移出群聊',
    body: `确定将「${nickname}」移出群聊吗？`,
    confirmBtn: { content: '移出', theme: 'danger' },
    onConfirm: async () => {
      try {
        await convApi.kick(conv.value!.id, userId)
        members.value = members.value.filter((m) => m.userId !== userId)
        await convStore.loadConversations()
        void MessagePlugin.success('已移出')
      } catch (e) {
        void MessagePlugin.error((e as Error).message)
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
    void MessagePlugin.success('群主已转让')
  } catch (e) {
    void MessagePlugin.error((e as Error).message)
  }
}

async function refreshMembers() {
  if (!conv.value) return
  members.value = await convApi.members(conv.value.id)
}

async function leave() {
  if (!conv.value) return
  const dialog = DialogPlugin.confirm({
    header: '退出群聊',
    body: isOwner.value ? '群主不能退群，请先转让群主。' : '确定退出该群聊吗？',
    confirmBtn: { content: isOwner.value ? '知道了' : '退出', theme: isOwner.value ? 'default' : 'danger' },
    onConfirm: async () => {
      if (isOwner.value) {
        dialog.destroy()
        return
      }
      try {
        await convApi.leave(conv.value!.id)
        await convStore.loadConversations()
        convStore.currentConvId = convStore.conversations[0]?.id ?? null
        void MessagePlugin.success('已退出群聊')
      } catch (e) {
        void MessagePlugin.error((e as Error).message)
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
          群聊信息
          <span v-if="isOwner" class="mc-detail__actions">
            <a @click="newName = conv.name ?? ''; renameVisible = true">改群名</a>
            <a @click="transferVisible = true">转让群主</a>
          </span>
        </div>

        <div class="mc-detail__member" v-for="m in members" :key="m.userId">
          <AvatarBadge :name="m.user.nickname" :color="m.user.avatarColor" :size="30" :online="ws.onlineUsers.has(m.userId)" />
          <span class="mc-detail__member-name">
            {{ m.user.nickname }}
            <span v-if="m.role === 'owner'" class="text-placeholder">（群主）</span>
            <span v-if="m.userId === auth.user?.id" class="text-placeholder">（我）</span>
          </span>
          <span v-if="isOwner && m.userId !== auth.user?.id" class="mc-detail__member-ops">
            <a @click="kick(m.userId, m.user.nickname)">移出</a>
          </span>
        </div>

        <div class="mc-detail__footer">
          <TButton variant="outline" theme="danger" size="small" block @click="leave">
            {{ isOwner ? '退群（需先转让）' : '退出群聊' }}
          </TButton>
        </div>
      </div>

      <!-- 单聊资料 -->
      <div v-else class="mc-detail__section">
        <div class="mc-detail__title">对方资料</div>
        <div class="mc-detail__member">
          <AvatarBadge :name="conv.peer?.nickname ?? '?'" :color="conv.peer?.avatarColor" :size="40" :online="conv.peer ? ws.onlineUsers.has(conv.peer.id) : false" />
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
  <TDialog v-model:visible="renameVisible" header="修改群名" @confirm="doRename">
    <TInput v-model="newName" placeholder="新群名" clearable @enter="doRename" />
  </TDialog>

  <!-- 转让群主 -->
  <TDialog v-model:visible="transferVisible" header="转让群主" @confirm="doTransfer">
    <TSelect v-model="transferTarget" :options="memberOptions" placeholder="选择新群主" clearable />
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
