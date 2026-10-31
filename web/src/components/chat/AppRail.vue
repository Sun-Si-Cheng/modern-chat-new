<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  Dialog as TDialog,
  DialogPlugin,
  Input as TInput,
  MessagePlugin,
  Popup as TPopup,
} from 'tdesign-vue-next'
import {
  ChatIcon,
  DashboardIcon,
  ExtensionIcon,
  SettingIcon,
  UserCircleIcon,
} from 'tdesign-icons-vue-next'

import AvatarBadge from './AvatarBadge.vue'
import ProfileCard from './ProfileCard.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import { convApi } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { useConversationsStore } from '@/stores/conversations'
import { useFriendsStore } from '@/stores/friends'
import { useWsStore } from '@/stores/ws'

/**
 * 最左侧图标栏（飞书式）：
 * 头像(资料卡) + 新建菜单 + 功能导航(消息/联系人/插件/后台) + 底部设置
 */
defineProps<{ view: 'chats' | 'contacts' }>()
const emit = defineEmits<{ 'update:view': [v: 'chats' | 'contacts']; 'open-plugins': [] }>()

const router = useRouter()
const auth = useAuthStore()
const convStore = useConversationsStore()
const friendsStore = useFriendsStore()
const ws = useWsStore()
const { t, te } = useAppI18n()

const profileVisible = ref(false)
const moreVisible = ref(false)
const createGroupVisible = ref(false)
const groupName = ref('')

const hasUnread = computed(() => convStore.conversations.some((c) => c.unreadCount > 0))

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
  emit('update:view', 'contacts')
}

function menuSearchUser() {
  moreVisible.value = false
  emit('update:view', 'chats')
}

function setView(v: 'chats' | 'contacts') {
  emit('update:view', v)
}

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
</script>

<template>
  <nav class="app-rail">
    <div class="app-rail__top">
      <!-- 头像：点击弹资料卡 -->
      <TPopup
        v-model:visible="profileVisible"
        trigger="click"
        placement="right-top"
        :show-arrow="false"
        attach="body"
      >
        <button type="button" class="app-rail__avatar" :aria-label="t('profile.menu')">
          <AvatarBadge
            :name="auth.user?.nickname ?? '?'"
            :src="auth.user?.avatar"
            :color="auth.user?.avatarColor ?? '#0052d9'"
            :online="ws.connected"
          />
        </button>
        <template #content>
          <ProfileCard @logout="confirmLogout" />
        </template>
      </TPopup>

      <!-- 新建菜单 -->
      <TPopup
        v-model:visible="moreVisible"
        trigger="click"
        placement="right-bottom"
        :show-arrow="false"
        attach="body"
      >
        <button type="button" class="app-rail__btn" :aria-label="t('chat.moreMenu')" :title="t('chat.moreMenu')">
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path
              d="M11 11V8a1 1 0 1 1 2 0v3h3a1 1 0 1 1 0 2h-3v3a1 1 0 1 1-2 0v-3H8a1 1 0 1 1 0-2h3Zm1 10a9 9 0 1 0 0-18 9 9 0 0 0 0 18Zm0 2C5.925 23 1 18.075 1 12S5.925 1 12 1s11 4.925 11 11-4.925 11-11 11Z"
              fill="currentColor"
            />
          </svg>
        </button>
        <template #content>
          <ul class="app-rail__menu">
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

    <div class="app-rail__nav">
      <button
        type="button"
        class="app-rail__btn"
        :class="{ 'is-active': view === 'chats' }"
        :aria-label="t('chat.tabChats')"
        :title="t('chat.tabChats')"
        @click="setView('chats')"
      >
        <span class="app-rail__icon">
          <ChatIcon />
          <i v-if="hasUnread" class="app-rail__dot" />
        </span>
      </button>

      <button
        type="button"
        class="app-rail__btn"
        :class="{ 'is-active': view === 'contacts' }"
        :aria-label="t('chat.tabContacts')"
        :title="t('chat.tabContacts')"
        @click="setView('contacts')"
      >
        <span class="app-rail__icon">
          <UserCircleIcon />
          <i v-if="friendsStore.requestCount > 0" class="app-rail__dot" />
        </span>
      </button>

      <button
        type="button"
        class="app-rail__btn"
        :aria-label="t('plugin.title')"
        :title="t('plugin.title')"
        @click="emit('open-plugins')"
      >
        <span class="app-rail__icon"><ExtensionIcon /></span>
      </button>

      <button
        v-if="auth.isAdmin"
        type="button"
        class="app-rail__btn"
        :aria-label="t('admin.title')"
        :title="t('admin.title')"
        @click="router.push('/admin')"
      >
        <span class="app-rail__icon"><DashboardIcon /></span>
      </button>
    </div>

    <div class="app-rail__bottom">
      <button
        type="button"
        class="app-rail__btn"
        :aria-label="t('settings.title')"
        :title="t('settings.title')"
        @click="router.push('/settings')"
      >
        <span class="app-rail__icon"><SettingIcon /></span>
      </button>
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
  </nav>
</template>

<style scoped>
.app-rail {
  width: 56px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  padding: 12px 0 10px;
  background: var(--td-bg-color-container);
  border-right: 1px solid var(--td-component-border);
}

.app-rail__top,
.app-rail__nav,
.app-rail__bottom {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
}

.app-rail__nav {
  flex: 1;
  margin-top: 6px;
}

.app-rail__avatar {
  padding: 0;
  border: none;
  background: none;
  border-radius: 50%;
  cursor: pointer;
  line-height: 0;
}

.app-rail__avatar:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 2px;
}

.app-rail__btn {
  width: 40px;
  height: 40px;
  padding: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: 10px;
  background: transparent;
  /* 未激活图标用 primary + 降不透明度：比 secondary 更清晰（暗色下 secondary 几乎糊在背景里） */
  color: var(--td-text-color-primary);
  opacity: 0.72;
  cursor: pointer;
  transition:
    opacity var(--mc-dur-fast, 120ms) ease,
    background-color var(--mc-dur-fast, 120ms) ease,
    color var(--mc-dur-fast, 120ms) ease;
}

.app-rail__btn:hover {
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-text-color-primary);
  opacity: 1;
}

.app-rail__btn.is-active {
  background: var(--td-brand-color-1, var(--td-brand-color));
  color: var(--td-brand-color);
  opacity: 1;
}

.app-rail__btn:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 1px;
}

.app-rail__icon {
  position: relative;
  display: inline-flex;
  line-height: 0;
}

.app-rail__icon :deep(svg) {
  font-size: 20px;
}

/* 未读/申请小红点 */
.app-rail__dot {
  position: absolute;
  top: -3px;
  right: -4px;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--td-error-color, #d54941);
}

/* 新建菜单 */
.app-rail__menu {
  list-style: none;
  margin: 0;
  padding: 4px;
  min-width: 128px;
}

.app-rail__menu button {
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

.app-rail__menu button:hover {
  background: var(--td-bg-color-secondarycontainer);
}
</style>
