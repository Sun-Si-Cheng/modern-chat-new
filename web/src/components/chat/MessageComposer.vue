<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import UiButton from '@/components/ui/UiButton.vue'

import { mediaApi } from '@/api'
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

// ===== 图片上传 =====

const imageInput = ref<HTMLInputElement | null>(null)
const uploading = ref(false)

function pickImage() {
  imageInput.value?.click()
}

async function onImageChange(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = '' // 允许连续选同一个文件
  const convId = convStore.currentConvId
  if (!file || !convId) return
  if (file.size > 8 << 20) {
    void MessagePlugin.warning(t('message.imageTooLarge'))
    return
  }
  uploading.value = true
  try {
    const r = await mediaApi.upload('image', file, file.name)
    convStore.sendMedia(convId, 'image', r.url)
  } catch (err) {
    void MessagePlugin.error(String(err))
  } finally {
    uploading.value = false
  }
}

// ===== 语音录制 =====
//
// 采用「点击开始 / 再点结束」而不是「按住说话」：
// 按住模式在移动端容易与页面滚动冲突，且手指滑出按钮就丢了。
const recording = ref(false)
const recSeconds = ref(0)
let recorder: MediaRecorder | null = null
let chunks: Blob[] = []
let recTimer: number | undefined

const recLabel = computed(() => {
  const m = Math.floor(recSeconds.value / 60)
  const s = String(recSeconds.value % 60).padStart(2, '0')
  return `${m}:${s} · ${t('message.stopRecord')}`
})

function clearRecTimer() {
  if (recTimer !== undefined) {
    window.clearInterval(recTimer)
    recTimer = undefined
  }
}

async function toggleRecord() {
  if (recording.value) {
    recorder?.stop() // onstop 里统一收尾
    return
  }
  const convId = convStore.currentConvId
  if (!convId || uploading.value) return
  if (!navigator.mediaDevices?.getUserMedia || typeof MediaRecorder === 'undefined') {
    void MessagePlugin.warning(t('message.recordUnsupported'))
    return
  }

  let stream: MediaStream
  try {
    stream = await navigator.mediaDevices.getUserMedia({ audio: true })
  } catch {
    void MessagePlugin.error(t('message.micDenied'))
    return
  }

  chunks = []
  recorder = new MediaRecorder(stream)
  recorder.ondataavailable = (ev) => {
    if (ev.data.size > 0) chunks.push(ev.data)
  }
  recorder.onstop = async () => {
    stream.getTracks().forEach((tr) => tr.stop())
    clearRecTimer()
    recording.value = false

    const type = recorder?.mimeType || 'audio/webm'
    const blob = new Blob(chunks, { type })
    if (recSeconds.value < 1 || blob.size < 1024) {
      void MessagePlugin.warning(t('message.recordTooShort'))
      return
    }
    uploading.value = true
    try {
      const r = await mediaApi.upload('audio', blob, 'voice.webm')
      convStore.sendMedia(convId, 'audio', r.url)
    } catch (err) {
      void MessagePlugin.error(String(err))
    } finally {
      uploading.value = false
    }
  }

  recorder.start()
  recording.value = true
  recSeconds.value = 0
  recTimer = window.setInterval(() => {
    recSeconds.value += 1
    // 60 秒自动停止，避免无声录制把文件撑大
    if (recSeconds.value >= 60) recorder?.stop()
  }, 1000)
}

onBeforeUnmount(() => {
  clearRecTimer()
  if (recording.value) {
    recorder?.stop()
    recording.value = false
  }
})

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
        <div class="mc-composer__tools">
          <!-- 图片 -->
          <button
            type="button"
            class="mc-tool"
            :disabled="uploading || recording"
            :title="t('message.sendImage')"
            :aria-label="t('message.sendImage')"
            @click="pickImage"
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true">
              <rect
                x="3"
                y="4.5"
                width="18"
                height="15"
                rx="2.5"
                stroke="currentColor"
                stroke-width="1.6"
              />
              <circle cx="8.5" cy="10" r="1.6" fill="currentColor" />
              <path
                d="M4 17.5l4.8-4.3a2 2 0 0 1 2.7 0L20 20"
                stroke="currentColor"
                stroke-width="1.6"
                stroke-linecap="round"
              />
            </svg>
          </button>
          <!-- 语音 -->
          <button
            type="button"
            class="mc-tool"
            :class="{ 'is-recording': recording }"
            :disabled="uploading"
            :title="recording ? t('message.stopRecord') : t('message.sendVoice')"
            :aria-label="recording ? t('message.stopRecord') : t('message.sendVoice')"
            @click="toggleRecord"
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true">
              <rect
                x="9"
                y="3"
                width="6"
                height="11"
                rx="3"
                stroke="currentColor"
                stroke-width="1.6"
              />
              <path
                d="M5.5 11.5a6.5 6.5 0 0 0 13 0M12 18v3"
                stroke="currentColor"
                stroke-width="1.6"
                stroke-linecap="round"
              />
            </svg>
          </button>

          <span v-if="recording" class="mc-composer__rec">
            <i class="mc-composer__dot" />{{ recLabel }}
          </span>
          <span v-else-if="uploading" class="mc-composer__hint text-placeholder">
            {{ t('message.uploading') }}
          </span>
          <!-- 完整说明放 title，行内只留不会截断的短提示（面板窄时 "Enter 发送 · Shift..." 会断成两截） -->
          <span v-else class="mc-composer__hint text-placeholder" :title="t('message.sendHint')">
            {{ t('message.sendHintShort') }}
          </span>
        </div>

        <input
          ref="imageInput"
          type="file"
          accept="image/png,image/jpeg,image/webp,image/gif"
          class="mc-composer__file"
          @change="onImageChange"
        />

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

/* ===== 工具按钮（图片 / 语音）===== */
.mc-composer__tools {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.mc-tool {
  width: 26px;
  height: 26px;
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: var(--mc-radius-sm);
  background: transparent;
  color: var(--td-text-color-secondary);
  cursor: pointer;
  transition:
    background-color var(--mc-dur-instant) var(--mc-ease-out),
    color var(--mc-dur-instant) var(--mc-ease-out);
}

.mc-tool:hover:not(:disabled) {
  background: var(--td-bg-color-container-hover);
  color: var(--td-text-color-primary);
}

.mc-tool:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.mc-tool.is-recording {
  color: var(--td-error-color);
  background: color-mix(in srgb, var(--td-error-color) 14%, transparent);
}

.mc-composer__file {
  display: none;
}

.mc-composer__rec {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: var(--mc-fs-sm);
  color: var(--td-error-color);
  white-space: nowrap;
}

.mc-composer__dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentcolor;
  animation: mc-rec-pulse 1s ease-in-out infinite;
}

@keyframes mc-rec-pulse {
  50% {
    opacity: 0.25;
  }
}

@media (prefers-reduced-motion: reduce) {
  .mc-composer__dot {
    animation: none;
  }
}

.mc-composer__hint {
  font-size: var(--mc-fs-sm);
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
