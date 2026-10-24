<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  Badge as TBadge,
  Button as TButton,
  Dialog as TDialog,
  DialogPlugin,
  Input as TInput,
  List as TList,
  ListItem as TListItem,
  MessagePlugin,
  Popup as TPopup,
  Tabs as TTabss,
  TabPanel as TTabPanel,
} from 'tdesign-vue-next'


import AvatarBadge from './AvatarBadge.vue'
import ProfileCard from './ProfileCard.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import { convApi, userApi } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { useConversationsStore } from '@/stores/conversations'
import { useFriendsStore } from '@/stores/friends'
import { useWsStore } from '@/stores/ws'
import type { Conversation, User } from '@/types/proto'

const router = useRouter()
const auth = useAuthStore()
const convStore = useConversationsStore()
const friendsStore = useFriendsStore()
const ws = useWsStore()
const { t, te } = useAppI18n()

const tab = ref<'chats' | 'contacts'>('chats')

// ===== 新建菜单（头像行右侧的 + 按钮） =====

const moreVisible = ref(false)
const profileVisible = ref(false)
const searchRef = ref<{ focus: () => void } | null>(null)

/** 退出登录：先关资料卡，再走二次确认 */
function confirmLogout() {
  profileVisible.value = false
  const dialog = DialogPlugin.confirm({
    header: t('chat.logoutTitle'),
    body: t('chat.logoutBody'),
    onConfirm: () => {
      auth.logout()
      ws.disconnect()
      convStore.reset()
      friendsStore.reset()
      void router.push('/login')
      dialog.destroy()
    },
  })
}

function menuNewGroup() {
  moreVisible.value = false
  createGroupVisible.value = true
}

function menuAddFriend() {
  moreVisible.value = false
  tab.value = 'contacts'
  void nextTick(() => searchRef.value?.focus())
}

function menuSearchUser() {
  moreVisible.value = false
  tab.value = 'chats'
  void nextTick(() => searchRef.value?.focus())
}

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
    tab.value = 'chats'
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
    tab.value = 'chats'
    kw.value = ''
    groupResults.value = []
    void MessagePlugin.success(t('chat.joinedGroup', { name: g.name ?? '' }))
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
}

// ===== 建群 =====

const createGroupVisible = ref(false)
const groupName = ref('')

async function doCreateGroup() {
  const name = groupName.value.trim()
  if (!name) return
  try {
    const conv = await convApi.createGroup(name)
    createGroupVisible.value = false
    groupName.value = ''
    await convStore.loadConversations()
    await convStore.openConversation(conv.id)
    void MessagePlugin.success(t('chat.createGroupOk'))
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
}

// ===== 好友 =====

const applyName = ref('')

async function applyFriend() {
  const name = applyName.value.trim()
  if (!name) return
  try {
    await friendsStore.apply(name)
    void MessagePlugin.success(t('friends.applyOk'))
    applyName.value = ''
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
}

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
    tab.value = 'chats'
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

const sortedConvs = computed(() =>
  [...convStore.conversations].sort((a, b) => (b.lastMessageAt ?? '').localeCompare(a.lastMessageAt ?? '')),
)

</script>

<template>
  <aside class="mc-sidebar">
    <!-- 顶部：产品名 + 当前用户 -->
    <header class="mc-sidebar__me">
      <div class="mc-sidebar__userrow">
        <!-- 点击头像行弹出个人资料卡（含更换头像、设置、退出、后台） -->
        <!-- attach="body"：脱离侧栏的 overflow:hidden 裁剪，面板才能完整显示在头像右侧 -->
        <TPopup
          v-model:visible="profileVisible"
          trigger="click"
          placement="right-top"
          :show-arrow="false"
          attach="body"
        >
          <button class="mc-sidebar__profile" :aria-label="t('profile.menu')">
            <AvatarBadge
              :name="auth.user?.nickname ?? '?'"
              :src="auth.user?.avatar"
              :color="auth.user?.avatarColor ?? '#0052d9'"
              :online="ws.connected"
            />
            <div class="mc-sidebar__me-info">
              <div class="mc-sidebar__me-name">{{ auth.user?.nickname }}</div>
              <div class="mc-sidebar__me-status" :class="ws.connected ? 'on' : 'off'">
                {{ ws.connected ? t('chat.online') : t('chat.connecting') }}
              </div>
            </div>
          </button>

          <template #content>
            <ProfileCard @logout="confirmLogout" />
          </template>
        </TPopup>

        <!-- 新建菜单：右侧 + 按钮 -->
        <!-- 按钮在侧栏最右，面板向右下展开（left 对齐按钮左边缘），避免向左盖住会话列表 -->
        <TPopup
          v-model:visible="moreVisible"
          trigger="click"
          placement="bottom-left"
          :show-arrow="false"
          attach="body"
        >
          <button type="button" class="mc-sidebar__more" :aria-label="t('chat.moreMenu')" :title="t('chat.moreMenu')">
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" aria-hidden="true">
              <path
                d="M11 11V8a1 1 0 1 1 2 0v3h3a1 1 0 1 1 0 2h-3v3a1 1 0 1 1-2 0v-3H8a1 1 0 1 1 0-2h3Zm1 10a9 9 0 1 0 0-18 9 9 0 0 0 0 18Zm0 2C5.925 23 1 18.075 1 12S5.925 1 12 1s11 4.925 11 11-4.925 11-11 11Z"
                fill="currentColor"
              />
            </svg>
          </button>

          <template #content>
            <ul class="mc-sidebar__menu">
              <li>
                <button type="button" @click="menuSearchUser">{{ t('chat.menuSearch') }}</button>
              </li>
              <li>
                <button type="button" @click="menuAddFriend">{{ t('chat.menuFriend') }}</button>
              </li>
              <li>
                <button type="button" @click="menuNewGroup">{{ t('chat.menuGroup') }}</button>
              </li>
            </ul>
          </template>
        </TPopup>
      </div>
    </header>

    <TTabss v-model="tab">
      <TTabPanel value="chats" :label="t('chat.tabChats')" />
      <TTabPanel value="contacts">
        <template #label>
          <TBadge :count="friendsStore.requestCount" :offset="[6, -2]" size="small">
            {{ t('chat.tabContacts') }}
          </TBadge>
        </template>
      </TTabPanel>
    </TTabss>

    <!-- 搜索：人 / 群 -->
    <div class="mc-sidebar__search">
      <TInput
        ref="searchRef"
        v-model="kw"
        :placeholder="t('chat.searchPlaceholder')"
        clearable
        @change="onSearch"
        @enter="onSearch"
      />
      <TButton v-if="tab === 'chats'" size="small" variant="outline" @click="createGroupVisible = true">
        {{ t('chat.createGroup') }}
      </TButton>
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
    <div v-else-if="tab === 'chats'" class="mc-sidebar__list">
      <div
        v-for="c in sortedConvs"
        :key="c.id"
        class="mc-conv-item"
        :class="{ active: c.id === convStore.currentConvId }"
        @click="convStore.openConversation(c.id)"
      >
        <AvatarBadge
          :name="convAvatar(c).name"
          :src="convAvatar(c).avatar"
          :color="convAvatar(c).color"
          :online="c.type === 1 && c.peer ? ws.onlineUsers.has(c.peer.id) : false"
        />
        <div class="mc-conv-item__mid">
          <div class="mc-conv-item__name">{{ convName(c) }}</div>
          <div class="mc-conv-item__preview text-secondary">{{ c.lastMessagePreview || t('chat.noMessage') }}</div>
        </div>
        <div class="mc-conv-item__right">
          <span class="text-placeholder mc-conv-item__time num">{{ fmtTime(c.lastMessageAt) }}</span>
          <TBadge v-if="c.unreadCount > 0" :count="c.unreadCount" size="small" />
        </div>
      </div>
      <div v-if="!sortedConvs.length" class="text-placeholder mc-sidebar__empty">{{ t('chat.noConversation') }}</div>
    </div>

    <!-- 联系人 -->
    <div v-else class="mc-sidebar__list">
      <div class="mc-sidebar__add-friend">
        <TInput v-model="applyName" :placeholder="t('friends.addPlaceholder')" clearable @enter="applyFriend" />
        <TButton size="small" variant="outline" @click="applyFriend">{{ t('friends.apply') }}</TButton>
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
      <div v-if="!friendsStore.accepted.length" class="text-placeholder mc-sidebar__empty">{{ t('friends.empty') }}</div>
    </div>



    <!-- 建群弹窗 -->
    <TDialog
      v-model:visible="createGroupVisible"
      :header="t('chat.createGroupTitle')"
      :confirm-btn="{ content: t('chat.createGroup'), theme: 'primary' }"
      @confirm="doCreateGroup"
    >
      <TInput v-model="groupName" :placeholder="t('chat.groupName')" clearable @enter="doCreateGroup" />
    </TDialog>
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

.mc-sidebar__me {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px 14px 10px;
}

.mc-sidebar__userrow {
  display: flex;
  align-items: center;
  gap: 10px;
}

/* 头像行右侧的「新建」按钮 */
.mc-sidebar__more {
  flex-shrink: 0;
  width: 30px;
  height: 30px;
  padding: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: 8px;
  background: transparent;
  color: var(--td-text-color-secondary);
  cursor: pointer;
}

.mc-sidebar__more:hover {
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-text-color-primary);
}

.mc-sidebar__more:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 1px;
}

/* 新建菜单 */
.mc-sidebar__menu {
  list-style: none;
  margin: 0;
  padding: 4px;
  min-width: 128px;
}

.mc-sidebar__menu button {
  display: block;
  width: 100%;
  padding: 8px 10px;
  border: none;
  border-radius: 6px;
  background: none;
  font: inherit;
  font-size: 13px;
  text-align: left;
  color: var(--td-text-color-primary);
  cursor: pointer;
}

.mc-sidebar__menu button:hover {
  background: var(--td-bg-color-secondarycontainer);
}

/* 用户行即资料入口 */
.mc-sidebar__profile {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 4px;
  margin: -4px;
  border: none;
  border-radius: 8px;
  background: transparent;
  cursor: pointer;
  text-align: left;
  font: inherit;
  color: inherit;
}

.mc-sidebar__profile:hover {
  background: var(--td-bg-color-secondarycontainer);
}

.mc-sidebar__profile:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 1px;
}

.mc-sidebar__me-info {
  flex: 1;
  min-width: 0;
}

.mc-sidebar__me-name {
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mc-sidebar__me-status {
  font-size: 12px;
}

.mc-sidebar__me-status.on {
  color: var(--td-success-color, #2ba471);
}

.mc-sidebar__me-status.off {
  color: var(--td-text-color-placeholder);
}

.mc-sidebar__search {
  display: flex;
  gap: 6px;
  padding: 8px 14px;
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
  text-align: center;
  padding: 24px 0;
  font-size: 13px;
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
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 10px;
  border-radius: 8px;
  cursor: pointer;
}

.mc-conv-item:hover {
  background: var(--td-bg-color-secondarycontainer);
}

/* 选中态用中性灰阶，品牌色留给未读数据 */
.mc-conv-item.active {
  background: var(--td-bg-color-container-active, #ebebeb);
}

.mc-conv-item__mid {
  flex: 1;
  min-width: 0;
}

.mc-conv-item__name {
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mc-conv-item__preview {
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
}

.mc-friend-item__actions {
  margin-left: auto;
  display: flex;
  gap: 2px;
}
</style>
