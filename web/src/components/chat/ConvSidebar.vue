<script setup lang="ts">
import { computed, ref } from 'vue'
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
  Tabs as TTabss,
  TabPanel as TTabPanel,
  Tooltip as TTooltip,
} from 'tdesign-vue-next'

import AvatarBadge from './AvatarBadge.vue'
import PluginDrawer from './PluginDrawer.vue'
import { DashboardIcon, ExtensionIcon, PoweroffIcon } from 'tdesign-icons-vue-next'
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

const tab = ref<'chats' | 'contacts'>('chats')
const pluginDrawerVisible = ref(false)

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
    void MessagePlugin.error((e as Error).message)
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
    void MessagePlugin.error((e as Error).message)
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
    void MessagePlugin.success(`已加入「${g.name}」`)
  } catch (e) {
    void MessagePlugin.error((e as Error).message)
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
    void MessagePlugin.success('群聊已创建')
  } catch (e) {
    void MessagePlugin.error((e as Error).message)
  }
}

// ===== 好友 =====

const applyName = ref('')

async function applyFriend() {
  const name = applyName.value.trim()
  if (!name) return
  try {
    await friendsStore.apply(name)
    void MessagePlugin.success('申请已发送')
    applyName.value = ''
  } catch (e) {
    void MessagePlugin.error((e as Error).message)
  }
}

async function handleRequest(id: number, accept: boolean) {
  try {
    await friendsStore.handle(id, accept)
    void MessagePlugin.success(accept ? '已同意' : '已拒绝')
  } catch (e) {
    void MessagePlugin.error((e as Error).message)
  }
}

async function removeFriend(friendId: number, nickname: string) {
  const dialog = DialogPlugin.confirm({
    header: '删除好友',
    body: `确定删除好友「${nickname}」吗？单聊记录将保留。`,
    confirmBtn: { content: '删除', theme: 'danger' },
    onConfirm: async () => {
      try {
        await friendsStore.remove(friendId)
        void MessagePlugin.success('已删除')
      } catch (e) {
        void MessagePlugin.error((e as Error).message)
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
    void MessagePlugin.error((e as Error).message)
  }
}

// ===== 会话展示 =====

function convName(c: Conversation): string {
  return c.type === 1 ? (c.peer?.nickname ?? '单聊') : (c.name ?? '群聊')
}

function convAvatar(c: Conversation): { name: string; color: string } {
  return c.type === 1
    ? { name: c.peer?.nickname ?? '?', color: c.peer?.avatarColor ?? '#0052d9' }
    : { name: c.name ?? '群', color: '#2ba471' }
}

function fmtTime(ts: string | null): string {
  if (!ts) return ''
  const d = new Date(ts)
  const now = new Date()
  const hm = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  if (d.toDateString() === now.toDateString()) return hm
  const yesterday = new Date(now)
  yesterday.setDate(now.getDate() - 1)
  if (d.toDateString() === yesterday.toDateString()) return '昨天'
  return `${d.getMonth() + 1}/${d.getDate()}`
}

const sortedConvs = computed(() =>
  [...convStore.conversations].sort((a, b) => (b.lastMessageAt ?? '').localeCompare(a.lastMessageAt ?? '')),
)

// ===== 个人操作 =====

function confirmLogout() {
  const dialog = DialogPlugin.confirm({
    header: '退出登录',
    body: '确定退出当前账号吗？',
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
</script>

<template>
  <aside class="mc-sidebar">
    <!-- 顶部：产品名 + 当前用户 -->
    <header class="mc-sidebar__me">
      <div class="mc-sidebar__wordmark">
        <span class="mc-sidebar__mark">M</span>
        <span class="mc-sidebar__product">ModernChat</span>
      </div>
      <div class="mc-sidebar__userrow">
        <AvatarBadge
          :name="auth.user?.nickname ?? '?'"
          :color="auth.user?.avatarColor ?? '#0052d9'"
          :online="ws.connected"
        />
        <div class="mc-sidebar__me-info">
          <div class="mc-sidebar__me-name">{{ auth.user?.nickname }}</div>
          <div class="mc-sidebar__me-status" :class="ws.connected ? 'on' : 'off'">
            {{ ws.connected ? '在线' : '连接中…' }}
          </div>
        </div>
        <div class="mc-sidebar__me-actions">
          <TTooltip content="插件管理">
            <TButton variant="text" shape="square" aria-label="插件管理" @click="pluginDrawerVisible = true">
              <template #icon><ExtensionIcon /></template>
            </TButton>
          </TTooltip>
          <TTooltip v-if="auth.isAdmin" content="站长后台">
            <TButton variant="text" shape="square" aria-label="站长后台" @click="router.push('/admin')">
              <template #icon><DashboardIcon /></template>
            </TButton>
          </TTooltip>
          <TTooltip content="退出登录">
            <TButton variant="text" shape="square" aria-label="退出登录" @click="confirmLogout">
              <template #icon><PoweroffIcon /></template>
            </TButton>
          </TTooltip>
        </div>
      </div>
    </header>

    <TTabss v-model="tab">
      <TTabPanel value="chats" label="聊天" />
      <TTabPanel value="contacts">
        <template #label>
          <TBadge :count="friendsStore.requestCount" :offset="[6, -2]" size="small">联系人</TBadge>
        </template>
      </TTabPanel>
    </TTabss>

    <!-- 搜索：人 / 群 -->
    <div class="mc-sidebar__search">
      <TInput v-model="kw" placeholder="搜索用户 / 群聊" clearable @change="onSearch" @enter="onSearch" />
      <TButton v-if="tab === 'chats'" size="small" variant="outline" @click="createGroupVisible = true">建群</TButton>
    </div>

    <div v-if="kw.trim()" class="mc-sidebar__results">
      <div v-if="userResults.length" class="mc-sidebar__group-label">用户</div>
      <TList v-if="userResults.length">
        <TListItem v-for="u in userResults" :key="u.id" class="mc-sidebar__result-item" @click="startSingle(u)">
          <AvatarBadge :name="u.nickname" :color="u.avatarColor" :size="32" />
          <span class="mc-sidebar__result-name">{{ u.nickname }}</span>
          <span class="text-placeholder">@{{ u.username }}</span>
        </TListItem>
      </TList>
      <div v-if="groupResults.length" class="mc-sidebar__group-label">群聊</div>
      <TList v-if="groupResults.length">
        <TListItem v-for="g in groupResults" :key="g.id" class="mc-sidebar__result-item" @click="joinGroup(g)">
          <AvatarBadge :name="g.name ?? '群'" color="#2ba471" :size="32" />
          <span class="mc-sidebar__result-name">{{ g.name }}</span>
          <span class="text-placeholder">{{ g.memberCount }} 人</span>
        </TListItem>
      </TList>
      <div v-if="!userResults.length && !groupResults.length && !searching" class="text-placeholder mc-sidebar__empty">
        无匹配结果
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
        <AvatarBadge :name="convAvatar(c).name" :color="convAvatar(c).color" :online="c.type === 1 && c.peer ? ws.onlineUsers.has(c.peer.id) : false" />
        <div class="mc-conv-item__mid">
          <div class="mc-conv-item__name">{{ convName(c) }}</div>
          <div class="mc-conv-item__preview text-secondary">{{ c.lastMessagePreview || '暂无消息' }}</div>
        </div>
        <div class="mc-conv-item__right">
          <span class="text-placeholder mc-conv-item__time num">{{ fmtTime(c.lastMessageAt) }}</span>
          <TBadge v-if="c.unreadCount > 0" :count="c.unreadCount" size="small" />
        </div>
      </div>
      <div v-if="!sortedConvs.length" class="text-placeholder mc-sidebar__empty">还没有会话，搜索用户开始聊天</div>
    </div>

    <!-- 联系人 -->
    <div v-else class="mc-sidebar__list">
      <div class="mc-sidebar__add-friend">
        <TInput v-model="applyName" placeholder="输入用户名添加好友" clearable @enter="applyFriend" />
        <TButton size="small" variant="outline" @click="applyFriend">申请</TButton>
      </div>

      <template v-if="friendsStore.requests.length">
        <div class="mc-sidebar__group-label">收到的好友申请</div>
        <div v-for="r in friendsStore.requests" :key="r.id" class="mc-friend-item">
          <AvatarBadge :name="r.from.nickname" :color="r.from.avatarColor" :size="32" />
          <span class="mc-conv-item__name">{{ r.from.nickname }}</span>
          <div class="mc-friend-item__actions">
            <TButton size="small" theme="primary" variant="text" @click="handleRequest(r.id, true)">同意</TButton>
            <TButton size="small" variant="text" @click="handleRequest(r.id, false)">拒绝</TButton>
          </div>
        </div>
      </template>

      <div class="mc-sidebar__group-label">我的好友（{{ friendsStore.accepted.length }}）</div>
      <div v-for="f in friendsStore.accepted" :key="f.id" class="mc-friend-item">
        <AvatarBadge :name="f.friend.nickname" :color="f.friend.avatarColor" :size="32" :online="ws.onlineUsers.has(f.friend.id)" />
        <span class="mc-conv-item__name">{{ f.friend.nickname }}</span>
        <div class="mc-friend-item__actions">
          <TButton size="small" variant="text" @click="openFriendChat(f.friend.id)">发消息</TButton>
          <TButton size="small" variant="text" theme="danger" @click="removeFriend(f.friend.id, f.friend.nickname)">删除</TButton>
        </div>
      </div>
      <div v-if="!friendsStore.accepted.length" class="text-placeholder mc-sidebar__empty">还没有好友</div>
    </div>

    <!-- 插件管理 -->
    <PluginDrawer v-model="pluginDrawerVisible" />

    <!-- 建群弹窗 -->
    <TDialog
      v-model:visible="createGroupVisible"
      header="创建群聊"
      :confirm-btn="{ content: '创建', theme: 'primary' }"
      @confirm="doCreateGroup"
    >
      <TInput v-model="groupName" placeholder="群名称" clearable @enter="doCreateGroup" />
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

.mc-sidebar__wordmark {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 2px 2px 0;
}

.mc-sidebar__mark {
  width: 22px;
  height: 22px;
  border-radius: 6px;
  background: var(--td-brand-color);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  font-weight: 700;
  flex-shrink: 0;
}

.mc-sidebar__product {
  font-size: 14px;
  font-weight: 600;
  letter-spacing: -0.01em;
}

.mc-sidebar__userrow {
  display: flex;
  align-items: center;
  gap: 10px;
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

.mc-sidebar__me-actions {
  display: flex;
  gap: 2px;
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
