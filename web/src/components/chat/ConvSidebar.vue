<script setup lang="ts">
import { computed, ref } from 'vue'
import {
  Badge as TBadge,
  Button as TButton,
  DialogPlugin,
  Input as TInput,
  List as TList,
  ListItem as TListItem,
  MessagePlugin,
} from 'tdesign-vue-next'
import { ChatIcon, UserCircleIcon } from 'tdesign-icons-vue-next'

import AvatarBadge from './AvatarBadge.vue'
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

async function startSingle(u: User) {
  try {
    const conv = await convApi.createSingle(u.id)
    convStore.upsertConversation(conv)
    await convStore.openConversation(conv.id)
    kw.value = ''
    userResults.value = []
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
}

async function joinGroup(g: Conversation) {
  try {
    await convApi.join(g.id)
    await convStore.loadConversations()
    await convStore.openConversation(g.id)
    kw.value = ''
    groupResults.value = []
    void MessagePlugin.success(t('chat.joinedGroup', { name: g.name ?? '' }))
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
}

// ===== 好友 =====

async function handleRequest(id: number, accept: boolean) {
  try {
    await friendsStore.handle(id, accept)
    void MessagePlugin.success(accept ? t('friends.acceptOk') : t('friends.rejectOk'))
  } catch (e) {
    void MessagePlugin.error(te(e))
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

const sortedConvs = computed(() => {
  let list = [...convStore.conversations].sort((a, b) =>
    (b.lastMessageAt ?? '').localeCompare(a.lastMessageAt ?? ''),
  )
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
      <TInput
        v-model="kw"
        :placeholder="t('chat.searchPlaceholder')"
        clearable
        @change="onSearch"
        @enter="onSearch"
      />
    </div>

    <div v-if="kw.trim()" class="mc-sidebar__results">
      <div v-if="userResults.length" class="mc-sidebar__group-label">{{ t('chat.groupUsers') }}</div>
      <TList v-if="userResults.length">
        <TListItem v-for="u in userResults" :key="u.id" class="mc-sidebar__result-item" @click="startSingle(u)">
          <AvatarBadge :name="u.nickname" :src="u.avatar" :color="u.avatarColor" :size="32" />
          <span class="mc-sidebar__result-name">{{ u.nickname }}</span>
          <span class="text-placeholder">@{{ u.username }}</span>
        </TListItem>
      </TList>
      <div v-if="groupResults.length" class="mc-sidebar__group-label">{{ t('chat.groupChats') }}</div>
      <TList v-if="groupResults.length">
        <TListItem v-for="g in groupResults" :key="g.id" class="mc-sidebar__result-item" @click="joinGroup(g)">
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
        :class="{ active: c.id === convStore.currentConvId }"
        :aria-current="c.id === convStore.currentConvId ? 'true' : undefined"
        @click="convStore.openConversation(c.id)"
      >
        <AvatarBadge
          :name="convAvatar(c).name"
          :src="convAvatar(c).avatar"
          :color="convAvatar(c).color"
          :online="c.type === 1 && c.peer ? ws.onlineUsers.has(c.peer.id) : false"
        />
        <span class="mc-conv-item__mid">
          <span class="mc-conv-item__name">{{ convName(c) }}</span>
          <span class="mc-conv-item__preview text-secondary">{{ c.lastMessagePreview || t('chat.noMessage') }}</span>
        </span>
        <span class="mc-conv-item__right">
          <span class="text-placeholder mc-conv-item__time num">{{ fmtTime(c.lastMessageAt) }}</span>
          <TBadge v-if="c.unreadCount > 0" :count="c.unreadCount" size="small" />
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
        <TButton block variant="outline" @click="emit('open-add-friend')">{{ t('friends.addTitle') }}</TButton>
      </div>

      <template v-if="friendsStore.requests.length">
        <div class="mc-sidebar__group-label">{{ t('friends.requests') }}</div>
        <div v-for="r in friendsStore.requests" :key="r.id" class="mc-friend-item">
          <AvatarBadge :name="r.from.nickname" :src="r.from.avatar" :color="r.from.avatarColor" :size="32" />
          <span class="mc-conv-item__name">{{ r.from.nickname }}</span>
          <div class="mc-friend-item__actions">
            <TButton size="small" theme="primary" variant="text" @click="handleRequest(r.id, true)">
              {{ t('friends.accept') }}
            </TButton>
            <TButton size="small" variant="text" @click="handleRequest(r.id, false)">{{ t('friends.reject') }}</TButton>
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
          <TButton size="small" variant="text" @click="openFriendChat(f.friend.id)">{{ t('friends.send') }}</TButton>
          <TButton size="small" variant="text" theme="danger" @click="removeFriend(f.friend.id, f.friend.nickname)">
            {{ t('friends.remove') }}
          </TButton>
        </div>
      </div>
      <div v-if="!friendsStore.accepted.length" class="mc-sidebar__empty">
        <UserCircleIcon />
        <p>{{ t('friends.empty') }}</p>
      </div>
    </div>
  </aside>
</template>

<style scoped>
.mc-sidebar {
  width: 300px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--td-component-border);
  background: var(--td-bg-color-container);
  overflow: hidden;
}

.mc-sidebar__search {
  display: flex;
  gap: 6px;
  padding: 10px 12px 8px;
  background: var(--td-bg-color-container);
  border-bottom: 1px solid var(--mc-hairline);
}

.mc-sidebar__results,
.mc-sidebar__list {
  flex: 1;
  overflow-y: auto;
  padding: 4px 8px 12px;
}

.mc-sidebar__group-label {
  font-size: 12px;
  color: var(--td-text-color-placeholder);
  padding: 10px 8px 4px;
}

.mc-sidebar__empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 40px 20px;
  color: var(--mc-text-faint);
  text-align: center;
}

.mc-sidebar__empty svg {
  width: 28px;
  height: 28px;
  opacity: 0.7;
}

.mc-sidebar__empty p {
  margin: 0;
  font-size: 12px;
  line-height: 1.6;
}

.mc-sidebar__add-friend {
  display: flex;
  gap: 6px;
  padding: 4px 8px 8px;
}

.mc-sidebar__result-item {
  cursor: pointer;
  border-radius: 6px;
  display: flex;
  align-items: center;
  gap: 8px;
}

.mc-sidebar__result-name {
  font-weight: 500;
}

.mc-conv-item {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 10px;
  border: none;
  border-radius: 8px;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition: background 0.12s ease-out;
}

.mc-conv-item:hover {
  background: var(--td-bg-color-secondarycontainer);
}

/* 选中态用中性灰阶，品牌色留给未读数据 */
.mc-conv-item.active {
  background: var(--td-bg-color-container-active, #ebebeb);
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

.mc-friend-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 10px;
  border-radius: 8px;
  transition: background 0.12s ease-out;
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
