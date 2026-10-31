<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { DialogPlugin, Popup as TPopup } from 'tdesign-vue-next'
import { ChatIcon, SettingIcon, UserCircleIcon } from 'tdesign-icons-vue-next'

import AvatarBadge from './AvatarBadge.vue'
import ProfileCard from './ProfileCard.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import { useAuthStore } from '@/stores/auth'
import { useConversationsStore } from '@/stores/conversations'
import { useFriendsStore } from '@/stores/friends'
import { useLayoutStore } from '@/stores/layout'
import { useWsStore } from '@/stores/ws'

/**
 * 最左侧图标栏（信息架构 L1，只放高频）：
 * 头像(资料卡) + 视图切换(消息/联系人) + 底部设置。
 *
 * 低频与配置类入口不在此处：「插件管理」「站长后台」统一收进设置页；
 * 「加好友 / 新建群聊」是高频动作，常驻在列表栏顶部的「+」里，不藏二级菜单。
 */
defineProps<{ view: 'chats' | 'contacts' }>()
const emit = defineEmits<{ 'update:view': [v: 'chats' | 'contacts'] }>()

const router = useRouter()
const auth = useAuthStore()
const convStore = useConversationsStore()
const friendsStore = useFriendsStore()
const layout = useLayoutStore()
const ws = useWsStore()
const { t } = useAppI18n()

const profileVisible = ref(false)

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

function setView(v: 'chats' | 'contacts') {
  emit('update:view', v)
  // 会话列表被设成抽屉形态时，图标栏的点击改为展开/收起该抽屉
  // （此时列表不占位，否则用户点了图标会「什么都没发生」）
  if (layout.panes.conv.drawer) layout.open('conv')
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

  </nav>
</template>

<style scoped>
.app-rail {
  width: 56px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 12px 0 12px;
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

/* 底部设置入口与高频区之间加一条极淡分隔，避免「设置」看起来像第三个导航项 */
.app-rail__bottom {
  width: 100%;
  padding-top: 8px;
  border-top: 1px solid var(--mc-hairline);
}

.app-rail__avatar {
  padding: 0;
  border: none;
  background: none;
  border-radius: 50%;
  cursor: pointer;
  line-height: 0;
  transition: box-shadow var(--mc-dur-fast) var(--mc-ease-out);
}

.app-rail__avatar:hover {
  box-shadow: 0 0 0 2px var(--mc-selection);
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
  border-radius: var(--mc-radius-md);
  background: transparent;
  /* 未激活图标用 primary + 降不透明度：比 secondary 更清晰（暗色下 secondary 几乎糊在背景里） */
  color: var(--td-text-color-primary);
  opacity: 0.72;
  cursor: pointer;
  transition:
    opacity var(--mc-dur-fast) var(--mc-ease-out),
    background-color var(--mc-dur-fast) var(--mc-ease-out),
    color var(--mc-dur-fast) var(--mc-ease-out);
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
</style>
