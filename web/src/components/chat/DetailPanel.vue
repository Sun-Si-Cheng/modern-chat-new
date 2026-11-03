<script setup lang="ts">
import UiDialog from '@/components/ui/UiDialog.vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiInput from '@/components/ui/UiInput.vue'
import { computed, ref, watch } from 'vue'
import { DialogPlugin, MessagePlugin, Select as TSelect } from 'tdesign-vue-next'

import AvatarBadge from './AvatarBadge.vue'
import ContextMenu, { type ContextMenuItem } from '@/components/common/ContextMenu.vue'
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

// 弹窗的确认按钮可重复点击（改名/转让都是重操作），用 busy 挡住第二次提交并给出加载反馈
const renaming = ref(false)
const transferring = ref(false)

async function doRename() {
  if (!conv.value || !newName.value.trim() || renaming.value) return
  renaming.value = true
  try {
    await convApi.rename(conv.value.id, newName.value.trim())
    await convStore.loadConversations()
    renameVisible.value = false
    void MessagePlugin.success(t('group.renameOk'))
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    renaming.value = false
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
  if (!conv.value || !transferTarget.value || transferring.value) return
  transferring.value = true
  try {
    await convApi.transfer(conv.value.id, transferTarget.value)
    transferVisible.value = false
    await Promise.all([convStore.loadConversations(), refreshMembers()])
    void MessagePlugin.success(t('group.transferOk'))
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    transferring.value = false
  }
}

// ===== 成员右键菜单 =====

const memberMenuVisible = ref(false)
const memberMenuX = ref(0)
const memberMenuY = ref(0)
const memberMenuTarget = ref<ConversationMember | null>(null)

function openMemberMenu(e: MouseEvent, m: ConversationMember) {
  memberMenuTarget.value = m
  memberMenuX.value = e.clientX
  memberMenuY.value = e.clientY
  memberMenuVisible.value = true
}

const memberMenuItems = computed<ContextMenuItem[]>(() => {
  const m = memberMenuTarget.value
  if (!m) return []
  const mine = m.userId === auth.user?.id
  const items: ContextMenuItem[] = [{ key: 'copyName', label: t('chat.memberCopyName') }]
  // 群主可以管理其他成员；自己的行不给踢出/转让入口
  if (isOwner.value && !mine) {
    items.push({ key: 'transfer', label: t('group.transfer'), dividerBefore: true })
    items.push({ key: 'kick', label: t('group.kick'), danger: true })
  }
  return items
})

function onMemberMenuSelect(key: string) {
  const m = memberMenuTarget.value
  if (!m) return
  if (key === 'copyName') {
    void navigator.clipboard
      .writeText(m.user.nickname)
      .then(() => MessagePlugin.success(t('chat.memberCopied')))
      .catch(() => MessagePlugin.error(t('chat.memberCopyFailed')))
  }
  if (key === 'kick') kick(m.userId, m.user.nickname)
  if (key === 'transfer') {
    // 复用现有的转让确认弹窗，把目标成员预选好
    transferTarget.value = m.userId
    transferVisible.value = true
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
          <span class="mc-detail__heading">
            {{ t('group.info') }}
            <span class="mc-detail__count num">
              {{ t('chat.membersCount', { n: members.length || conv.memberCount }) }}
            </span>
          </span>
          <span v-if="isOwner" class="mc-detail__actions">
            <a @click="newName = conv.name ?? ''; renameVisible = true">{{ t('group.rename') }}</a>
            <a @click="transferVisible = true">{{ t('group.transfer') }}</a>
          </span>
        </div>

        <div
          class="mc-detail__member"
          v-for="m in members"
          :key="m.userId"
          @contextmenu.prevent="openMemberMenu($event, m)"
        >
          <AvatarBadge
            :name="m.user.nickname"
            :src="m.user.avatar"
            :color="m.user.avatarColor"
            :size="30"
            :online="ws.onlineUsers.has(m.userId)"
          />
          <span class="mc-detail__member-name">
            <span class="mc-detail__member-nick">{{ m.user.nickname }}</span>
            <span v-if="m.role === 'owner'" class="mc-detail__badge is-owner">{{ t('group.owner') }}</span>
            <span v-if="m.userId === auth.user?.id" class="mc-detail__badge">{{ t('group.me') }}</span>
          </span>
          <span v-if="isOwner && m.userId !== auth.user?.id" class="mc-detail__member-ops">
            <a @click="kick(m.userId, m.user.nickname)">{{ t('group.kick') }}</a>
          </span>
        </div>

        <div class="mc-detail__footer">
          <UiButton variant="outline" theme="danger" size="small" block @click="leave">
            {{ isOwner ? t('group.leaveOwner') : t('group.leave') }}
          </UiButton>
        </div>
      </div>

      <!-- 单聊资料 -->
      <div v-else class="mc-detail__section">
        <div class="mc-detail__title">{{ t('group.peerInfo') }}</div>
        <div class="mc-detail__member">
          <AvatarBadge
            :name="conv.peer?.nickname ?? '?'"
            :src="conv.peer?.avatar"
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

  <!-- 成员右键菜单 -->
  <ContextMenu
    v-model:visible="memberMenuVisible"
    :x="memberMenuX"
    :y="memberMenuY"
    :items="memberMenuItems"
    @select="onMemberMenuSelect"
  />

  <!-- 改群名 -->
  <UiDialog
    v-model:visible="renameVisible"
    :header="t('group.renameTitle')"
    :confirm-btn-loading="renaming"
    @confirm="doRename"
  >
    <UiInput v-model="newName" :placeholder="t('group.newName')" clearable @enter="doRename" />
  </UiDialog>

  <!-- 转让群主 -->
  <UiDialog
    v-model:visible="transferVisible"
    :header="t('group.transferTitle')"
    :confirm-btn-loading="transferring"
    @confirm="doTransfer"
  >
    <TSelect v-model="transferTarget" :options="memberOptions" :placeholder="t('group.transferPlaceholder')" clearable />
  </UiDialog>
</template>

<style scoped>
.mc-detail {
  /* 宽度由布局 store 提供（可拖拽调整），默认值与历史实现一致 */
  width: var(--mc-w-detail, 280px);
  flex-shrink: 0;
  border-left: 1px solid var(--td-component-border);
  background: var(--td-bg-color-container);
  overflow-y: auto;
  display: flex;
  flex-direction: column;
}

.mc-detail__section {
  padding: 16px;
  border-bottom: 1px solid var(--td-component-border);
}

.mc-detail__title {
  font-weight: 600;
  font-size: 15px;
  letter-spacing: -0.01em;
  margin-bottom: 12px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
}

.mc-detail__heading {
  min-width: 0;
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.mc-detail__count {
  font-size: 11.5px;
  font-weight: 400;
  color: var(--mc-text-faint);
  white-space: nowrap;
}

.mc-detail__actions {
  display: flex;
  gap: 8px;
  font-weight: 400;
  font-size: 12px;
}

.mc-detail__actions a {
  cursor: pointer;
  color: var(--td-brand-color);
  border-radius: 4px;
  padding: 1px 2px;
  transition: background-color var(--mc-dur-fast, 120ms) ease;
}

.mc-detail__actions a:hover {
  background: var(--td-brand-color-light, rgba(0, 82, 217, 0.08));
  text-decoration: none;
}

.mc-detail__actions a:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 1px;
}

.mc-detail__member {
  display: flex;
  align-items: center;
  gap: 8px;
  /* 用负 margin + 内边距让 hover 背景延伸到面板边缘，成员行不再是一条条裸文字 */
  padding: 7px 8px;
  margin: 0 -8px;
  border-radius: var(--mc-radius-sm);
  transition: background-color var(--mc-dur-fast) var(--mc-ease-out);
}

.mc-detail__member:hover {
  background: var(--td-bg-color-container-hover);
}

.mc-detail__member-name {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 4px;
  overflow: hidden;
}

.mc-detail__member-nick {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mc-detail__badge {
  flex-shrink: 0;
  padding: 1px 6px;
  border-radius: 999px;
  background: var(--td-bg-color-secondarycontainer);
  font-size: 11px;
  font-weight: 400;
  line-height: 1.5;
  color: var(--td-text-color-secondary);
}

.mc-detail__badge.is-owner {
  background: var(--td-brand-color-1, rgba(0, 82, 217, 0.08));
  color: var(--td-brand-color);
}

.mc-detail__member-ops a {
  font-size: 12px;
  cursor: pointer;
}

.mc-detail__footer {
  margin-top: 14px;
}
</style>
