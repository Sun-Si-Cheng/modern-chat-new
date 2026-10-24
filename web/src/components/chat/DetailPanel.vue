<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { Dialog as TDialog, DialogPlugin, Input as TInput, MessagePlugin, Select as TSelect } from 'tdesign-vue-next'

import AvatarBadge from './AvatarBadge.vue'
import PluginPanel from '@/components/declarative/PluginPanel.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import { convApi } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { useConversationsStore } from '@/stores/conversations'
import { useFriendsStore } from '@/stores/friends'
import { useWsStore } from '@/stores/ws'
import type { ConversationMember } from '@/types/proto'

const auth = useAuthStore()
const convStore = useConversationsStore()
const friendsStore = useFriendsStore()
const ws = useWsStore()
const { t, te } = useAppI18n()

const conv = computed(() => convStore.currentConv)
const isGroup = computed(() => conv.value?.type === 2)
const isOwner = computed(() => conv.value?.ownerId === auth.user?.id)

// ===== 单聊：加好友 =====

/** 本次会话中已成功发出的申请（接口不返回「我发出的待处理申请」） */
const sentRequestIds = reactive(new Set<number>())
const addingFriend = ref(false)

const peerId = computed(() => conv.value?.peer?.id ?? 0)

const isPeerFriend = computed(
  () => peerId.value > 0 && friendsStore.accepted.some((f) => f.friend.id === peerId.value),
)

const incomingRequest = computed(() =>
  peerId.value > 0 ? friendsStore.requests.find((r) => r.from.id === peerId.value) : undefined,
)

const peerRequestSent = computed(() => sentRequestIds.has(peerId.value))

async function addPeerAsFriend() {
  const peer = conv.value?.peer
  if (!peer || addingFriend.value) return
  addingFriend.value = true
  try {
    await friendsStore.apply(peer.username)
    sentRequestIds.add(peer.id)
    void MessagePlugin.success(t('friends.applyOk'))
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    addingFriend.value = false
  }
}

async function acceptPeerRequest() {
  const req = incomingRequest.value
  if (!req) return
  try {
    await friendsStore.handle(req.id, true)
    void MessagePlugin.success(t('friends.acceptOk'))
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
}

const members = ref<ConversationMember[]>([])

// 群成员在任意端改头像/昵称后，成员列表就地更新
const offUserChange = ws.onFrame((frame) => {
  if (frame.type !== 'user_change') return
  for (const m of members.value) {
    if (m.userId !== frame.userId) continue
    m.user = {
      ...m.user,
      nickname: frame.nickname,
      avatar: frame.avatar,
      avatarColor: frame.avatarColor,
    }
  }
})

onBeforeUnmount(offUserChange)

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
      <div v-if="isGroup" :key="`group-${conv.id}`" class="mc-detail__section">
        <div class="mc-detail__group">
          <AvatarBadge :name="conv.name ?? '#'" color="#2ba471" :size="40" />
          <div class="mc-detail__group-meta">
            <div class="mc-detail__group-name">{{ conv.name }}</div>
            <div class="text-placeholder">{{ t('chat.membersCount', { n: conv.memberCount }) }}</div>
          </div>
        </div>

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
            :src="m.user.avatar"
            :color="m.user.avatarColor"
            :size="30"
            :online="ws.onlineUsers.has(m.userId)"
          />
          <span class="mc-detail__member-name">
            {{ m.user.nickname }}
            <span v-if="m.role === 'owner'" class="mc-detail__role">{{ t('group.owner') }}</span>
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
      <div v-else :key="`peer-${conv.id}`" class="mc-detail__section">
        <div class="mc-detail__title">{{ t('group.peerInfo') }}</div>
        <div class="mc-detail__peer">
          <AvatarBadge
            :name="conv.peer?.nickname ?? '?'"
            :src="conv.peer?.avatar"
            :color="conv.peer?.avatarColor"
            :size="48"
            :online="conv.peer ? ws.onlineUsers.has(conv.peer.id) : false"
          />
          <div>
            <div class="mc-detail__peer-name">{{ conv.peer?.nickname }}</div>
            <div class="text-placeholder mc-detail__peer-meta">
              @{{ conv.peer?.username }} ·
              {{ conv.peer && ws.onlineUsers.has(conv.peer.id) ? t('chat.online') : t('chat.offline') }}
            </div>
          </div>
        </div>

        <div class="mc-detail__peer-actions">
          <span v-if="isPeerFriend" class="mc-detail__tag">{{ t('friends.alreadyFriend') }}</span>
          <TButton v-else-if="incomingRequest" block size="small" theme="primary" @click="acceptPeerRequest">
            {{ t('friends.accept') }}
          </TButton>
          <span v-else-if="peerRequestSent" class="mc-detail__tag">{{ t('friends.sent') }}</span>
          <TButton
            v-else
            block
            size="small"
            theme="primary"
            :loading="addingFriend"
            @click="addPeerAsFriend"
          >
            {{ t('friends.addAction') }}
          </TButton>
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
  padding: 16px 14px;
  border-bottom: 1px solid var(--mc-hairline);
  animation: mc-fade-up var(--mc-dur-base) var(--mc-ease-out);
}

/* 群身份：头像 + 群名 + 人数，先回答“这是哪个群” */
.mc-detail__group {
  display: flex;
  align-items: center;
  gap: 10px;
  padding-bottom: 12px;
  margin-bottom: 12px;
  border-bottom: 1px solid var(--mc-hairline);
}

.mc-detail__group-meta {
  min-width: 0;
}

.mc-detail__group-name {
  font-size: 14px;
  font-weight: 600;
  line-height: 1.35;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mc-detail__group-meta .text-placeholder {
  font-size: 12px;
}

.mc-detail__title {
  font-weight: 600;
  font-size: 13px;
  color: var(--td-text-color-secondary);
  margin-bottom: 8px;
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
  padding: 5px 6px;
  margin: 0 -6px;
  border-radius: 6px;
  transition: background 0.12s ease-out;
}

.mc-detail__member:hover {
  background: var(--td-bg-color-secondarycontainer);
}

.mc-detail__member-name {
  flex: 1;
  min-width: 0;
  font-size: 13px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mc-detail__role {
  margin-left: 4px;
  padding: 1px 5px;
  border-radius: 4px;
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-text-color-secondary);
  font-size: 10px;
  font-weight: 500;
}

.mc-detail__member-ops a {
  font-size: 12px;
  color: var(--td-text-color-secondary);
  cursor: pointer;
}

.mc-detail__member-ops a:hover {
  color: var(--td-error-color);
}

/* 单聊资料：更大的头像承担身份识别 */
.mc-detail__peer {
  display: flex;
  align-items: center;
  gap: 12px;
}

.mc-detail__peer-name {
  font-size: 14px;
  font-weight: 600;
  line-height: 1.4;
}

.mc-detail__peer-meta {
  margin-top: 2px;
  font-size: 12px;
  line-height: 1.4;
}

/* 单聊加好友：已是好友/已申请用状态标签，其余情况给一个可执行按钮 */
.mc-detail__peer-actions {
  display: flex;
  align-items: center;
  margin-top: 12px;
  animation: mc-fade-up var(--mc-dur-slow) var(--mc-ease-out);
}

.mc-detail__tag {
  padding: 3px 9px;
  border: 1px solid var(--mc-hairline);
  border-radius: 999px;
  font-size: 12px;
  color: var(--td-text-color-placeholder);
}

.mc-detail__footer {
  margin-top: 12px;
}
</style>
