<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import UiButton from '@/components/ui/UiButton.vue'

import AvatarBadge from './AvatarBadge.vue'
import { mediaApi, stickerApi, type Sticker } from '@/api'
import { useAppI18n } from '@/composables/useAppI18n'
import { useAuthStore } from '@/stores/auth'
import { useConversationsStore } from '@/stores/conversations'
import { useSiteStore } from '@/stores/site'
import type { ConversationMember } from '@/types/proto'

const convStore = useConversationsStore()
const auth = useAuthStore()
const siteStore = useSiteStore()
const { t } = useAppI18n()

/** 消息长度上限跟随后台配置；达到 90% 时计数变警示色 */
const maxMessageLength = computed(() => siteStore.config.maxMessageLength)
/** 正在编辑的消息：非 null 时输入区进入编辑模式 */
const editing = computed(() => convStore.editingMessage)

/** 我在当前会话的成员行：用于判断是否被禁言 */
const myMember = computed(() => {
  const id = convStore.currentConvId
  if (!id) return null
  return (convStore.membersByConv[id] ?? []).find((m) => m.userId === auth.user?.id) ?? null
})

const mutedUntil = computed(() => {
  const v = myMember.value?.mutedUntil
  if (!v) return null
  return new Date(v).getTime() > Date.now() ? v : null
})

/** 群全员禁言：普通成员不能发言，群主/管理员不受影响 */
const muteAllBlocked = computed(() => {
  const id = convStore.currentConvId
  const c = id ? convStore.conversations.find((x) => x.id === id) : null
  if (!c || c.type !== 2 || !c.muteAll) return false
  const role = myMember.value?.role
  return !role || role === 'member'
})

const canSpeak = computed(() => !mutedUntil.value && !muteAllBlocked.value)

function fmtMuted(ts: string): string {
  const d = new Date(ts)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getMonth() + 1}/${d.getDate()} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

// 群成员变更（被禁言/解禁）后刷新成员缓存
watch(
  () => convStore.convChangeTick,
  () => {
    const id = convStore.currentConvId
    if (id) void convStore.loadMembers(id, true)
  },
)

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
  // 编辑态不回写草稿，否则会把原本打了一半的内容覆盖成被编辑消息的正文
  if (convStore.currentConvId && !editing.value) convStore.setDraft(convStore.currentConvId, v)
})

/** 进入编辑态前的正文：取消编辑要还原它，而不是把用户的草稿清空 */
const draftBackup = ref('')

// 进入编辑态：把正文填进输入框并聚焦
watch(editing, (m) => {
  if (!m) return
  draftBackup.value = text.value
  text.value = m.content
  stickerOpen.value = false
  void nextTick(() => {
    resize()
    inputRef.value?.focus()
  })
})

function cancelEdit() {
  convStore.setEditingMessage(null)
  text.value = draftBackup.value
  draftBackup.value = ''
}

onMounted(() => {
  resize()
  // @ 选人需要成员列表；进会话时预热一次，输入 @ 就不用等接口
  if (convStore.currentConvId) void convStore.loadMembers(convStore.currentConvId)
})

// ===== 图片上传 =====

const imageInput = ref<HTMLInputElement | null>(null)
const uploading = ref(false)

function pickImage() {
  imageInput.value?.click()
}

/**
 * 发送前压缩：手机直出照片动辄 3-5 MB，实际显示宽度不过 240px，
 * 直传是纯粹的带宽与存储浪费。这里按最大边 1600 缩到 webp，
 * 实测常见照片能压掉 70-90%。
 *
 * 几点保守处理：
 *  - 小图（<300 KB）不动，避免二次编码反而变大
 *  - 压缩结果比原图大就用原图
 *  - 任何一步失败都回退原图，不能因为压缩失败就发不出去
 */
const COMPRESS_MAX_EDGE = 1600
const COMPRESS_QUALITY = 0.82
const COMPRESS_SKIP_BELOW = 300 * 1024

async function compressImage(file: File): Promise<Blob> {
  if (file.size < COMPRESS_SKIP_BELOW || !file.type.startsWith('image/')) return file
  try {
    const bitmap = await createImageBitmap(file)
    const scale = Math.min(1, COMPRESS_MAX_EDGE / Math.max(bitmap.width, bitmap.height))
    const w = Math.max(1, Math.round(bitmap.width * scale))
    const h = Math.max(1, Math.round(bitmap.height * scale))

    const canvas = document.createElement('canvas')
    canvas.width = w
    canvas.height = h
    const ctx = canvas.getContext('2d')
    if (!ctx) {
      bitmap.close?.()
      return file
    }
    ctx.drawImage(bitmap, 0, 0, w, h)
    bitmap.close?.()

    const blob = await new Promise<Blob | null>((resolve) =>
      canvas.toBlob(resolve, 'image/webp', COMPRESS_QUALITY),
    )
    if (!blob || blob.size >= file.size) return file
    return blob
  } catch {
    return file // 浏览器不支持 webp / 解码失败：原图上传
  }
}

async function onImageChange(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = '' // 允许连续选同一个文件
  const convId = convStore.currentConvId
  if (!file || !convId) return
  // 图片上限跟随后台配置（提示文案里的数字也一起换）
  const maxImage = siteStore.config.maxImageMB << 20
  if (file.size > maxImage) {
    void MessagePlugin.warning(t('message.imageTooLargeMB', { mb: siteStore.config.maxImageMB }))
    return
  }
  uploading.value = true
  try {
    const payload = await compressImage(file)
    const name = payload === file ? file.name : file.name.replace(/\.\w+$/, '') + '.webp'
    const r = await mediaApi.upload('image', payload, name)
    sendMediaMsg(convId, 'image', r.url)
  } catch (err) {
    void MessagePlugin.error(String(err))
  } finally {
    uploading.value = false
  }
}

// ===== 视频上传 =====
//
// 不做前端转码：浏览器里转视频必然要 WebAssembly + 长时间占用主线程，
// 收益远不如「压缩图片」明显。体积上限跟随后台配置（默认 30 MB）。
const videoInput = ref<HTMLInputElement | null>(null)

function pickVideo() {
  videoInput.value?.click()
}

async function onVideoChange(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = '' // 允许连续选同一个文件
  const convId = convStore.currentConvId
  if (!file || !convId) return
  const maxVideo = siteStore.config.maxVideoMB << 20
  if (file.size > maxVideo) {
    void MessagePlugin.warning(t('message.videoTooLargeMB', { mb: siteStore.config.maxVideoMB }))
    return
  }
  uploading.value = true
  try {
    const r = await mediaApi.upload('video', file, file.name || 'video.mp4')
    sendMediaMsg(convId, 'video', r.url)
  } catch (err) {
    void MessagePlugin.error(String(err))
  } finally {
    uploading.value = false
  }
}

// ===== 表情包 =====
//
// 表情是"收藏关系 + URL"，文件本身走 upload(kind=sticker)。
// 点一下就直接发（和微信一致），不做"插入到输入框再发送"。

const stickers = ref<Sticker[]>([])
const stickerOpen = ref(false)
const stickerUploading = ref(false)
const stickerInput = ref<HTMLInputElement | null>(null)

async function loadStickers() {
  try {
    stickers.value = await stickerApi.list()
  } catch {
    // 拉取失败留着空面板，不阻断聊天
  }
}

function toggleStickerPanel() {
  stickerOpen.value = !stickerOpen.value
  if (stickerOpen.value) void loadStickers()
}

/**
 * 媒体发送统一入口：图片 / 语音 / 表情都要带上当前引用状态。
 *
 * 之前三处各自直接调 sendMedia，导致「先引用再发图」时引用被静默丢掉，
 * 而且引用条还挂在输入框上，用户以为引用发出去了。
 */
function sendMediaMsg(
  convId: number,
  msgType: 'image' | 'audio' | 'sticker' | 'video',
  url: string,
) {
  convStore.sendMedia(convId, msgType, url, {
    replyToId: convStore.replyTarget?.id || undefined,
  })
  convStore.setReplyTarget(null)
  // 媒体消息没有 @ 语义，清掉避免残留到下一次文本发送
  mentionIds.value = []
  mentionQuery.value = null
}

function sendSticker(s: Sticker) {
  const convId = convStore.currentConvId
  if (!convId || !s.url) return
  sendMediaMsg(convId, 'sticker', s.url)
  stickerOpen.value = false
}

function pickStickerFile() {
  stickerInput.value?.click()
}

async function onStickerChange(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  if (file.size > 2 << 20) {
    void MessagePlugin.warning(t('message.stickerTooLarge'))
    return
  }
  stickerUploading.value = true
  try {
    const up = await mediaApi.upload('sticker', file, file.name)
    if (!stickers.value.some((s) => s.url === up.url)) {
      const row = await stickerApi.add(up.url)
      stickers.value = [row, ...stickers.value]
    }
  } catch (err) {
    void MessagePlugin.error(String(err))
  } finally {
    stickerUploading.value = false
  }
}

async function removeSticker(s: Sticker) {
  try {
    await stickerApi.remove(s.id)
    stickers.value = stickers.value.filter((x) => x.id !== s.id)
  } catch (err) {
    void MessagePlugin.error(String(err))
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
/** 组件卸载（切会话会重建整个消息区）导致的停止：录音作废，不再发送 */
let recordingCancelled = false

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
  recordingCancelled = false
  recorder.onstop = async () => {
    stream.getTracks().forEach((tr) => tr.stop())
    clearRecTimer()
    recording.value = false

    // 卸载时被动停止：直接丢弃，否则这段录音会被发到上一个会话
    if (recordingCancelled) {
      recordingCancelled = false
      chunks = []
      return
    }

    const type = recorder?.mimeType || 'audio/webm'
    const blob = new Blob(chunks, { type })
    if (recSeconds.value < 1 || blob.size < 1024) {
      void MessagePlugin.warning(t('message.recordTooShort'))
      return
    }
    uploading.value = true
    try {
      const r = await mediaApi.upload('audio', blob, 'voice.webm')
      sendMediaMsg(convId, 'audio', r.url)
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
    recordingCancelled = true
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
  detectMention()
}

function send() {
  const content = text.value.trim()
  const convId = convStore.currentConvId
  if (!content || !convId || !canSpeak.value) return
  // 编辑模式：只提交正文，引用 / @ / 转发语义保持不变
  if (editing.value) {
    const ok = convStore.editMessage(editing.value.id, content)
    if (!ok) {
      void MessagePlugin.error(t('message.editFailed'))
      return
    }
    convStore.setEditingMessage(null)
    text.value = ''
    return
  }
  // 只保留文本里仍然存在的 @：选了人之后又把「@昵称」删掉的，不该还提醒对方
  const activeMentions = mentionIds.value.filter((id) => {
    const name = convStore.memberNickname(convId, id)
    return name !== '' && content.includes(`@${name}`)
  })
  convStore.sendText(convId, content, {
    replyToId: convStore.replyTarget?.id || undefined,
    mentions: activeMentions.length ? activeMentions : undefined,
  })
  text.value = ''
  mentionIds.value = []
  mentionQuery.value = null
  convStore.setReplyTarget(null)
}

// ===== 引用 =====

/** 引用条上的名字：自己显示「我」，别人查成员缓存 */
const replyName = computed(() => {
  const r = convStore.replyTarget
  if (!r) return ''
  if (r.senderId === auth.user?.id) return t('message.quoteMe')
  const convId = convStore.currentConvId
  return (convId ? convStore.memberNickname(convId, r.senderId) : '') || '…'
})

const replyText = computed(() => {
  const r = convStore.replyTarget
  if (!r) return ''
  if (r.msgType === 'image') return t('message.quoteImage')
  if (r.msgType === 'audio') return t('message.quoteAudio')
  // 与消息列表里的引用块保持一致，否则引用表情时这里会显示一串 /uploads/... 地址
  if (r.msgType === 'sticker') return t('message.quoteSticker')
  return [...r.content].slice(0, 40).join('')
})

// ===== @ 提醒 =====

const mentionIds = ref<number[]>([])
/** null = 未在输入 @；'' = 刚打 @ 还没输入名字 */
const mentionQuery = ref<string | null>(null)

const mentionCandidates = computed(() => {
  const q = mentionQuery.value
  const convId = convStore.currentConvId
  // @ 只对群聊有意义
  if (q === null || !convId || convStore.currentConv?.type !== 2) return []
  const kw = q.trim().toLowerCase()
  return (convStore.membersByConv[convId] ?? [])
    .filter((m) => m.user.id !== auth.user?.id)
    .filter((m) => !kw || m.user.nickname.toLowerCase().includes(kw))
    .slice(0, 8)
})

/** 光标前是否是「@xxx」形式：只认最后一个 @ 且不含空格 */
function detectMention() {
  if (convStore.currentConv?.type !== 2) {
    mentionQuery.value = null
    return
  }
  const el = inputRef.value
  const pos = el?.selectionStart ?? text.value.length
  const before = text.value.slice(0, pos)
  const m = /@([^\s@]{0,12})$/.exec(before)
  mentionQuery.value = m ? (m[1] ?? '') : null
  // 成员缓存可能已被 conv_change 清掉（群里刚加过人），这里按需补拉一次
  const convId = convStore.currentConvId
  if (m && convId && !convStore.membersByConv[convId]) void convStore.loadMembers(convId)
}

function pickMention(m: ConversationMember) {
  const el = inputRef.value
  const pos = el?.selectionStart ?? text.value.length
  const before = text.value.slice(0, pos)
  const match = /@([^\s@]{0,12})$/.exec(before)
  if (!match) return
  const start = pos - match[0].length
  const inserted = `@${m.user.nickname} `
  text.value = text.value.slice(0, start) + inserted + text.value.slice(pos)
  if (!mentionIds.value.includes(m.user.id)) mentionIds.value = [...mentionIds.value, m.user.id]
  mentionQuery.value = null
  void nextTick(() => {
    el?.focus()
    const p = start + inserted.length
    el?.setSelectionRange(p, p)
  })
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
      <!-- 禁言提示：到期自动恢复，无需手动操作 -->
      <div v-if="mutedUntil" class="mc-composer__mute">
        {{ t('group.mutedHint', { time: fmtMuted(mutedUntil) }) }}
      </div>
      <div v-else-if="muteAllBlocked" class="mc-composer__mute">
        {{ t('group.muteAllHint') }}
      </div>

      <!-- 编辑态：提示当前正在改哪条消息，取消后清空输入 -->
      <div v-if="editing" class="mc-composer__edit">
        <span class="mc-composer__edit-label">{{ t('message.editing') }}</span>
        <button
          type="button"
          class="mc-composer__quote-close"
          :aria-label="t('common.cancel')"
          @click="cancelEdit"
        >
          <svg width="11" height="11" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
          </svg>
        </button>
      </div>

      <!-- 引用条：右键「引用」后出现，发送/取消即消失 -->
      <div v-if="convStore.replyTarget" class="mc-composer__quote">
        <span class="mc-composer__quote-name">{{ replyName }}：</span>
        <span class="mc-composer__quote-text">{{ replyText }}</span>
        <button
          type="button"
          class="mc-composer__quote-close"
          :aria-label="t('common.cancel')"
          @click="convStore.setReplyTarget(null)"
        >
          <svg width="11" height="11" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
          </svg>
        </button>
      </div>

      <!-- 表情面板：点表情按钮展开，点一下直接发送 -->
      <div v-if="stickerOpen" class="mc-composer__sticker">
        <div class="mc-composer__sticker-head">
          <span class="mc-composer__sticker-title">{{ t('message.stickers') }}</span>
          <button
            type="button"
            class="mc-composer__sticker-add"
            :disabled="stickerUploading"
            @click="pickStickerFile"
          >
            {{ stickerUploading ? t('message.uploading') : t('message.addSticker') }}
          </button>
        </div>

        <div v-if="stickers.length" class="mc-composer__sticker-grid">
          <button
            v-for="s in stickers"
            :key="s.id"
            type="button"
            class="mc-composer__sticker-item"
            :title="t('message.stickerDeleteHint')"
            @click="sendSticker(s)"
            @contextmenu.prevent="removeSticker(s)"
          >
            <img :src="s.url" alt="" decoding="async" />
          </button>
        </div>
        <p v-else class="mc-composer__sticker-empty text-placeholder">
          {{ t('message.stickerEmpty') }}
        </p>
      </div>

      <!-- @ 选人：输入 @ 后浮在输入框上方 -->
      <div v-if="mentionCandidates.length" class="mc-composer__mention" role="listbox">
        <button
          v-for="m in mentionCandidates"
          :key="m.user.id"
          type="button"
          class="mc-composer__mention-item"
          role="option"
          :aria-selected="false"
          @click="pickMention(m)"
        >
          <AvatarBadge
            :name="m.user.nickname"
            :src="m.user.avatar"
            :color="m.user.avatarColor"
            :size="22"
          />
          <span class="mc-composer__mention-name">{{ m.user.nickname }}</span>
        </button>
      </div>

      <textarea
        ref="inputRef"
        v-model="text"
        class="mc-composer__input"
        :placeholder="editing ? t('message.editPlaceholder') : t('message.composerPlaceholder')"
        :maxlength="maxMessageLength"
        :disabled="!canSpeak"
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
            :disabled="uploading || recording || !!editing || !canSpeak"
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
          <!-- 视频 -->
          <button
            type="button"
            class="mc-tool"
            :disabled="uploading || recording || !!editing || !canSpeak"
            :title="t('message.sendVideo')"
            :aria-label="t('message.sendVideo')"
            @click="pickVideo"
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true">
              <rect
                x="3"
                y="6"
                width="12"
                height="12"
                rx="2.5"
                stroke="currentColor"
                stroke-width="1.6"
              />
              <path
                d="M15 10.6l4.1-2.3a.8.8 0 0 1 1.2.7v6a.8.8 0 0 1-1.2.7L15 13.4"
                stroke="currentColor"
                stroke-width="1.6"
                stroke-linejoin="round"
              />
            </svg>
          </button>
          <!-- 表情 -->
          <button
            type="button"
            class="mc-tool"
            :class="{ 'is-active': stickerOpen }"
            :disabled="uploading || recording || !!editing || !canSpeak"
            :title="t('message.stickers')"
            :aria-label="t('message.stickers')"
            :aria-expanded="stickerOpen"
            @click="toggleStickerPanel"
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true">
              <circle cx="12" cy="12" r="8.5" stroke="currentColor" stroke-width="1.6" />
              <circle cx="9" cy="10" r="1.1" fill="currentColor" />
              <circle cx="15" cy="10" r="1.1" fill="currentColor" />
              <path
                d="M8.4 14.2c.9 1.3 2.1 2 3.6 2s2.7-.7 3.6-2"
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
            :disabled="uploading || !!editing || !canSpeak"
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
        <input
          ref="videoInput"
          type="file"
          accept="video/mp4,video/webm,video/quicktime,video/x-m4v"
          class="mc-composer__file"
          @change="onVideoChange"
        />
        <input
          ref="stickerInput"
          type="file"
          accept="image/png,image/jpeg,image/webp,image/gif"
          class="mc-composer__file"
          @change="onStickerChange"
        />

        <div class="mc-composer__right">
          <span
            v-if="text.length"
            class="mc-composer__count text-placeholder num"
            :class="{ 'is-near-limit': text.length >= maxMessageLength * 0.9 }"
          >
            {{ text.length }}/{{ maxMessageLength }}
          </span>
          <UiButton theme="primary" size="small" :disabled="!text.trim() || !canSpeak" @click="send">
            {{ editing ? t('common.save') : t('message.send') }}
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
  position: relative;
  border: 1px solid var(--td-component-border);
  border-radius: var(--mc-radius-lg);
  background: var(--td-bg-color-container);
  padding: 12px 14px 10px;
  box-shadow: var(--mc-shadow-1);
  transition:
    border-color var(--mc-dur-fast) var(--mc-ease-out),
    box-shadow var(--mc-dur-fast) var(--mc-ease-out);
}

/* ===== 禁言提示 ===== */
.mc-composer__mute {
  margin-bottom: 8px;
  padding: 5px 9px;
  border-left: 2px solid var(--td-error-color, #d54941);
  border-radius: 4px;
  background: var(--td-bg-color-secondarycontainer);
  font-size: var(--mc-fs-sm);
  line-height: 1.4;
  color: var(--td-error-color, #d54941);
}

/* ===== 编辑态 ===== */
.mc-composer__edit {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  padding: 5px 9px;
  border-left: 2px solid var(--td-warning-color, #ed7b2f);
  border-radius: 4px;
  background: var(--td-bg-color-secondarycontainer);
  font-size: var(--mc-fs-sm);
  line-height: 1.4;
}

.mc-composer__edit-label {
  flex: 1;
  min-width: 0;
  color: var(--td-text-color-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* ===== 引用条 ===== */
.mc-composer__quote {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-bottom: 8px;
  padding: 5px 9px;
  border-left: 2px solid var(--td-brand-color);
  border-radius: 4px;
  background: var(--td-bg-color-secondarycontainer);
  font-size: var(--mc-fs-sm);
  line-height: 1.4;
}

.mc-composer__quote-name {
  flex-shrink: 0;
  color: var(--td-brand-color);
}

.mc-composer__quote-text {
  flex: 1;
  min-width: 0;
  color: var(--td-text-color-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mc-composer__quote-close {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  padding: 0;
  border: none;
  border-radius: 50%;
  background: transparent;
  color: var(--td-text-color-placeholder);
  cursor: pointer;
}

.mc-composer__quote-close:hover {
  background: var(--td-bg-color-container-hover);
  color: var(--td-text-color-primary);
}

/* ===== @ 选人浮层 ===== */
.mc-composer__mention {
  position: absolute;
  left: 12px;
  right: 12px;
  bottom: calc(100% + 6px);
  z-index: 20;
  max-height: 208px;
  overflow-y: auto;
  padding: 4px;
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-md);
  background: var(--mc-material);
  backdrop-filter: var(--mc-blur);
  box-shadow: var(--mc-elev-2);
}

.mc-composer__mention-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 6px 8px;
  border: none;
  border-radius: var(--mc-radius-sm);
  background: transparent;
  font: inherit;
  font-size: var(--mc-fs-body);
  color: var(--td-text-color-primary);
  text-align: left;
  cursor: pointer;
}

.mc-composer__mention-item:hover {
  background: var(--td-bg-color-container-hover);
}

.mc-composer__mention-name {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 表情按钮的「展开中」态：和录音一样给个底色，表明面板由它控制 */
.mc-tool.is-active {
  color: var(--td-brand-color);
  background: color-mix(in srgb, var(--td-brand-color) 14%, transparent);
}

/* ===== 表情面板 ===== */
.mc-composer__sticker {
  position: absolute;
  left: 12px;
  right: 12px;
  bottom: calc(100% + 6px);
  z-index: 20;
  padding: 10px;
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-md);
  background: var(--mc-material);
  backdrop-filter: var(--mc-blur);
  box-shadow: var(--mc-elev-2);
}

.mc-composer__sticker-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}

.mc-composer__sticker-title {
  font-size: var(--mc-fs-sm);
  color: var(--td-text-color-secondary);
}

.mc-composer__sticker-add {
  padding: 3px 8px;
  border: 1px solid var(--td-component-border);
  border-radius: var(--mc-radius-sm);
  background: transparent;
  font: inherit;
  font-size: var(--mc-fs-sm);
  color: var(--td-text-color-primary);
  cursor: pointer;
  transition: background-color var(--mc-dur-instant) var(--mc-ease-out);
}

.mc-composer__sticker-add:hover:not(:disabled) {
  background: var(--td-bg-color-container-hover);
}

.mc-composer__sticker-add:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.mc-composer__sticker-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(64px, 1fr));
  gap: 6px;
  max-height: 208px;
  overflow-y: auto;
}

.mc-composer__sticker-item {
  display: flex;
  align-items: center;
  justify-content: center;
  aspect-ratio: 1;
  padding: 4px;
  border: none;
  border-radius: var(--mc-radius-sm);
  background: transparent;
  cursor: pointer;
  transition: background-color var(--mc-dur-instant) var(--mc-ease-out);
}

.mc-composer__sticker-item:hover {
  background: var(--td-bg-color-container-hover);
}

.mc-composer__sticker-item img {
  max-width: 100%;
  max-height: 100%;
  object-fit: contain;
}

.mc-composer__sticker-empty {
  margin: 12px 0;
  font-size: var(--mc-fs-sm);
  text-align: center;
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
