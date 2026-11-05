<script setup lang="ts">
import { computed } from 'vue'

import { useAppI18n } from '@/composables/useAppI18n'
import { useConversationsStore } from '@/stores/conversations'

/** 分组栏（飞书式）：按全部 / 未读 / 单聊 / 群聊筛选会话列表 */
const convStore = useConversationsStore()
const { t } = useAppI18n()

const items = computed(() => {
  // type=3 系统公告会话不展示在列表里，计数也一并排除
  const convs = convStore.conversations.filter((c) => c.type !== 3)
  const active = convs.filter((c) => !c.archived)
  const archived = convs.filter((c) => c.archived)
  // 计数口径统一为「会话数」，避免未读显示成消息条数而其他项是会话数
  const unread = active.filter((c) => c.unreadCount > 0).length
  return [
    { value: 'all' as const, label: t('chat.filterAll'), count: active.length },
    { value: 'unread' as const, label: t('chat.filterUnread'), count: unread },
    {
      value: 'single' as const,
      label: t('chat.filterSingle'),
      count: active.filter((c) => c.type === 1).length,
    },
    {
      value: 'group' as const,
      label: t('chat.filterGroup'),
      count: active.filter((c) => c.type === 2).length,
    },
    {
      value: 'archived' as const,
      label: t('chat.filterArchived'),
      count: archived.length,
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
  /* 宽度由布局 store 提供（可拖拽调整），默认值与历史实现一致 */
  width: var(--mc-w-group, 148px);
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  padding: 12px 8px;
  background: var(--td-bg-color-secondarycontainer);
  border-right: 1px solid var(--td-component-border);
  overflow-y: auto;
}

.group-pane__title {
  font-size: 12px;
  font-weight: 500;
  letter-spacing: 0.02em;
  color: var(--mc-text-faint);
  padding: 4px 10px 10px;
}

.group-pane__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.group-pane__item {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 8px 10px;
  border: none;
  border-radius: var(--mc-radius-sm);
  background: transparent;
  font: inherit;
  font-size: 13px;
  color: var(--td-text-color-primary);
  cursor: pointer;
  text-align: left;
  transition:
    background-color var(--mc-dur-fast) var(--mc-ease-out),
    color var(--mc-dur-fast) var(--mc-ease-out),
    box-shadow var(--mc-dur-fast) var(--mc-ease-out);
}

.group-pane__item:hover {
  background: var(--td-bg-color-container);
}

/* 选中态：中性底色 + 品牌色文字，再用左侧细线明确「当前筛选」。
   只靠底色在暗色下与栏背景几乎同色（--td-bg-color-container 贴近页面色），看不出选中的是哪一项。 */
.group-pane__item.is-active {
  background: var(--td-bg-color-container);
  font-weight: 600;
  color: var(--td-brand-color);
  box-shadow: inset 2px 0 0 var(--td-brand-color);
}

.group-pane__item:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 1px;
}

/* 计数做成小胶囊，和列表项文字拉开层次 */
.group-pane__count {
  min-width: 18px;
  height: 18px;
  padding: 0 6px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 999px;
  background: var(--td-bg-color-container);
  font-size: 11px;
  color: var(--mc-text-faint);
}

.group-pane__item.is-active .group-pane__count {
  background: var(--td-brand-color-1, var(--td-bg-color-container));
  color: var(--td-brand-color);
}
</style>
