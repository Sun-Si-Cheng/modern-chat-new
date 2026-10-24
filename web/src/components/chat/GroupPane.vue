<script setup lang="ts">
import { computed } from 'vue'

import { useAppI18n } from '@/composables/useAppI18n'
import { useConversationsStore } from '@/stores/conversations'

/** 分组栏（飞书式）：按全部 / 未读 / 单聊 / 群聊筛选会话列表 */
const convStore = useConversationsStore()
const { t } = useAppI18n()

const items = computed(() => {
  const convs = convStore.conversations
  // 计数口径统一为「会话数」，避免未读显示成消息条数而其他项是会话数
  const unread = convs.filter((c) => c.unreadCount > 0).length
  return [
    { value: 'all' as const, label: t('chat.filterAll'), count: convs.length },
    { value: 'unread' as const, label: t('chat.filterUnread'), count: unread },
    {
      value: 'single' as const,
      label: t('chat.filterSingle'),
      count: convs.filter((c) => c.type === 1).length,
    },
    {
      value: 'group' as const,
      label: t('chat.filterGroup'),
      count: convs.filter((c) => c.type === 2).length,
    },
  ]
})
</script>

<template>
  <aside class="group-pane">
    <div class="group-pane__title">{{ t('chat.filterTitle') }}</div>
    <ul class="group-pane__list">
      <li v-for="it in items" :key="it.value">
        <button
          type="button"
          class="group-pane__item"
          :class="{ 'is-active': convStore.filter === it.value }"
          :aria-pressed="convStore.filter === it.value"
          @click="convStore.setFilter(it.value)"
        >
          <span class="group-pane__label">{{ it.label }}</span>
          <span v-if="it.count > 0" class="group-pane__count num">{{ it.count }}</span>
        </button>
      </li>
    </ul>
  </aside>
</template>

<style scoped>
.group-pane {
  width: 148px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  padding: 10px 8px;
  background: var(--td-bg-color-secondarycontainer);
  border-right: 1px solid var(--td-component-border);
  overflow-y: auto;
}

.group-pane__title {
  font-size: 12px;
  font-weight: 500;
  color: var(--td-text-color-placeholder);
  padding: 6px 10px 10px;
}

.group-pane__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.group-pane__item {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  padding: 7px 10px;
  border: none;
  border-radius: 6px;
  background: transparent;
  font: inherit;
  font-size: 13px;
  color: var(--td-text-color-primary);
  cursor: pointer;
  text-align: left;
  transition:
    background-color var(--mc-dur-fast) var(--mc-ease-out),
    color var(--mc-dur-fast) var(--mc-ease-out),
    transform var(--mc-dur-fast) var(--mc-ease-out);
}

.group-pane__item:hover {
  background: var(--td-bg-color-container);
  transform: translateX(2px);
}

/* 选中态用一块中性底色表达，不加阴影 */
.group-pane__item.is-active {
  background: var(--td-bg-color-container);
  font-weight: 600;
}

.group-pane__item:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 1px;
}

.group-pane__count {
  font-size: 11px;
  color: var(--mc-text-faint);
}

.group-pane__item.is-active .group-pane__count {
  color: var(--td-brand-color);
}
</style>
