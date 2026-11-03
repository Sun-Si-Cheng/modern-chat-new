<script setup lang="ts">
import UiDialog from '@/components/ui/UiDialog.vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiInput from '@/components/ui/UiInput.vue'
import { computed, nextTick, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'

import AvatarBadge from './AvatarBadge.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import { userApi } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { useFriendsStore } from '@/stores/friends'
import type { User } from '@/types/proto'

/**
 * 添加好友：按用户名 / 昵称搜索 → 从结果中选择添加。
 * 已是好友的显示状态；对方已发来申请的，直接就地同意。
 */
const visible = defineModel<boolean>({ default: false })

const auth = useAuthStore()
const friendsStore = useFriendsStore()
const { t, te } = useAppI18n()

const keyword = ref('')
const inputRef = ref<{ focus: () => void } | null>(null)
const results = ref<User[]>([])
const searching = ref(false)
const searched = ref(false)
/** 本次弹窗内已成功发出申请的 userId */
const sentIds = reactive(new Set<number>())

let debounceTimer: ReturnType<typeof setTimeout> | null = null
let searchSeq = 0

const trimmedKeyword = computed(() => keyword.value.trim())

watch(trimmedKeyword, (kw) => {
  if (debounceTimer) clearTimeout(debounceTimer)
  if (!kw) {
    searchSeq++
    results.value = []
    searched.value = false
    searching.value = false
    return
  }
  debounceTimer = setTimeout(() => void search(kw), 300)
})

watch(visible, (open) => {
  if (!open) {
    reset()
    return
  }
  void nextTick(() => inputRef.value?.focus())
})

async function search(kw: string) {
  const seq = ++searchSeq
  searching.value = true
  try {
    const list = await userApi.search(kw)
    if (seq !== searchSeq) return
    results.value = list.filter((u) => u.id !== auth.user?.id)
    searched.value = true
  } catch (e) {
    if (seq !== searchSeq) return
    results.value = []
    searched.value = true
    void MessagePlugin.error(te(e))
  } finally {
    if (seq === searchSeq) searching.value = false
  }
}

function isFriend(u: User): boolean {
  return friendsStore.accepted.some((f) => f.friend.id === u.id)
}

function incomingRequest(u: User) {
  return friendsStore.requests.find((r) => r.from.id === u.id)
}

async function add(u: User) {
  try {
    await friendsStore.apply(u.username)
    sentIds.add(u.id)
    void MessagePlugin.success(t('friends.applyOk'))
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
}

async function accept(u: User) {
  const req = incomingRequest(u)
  if (!req) return
  try {
    await friendsStore.handle(req.id, true)
    void MessagePlugin.success(t('friends.acceptOk'))
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
}

function reset() {
  keyword.value = ''
  results.value = []
  searched.value = false
  searching.value = false
  sentIds.clear()
  if (debounceTimer) {
    clearTimeout(debounceTimer)
    debounceTimer = null
  }
}

onBeforeUnmount(() => {
  if (debounceTimer) clearTimeout(debounceTimer)
})
</script>

<template>
  <UiDialog
    v-model:visible="visible"
    :header="t('friends.addTitle')"
    width="440px"
    :footer="false"
  >
    <div class="add-friend">
      <UiInput
        ref="inputRef"
        v-model="keyword"
        size="large"
        :placeholder="t('friends.searchPlaceholder')"
        clearable
      />

      <ul v-if="results.length" class="add-friend__list">
        <li v-for="u in results" :key="u.id" class="add-friend__item">
          <AvatarBadge :name="u.nickname" :src="u.avatar" :color="u.avatarColor" :size="36" />
          <span class="add-friend__meta">
            <span class="add-friend__name">{{ u.nickname }}</span>
            <span class="add-friend__username">@{{ u.username }}</span>
          </span>
          <span v-if="isFriend(u)" class="add-friend__tag">{{ t('friends.alreadyFriend') }}</span>
          <UiButton
            v-else-if="incomingRequest(u)"
            size="small"
            theme="primary"
            variant="outline"
            @click="accept(u)"
          >
            {{ t('friends.accept') }}
          </UiButton>
          <span v-else-if="sentIds.has(u.id)" class="add-friend__tag">{{ t('friends.sent') }}</span>
          <UiButton v-else size="small" theme="primary" @click="add(u)">
            {{ t('friends.addAction') }}
          </UiButton>
        </li>
      </ul>

      <div v-else-if="searching" class="add-friend__state text-placeholder">{{ t('common.loading') }}</div>
      <div v-else-if="searched" class="add-friend__state text-placeholder">{{ t('chat.noResults') }}</div>
      <div v-else class="add-friend__state text-placeholder">{{ t('friends.searchHint') }}</div>
    </div>
  </UiDialog>
</template>

<style scoped>
.add-friend {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.add-friend__list {
  list-style: none;
  margin: 0;
  padding: 0;
  max-height: 360px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.add-friend__item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 8px;
  border-radius: var(--mc-radius-md);
  transition: background-color var(--mc-dur-fast) var(--mc-ease-out);
  animation: mc-fade-up var(--mc-dur-base) var(--mc-ease-out) both;
}

.add-friend__item:hover {
  background: var(--td-bg-color-secondarycontainer);
}

.add-friend__meta {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.add-friend__name {
  font-size: 14px;
  font-weight: 500;
  line-height: 1.4;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.add-friend__username {
  font-size: 12px;
  line-height: 1.4;
  color: var(--mc-text-faint);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.add-friend__tag {
  flex-shrink: 0;
  padding: 3px 9px;
  border-radius: 999px;
  background: var(--td-bg-color-secondarycontainer);
  font-size: 11.5px;
  color: var(--td-text-color-secondary);
}

.add-friend__state {
  min-height: 168px;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 28px 8px;
  text-align: center;
  font-size: 12.5px;
  line-height: 1.6;
  animation: mc-fade-in var(--mc-dur-base) var(--mc-ease-out);
}
</style>
