<script setup lang="ts">
import UiSelect from '@/components/ui/UiSelect.vue'
import UiDialog from '@/components/ui/UiDialog.vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiInput from '@/components/ui/UiInput.vue'
import { computed, ref, watch } from 'vue'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'

import AvatarBadge from './AvatarBadge.vue'
import ContextMenu, { type ContextMenuItem } from '@/components/common/ContextMenu.vue'
import PluginPanel from '@/components/declarative/PluginPanel.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import { convApi, mediaApi, type ConversationFile } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { useConversationsStore } from '@/stores/conversations'
import { useWsStore } from '@/stores/ws'
import type { ConversationMember } from '@/types/proto'

const auth = useAuthStore()
const convStore = useConversationsStore()
const ws = useWsStore()
const { t, te } = useAppI18n()

const conv = computed(() => convStore.currentConv)
const isGroup = computed(() => conv.value?.type === 2)
const isOwner = computed(() => conv.value?.ownerId === auth.user?.id)

const members = ref<ConversationMember[]>([])

watch(
  () => conv.value?.id,
  async (id) => {
    members.value = []
    if (id === undefined || !isGroup.value) return
    try {
      members.value = await convApi.members(id)
    } catch {
      /* 忽略 */
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
      files.value = await convApi.files(id)
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
      members.value = await convApi.members(id)
      files.value = await convApi.files(id)
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
  // 群主可以管理其他成员；自己的行不给踢出/转让入口
  if (isOwner.value && !mine) {
    items.push({ key: 'transfer', label: t('group.transfer'), dividerBefore: true })
    items.push({ key: 'kick', label: t('group.kick'), danger: true })
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
  if (key === 'transfer') {
    // 复用现有的转让确认弹窗，把目标成员预选好
    transferTarget.value = m.userId
    transferVisible.value = true
  }
}

async function refreshMembers() {
  if (!conv.value) return
  members.value = await convApi.members(conv.value.id)
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
        convStore.currentConvId = convStore.conversations[0]?.id ?? null
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
          <span v-if="isOwner" class="mc-detail__actions">
            <a @click="newName = conv.name ?? ''; renameVisible = true">{{ t('group.rename') }}</a>
            <a @click="transferVisible = true">{{ t('group.transfer') }}</a>
          </span>
        </div>

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
            <span v-if="m.userId === auth.user?.id" class="mc-detail__badge">{{ t('group.me') }}</span>
          </span>
          <span v-if="isOwner && m.userId !== auth.user?.id" class="mc-detail__member-ops">
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

.mc-detail__member-ops a {
  font-size: 12px;
  cursor: pointer;
}

.mc-detail__footer {
  margin-top: 14px;
  flex-shrink: 0;
}
</style>
