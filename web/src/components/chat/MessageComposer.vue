<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from 'vue'
import UiButton from '@/components/ui/UiButton.vue'

import { useAppI18n } from '@/composables/useAppI18n'
import { useConversationsStore } from '@/stores/conversations'

const convStore = useConversationsStore()
const { t } = useAppI18n()

// 从草稿恢复：切换会话回来时，之前打了一半的内容还在
const text = ref(convStore.currentConvId ? convStore.draftOf(convStore.currentConvId) : '')
const lastTypingAt = ref(0)
const inputRef = ref<HTMLTextAreaElement | null>(null)
/** 中文输入法组合期间按 Enter 是选词，不应发送 */
const composing = ref(false)

const MAX_ROWS_HEIGHT = 144

function resize() {
  const el = inputRef.value
  if (!el) return
  el.style.height = 'auto'
  el.style.height = `${Math.min(el.scrollHeight, MAX_ROWS_HEIGHT)}px`
}

watch(text, (v) => {
  void nextTick(resize)
  // 输入即写草稿；发送后 text 被清空，草稿随之删除
  if (convStore.currentConvId) convStore.setDraft(convStore.currentConvId, v)
})

onMounted(resize)

function onInput() {
  if (!convStore.currentConvId) return
  const now = Date.now()
  if (now - lastTypingAt.value > 2000) {
    lastTypingAt.value = now
    convStore.sendTyping(convStore.currentConvId)
  }
}

function send() {
  const content = text.value.trim()
  if (!content || !convStore.currentConvId) return
  convStore.sendText(convStore.currentConvId, content)
  text.value = ''
}

function onKeydown(e: KeyboardEvent) {
  if (e.key !== 'Enter' || e.shiftKey) return
  if (e.isComposing || composing.value) return
  e.preventDefault()
  send()
}
</script>

<template>
  <footer class="mc-composer">
    <div class="mc-composer__box">
      <textarea
        ref="inputRef"
        v-model="text"
        class="mc-composer__input"
        :placeholder="t('message.composerPlaceholder')"
        maxlength="1000"
        rows="1"
        @keydown="onKeydown"
        @input="onInput"
        @compositionstart="composing = true"
        @compositionend="composing = false"
      />
      <div class="mc-composer__footer">
        <!-- 完整说明放 title，行内只留不会截断的短提示（面板窄时 "Enter 发送 · Shift..." 会断成两截） -->
        <span class="mc-composer__hint text-placeholder" :title="t('message.sendHint')">
          {{ t('message.sendHintShort') }}
        </span>
        <div class="mc-composer__right">
          <span v-if="text.length" class="mc-composer__count text-placeholder num" :class="{ 'is-near-limit': text.length >= 900 }">
            {{ text.length }}/1000
          </span>
          <UiButton theme="primary" size="small" :disabled="!text.trim()" @click="send">
            {{ t('message.send') }}
          </UiButton>
        </div>
      </div>
    </div>
  </footer>
</template>

<style scoped>
.mc-composer {
  flex-shrink: 0;
  border-top: 1px solid var(--td-component-border);
  background: var(--td-bg-color-container);
  padding: 12px 20px 16px;
}

/* 输入区做成一块完整的输入面板，聚焦时整块响应，而不是只给底边变色 */
.mc-composer__box {
  border: 1px solid var(--td-component-border);
  border-radius: var(--mc-radius-lg);
  background: var(--td-bg-color-container);
  padding: 12px 14px 10px;
  box-shadow: var(--mc-shadow-1);
  transition:
    border-color var(--mc-dur-fast) var(--mc-ease-out),
    box-shadow var(--mc-dur-fast) var(--mc-ease-out);
}

.mc-composer__box:focus-within {
  border-color: var(--td-brand-color);
  box-shadow: 0 0 0 3px var(--mc-selection);
}

.mc-composer__input {
  display: block;
  width: 100%;
  min-height: 24px;
  max-height: 144px;
  border: none;
  outline: none;
  resize: none;
  overflow-y: auto;
  background: transparent;
  color: var(--td-text-color-primary);
  caret-color: var(--td-brand-color);
  font-family: inherit;
  font-size: 14px;
  line-height: 1.6;
}

.mc-composer__input::placeholder {
  color: var(--td-text-color-placeholder);
}

.mc-composer__footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  margin-top: 8px;
  font-size: 12px;
}

.mc-composer__hint {
  font-size: 11.5px;
  white-space: nowrap;
  /* 不允许被右侧「字数 + 发送」挤压截断（之前会显示成 "Enter 发送 · Sh..."） */
  flex-shrink: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mc-composer__right {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-shrink: 0;
}

.mc-composer__count {
  font-size: 11px;
  animation: mc-fade-in var(--mc-dur-fast) var(--mc-ease-out);
}

.mc-composer__count.is-near-limit {
  color: var(--td-error-color);
}

@media (max-width: 768px) {
  .mc-composer {
    padding: 10px 12px 12px;
  }

  .mc-composer__hint {
    display: none;
  }

  .mc-composer__footer {
    justify-content: flex-end;
  }
}

@media (prefers-reduced-motion: reduce) {
  .mc-composer__box {
    transition: none;
  }
}
</style>
