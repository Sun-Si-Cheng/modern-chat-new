<script setup lang="ts">
import UiSelect from '@/components/ui/UiSelect.vue'
import UiDialog from '@/components/ui/UiDialog.vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiCheckbox from '@/components/ui/UiCheckbox.vue'
import UiInput from '@/components/ui/UiInput.vue'
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
import { SearchIcon } from 'tdesign-icons-vue-next'

import AvatarBadge from './AvatarBadge.vue'
import ContextMenu, { type ContextMenuItem } from '@/components/common/ContextMenu.vue'
import PluginPanel from '@/components/declarative/PluginPanel.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import { convApi, mediaApi, searchApi, userApi, type ConversationFile } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { useConversationsStore } from '@/stores/conversations'
import { useFriendsStore } from '@/stores/friends'
import { useLayoutStore } from '@/stores/layout'
import { useSiteStore } from '@/stores/site'
import { useWsStore } from '@/stores/ws'
import type { ConversationMember, SearchHit } from '@/types/proto'

const auth = useAuthStore()
const convStore = useConversationsStore()
const friendsStore = useFriendsStore()
const layout = useLayoutStore()
const siteStore = useSiteStore()
const ws = useWsStore()
const { t, te } = useAppI18n()

const conv = computed(() => convStore.currentConv)
const isGroup = computed(() => conv.value?.type === 2)
const isOwner = computed(() => conv.value?.ownerId === auth.user?.id)
const myMember = computed(() => members.value.find((m) => m.userId === auth.user?.id))
const isAdmin = computed(() => myMember.value?.role === 'admin')
/** 群主与管理员都能踢人 / 禁言；具体边界在操作处再判断 */
const canManage = computed(() => isOwner.value || isAdmin.value)

// ===== 会话内搜索 =====
// 搜索入口从消息区标题栏迁入详情面板（L2：针对当前会话的动作）。
// Ctrl/Cmd+F 由 MessagePane 转成 layout.requestDetailSearch()，这里聚焦输入框。

const searchKw = ref('')
const searching = ref(false)
const searchResults = ref<SearchHit[]>([])
const searchInputRef = ref<InstanceType<typeof UiInput> | null>(null)
let searchTimer: number | undefined

watch(
  () => conv.value?.id,
  () => {
    // 切换会话：清空上一会话的搜索状态
    window.clearTimeout(searchTimer)
    searchKw.value = ''
    searchResults.value = []
    searching.value = false
  },
)

watch(
  () => layout.detailSearchTick.n,
  () => void nextTick(() => searchInputRef.value?.focus()),
)

function onSearchInput() {
  window.clearTimeout(searchTimer)
  const kw = searchKw.value.trim()
  const c = conv.value
  if (!kw || !c) {
    searchResults.value = []
    return
  }
  searchTimer = window.setTimeout(async () => {
    searching.value = true
    try {
      const rows = await searchApi.messages(kw, c.id, 30)
      // 输入可能已经变了：只接受与当前关键词/会话匹配的结果
      if (conv.value?.id === c.id && searchKw.value.trim() === kw) {
        searchResults.value = rows ?? []
      }
    } catch (e) {
      void MessagePlugin.error(te(e))
    } finally {
      searching.value = false
    }
  }, 250)
}

async function openSearchHit(hit: SearchHit) {
  const c = conv.value
  if (!c) return
  searchKw.value = ''
  searchResults.value = []
  const ok = await convStore.openConversationAtMessage(c.id, hit.messageId)
  if (!ok) void MessagePlugin.warning(t('message.searchJumpOld'))
}

/** 搜索结果里的消息正文：媒体消息不能直接展示 URL */
function searchPreview(hit: SearchHit): string {
  if (hit.msgType === 'image') return t('message.quoteImage')
  if (hit.msgType === 'audio') return t('message.quoteAudio')
  if (hit.msgType === 'sticker') return t('message.quoteSticker')
  if (hit.msgType !== 'text') return t('message.unknownType', { type: hit.msgType })
  return hit.content
}

function escapeRe(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/** 把内容按关键词切成普通段/命中段，渲染时用 <mark> 高亮 */
function searchParts(content: string): { t: string; hit: boolean }[] {
  const kw = searchKw.value.trim()
  if (!kw) return [{ t: content, hit: false }]
  const re = new RegExp(`(${escapeRe(kw)})`, 'gi')
  return content
    .split(re)
    .filter((s) => s !== '')
    .map((s) => ({ t: s, hit: s.toLowerCase() === kw.toLowerCase() }))
}

// 详情抽屉收起 / 切到设置页时组件会卸载：定时器不清就会在 250ms 后
// 继续发一次搜索请求并写已卸载组件的 ref
onBeforeUnmount(() => window.clearTimeout(searchTimer))

function fmtSearchTime(ts: string): string {
  const d = new Date(ts)
  const hm = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  return `${d.getMonth() + 1}/${d.getDate()} ${hm}`
}

// ===== 拉黑（仅单聊）=====
const peerBlocked = ref(false)
const blockBusy = ref(false)

watch(
  () => conv.value?.id,
  async () => {
    peerBlocked.value = false
    const peerId = conv.value?.peer?.id
    if (!peerId || isGroup.value) return
    try {
      peerBlocked.value = (await userApi.blockStatus(peerId)).blocked
    } catch {
      /* 忽略：查询失败时按未拉黑展示，操作时服务端会再校验 */
    }
  },
  { immediate: true },
)

async function toggleBlock() {
  const peer = conv.value?.peer
  if (!peer || blockBusy.value) return
  const next = !peerBlocked.value
  const confirmed = await new Promise<boolean>((resolve) => {
    const dlg = DialogPlugin.confirm({
      header: next ? t('group.blockTitle') : t('group.unblockTitle'),
      body: next
        ? t('group.blockBody', { name: peer.nickname })
        : t('group.unblockBody', { name: peer.nickname }),
      theme: next ? 'warning' : 'default',
      onConfirm: () => {
        dlg.destroy()
        resolve(true)
      },
      onClose: () => {
        dlg.destroy()
        resolve(false)
      },
    })
  })
  if (!confirmed) return
  blockBusy.value = true
  try {
    if (next) await userApi.block(peer.id)
    else await userApi.unblock(peer.id)
    peerBlocked.value = next
    void MessagePlugin.success(next ? t('group.blockOk') : t('group.unblockOk'))
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    blockBusy.value = false
  }
}

// ===== 全员禁言 =====
const mutingAll = ref(false)

async function toggleMuteAll() {
  const c = conv.value
  if (!c || mutingAll.value) return
  mutingAll.value = true
  try {
    await convApi.setMuteAll(c.id, !c.muteAll)
    await convStore.loadConversations()
    void MessagePlugin.success(c.muteAll ? t('group.muteAllOff') : t('group.muteAllOn'))
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    mutingAll.value = false
  }
}

const members = ref<ConversationMember[]>([])
/** 成员列表加载失败：给一次明确的重试入口，而不是让面板空着 */
const membersError = ref('')

watch(
  () => conv.value?.id,
  async (id) => {
    members.value = []
    membersError.value = ''
    if (id === undefined || !isGroup.value) return
    try {
      const rows = await convApi.members(id)
      // 接口慢、用户已经切到别的会话：丢弃迟到的响应，否则会把 A 群成员画到 B 群
      if (conv.value?.id !== id) return
      members.value = rows
    } catch (e) {
      if (conv.value?.id === id) membersError.value = te(e)
    }
  },
  { immediate: true },
)

// 群成员变动（他人入群 / 被踢 / 转让）后刷新成员列表
watch(
  () => conv.value?.memberCount,
  async (count, prev) => {
    if (prev === undefined || count === prev || !conv.value || !isGroup.value) return
    await refreshMembers().catch(() => undefined)
  },
)

// ===== 群管理 =====

// ===== 群文件 =====
const files = ref<ConversationFile[]>([])
const fileInput = ref<HTMLInputElement | null>(null)
const uploadingFile = ref(false)

watch(
  () => conv.value?.id,
  async (id) => {
    files.value = []
    if (id === undefined || !isGroup.value) return
    try {
      const rows = await convApi.files(id)
      if (conv.value?.id !== id) return
      files.value = rows
    } catch {
      /* 忽略：列表为空即可 */
    }
  },
  { immediate: true },
)

// 群成员/群名/公告变更（含别人加人、改公告）：详情面板是常驻组件，
// 不主动重拉就会一直显示旧数据
watch(
  () => convStore.convChangeTick,
  async () => {
    const id = conv.value?.id
    if (!id || !isGroup.value) return
    try {
      const [rows, list] = await Promise.all([convApi.members(id), convApi.files(id)])
      if (conv.value?.id !== id) return
      members.value = rows
      files.value = list
    } catch {
      /* 忽略：保留旧列表 */
    }
  },
)

function pickFile() {
  fileInput.value?.click()
}

function fmtSize(n: number): string {
  if (!n) return '0 B'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

function canDeleteFile(f: ConversationFile): boolean {
  return f.uploaderId === auth.user?.id || isOwner.value
}

async function onFileChange(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = '' // 允许连续选同一个文件
  const c = conv.value
  if (!file || !c) return
  if (file.size > 20 << 20) {
    void MessagePlugin.warning(t('group.fileTooLarge'))
    return
  }
  uploadingFile.value = true
  try {
    const up = await mediaApi.upload('file', file, file.name)
    await convApi.addFile(c.id, { name: file.name, url: up.url, size: file.size })
    // 上传期间用户可能切走了会话：只有还停在这个会话时才覆盖列表，
    // 否则会把新会话的文件列表刷成旧会话的
    if (conv.value?.id === c.id) files.value = await convApi.files(c.id)
  } catch (err) {
    void MessagePlugin.error(String(err))
  } finally {
    uploadingFile.value = false
  }
}

async function removeFile(f: ConversationFile) {
  const c = conv.value
  if (!c) return
  // 删除不可撤销（记录的只是目录项，文件本体仍占存储），先确认一次
  const confirmed = await new Promise<boolean>((resolve) => {
    const dlg = DialogPlugin.confirm({
      header: t('group.fileDelete'),
      body: t('group.fileDeleteConfirm', { name: f.name }),
      theme: 'warning',
      onConfirm: () => {
        dlg.destroy()
        resolve(true)
      },
      onClose: () => {
        dlg.destroy()
        resolve(false)
      },
    })
  })
  if (!confirmed) return
  try {
    await convApi.removeFile(f.id)
    files.value = files.value.filter((x) => x.id !== f.id)
  } catch (err) {
    void MessagePlugin.error(te(err))
  }
}

// ===== 群头像 =====

const groupAvatarInput = ref<HTMLInputElement | null>(null)
const avatarUploading = ref(false)

function pickGroupAvatar() {
  if (!canManage.value || avatarUploading.value) return
  groupAvatarInput.value?.click()
}

/** 群头像最大边 512，足够列表/头部显示，避免原图直传 */
async function compressGroupAvatar(file: File): Promise<Blob> {
  if (file.size < 300 * 1024 || !file.type.startsWith('image/')) return file
  try {
    const bitmap = await createImageBitmap(file)
    const scale = Math.min(1, 512 / Math.max(bitmap.width, bitmap.height))
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
      canvas.toBlob(resolve, 'image/webp', 0.82),
    )
    return blob && blob.size < file.size ? blob : file
  } catch {
    return file
  }
}

async function onGroupAvatarChange(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  const c = conv.value
  if (!file || !c) return
  if (!['image/png', 'image/jpeg', 'image/webp'].includes(file.type)) {
    void MessagePlugin.warning(t('group.avatarInvalid'))
    return
  }
  if (file.size > siteStore.config.maxImageMB << 20) {
    void MessagePlugin.warning(t('group.avatarTooLarge', { mb: siteStore.config.maxImageMB }))
    return
  }
  avatarUploading.value = true
  try {
    const blob = await compressGroupAvatar(file)
    const name = blob === file ? file.name : file.name.replace(/\.\w+$/, '') + '.webp'
    const up = await mediaApi.upload('image', blob, name)
    await convApi.setAvatar(c.id, up.url)
    c.avatar = up.url
    await convStore.loadConversations()
    void MessagePlugin.success(t('group.avatarOk'))
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    avatarUploading.value = false
  }
}

async function removeGroupAvatar() {
  const c = conv.value
  if (!c || avatarUploading.value) return
  avatarUploading.value = true
  try {
    await convApi.setAvatar(c.id, '')
    c.avatar = ''
    await convStore.loadConversations()
    void MessagePlugin.success(t('group.avatarRemoved'))
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    avatarUploading.value = false
  }
}

// ===== 群公告（仅群主可编辑）=====
const editingAnnounce = ref(false)
const announceDraft = ref('')
const savingAnnounce = ref(false)

function startEditAnnounce() {
  announceDraft.value = conv.value?.announcement ?? ''
  editingAnnounce.value = true
}

/** content 传空串表示撤销公告 */
async function saveAnnouncement(content: string) {
  const c = conv.value
  if (!c) return
  savingAnnounce.value = true
  try {
    await convApi.setAnnouncement(c.id, content)
    editingAnnounce.value = false
    void MessagePlugin.success(t('group.announcementOk'))
    // 公告在 conversations 上，重新拉一次列表拿到最新值（含发布者与时间）
    await convStore.loadConversations()
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    savingAnnounce.value = false
  }
}

function saveAnnounce() {
  void saveAnnouncement(announceDraft.value.trim())
}

function clearAnnounce() {
  void saveAnnouncement('')
}

const renameVisible = ref(false)
const newName = ref('')
const transferVisible = ref(false)
const transferTarget = ref<number | undefined>(undefined)

const memberOptions = computed(() =>
  members.value
    .filter((m) => m.userId !== auth.user?.id)
    .map((m) => ({ label: m.user.nickname, value: m.userId })),
)

// 弹窗的确认按钮可重复点击（改名/转让都是重操作），用 busy 挡住第二次提交并给出加载反馈
const renaming = ref(false)
const transferring = ref(false)

// ===== 拉人入群 =====
const inviteVisible = ref(false)
const inviteSelected = ref<number[]>([])
const inviting = ref(false)

/** 可邀请的好友：排除已在群里的成员（含自己） */
const inviteCandidates = computed(() => {
  const memberIds = new Set(members.value.map((m) => m.userId))
  return friendsStore.accepted.filter((f) => !memberIds.has(f.friend.id))
})

function openInvite() {
  inviteSelected.value = []
  inviteVisible.value = true
}

function toggleInvite(userId: number, checked: boolean) {
  if (checked) {
    if (!inviteSelected.value.includes(userId)) {
      inviteSelected.value = [...inviteSelected.value, userId]
    }
  } else {
    inviteSelected.value = inviteSelected.value.filter((id) => id !== userId)
  }
}

/** 生成邀请链接并复制到剪贴板（有效期 7 天） */
async function copyInviteLink() {
  const c = conv.value
  if (!c) return
  try {
    const r = await convApi.createInviteLink(c.id, 7)
    const url = `${window.location.origin}${r.path}`
    await navigator.clipboard.writeText(url)
    void MessagePlugin.success(t('group.inviteLinkOk'))
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
}

async function doInvite() {
  const c = conv.value
  if (!c || !inviteSelected.value.length || inviting.value) return
  inviting.value = true
  try {
    const r = await convApi.invite(c.id, inviteSelected.value)
    inviteVisible.value = false
    inviteSelected.value = []
    await Promise.all([convStore.loadConversations(), refreshMembers()])
    void MessagePlugin.success(t('group.inviteOk', { n: r.added }))
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    inviting.value = false
  }
}

async function doRename() {
  if (!conv.value || !newName.value.trim() || renaming.value) return
  renaming.value = true
  try {
    await convApi.rename(conv.value.id, newName.value.trim())
    await convStore.loadConversations()
    renameVisible.value = false
    void MessagePlugin.success(t('group.renameOk'))
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    renaming.value = false
  }
}

function kick(userId: number, nickname: string) {
  if (!conv.value) return
  const dialog = DialogPlugin.confirm({
    header: t('group.kickTitle'),
    body: t('group.kickBody', { name: nickname }),
    confirmBtn: { content: t('group.kickBtn'), theme: 'danger' },
    onConfirm: async () => {
      try {
        await convApi.kick(conv.value!.id, userId)
        members.value = members.value.filter((m) => m.userId !== userId)
        await convStore.loadConversations()
        void MessagePlugin.success(t('group.kickOk'))
      } catch (e) {
        void MessagePlugin.error(te(e))
      }
      dialog.destroy()
    },
  })
}

async function doTransfer() {
  if (!conv.value || !transferTarget.value || transferring.value) return
  transferring.value = true
  try {
    await convApi.transfer(conv.value.id, transferTarget.value)
    transferVisible.value = false
    await Promise.all([convStore.loadConversations(), refreshMembers()])
    void MessagePlugin.success(t('group.transferOk'))
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    transferring.value = false
  }
}

// ===== 成员右键菜单 =====

const memberMenuVisible = ref(false)
const memberMenuX = ref(0)
const memberMenuY = ref(0)
const memberMenuTarget = ref<ConversationMember | null>(null)

function isMuted(m: ConversationMember): boolean {
  return !!m.mutedUntil && new Date(m.mutedUntil).getTime() > Date.now()
}

function openMemberMenu(e: MouseEvent, m: ConversationMember) {
  memberMenuTarget.value = m
  memberMenuX.value = e.clientX
  memberMenuY.value = e.clientY
  memberMenuVisible.value = true
}

const memberMenuItems = computed<ContextMenuItem[]>(() => {
  const m = memberMenuTarget.value
  if (!m) return []
  const mine = m.userId === auth.user?.id
  const items: ContextMenuItem[] = [{ key: 'copyName', label: t('chat.memberCopyName') }]
  if (mine || !canManage.value || m.role === 'owner') return items

  // 管理员不能管理其他管理员，只有群主可以
  const blockedByRole = isAdmin.value && m.role === 'admin'
  if (!blockedByRole) {
    items.push({
      key: 'mute',
      label: isMuted(m) ? t('group.unmute') : t('group.mute'),
      dividerBefore: true,
    })
    items.push({ key: 'kick', label: t('group.kick'), danger: true })
  }
  if (isOwner.value) {
    items.push({
      key: 'toggleAdmin',
      label: m.role === 'admin' ? t('group.removeAdmin') : t('group.makeAdmin'),
      dividerBefore: items.length > 0,
    })
  }
  return items
})

function onMemberMenuSelect(key: string) {
  const m = memberMenuTarget.value
  if (!m) return
  if (key === 'copyName') {
    void navigator.clipboard
      .writeText(m.user.nickname)
      .then(() => MessagePlugin.success(t('chat.memberCopied')))
      .catch(() => MessagePlugin.error(t('chat.memberCopyFailed')))
  }
  if (key === 'kick') kick(m.userId, m.user.nickname)
  if (key === 'mute') openMute(m)
  if (key === 'toggleAdmin') void toggleAdmin(m)
  if (key === 'transfer') {
    // 复用现有的转让确认弹窗，把目标成员预选好
    transferTarget.value = m.userId
    transferVisible.value = true
  }
}

// ===== 管理员 / 禁言 =====

const muteVisible = ref(false)
const muteTarget = ref<ConversationMember | null>(null)
const muteMinutes = ref(60)
const muting = ref(false)

const muteOptions = computed(() => [
  { label: t('group.mute10m'), value: 10 },
  { label: t('group.mute1h'), value: 60 },
  { label: t('group.mute1d'), value: 1440 },
  { label: t('group.mute30d'), value: 43200 },
])

async function setMute(m: ConversationMember, minutes: number) {
  if (!conv.value || muting.value) return
  muting.value = true
  try {
    await convApi.mute(conv.value.id, m.userId, minutes)
    muteVisible.value = false
    await Promise.all([convStore.loadConversations(), refreshMembers()])
    void MessagePlugin.success(minutes > 0 ? t('group.muteOk', { name: m.user.nickname }) : t('group.unmuteOk'))
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    muting.value = false
  }
}

function doMute() {
  if (muteTarget.value) void setMute(muteTarget.value, muteMinutes.value)
}

/** 已禁言则直接解除，否则打开时长选择弹窗 */
function openMute(m: ConversationMember) {
  if (isMuted(m)) {
    void setMute(m, 0)
    return
  }
  muteTarget.value = m
  muteMinutes.value = 60
  muteVisible.value = true
}

function toggleAdmin(m: ConversationMember) {
  const makeAdmin = m.role !== 'admin'
  const dialog = DialogPlugin.confirm({
    header: t('group.adminTitle'),
    body: makeAdmin
      ? t('group.adminBody', { name: m.user.nickname })
      : t('group.removeAdminBody', { name: m.user.nickname }),
    onConfirm: async () => {
      if (!conv.value) return dialog.destroy()
      try {
        await convApi.setAdmin(conv.value.id, m.userId, makeAdmin)
        await Promise.all([convStore.loadConversations(), refreshMembers()])
        void MessagePlugin.success(t('group.adminOk'))
      } catch (e) {
        void MessagePlugin.error(te(e))
      }
      dialog.destroy()
    },
  })
}

async function refreshMembers() {
  const id = conv.value?.id
  if (!id) return
  try {
    const rows = await convApi.members(id)
    if (conv.value?.id !== id) return
    members.value = rows
    membersError.value = ''
  } catch (e) {
    if (conv.value?.id === id) membersError.value = te(e)
  }
}

async function leave() {
  if (!conv.value) return
  const dialog = DialogPlugin.confirm({
    header: t('group.leaveTitle'),
    body: isOwner.value ? t('group.leaveOwnerBody') : t('group.leaveBody'),
    confirmBtn: {
      content: isOwner.value ? t('group.gotIt') : t('group.leaveBtn'),
      theme: isOwner.value ? 'default' : 'danger',
    },
    onConfirm: async () => {
      if (isOwner.value) {
        dialog.destroy()
        return
      }
      try {
        await convApi.leave(conv.value!.id)
        await convStore.loadConversations()
        // 必须走 openConversation：直接改 currentConvId 不会拉消息、不会上报已读，
        // 界面会停在一空白会话上（上翻也拿不到历史）
        const next = convStore.conversations[0]?.id
        if (next) await convStore.openConversation(next)
        else convStore.currentConvId = null
        void MessagePlugin.success(t('group.leaveOk'))
      } catch (e) {
        void MessagePlugin.error(te(e))
      }
      dialog.destroy()
    },
  })
}
</script>

<template>
  <div class="mc-detail">
    <!-- 群信息 / 群管理 -->
    <template v-if="conv">
      <!-- 群头像：群主 / 管理员可点击更换 -->
      <div v-if="isGroup" class="mc-detail__group-head">
        <button
          v-if="canManage"
          type="button"
          class="mc-detail__group-avatar is-editable"
          :disabled="avatarUploading"
          :title="t('group.changeAvatar')"
          @click="pickGroupAvatar"
        >
          <AvatarBadge :name="conv.name ?? '#'" :src="conv.avatar" color="#2ba471" :size="48" />
          <span class="mc-detail__avatar-edit">
            {{ avatarUploading ? t('message.uploading') : t('group.changeAvatar') }}
          </span>
        </button>
        <AvatarBadge v-else :name="conv.name ?? '#'" :src="conv.avatar" color="#2ba471" :size="48" />
        <div class="mc-detail__group-meta">
          <span class="mc-detail__group-name">{{ conv.name }}</span>
          <a
            v-if="canManage && conv.avatar"
            class="mc-detail__avatar-remove"
            @click="removeGroupAvatar"
          >
            {{ t('group.removeAvatar') }}
          </a>
        </div>
        <input
          ref="groupAvatarInput"
          type="file"
          accept="image/png,image/jpeg,image/webp"
          class="mc-detail__file-input"
          @change="onGroupAvatarChange"
        />
      </div>

      <!-- 会话内搜索：针对当前会话（L2）；Ctrl/Cmd+F 聚焦 -->
      <div class="mc-detail__section">
        <div class="mc-detail__title">
          <span class="mc-detail__heading">{{ t('message.search') }}</span>
        </div>
        <UiInput
          ref="searchInputRef"
          v-model="searchKw"
          size="small"
          clearable
          :placeholder="t('message.searchPlaceholder')"
          @update:model-value="onSearchInput"
          @clear="onSearchInput"
        >
          <template #prefix-icon>
            <SearchIcon size="13px" />
          </template>
        </UiInput>

        <div v-if="searching" class="mc-detail__search-state text-placeholder">
          {{ t('common.loading') }}
        </div>
        <div v-else-if="searchResults.length" class="mc-detail__search-list">
          <button
            v-for="hit in searchResults"
            :key="hit.messageId"
            type="button"
            class="mc-detail__search-hit"
            @click="openSearchHit(hit)"
          >
            <span class="mc-detail__search-meta text-placeholder">
              <span class="mc-detail__search-sender">{{ hit.senderNickname }}</span>
              <span class="num">{{ fmtSearchTime(hit.createdAt) }}</span>
            </span>
            <span class="mc-detail__search-text">
              <template v-for="(p, i) in searchParts(searchPreview(hit))" :key="i">
                <mark v-if="p.hit">{{ p.t }}</mark>
                <template v-else>{{ p.t }}</template>
              </template>
            </span>
          </button>
        </div>
        <p v-else-if="searchKw.trim()" class="mc-detail__search-state text-placeholder">
          {{ t('message.searchEmpty') }}
        </p>
      </div>

      <!-- 群公告：所有成员可见，仅群主可编辑 -->
      <div v-if="isGroup" class="mc-detail__section">
        <div class="mc-detail__title">
          <span class="mc-detail__heading">{{ t('group.announcement') }}</span>
          <span v-if="isOwner && !editingAnnounce" class="mc-detail__actions">
            <a @click="startEditAnnounce">
              {{ conv.announcement ? t('group.announcementEdit') : t('group.announcementSet') }}
            </a>
          </span>
        </div>

        <template v-if="editingAnnounce">
          <textarea
            v-model="announceDraft"
            class="mc-detail__announce-input"
            rows="3"
            maxlength="500"
            :placeholder="t('group.announcementPlaceholder')"
          />
          <div class="mc-detail__announce-actions">
            <UiButton size="small" theme="primary" :loading="savingAnnounce" @click="saveAnnounce">
              {{ t('group.announcementSave') }}
            </UiButton>
            <UiButton
              v-if="conv.announcement"
              size="small"
              variant="outline"
              :disabled="savingAnnounce"
              @click="clearAnnounce"
            >
              {{ t('group.announcementClear') }}
            </UiButton>
            <UiButton
              size="small"
              variant="text"
              :disabled="savingAnnounce"
              @click="editingAnnounce = false"
            >
              {{ t('common.cancel') }}
            </UiButton>
          </div>
        </template>
        <p v-else-if="conv.announcement" class="mc-detail__announce-text">{{ conv.announcement }}</p>
        <p v-else class="mc-detail__announce-empty text-placeholder">
          {{ t('group.announcementEmpty') }}
        </p>
      </div>

      <!-- 群文件：成员都可上传，上传者本人或群主可删 -->
      <div v-if="isGroup" class="mc-detail__section">
        <div class="mc-detail__title">
          <span class="mc-detail__heading">{{ t('group.files') }}</span>
          <span class="mc-detail__actions">
            <a @click="pickFile">
              {{ uploadingFile ? t('message.uploading') : t('group.fileUpload') }}
            </a>
          </span>
        </div>

        <div v-if="files.length" class="mc-detail__files">
          <div v-for="f in files" :key="f.id" class="mc-detail__file">
            <a class="mc-detail__file-name" :href="f.url" :download="f.name" :title="f.name">
              {{ f.name }}
            </a>
            <span class="mc-detail__file-meta text-placeholder">
              {{ fmtSize(f.size) }} · {{ f.uploaderName }}
            </span>
            <button
              v-if="canDeleteFile(f)"
              type="button"
              class="mc-detail__file-del"
              :title="t('group.fileDelete')"
              :aria-label="t('group.fileDelete')"
              @click="removeFile(f)"
            >
              <svg width="11" height="11" viewBox="0 0 24 24" fill="none" aria-hidden="true">
                <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
              </svg>
            </button>
          </div>
        </div>
        <p v-else class="mc-detail__announce-empty text-placeholder">{{ t('group.fileEmpty') }}</p>

        <input ref="fileInput" type="file" class="mc-detail__file-input" @change="onFileChange" />
      </div>

      <div v-if="isGroup" class="mc-detail__section">
        <div class="mc-detail__title">
          <span class="mc-detail__heading">
            {{ t('group.info') }}
            <span class="mc-detail__count num">
              {{ t('chat.membersCount', { n: members.length || conv.memberCount }) }}
            </span>
          </span>
          <span class="mc-detail__actions">
            <a @click="openInvite">{{ t('group.invite') }}</a>
            <a @click="copyInviteLink">{{ t('group.inviteLink') }}</a>
            <template v-if="isOwner">
              <a @click="newName = conv.name ?? ''; renameVisible = true">{{ t('group.rename') }}</a>
              <a @click="transferVisible = true">{{ t('group.transfer') }}</a>
            </template>
            <a v-if="canManage" @click="toggleMuteAll">
              {{ conv.muteAll ? t('group.unmuteAll') : t('group.muteAll') }}
            </a>
          </span>
        </div>

        <!-- 成员加载失败：给一次重试，而不是让面板空着 -->
        <p v-if="membersError && !members.length" class="mc-detail__members-error text-placeholder">
          {{ membersError }}
          <a role="button" tabindex="0" @click="refreshMembers" @keydown.enter="refreshMembers">
            {{ t('common.retry') }}
          </a>
        </p>

        <div
          class="mc-detail__member"
          v-for="m in members"
          :key="m.userId"
          @contextmenu.prevent="openMemberMenu($event, m)"
        >
          <AvatarBadge
            :name="m.user.nickname"
            :src="m.user.avatar"
            :color="m.user.avatarColor"
            :size="30"
            :online="ws.onlineUsers.has(m.userId)"
          />
          <span class="mc-detail__member-name">
            <span class="mc-detail__member-nick">{{ m.user.nickname }}</span>
            <span v-if="m.role === 'owner'" class="mc-detail__badge is-owner">{{ t('group.owner') }}</span>
            <span v-if="m.role === 'admin'" class="mc-detail__badge is-admin">{{ t('group.admin') }}</span>
            <span v-if="isMuted(m)" class="mc-detail__badge is-muted">{{ t('group.muted') }}</span>
            <span v-if="m.userId === auth.user?.id" class="mc-detail__badge">{{ t('group.me') }}</span>
          </span>
          <span
            v-if="
              canManage &&
              m.userId !== auth.user?.id &&
              m.role !== 'owner' &&
              !(isAdmin && m.role === 'admin')
            "
            class="mc-detail__member-ops"
          >
            <a @click="openMute(m)">{{ isMuted(m) ? t('group.unmute') : t('group.mute') }}</a>
            <a @click="kick(m.userId, m.user.nickname)">{{ t('group.kick') }}</a>
          </span>
        </div>

        <div class="mc-detail__footer">
          <UiButton variant="outline" theme="danger" size="small" block @click="leave">
            {{ isOwner ? t('group.leaveOwner') : t('group.leave') }}
          </UiButton>
        </div>
      </div>

      <!-- 单聊资料 -->
      <div v-else class="mc-detail__section">
        <div class="mc-detail__title">{{ t('group.peerInfo') }}</div>
        <div class="mc-detail__member">
          <AvatarBadge
            :name="conv.peer?.nickname ?? '?'"
            :src="conv.peer?.avatar"
            :color="conv.peer?.avatarColor"
            :size="40"
            :online="conv.peer ? ws.onlineUsers.has(conv.peer.id) : false"
          />
          <div>
            <div>{{ conv.peer?.nickname }}</div>
            <div class="text-placeholder">@{{ conv.peer?.username }}</div>
          </div>
        </div>
        <p v-if="peerBlocked" class="mc-detail__hint text-placeholder">{{ t('group.blockedHint') }}</p>
        <div class="mc-detail__footer">
          <UiButton
            variant="outline"
            :theme="peerBlocked ? 'default' : 'danger'"
            size="small"
            block
            :loading="blockBusy"
            @click="toggleBlock"
          >
            {{ peerBlocked ? t('group.unblock') : t('group.block') }}
          </UiButton>
        </div>
      </div>
    </template>

    <!-- 插件侧边栏 -->
    <PluginPanel />
  </div>

  <!-- 成员右键菜单 -->
  <ContextMenu
    v-model:visible="memberMenuVisible"
    :x="memberMenuX"
    :y="memberMenuY"
    :items="memberMenuItems"
    @select="onMemberMenuSelect"
  />

  <!-- 改群名 -->
  <UiDialog
    v-model:visible="renameVisible"
    :header="t('group.renameTitle')"
    :confirm-btn-loading="renaming"
    @confirm="doRename"
  >
    <UiInput v-model="newName" :placeholder="t('group.newName')" clearable @enter="doRename" />
  </UiDialog>

  <!-- 转让群主 -->
  <UiDialog
    v-model:visible="transferVisible"
    :header="t('group.transferTitle')"
    :confirm-btn-loading="transferring"
    @confirm="doTransfer"
  >
    <UiSelect v-model="transferTarget" :options="memberOptions" :placeholder="t('group.transferPlaceholder')" clearable />
  </UiDialog>

  <!-- 禁言时长 -->
  <UiDialog
    v-model:visible="muteVisible"
    :header="t('group.muteTitle')"
    :confirm-btn-loading="muting"
    @confirm="doMute"
  >
    <UiSelect v-model="muteMinutes" :options="muteOptions" />
  </UiDialog>

  <!-- 拉人入群：多选自己的好友 -->
  <UiDialog
    v-model:visible="inviteVisible"
    :header="t('group.inviteTitle')"
    :confirm-btn="{ content: t('group.inviteAction'), theme: 'primary' }"
    :confirm-btn-loading="inviting"
    @confirm="doInvite"
  >
    <div class="mc-detail__invite">
      <p v-if="!inviteCandidates.length" class="mc-detail__invite-empty text-placeholder">
        {{ t('group.inviteEmpty') }}
      </p>
      <div v-for="f in inviteCandidates" :key="f.friend.id" class="mc-detail__invite-item">
        <UiCheckbox
          :model-value="inviteSelected.includes(f.friend.id)"
          @update:model-value="toggleInvite(f.friend.id, $event)"
        >
          <AvatarBadge
            :name="f.friend.nickname"
            :src="f.friend.avatar"
            :color="f.friend.avatarColor"
            :size="28"
          />
          <span class="mc-detail__invite-name">{{ f.friend.nickname }}</span>
          <span class="text-placeholder mc-detail__invite-username">@{{ f.friend.username }}</span>
        </UiCheckbox>
      </div>
    </div>
  </UiDialog>
</template>

<style scoped>
.mc-detail {
  /* 宽度由布局 store 提供（可拖拽调整），默认值与历史实现一致 */
  width: var(--mc-w-detail, 280px);
  flex-shrink: 0;
  border-left: 1px solid var(--td-component-border);
  background: var(--td-bg-color-container);
  overflow-y: auto;
  display: flex;
  flex-direction: column;
}

.mc-detail__section {
  padding: 16px;
  border-bottom: 1px solid var(--td-component-border);
  /* 面板是 flex column 且整体滚动：子项默认会被等比压缩，
     成员一多就把底部「退群」按钮挤成一条细线。固定住高度让它滚动而不是压缩。 */
  flex-shrink: 0;
}

/* 成员加载失败 + 重试 */
.mc-detail__members-error {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 8px 0;
  font-size: 12px;
  line-height: 1.5;
}

.mc-detail__members-error a {
  color: var(--mc-link);
  text-decoration: underline;
  text-underline-offset: 2px;
  cursor: pointer;
}

/* 会话内搜索（从 MessagePane 迁入，样式口径保持一致） */
.mc-detail__search-state {
  padding: 10px 0 0;
  text-align: center;
  font-size: 12px;
}

.mc-detail__search-list {
  margin-top: 8px;
  max-height: 300px;
  overflow-y: auto;
}

.mc-detail__search-hit {
  display: flex;
  flex-direction: column;
  gap: 2px;
  width: 100%;
  padding: 7px 8px;
  border: none;
  border-radius: var(--mc-radius-sm);
  background: transparent;
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition: background-color var(--mc-dur-fast) var(--mc-ease-out);
}

.mc-detail__search-hit:hover {
  background: var(--td-bg-color-container-hover);
}

.mc-detail__search-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 11px;
}

.mc-detail__search-sender {
  flex: 1;
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mc-detail__search-text {
  font-size: 13px;
  line-height: 1.5;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mc-detail__search-text mark {
  padding: 0 1px;
  border-radius: 2px;
  background: var(--td-brand-color-1, rgba(0, 82, 217, 0.16));
  color: var(--td-brand-color);
}

/* 群头像 */
.mc-detail__group-head {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 16px;
  border-bottom: 1px solid var(--td-component-border);
  flex-shrink: 0;
}

.mc-detail__group-avatar {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  padding: 0;
  border: none;
  background: transparent;
  font: inherit;
  cursor: default;
}

.mc-detail__group-avatar.is-editable {
  cursor: pointer;
}

.mc-detail__group-avatar.is-editable:hover :deep(.mc-avatar) {
  box-shadow: 0 0 0 2px var(--mc-selection);
}

.mc-detail__group-avatar:disabled {
  opacity: 0.6;
  cursor: wait;
}

.mc-detail__avatar-edit {
  font-size: 11px;
  line-height: 1.3;
  color: var(--td-brand-color);
  white-space: nowrap;
}

.mc-detail__group-meta {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.mc-detail__group-name {
  font-size: 14px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mc-detail__avatar-remove {
  font-size: 12px;
  color: var(--td-error-color);
  cursor: pointer;
}

.mc-detail__avatar-remove:hover {
  text-decoration: underline;
}

/* 群文件 */
.mc-detail__files {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.mc-detail__file {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 4px;
  border-radius: var(--mc-radius-sm);
}

.mc-detail__file:hover {
  background: var(--td-bg-color-container-hover);
}

.mc-detail__file-name {
  flex: 1;
  min-width: 0;
  font-size: var(--mc-fs-body);
  color: var(--td-brand-color);
  text-decoration: none;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mc-detail__file-name:hover {
  text-decoration: underline;
}

.mc-detail__file-meta {
  flex-shrink: 0;
  font-size: var(--mc-fs-caption);
}

.mc-detail__file-del {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  padding: 0;
  border: none;
  border-radius: 50%;
  background: transparent;
  color: var(--td-text-color-placeholder);
  cursor: pointer;
}

.mc-detail__file-del:hover {
  background: color-mix(in srgb, var(--td-error-color) 16%, transparent);
  color: var(--td-error-color);
}

.mc-detail__file-input {
  display: none;
}

/* 群公告 */
.mc-detail__announce-text {
  margin: 0;
  font-size: var(--mc-fs-body);
  line-height: 1.55;
  white-space: pre-wrap;
  word-break: break-word;
  user-select: text;
}

.mc-detail__announce-empty {
  margin: 0;
  font-size: var(--mc-fs-sm);
}

.mc-detail__announce-input {
  width: 100%;
  padding: 8px 10px;
  border: 1px solid var(--td-component-border);
  border-radius: var(--mc-radius-sm);
  background: var(--td-bg-color-page);
  color: var(--td-text-color-primary);
  font: inherit;
  font-size: var(--mc-fs-body);
  line-height: 1.5;
  resize: vertical;
}

.mc-detail__announce-input:focus {
  outline: none;
  border-color: var(--td-brand-color);
}

.mc-detail__announce-actions {
  display: flex;
  gap: 8px;
  margin-top: 8px;
}

.mc-detail__title {
  font-weight: 600;
  font-size: var(--mc-fs-title);
  letter-spacing: -0.01em;
  margin-bottom: 12px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
}

.mc-detail__heading {
  min-width: 0;
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.mc-detail__count {
  font-size: var(--mc-fs-sm);
  font-weight: 400;
  color: var(--mc-text-faint);
  white-space: nowrap;
}

.mc-detail__actions {
  display: flex;
  gap: 8px;
  font-weight: 400;
  font-size: 12px;
}

.mc-detail__actions a {
  cursor: pointer;
  color: var(--td-brand-color);
  border-radius: 4px;
  padding: 1px 2px;
  transition: background-color var(--mc-dur-fast, 120ms) var(--mc-ease-in-out);
}

.mc-detail__actions a:hover {
  background: var(--td-brand-color-light, rgba(0, 82, 217, 0.08));
  text-decoration: none;
}

.mc-detail__actions a:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 1px;
}

.mc-detail__member {
  display: flex;
  align-items: center;
  gap: 8px;
  /* 用负 margin + 内边距让 hover 背景延伸到面板边缘，成员行不再是一条条裸文字 */
  padding: 7px 8px;
  margin: 0 -8px;
  border-radius: var(--mc-radius-sm);
  transition: background-color var(--mc-dur-fast) var(--mc-ease-out);
}

.mc-detail__member:hover {
  background: var(--td-bg-color-container-hover);
}

.mc-detail__member-name {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 4px;
  overflow: hidden;
}

.mc-detail__member-nick {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mc-detail__badge {
  flex-shrink: 0;
  padding: 1px 6px;
  border-radius: 999px;
  background: var(--td-bg-color-secondarycontainer);
  font-size: 11px;
  font-weight: 400;
  line-height: 1.5;
  color: var(--td-text-color-secondary);
}

.mc-detail__badge.is-owner {
  background: var(--td-brand-color-1, rgba(0, 82, 217, 0.08));
  color: var(--td-brand-color);
}

.mc-detail__badge.is-admin {
  background: color-mix(in srgb, var(--td-success-color, #2ba471) 12%, transparent);
  color: var(--td-success-color, #2ba471);
}

.mc-detail__badge.is-muted {
  background: color-mix(in srgb, var(--td-error-color, #d54941) 12%, transparent);
  color: var(--td-error-color, #d54941);
}

.mc-detail__member-ops {
  display: flex;
  flex-shrink: 0;
  gap: 8px;
}

.mc-detail__member-ops a {
  font-size: 12px;
  cursor: pointer;
}

/* 拉人弹窗：好友多选列表 */
.mc-detail__invite {
  display: flex;
  flex-direction: column;
  gap: 2px;
  max-height: 340px;
  overflow-y: auto;
}

.mc-detail__invite-item {
  padding: 6px 8px;
  border-radius: var(--mc-radius-sm);
  transition: background-color var(--mc-dur-fast) var(--mc-ease-out);
}

.mc-detail__invite-item:hover {
  background: var(--td-bg-color-container-hover);
}

.mc-detail__invite-name {
  font-size: var(--mc-fs-body);
}

.mc-detail__invite-username {
  font-size: var(--mc-fs-caption);
}

.mc-detail__invite-empty {
  margin: 0;
  padding: 24px 0;
  text-align: center;
  font-size: 13px;
}

.mc-detail__footer {
  margin-top: 14px;
  flex-shrink: 0;
}

.mc-detail__hint {
  margin: 10px 0 0;
  font-size: var(--mc-fs-caption);
  line-height: 1.5;
}
</style>
