<script setup lang="ts">
import { ref } from 'vue'
import { Button as TButton } from 'tdesign-vue-next'

import { useConversationsStore } from '@/stores/conversations'

const convStore = useConversationsStore()
const text = ref('')
const lastTypingAt = ref(0)

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
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    send()
  }
}
</script>

<template>
  <footer class="mc-composer">
    <textarea
      v-model="text"
      class="mc-composer__input"
      placeholder="输入消息，Enter 发送，Shift+Enter 换行"
      maxlength="1000"
      rows="3"
      @keydown="onKeydown"
      @input="onInput"
    />
    <div class="mc-composer__footer">
      <span class="text-placeholder">{{ text.length }}/1000</span>
      <TButton theme="primary" size="small" :disabled="!text.trim()" @click="send">发送</TButton>
    </div>
  </footer>
</template>

<style scoped>
.mc-composer {
  flex-shrink: 0;
  border-top: 1px solid var(--td-component-border);
  background: var(--td-bg-color-container);
  padding: 10px 14px;
}

.mc-composer__input {
  width: 100%;
  border: none;
  outline: none;
  resize: none;
  background: transparent;
  color: var(--td-text-color-primary);
  font-family: inherit;
  font-size: 14px;
  line-height: 1.5;
}

.mc-composer__footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 6px;
  font-size: 12px;
}
</style>
