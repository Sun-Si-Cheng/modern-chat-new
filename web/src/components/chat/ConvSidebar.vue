<script setup lang="ts">
import UiDialog from '@/components/ui/UiDialog.vue'
import UiPopup from '@/components/ui/UiPopup.vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiInput from '@/components/ui/UiInput.vue'
import UiBadge from '@/components/ui/UiBadge.vue'
import { computed, ref } from 'vue'
import { DialogPlugin, List as TList, ListItem as TListItem, MessagePlugin } from 'tdesign-vue-next'
import { AddIcon, ChatIcon, SearchIcon, UserCircleIcon } from 'tdesign-icons-vue-next'

import AvatarBadge from './AvatarBadge.vue'
import ContextMenu, { type ContextMenuItem } from '@/components/common/ContextMenu.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import { convApi, userApi } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { useConversationsStore } from '@/stores/conversations'
import { useFriendsStore } from '@/stores/friends'
import { useWsStore } from '@/stores/ws'
import type { Conversation, User } from '@/types/proto'

/** 会话 / 联系人列表栏（第二栏，视图由图标栏驱动） */
const props = defineProps<{ view: 'chats' | 'contacts' }>()
const emit = defineEmits<{ 'open-add-friend': [] }>()

const auth = useAuthStore()
const convStore = useConversationsStore()
const friendsStore = useFriendsStore()
const ws = useWsStore()
const { t, te } = useAppI18n()

// ===== 搜索（人 + 群） =====

const kw = ref('')
const searching = ref(false)
const userResults = ref<User[]>([])
const groupResults = ref<Conversation[]>([])

async function onSearch() {
  const k = kw.value.trim()
  if (!k) {
    userResults.value = []
    groupResults.value = []
    return
  }
  searching.value = true
  try {
    const [users, groups] = await Promise.all([userApi.search(k), convApi.searchGroups(k)])
    userResults.value = users.filter((u) => u.id !== auth.user?.id)
    groupResults.value = groups
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    searching.value = false
  }
}

/**
 * 正在处理的条目（`single-3` / `join-5` / `req-7`）。
 * 这些操作都在列表项上单击触发，连续点两下会发两次请求：
 * 加群第二次必然报「已在群中」、同意好友第二次报「已处理」——对用户就是莫名报错。
 */
const busyKey = ref('')

async function startSingle(u: User) {
  if (busyKey.value) return
  busyKey.value = `single-${u.id}`
  try {
    const conv = await convApi.createSingle(u.id)
    convStore.upsertConversation(conv)
    await convStore.openConversation(conv.id)
    kw.value = ''
    userResults.value = []
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    busyKey.value = ''
  }
}

async function joinGroup(g: Conversation) {
  if (busyKey.value) return
  busyKey.value = `join-${g.id}`
  try {
    await convApi.join(g.id)
    await convStore.loadConversations()
    await convStore.openConversation(g.id)
    kw.value = ''
    groupResults.value = []
    void MessagePlugin.success(t('chat.joinedGroup', { name: g.name ?? '' }))
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    busyKey.value = ''
  }
}

// ===== 好友 =====

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

async function removeFriend(friendId: number, nickname: string) {
  const dialog = DialogPlugin.confirm({
    header: t('friends.removeTitle'),
    body: t('friends.removeBody', { name: nickname }),
    confirmBtn: { content: t('friends.remove'), theme: 'danger' },
    onConfirm: async () => {
      try {
        await friendsStore.remove(friendId)
        void MessagePlugin.success(t('friends.removeOk'))
      } catch (e) {
        void MessagePlugin.error(te(e))
      }
      dialog.destroy()
    },
  })
}

async function openFriendChat(friendId: number) {
  try {
    const conv = await convApi.createSingle(friendId)
    convStore.upsertConversation(conv)
    await convStore.openConversation(conv.id)
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
}

// ===== 会话展示 =====

function convName(c: Conversation): string {
  return c.type === 1 ? (c.peer?.nickname ?? '') : (c.name ?? '')
}

function convAvatar(c: Conversation): { name: string; color: string; avatar?: string } {
  return c.type === 1
    ? {
        name: c.peer?.nickname ?? '?',
        color: c.peer?.avatarColor ?? '#0052d9',
        avatar: c.peer?.avatar,
      }
    : { name: c.name ?? '#', color: '#2ba471' }
}

function fmtTime(ts: string | null): string {
  if (!ts) return ''
  const d = new Date(ts)
  const now = new Date()
  const hm = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  if (d.toDateString() === now.toDateString()) return hm
  const yesterday = new Date(now)
  yesterday.setDate(now.getDate() - 1)
  if (d.toDateString() === yesterday.toDateString()) return t('message.yesterday')
  return `${d.getMonth() + 1}/${d.getDate()}`
}

// ===== 新建（加好友 / 建群）=====
// 信息架构：这两个是高频动作，常驻列表栏顶部，不再藏进图标栏的二级菜单。
const newMenuVisible = ref(false)
const createGroupVisible = ref(false)
const groupName = ref('')
const creatingGroup = ref(false)

function menuAddFriend() {
  newMenuVisible.value = false
  emit('open-add-friend')
}

function menuNewGroup() {
  newMenuVisible.value = false
  groupName.value = ''
  createGroupVisible.value = true
}

async function doCreateGroup() {
  const name = groupName.value.trim()
  if (!name || creatingGroup.value) return
  creatingGroup.value = true
  try {
    const conv = await convApi.createGroup(name)
    createGroupVisible.value = false
    groupName.value = ''
    await convStore.loadConversations()
    await convStore.openConversation(conv.id)
    void MessagePlugin.success(t('chat.createGroupOk'))
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    creatingGroup.value = false
  }
}

/**
 * 会话列表的预览文案。
 * 图片/语音的 content 是文件地址，直接显示会在列表里出现一串 /uploads/xxx.png，
 * 这里按 msgType 换成占位文案。
 */
function previewText(c: Conversation): string {
  if (!c.lastMessagePreview) return t('chat.noMessage')
  if (c.lastMessageType === 'image') return t('chat.previewImage')
  if (c.lastMessageType === 'audio') return t('chat.previewVoice')
  return c.lastMessagePreview
}

// ===== 会话右键菜单 =====

const convMenuVisible = ref(false)
const convMenuX = ref(0)
const convMenuY = ref(0)
const convMenuTarget = ref<Conversation | null>(null)

function openConvMenu(e: MouseEvent, c: Conversation) {
  convMenuTarget.value = c
  convMenuX.value = e.clientX
  convMenuY.value = e.clientY
  convMenuVisible.value = true
}

const convMenuItems = computed<ContextMenuItem[]>(() => {
  const c = convMenuTarget.value
  if (!c) return []
  const items: ContextMenuItem[] = [
    { key: 'pin', label: convStore.isPinned(c.id) ? t('chat.unpin') : t('chat.pin') },
    { key: 'mute', label: convStore.isMuted(c.id) ? t('chat.unmute') : t('chat.mute') },
    ]
  if (c.unreadCount > 0) items.push({ key: 'read', label: t('chat.markRead') })
  items.push({ key: 'copyName', label: t('chat.copyName'), dividerBefore: true })
  return items
})

/** 标记已读：直接用会话上的 lastMessageId 作为 upToId，不必先打开会话 */
async function markConvRead(c: Conversation) {
  const upTo = c.lastMessageId ?? 0
  if (upTo <= 0) return
  try {
    await convApi.read(c.id, upTo)
    c.unreadCount = 0
    void MessagePlugin.success(t('chat.markedRead'))
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
}

async function copyName(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    void MessagePlugin.success(t('chat.copied'))
  } catch {
    void MessagePlugin.error(t('chat.copyFailed'))
  }
}

function onConvMenuSelect(key: string) {
  const c = convMenuTarget.value
  if (!c) return
  if (key === 'pin') void convStore.togglePin(c.id).catch((e) => MessagePlugin.error(te(e)))
  if (key === 'mute') void convStore.toggleMute(c.id).catch((e) => MessagePlugin.error(te(e)))
  if (key === 'read') void markConvRead(c)
  if (key === 'copyName') void copyName(convName(c))
}

const sortedConvs = computed(() => {
  const pinned = new Set(convStore.pinnedIds)
  // type=3 是系统公告会话：公告走弹窗提醒，不在会话列表里展示
  let list = convStore.conversations.filter((c) => c.type !== 3).sort((a, b) => {
    // 置顶优先，其余按最后消息时间倒序
    const pa = pinned.has(a.id) ? 1 : 0
    const pb = pinned.has(b.id) ? 1 : 0
    if (pa !== pb) return pb - pa
    return (b.lastMessageAt ?? '').localeCompare(a.lastMessageAt ?? '')
  })
  const f = convStore.filter
  if (f === 'unread') list = list.filter((c) => c.unreadCount > 0)
  else if (f === 'single') list = list.filter((c) => c.type === 1)
  else if (f === 'group') list = list.filter((c) => c.type === 2)
  return list
})

</script>

<template>
  <aside class="mc-sidebar">
    <!-- 搜索：人 / 群 -->
    <div class="mc-sidebar__search">
      <UiInput
        v-model="kw"
        :placeholder="t('chat.searchPlaceholder')"
        clearable
        @change="onSearch"
        @enter="onSearch"
      >
        <template #prefix-icon><SearchIcon /></template>
      </UiInput>
      <!-- 新建：加好友 / 建群（高频动作，一层可达） -->
      <UiPopup
        v-model:visible="newMenuVisible"
        placement="bottom-right"
      >
        <UiButton
          class="mc-sidebar__new"
          variant="outline"
          :aria-label="t('chat.moreMenu')"
          :title="t('chat.moreMenu')"
        >
          <AddIcon />
        </UiButton>
        <template #content>
          <ul class="mc-sidebar__new-menu">
            <li>
              <button type="button" @click="menuAddFriend">{{ t('chat.menuFriend') }}</button>
            </li>
            <li>
              <button type="button" @click="menuNewGroup">{{ t('chat.menuGroup') }}</button>
            </li>
          </ul>
        </template>
      </UiPopup>
    </div>

    <div v-if="kw.trim()" class="mc-sidebar__results">
      <div v-if="userResults.length" class="mc-sidebar__group-label">{{ t('chat.groupUsers') }}</div>
      <TList v-if="userResults.length">
        <TListItem
          v-for="u in userResults"
          :key="u.id"
          class="mc-sidebar__result-item"
          :class="{ 'is-busy': busyKey === `single-${u.id}` }"
          @click="startSingle(u)"
        >
          <AvatarBadge :name="u.nickname" :src="u.avatar" :color="u.avatarColor" :size="32" />
          <span class="mc-sidebar__result-name">{{ u.nickname }}</span>
          <span class="text-placeholder">@{{ u.username }}</span>
        </TListItem>
      </TList>
      <div v-if="groupResults.length" class="mc-sidebar__group-label">{{ t('chat.groupChats') }}</div>
      <TList v-if="groupResults.length">
        <TListItem
          v-for="g in groupResults"
          :key="g.id"
          class="mc-sidebar__result-item"
          :class="{ 'is-busy': busyKey === `join-${g.id}` }"
          @click="joinGroup(g)"
        >
          <AvatarBadge :name="g.name ?? '#'" color="#2ba471" :size="32" />
          <span class="mc-sidebar__result-name">{{ g.name }}</span>
          <span class="text-placeholder num">{{ t('chat.membersCount', { n: g.memberCount }) }}</span>
        </TListItem>
      </TList>
      <div v-if="!userResults.length && !groupResults.length && !searching" class="text-placeholder mc-sidebar__empty">
        {{ t('chat.noResults') }}
      </div>
    </div>

    <!-- 会话列表 -->
    <div v-else-if="props.view === 'chats'" class="mc-sidebar__list">
      <button
        v-for="c in sortedConvs"
        :key="c.id"
        type="button"
        class="mc-conv-item"
        :class="{ active: c.id === convStore.currentConvId, 'is-pinned': convStore.isPinned(c.id) }"
        :aria-current="c.id === convStore.currentConvId ? 'true' : undefined"
        @click="convStore.openConversation(c.id)"
        @contextmenu.prevent="openConvMenu($event, c)"
      >
        <AvatarBadge
          :name="convAvatar(c).name"
          :src="convAvatar(c).avatar"
          :color="convAvatar(c).color"
          :online="c.type === 1 && c.peer ? ws.onlineUsers.has(c.peer.id) : false"
        />
        <span class="mc-conv-item__mid">
          <span class="mc-conv-item__name">
            {{ convName(c) }}
            <!-- 免打扰：名称后一个小铃铛划掉，让人一眼看出「这个不响」 -->
            <svg
              v-if="c.muted"
              class="mc-conv-item__mute"
              width="11"
              height="11"
              viewBox="0 0 24 24"
              fill="none"
              aria-hidden="true"
            >
              <path
                d="M12 3.5a2 2 0 0 1 2 2v5.2l2.2 2.6a1 1 0 0 1-.77 1.64H8.57a1 1 0 0 1-.77-1.64L10 10.7V5.5a2 2 0 0 1 2-2Z"
                stroke="currentColor"
                stroke-width="1.5"
              />
              <path d="M4 4l16 16" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
            </svg>
          </span>
          <span class="mc-conv-item__preview text-secondary">
            <span v-if="c.mentionUnread" class="mc-conv-item__mention">{{ t('chat.someoneAtMe') }}</span>
            <span v-else-if="convStore.draftOf(c.id)" class="mc-conv-item__draft">
              {{ t('chat.draft') }}
            </span>
            {{ previewText(c) }}
          </span>
        </span>
        <span class="mc-conv-item__right">
          <span class="text-placeholder mc-conv-item__time num">{{ fmtTime(c.lastMessageAt) }}</span>
          <!-- 免打扰：只显示小圆点，不显示数字（避免用未读数制造焦虑） -->
          <span v-if="c.muted && c.unreadCount > 0" class="mc-conv-item__dot" aria-hidden="true" />
          <UiBadge v-else-if="c.unreadCount > 0" :count="c.unreadCount" size="small" />
        </span>
      </button>
      <div v-if="!sortedConvs.length" class="mc-sidebar__empty">
        <ChatIcon />
        <p>{{ t('chat.noConversation') }}</p>
      </div>
    </div>

    <!-- 联系人 -->
    <div v-else class="mc-sidebar__list">
      <div class="mc-sidebar__add-friend">
        <UiButton block variant="outline" @click="emit('open-add-friend')">{{ t('friends.addTitle') }}</UiButton>
      </div>

      <template v-if="friendsStore.pendingRequests.length">
        <div class="mc-sidebar__group-label">{{ t('friends.requests') }}</div>
        <div v-for="r in friendsStore.pendingRequests" :key="r.id" class="mc-friend-item">
          <AvatarBadge :name="r.from.nickname" :src="r.from.avatar" :color="r.from.avatarColor" :size="32" />
          <span class="mc-conv-item__name">{{ r.from.nickname }}</span>
          <div class="mc-friend-item__actions">
            <UiButton
              size="small"
              theme="primary"
              variant="text"
              :loading="busyKey === `req-${r.id}`"
              :disabled="!!busyKey && busyKey !== `req-${r.id}`"
              @click="handleRequest(r.id, true)"
            >
              {{ t('friends.accept') }}
            </UiButton>
            <UiButton
              size="small"
              variant="text"
              :disabled="!!busyKey && busyKey !== `req-${r.id}`"
              @click="handleRequest(r.id, false)"
            >
              {{ t('friends.reject') }}
            </UiButton>
          </div>
        </div>
      </template>

      <div class="mc-sidebar__group-label">{{ t('friends.mine', { n: friendsStore.accepted.length }) }}</div>
      <div v-for="f in friendsStore.accepted" :key="f.id" class="mc-friend-item">
        <AvatarBadge
          :name="f.friend.nickname"
          :src="f.friend.avatar"
          :color="f.friend.avatarColor"
          :size="32"
          :online="ws.onlineUsers.has(f.friend.id)"
        />
        <span class="mc-conv-item__name">{{ f.friend.nickname }}</span>
        <div class="mc-friend-item__actions">
          <UiButton size="small" variant="text" @click="openFriendChat(f.friend.id)">{{ t('friends.send') }}</UiButton>
          <UiButton size="small" variant="text" theme="danger" @click="removeFriend(f.friend.id, f.friend.nickname)">
            {{ t('friends.remove') }}
          </UiButton>
        </div>
      </div>
      <div v-if="!friendsStore.accepted.length" class="mc-sidebar__empty">
        <UserCircleIcon />
        <p>{{ t('friends.empty') }}</p>
      </div>
    </div>

    <!-- 会话右键菜单 -->
    <ContextMenu
      v-model:visible="convMenuVisible"
      :x="convMenuX"
      :y="convMenuY"
      :items="convMenuItems"
      @select="onConvMenuSelect"
    />

    <!-- 新建群聊 -->
    <UiDialog
      v-model:visible="createGroupVisible"
      :header="t('chat.createGroupTitle')"
      :confirm-btn="{ content: t('chat.createGroup'), theme: 'primary' }"
      :confirm-btn-loading="creatingGroup"
      @confirm="doCreateGroup"
    >
      <UiInput v-model="groupName" :placeholder="t('chat.groupName')" clearable @enter="doCreateGroup" />
    </UiDialog>
  </aside>
</template>

<style scoped>
.mc-sidebar {
  /* 宽度由布局 store 提供（可拖拽调整），默认值与历史实现一致 */
  width: var(--mc-w-conv, 300px);
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--td-component-border);
  background: var(--td-bg-color-container);
  overflow: hidden;
}

.mc-sidebar__search {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 12px 10px;
  background: var(--td-bg-color-container);
  border-bottom: 1px solid var(--mc-hairline);
}

/* 新建按钮：与搜索框等高，不抢视觉重心 */
.mc-sidebar__new {
  width: 32px;
  height: 32px;
  padding: 0;
  flex-shrink: 0;
  border-radius: var(--mc-radius-sm);
}

.mc-sidebar__new-menu {
  list-style: none;
  margin: 0;
  padding: 4px;
  min-width: 132px;
}

.mc-sidebar__new-menu button {
  display: block;
  width: 100%;
  padding: 8px 10px;
  border: none;
  border-radius: var(--mc-radius-sm);
  background: none;
  font: inherit;
  font-size: 13px;
  text-align: left;
  color: var(--td-text-color-primary);
  cursor: pointer;
  transition: background-color var(--mc-dur-fast) var(--mc-ease-out);
}

.mc-sidebar__new-menu button:hover {
  background: var(--td-bg-color-secondarycontainer);
}

.mc-sidebar__results,
.mc-sidebar__list {
  flex: 1;
  overflow-y: auto;
  padding: 6px 8px 12px;
  animation: mc-fade-up var(--mc-dur-base) var(--mc-ease-out);
}

.mc-sidebar__group-label {
  font-size: 12px;
  font-weight: 500;
  letter-spacing: 0.02em;
  color: var(--mc-text-faint);
  padding: 16px 8px 6px;
}

.mc-sidebar__empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  padding: 44px 20px;
  color: var(--mc-text-faint);
  text-align: center;
  animation: mc-fade-up var(--mc-dur-slow) var(--mc-ease-out);
}

/* 空态图标收进一个浅底圆形，避免裸图标在列表里显得单薄 */
.mc-sidebar__empty svg {
  width: 40px;
  height: 40px;
  padding: 10px;
  border-radius: 50%;
  background: var(--td-bg-color-secondarycontainer);
  opacity: 0.85;
  box-sizing: border-box;
}

.mc-sidebar__empty p {
  margin: 0;
  font-size: 12px;
  line-height: 1.6;
}

.mc-sidebar__add-friend {
  display: flex;
  gap: 6px;
  padding: 6px 8px 10px;
}

.mc-sidebar__result-item {
  cursor: pointer;
  border-radius: var(--mc-radius-sm);
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  animation: mc-fade-up var(--mc-dur-base) var(--mc-ease-out) both;
}

/* 正在处理（发起会话 / 加群）：给出进行中文案之外的视觉反馈，并挡掉重复点击 */
.mc-sidebar__result-item.is-busy {
  opacity: 0.55;
  pointer-events: none;
}

.mc-sidebar__result-name {
  font-weight: 500;
}

.mc-conv-item {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px;
  border: none;
  border-radius: var(--mc-radius-md);
  background: transparent;
  /* 注：曾尝试 content-visibility: auto + contain-intrinsic-size 跳过
     屏幕外项的渲染，实测 100 个会话时 scrollHeight 会从估算值跳变到实测值
     （滚动条"跳一下"），收益不抵副作用，已回退。消息列表由 DynamicScroller
     负责虚拟化；会话列表在 100 项量级下 DOM 约 950 节点，实测无卡顿。 */
  box-sizing: border-box;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition:
    background-color var(--mc-dur-fast) var(--mc-ease-out),
    color var(--mc-dur-fast) var(--mc-ease-out),
    box-shadow var(--mc-dur-fast) var(--mc-ease-out);
}

.mc-conv-item:hover {
  background: var(--td-bg-color-secondarycontainer);
}

.mc-conv-item:hover :deep(.mc-avatar) {
  transform: scale(1.04);
}

/* 选中态用中性灰阶，品牌色留给未读数据；头像加一圈弱高亮作为第二重提示 */
.mc-conv-item.active {
  background: var(--td-bg-color-container-active, #ebebeb);
}

.mc-conv-item.active :deep(.mc-avatar) {
  box-shadow: 0 0 0 2px var(--mc-selection);
}

/* 置顶：左侧一条品牌色细线。
   用 inset 阴影而不是 ::before —— 容器是 flex，伪元素加绝对定位在这里不稳。 */
.mc-conv-item.is-pinned {
  box-shadow: inset 2px 0 0 var(--td-brand-color);
}

.mc-conv-item:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: -2px;
}

.mc-conv-item__mid {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.mc-conv-item__name {
  display: block;
  /* 之前没定义字号，靠继承拿到 16px（比预览的 12px 大了一档多）。
     列表标题按规范用 emphasis 档：14px / 500。 */
  font-size: var(--mc-fs-emphasis);
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mc-conv-item.active .mc-conv-item__name {
  font-weight: 600;
}

.mc-conv-item__preview {
  display: block;
  font-size: 12px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mc-conv-item__right {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 4px;
  flex-shrink: 0;
}

.mc-conv-item__time {
  font-size: 11px;
}

/* 免打扰 / @我 / 草稿 三个小标记 */
.mc-conv-item__mute {
  flex-shrink: 0;
  margin-left: 4px;
  vertical-align: -1px;
  color: var(--td-text-color-placeholder);
}

.mc-conv-item__mention,
.mc-conv-item__draft {
  margin-right: 3px;
}

.mc-conv-item__mention {
  color: var(--td-error-color);
}

.mc-conv-item__draft {
  color: var(--td-brand-color);
}

/* 免打扰只显示小圆点 */
.mc-conv-item__dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--td-error-color);
}

.mc-friend-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  border-radius: var(--mc-radius-md);
  transition: background-color var(--mc-dur-fast) var(--mc-ease-out);
}

.mc-friend-item:hover {
  background: var(--td-bg-color-secondarycontainer);
}

.mc-friend-item__actions {
  margin-left: auto;
  display: flex;
  gap: 2px;
}
</style>
