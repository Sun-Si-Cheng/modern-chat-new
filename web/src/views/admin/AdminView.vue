<script setup lang="ts">
import UiInputNumber from '@/components/ui/UiInputNumber.vue'
import UiSelect from '@/components/ui/UiSelect.vue'
import UiDialog from '@/components/ui/UiDialog.vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiInput from '@/components/ui/UiInput.vue'
import UiSwitch from '@/components/ui/UiSwitch.vue'
import UiTag from '@/components/ui/UiTag.vue'
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { DialogPlugin, MessagePlugin, Table as TTable } from 'tdesign-vue-next'
import { SettingIcon } from 'tdesign-icons-vue-next'

import AvatarBadge from '@/components/chat/AvatarBadge.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import { markAnnouncementSeen } from '@/composables/announcement'
import {
  adminApi,
  systemApi,
  type AdminAudit,
  type AdminPluginRow,
  type AdminSettingsPayload,
  type AdminStats,
  type AdminUserDetail,
  type AdminUserRow,
  type BackupFile,
  type InviteCode,
  type StorageStats,
  type VersionInfo,
} from '@/api'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const auth = useAuthStore()
const { t, te } = useAppI18n()

type AdminTab = 'users' | 'plugins' | 'audits' | 'invites' | 'settings'
const tab = ref<AdminTab>('users')

/**
 * 页签切换。
 * 这里不用 TDesign Tabs：四个页签里各有一张表格，Tabs 会同时挂载全部面板，
 * 点击切换时所有表格都会重渲染（隐藏面板还要做宽度测量），在低配机器 / 大数据量下
 * 会造成明显的卡顿甚至页面无响应。改用轻量页签 + v-if 只挂载当前面板。
 */
const settingsLoaded = ref(false)
const settingsLoading = ref(false)
/** 设置加载失败：此时表单里全是默认值，保存会覆盖线上配置，必须锁住保存 */
const settingsLoadFailed = ref(false)

function switchTab(next: AdminTab) {
  if (tab.value === next) return
  // 切换时先清掉上一条请求的 loading：请求若被代理/扩展挂住，
  // 表格的遮罩会跟着页签一起残留，看起来像「一直转圈」。
  loading.value = false
  pluginsLoading.value = false
  auditsLoading.value = false
  tab.value = next
  // 数据按页签懒加载：打开页面时只拉当前页签需要的内容，减少首屏请求与渲染压力
  if (next === 'plugins') void fetchPlugins()
  // 审计记录会随操作持续增长，进入页签时重新拉取，避免显示旧数据
  if (next === 'audits') void fetchAudits()
  if (next === 'invites') void fetchInvites()
  // 设置只加载一次：反复拉取会把用户尚未保存的修改覆盖掉
  if (next === 'settings' && !settingsLoaded.value && !settingsLoading.value) {
    void fetchSettings()
  }
}

const tabItems = computed<Array<{ value: AdminTab; label: string }>>(() => [
  { value: 'users', label: t('admin.tabUsers') },
  { value: 'plugins', label: t('admin.tabPlugins') },
  { value: 'audits', label: t('admin.tabAudits') },
  { value: 'invites', label: t('admin.tabInvites') },
  { value: 'settings', label: t('admin.tabSettings') },
])

// ---- 注册邀请码 ----
const invites = ref<InviteCode[]>([])
const invitesLoading = ref(false)
const inviteForm = reactive({ maxUses: 1, expiresDays: 7, count: 1 })

async function fetchInvites() {
  invitesLoading.value = true
  try {
    const list = await adminApi.invites()
    invites.value = Array.isArray(list) ? list : []
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    invitesLoading.value = false
  }
}

async function createInvites() {
  try {
    const created = await adminApi.createInvites({
      maxUses: inviteForm.maxUses,
      expiresDays: inviteForm.expiresDays,
      count: inviteForm.count,
    })
    invites.value = [...(Array.isArray(created) ? created : []), ...invites.value]
    void MessagePlugin.success(t('admin.inviteCreated', { n: created?.length ?? 0 }))
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
}

async function toggleInvite(row: InviteCode) {
  try {
    await adminApi.disableInvite(row.id, !row.disabled)
    row.disabled = !row.disabled
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
}

async function copyInvite(code: string) {
  try {
    await navigator.clipboard.writeText(code)
    void MessagePlugin.success(t('admin.inviteCopied'))
  } catch {
    void MessagePlugin.error(t('admin.inviteCopyFailed'))
  }
}

function inviteExpired(row: InviteCode): boolean {
  if (!row.expiresAt) return false
  return new Date(row.expiresAt).getTime() <= Date.now()
}

function fmtInviteDate(s: string | null): string {
  if (!s) return t('admin.inviteNeverExpires')
  const d = new Date(s)
  return `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

// ---- 全站公告 ----
const announceText = ref('')
const announceSending = ref(false)

// ---- 系统设置（SMTP / Redis / 对象存储）----

const savingSettings = ref(false)
const testingSMTP = ref(false)
const testEmail = ref('')
/** 服务器实际生效状态（与表单值不是一回事：Redis 改了要重启才生效） */
const settingsMeta = ref({ smtpEnabled: false, redisEnabled: false, storageApplied: false })
/** 密钥字段是否已设置：接口只回 hasXxx，密码框永远空白，不提示的话站长无从判断 */
const secretMeta = ref({ smtpPass: false, redisPass: false, storageKey: false })

const settingsForm = ref<AdminSettingsPayload>({
  smtp: { host: '', port: '465', user: '', from: '', pass: '' },
  redis: { addr: '', db: '0', pass: '' },
  storage: {
    endpoint: '',
    region: '',
    bucket: '',
    accessKey: '',
    secretKey: '',
    publicBase: '',
  },
  site: {
    name: 'ModernChat',
    allowRegister: true,
    maintenanceMode: false,
    maintenanceMessage: '',
    requireInviteCode: false,
    requireEmailVerify: true,
  },
  chat: { maxMessageLength: 1000, recallMinutes: 2, maxGroupMembers: 200, blockedWords: '' },
  upload: { maxImageMB: 8, maxAudioMB: 5, maxFileMB: 20, maxStorageMB: 0 },
  security: { loginMaxAttempts: 5, loginLockMinutes: 15 },
})

/**
 * 建议的 nginx client_max_body_size。
 * 上传上限可以在后台调，但 nginx 那一层不归我们管——超了会直接 413，
 * 而 413 是 nginx 返回的，用户在应用里只会看到「上传失败」，很难联想到配置。
 */
const nginxBodyHint = computed(() => {
  const max = Math.max(
    settingsForm.value.upload.maxImageMB,
    settingsForm.value.upload.maxAudioMB,
    settingsForm.value.upload.maxFileMB,
  )
  return max + 2
})

async function fetchSettings() {
  settingsLoading.value = true
  try {
    const s = await adminApi.settings()
    settingsForm.value = {
      smtp: {
        host: s.smtp.host,
        port: s.smtp.port || '465',
        user: s.smtp.user,
        from: s.smtp.from,
        // 密码永不回显：留空 = 不修改
        pass: '',
      },
      redis: { addr: s.redis.addr, db: s.redis.db || '0', pass: '' },
      storage: {
        endpoint: s.storage.endpoint,
        region: s.storage.region,
        bucket: s.storage.bucket,
        accessKey: s.storage.accessKey,
        secretKey: '',
        publicBase: s.storage.publicBase,
      },
      site: { ...s.site },
      chat: { ...s.chat },
      upload: { ...s.upload },
      security: { ...s.security },
    }
    void loadStorageStats()
    void loadBackups()
    settingsMeta.value = {
      smtpEnabled: s.smtpEnabled,
      redisEnabled: s.redisEnabled,
      storageApplied: s.storageApplied,
    }
    secretMeta.value = {
      smtpPass: s.smtp.hasPass,
      redisPass: s.redis.hasPass,
      storageKey: s.storage.hasSecretKey,
    }
    settingsLoaded.value = true
    settingsLoadFailed.value = false
  } catch (e) {
    // 关键：加载失败时表单里全是硬编码默认值（站点名、注册开关、上传上限…），
    // 若此时允许保存，会把线上真实配置整份覆盖掉
    settingsLoaded.value = false
    settingsLoadFailed.value = true
    void MessagePlugin.error(te(e))
  } finally {
    settingsLoading.value = false
  }
}

// ---- 运维：附件统计 / 孤儿清理 / 数据库备份 ----
const storageStats = ref<StorageStats | null>(null)
const backupDir = ref('')
const backups = ref<BackupFile[]>([])
const storageBusy = ref(false)
const backupBusy = ref(false)

function fmtBytes(n: number): string {
  if (!n) return '0 B'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`
}

async function loadStorageStats() {
  try {
    storageStats.value = await adminApi.storageStats()
  } catch {
    /* 忽略：运维信息属于辅助展示 */
  }
}

async function loadBackups() {
  try {
    const r = await adminApi.backups()
    backupDir.value = r.dir
    backups.value = r.backups ?? []
  } catch {
    /* 忽略 */
  }
}

async function gcStorage(dryRun: boolean) {
  if (!dryRun) {
    const confirmed = await new Promise<boolean>((resolve) => {
      const dlg = DialogPlugin.confirm({
        header: t('admin.gcRun'),
        body: t('admin.gcConfirm'),
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
  }
  storageBusy.value = true
  try {
    const r = await adminApi.gcStorage(dryRun)
    void MessagePlugin.success(
      dryRun
        ? t('admin.gcDryRunOk', { n: r.deleted })
        : t('admin.gcOk', { n: r.deleted, size: fmtBytes(r.freedBytes) }),
    )
    await loadStorageStats()
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    storageBusy.value = false
  }
}

async function doBackup() {
  backupBusy.value = true
  try {
    const r = await adminApi.runBackup()
    void MessagePlugin.success(t('admin.backupOk', { file: r.file }))
    await loadBackups()
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    backupBusy.value = false
  }
}

async function saveSettings() {
  // 兜底：没成功加载过就不允许提交（表单内容不可信）
  if (!settingsLoaded.value) {
    void MessagePlugin.warning(t('admin.settingsNotLoaded'))
    return
  }
  savingSettings.value = true
  try {
    const r = await adminApi.saveSettings(settingsForm.value)
    void MessagePlugin.success(
      r.redisNeedsRestart ? t('admin.settingsSavedNeedRestart') : t('admin.settingsSaved'),
    )
    // 保存后清空密码输入框（已落库，不需要留在表单里）
    settingsForm.value.smtp.pass = ''
    settingsForm.value.redis.pass = ''
    settingsForm.value.storage.secretKey = ''
    await fetchSettings()
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    savingSettings.value = false
  }
}

async function sendTestMail() {
  const to = testEmail.value.trim()
  if (!to) return
  testingSMTP.value = true
  try {
    await adminApi.testSMTP(to)
    void MessagePlugin.success(t('admin.testMailOk'))
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    testingSMTP.value = false
  }
}

// ---- 操作审计 ----
const audits = ref<AdminAudit[]>([])
const auditsLoading = ref(false)
const auditPagination = ref({ current: 1, pageSize: 20, total: 0 })

const loading = ref(false)
const users = ref<AdminUserRow[]>([])
const pagination = ref({ current: 1, pageSize: 20, total: 0 })

// ---- 插件聚合管理 ----
const pluginsLoading = ref(false)
const plugins = ref<AdminPluginRow[]>([])
const pluginPagination = ref({ current: 1, pageSize: 20, total: 0 })

// 当前构建版本（升级后刷新本页即可确认是否生效）
const buildInfo = ref<VersionInfo | null>(null)

// ---- 概览统计 ----
const stats = ref<AdminStats | null>(null)
const statsLoading = ref(false)

// ---- 用户筛选 ----
const keyword = ref('')
const roleFilter = ref<'' | 'user' | 'admin'>('')
const statusFilter = ref<'' | 'active' | 'banned'>('')

// ---- 用户详情 ----
const detailVisible = ref(false)
const detail = ref<AdminUserDetail | null>(null)
const detailLoading = ref(false)

// TDesign 的 TableProps['columns'] 递归类型过深（TS2589），此处放宽为宽松列定义
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const columns = ref<any[]>([])
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const pluginColumns = ref<any[]>([])
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const auditColumns = ref<any[]>([])

function buildColumns() {
  columns.value = [
    { colKey: 'user', title: t('admin.colUser'), width: 200 },
    { colKey: 'username', title: t('admin.colUsername') },
    { colKey: 'role', title: t('admin.colRole'), width: 90 },
    { colKey: 'status', title: t('admin.colStatus'), width: 160 },
    { colKey: 'ops', title: t('admin.colOps'), width: 300 },
  ]
  pluginColumns.value = [
    { colKey: 'name', title: t('admin.colPlugin'), width: 220 },
    { colKey: 'version', title: t('admin.colVersion'), width: 100 },
    { colKey: 'installCount', title: t('admin.colInstalls'), width: 100 },
    { colKey: 'enabledCount', title: t('admin.colEnabled'), width: 100 },
    { colKey: 'globalStatus', title: t('admin.colGlobal'), width: 120 },
    { colKey: 'ops', title: t('admin.colOps'), width: 160 },
  ]
  auditColumns.value = [
    { colKey: 'createdAt', title: t('admin.auditTime'), width: 170 },
    { colKey: 'action', title: t('admin.auditAction'), width: 120 },
    { colKey: 'targetName', title: t('admin.auditTarget'), width: 140 },
    { colKey: 'detail', title: t('admin.auditDetail') },
    { colKey: 'ip', title: t('admin.auditIp'), width: 130 },
  ]
}

onMounted(() => {
  if (auth.user?.role !== 'admin') {
    void router.replace('/chat')
    return
  }
  buildColumns()
  void fetchUsers()
  void fetchStats()
  systemApi
    .version()
    .then((v) => {
      buildInfo.value = v
    })
    .catch(() => undefined)
})

// 切到「审计」页签时重新拉取（见 switchTab）

async function fetchUsers() {
  loading.value = true
  try {
    const page = await adminApi.users(pagination.value.current, pagination.value.pageSize, {
      kw: keyword.value.trim(),
      role: roleFilter.value,
      status: statusFilter.value,
    })
    users.value = page.users ?? []
    pagination.value.total = page.total
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    loading.value = false
  }
}

function onPageChange(info: { current: number; pageSize: number }) {
  pagination.value.current = info.current
  pagination.value.pageSize = info.pageSize
  void fetchUsers()
}

// ---- 概览 ----

/**
 * 写操作后的统一刷新。
 * 审计与概览只在进入页面时拉过一次，操作后不重拉会一直显示旧数据
 * （典型表现：封禁/公告已经成功，审计页签里却看不到记录）。
 */
function refreshAfterWrite() {
  void fetchStats()
  void fetchAudits()
}

async function fetchStats() {
  statsLoading.value = true
  try {
    stats.value = await adminApi.stats()
  } catch {
    /* 概览失败不影响列表主流程 */
  } finally {
    statsLoading.value = false
  }
}

// ---- 筛选 ----

function filterRoleOptions() {
  return [
    { label: t('admin.filterAll'), value: '' },
    { label: t('admin.roleUser'), value: 'user' },
    { label: t('admin.roleAdmin'), value: 'admin' },
  ]
}

function filterStatusOptions() {
  return [
    { label: t('admin.filterAll'), value: '' },
    { label: t('admin.filterActive'), value: 'active' },
    { label: t('admin.filterBanned'), value: 'banned' },
  ]
}

function applyFilter() {
  pagination.value.current = 1
  void fetchUsers()
}

function resetFilter() {
  keyword.value = ''
  roleFilter.value = ''
  statusFilter.value = ''
  applyFilter()
}

// ---- 详情 / 强制下线 ----

async function openDetail(row: AdminUserRow) {
  detailVisible.value = true
  detailLoading.value = true
  detail.value = null
  try {
    detail.value = await adminApi.userDetail(row.id)
  } catch (e) {
    void MessagePlugin.error(te(e))
    detailVisible.value = false
  } finally {
    detailLoading.value = false
  }
}

async function kick(row: AdminUserRow) {
  try {
    const r = await adminApi.kick(row.id)
    void MessagePlugin.success(r.wasOnline ? t('admin.kickOk') : t('admin.kickOffline'))
    refreshAfterWrite()
    void fetchUsers()
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
}

function formatDate(iso: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString()
}

// ---- 全站公告 ----

async function doAnnounce() {
  const content = announceText.value.trim()
  if (!content) return
  announceSending.value = true
  try {
    const r = await adminApi.announce(content)
    announceText.value = ''
    // 站长自己也会收到 message 帧：记下已读位置，回到聊天页时不再弹自己的公告
    markAnnouncementSeen(r.messageId)
    void MessagePlugin.success(t('admin.announceOk', { n: r.recipients }))
    refreshAfterWrite()
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    announceSending.value = false
  }
}

// ---- 角色调整 ----

function confirmDialog(header: string, body: string): Promise<boolean> {
  return new Promise((resolve) => {
    const d = DialogPlugin.confirm({
      header,
      body,
      onConfirm: () => {
        d.hide()
        resolve(true)
      },
      onClose: () => {
        d.hide()
        resolve(false)
      },
    })
  })
}

async function setRole(row: AdminUserRow) {
  const next = row.role === 'admin' ? 'user' : 'admin'
  const ok = await confirmDialog(
    t('admin.setRoleTitle'),
    next === 'admin'
      ? t('admin.setRoleAdminBody', { name: row.nickname })
      : t('admin.setRoleUserBody', { name: row.nickname }),
  )
  if (!ok) return
  try {
    await adminApi.setRole(row.id, next)
    void MessagePlugin.success(t('admin.setRoleOk'))
    refreshAfterWrite()
    void fetchUsers()
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
}

// ---- 强制卸载插件 ----

async function purgePlugin(row: AdminPluginRow) {
  const ok = await confirmDialog(
    t('admin.purgeTitle'),
    t('admin.purgeBody', { name: row.name, n: row.installCount }),
  )
  if (!ok) return
  try {
    const r = await adminApi.purgePlugin(row.name)
    void MessagePlugin.success(t('admin.purgeOk', { n: r.affected }))
    refreshAfterWrite()
    void fetchPlugins()
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
}

// ---- 审计 ----

async function fetchAudits() {
  auditsLoading.value = true
  try {
    const page = await adminApi.audits(
      auditPagination.value.current,
      auditPagination.value.pageSize,
    )
    audits.value = page.audits ?? []
    auditPagination.value.total = page.total
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    auditsLoading.value = false
  }
}

function onAuditPageChange(info: { current: number; pageSize: number }) {
  auditPagination.value.current = info.current
  auditPagination.value.pageSize = info.pageSize
  void fetchAudits()
}

/** 动作名 → 可读文案 */
function actionLabel(action: string): string {
  const map: Record<string, string> = {
    ban: t('admin.actBan'),
    unban: t('admin.actUnban'),
    kick: t('admin.actKick'),
    reset_password: t('admin.actResetPassword'),
    set_role: t('admin.actSetRole'),
    purge_plugin: t('admin.actPurgePlugin'),
    disable_plugin: t('admin.actDisablePlugin'),
    restore_plugin: t('admin.actRestorePlugin'),
    announce: t('admin.actAnnounce'),
  }
  return map[action] ?? action
}

async function fetchPlugins() {
  pluginsLoading.value = true
  try {
    const page = await adminApi.plugins(
      pluginPagination.value.current,
      pluginPagination.value.pageSize,
    )
    // 后端空列表历史上可能序列化成 null（Go nil slice），TDesign 表格拿到 null 会抛错
    plugins.value = page.plugins ?? []
    pluginPagination.value.total = page.total
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    pluginsLoading.value = false
  }
}

function onPluginPageChange(info: { current: number; pageSize: number }) {
  pluginPagination.value.current = info.current
  pluginPagination.value.pageSize = info.pageSize
  void fetchPlugins()
}

// ===== 封禁 =====

const banVisible = ref(false)
const banTarget = ref<AdminUserRow | null>(null)
const banDays = ref(3)
const banReason = ref('')

function openBan(row: AdminUserRow) {
  banTarget.value = row
  banDays.value = 3
  banReason.value = ''
  banVisible.value = true
}

async function doBan() {
  if (!banTarget.value) return
  try {
    await adminApi.ban(banTarget.value.id, banDays.value, banReason.value.trim())
    banVisible.value = false
    void MessagePlugin.success(t('admin.banOk'))
    refreshAfterWrite()
    await fetchUsers()
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
}

async function unban(row: AdminUserRow) {
  try {
    await adminApi.unban(row.id)
    void MessagePlugin.success(t('admin.unbanOk'))
    refreshAfterWrite()
    await fetchUsers()
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
}

async function resetPassword(row: AdminUserRow) {
  const dialog = DialogPlugin.confirm({
    header: t('admin.resetTitle'),
    body: t('admin.resetBody', { name: row.nickname }),
    onConfirm: async () => {
      try {
        const r = await adminApi.resetPassword(row.id)
        DialogPlugin.alert({
          header: t('admin.newPasswordTitle'),
          body: t('admin.newPasswordBody', { pwd: r.newPassword }),
        })
        refreshAfterWrite()
        await fetchUsers()
      } catch (e) {
        void MessagePlugin.error(te(e))
      }
      dialog.destroy()
    },
  })
}

function isBanned(row: AdminUserRow): boolean {
  return !!row.bannedUntil && new Date(row.bannedUntil).getTime() > Date.now()
}

async function toggleGlobalDisabled(row: AdminPluginRow) {
  const next = !row.globalDisabled
  const dialog = DialogPlugin.confirm({
    header: next ? t('admin.disableTitle') : t('admin.enableTitle'),
    body: next
      ? t('admin.disableBody', { name: row.name, n: row.installCount })
      : t('admin.enableBody', { name: row.name }),
    confirmBtn: next ? { content: t('admin.disableBtn'), theme: 'danger' } : t('admin.enableBtn'),
    onConfirm: async () => {
      try {
        const r = await adminApi.setPluginGlobalDisabled(row.name, next)
        void MessagePlugin.success(
          next ? t('admin.disabledOk', { n: r.affected }) : t('admin.enabledOk'),
        )
        refreshAfterWrite()
        await fetchPlugins()
      } catch (e) {
        void MessagePlugin.error(te(e))
      }
      dialog.destroy()
    },
  })
}
</script>

<template>
  <div class="mc-admin">
    <header class="mc-admin__header">
      <h2>{{ t('admin.title') }}</h2>
      <div class="mc-admin__header-right">
        <span
          v-if="buildInfo"
          class="mc-admin__build text-placeholder num"
          :title="t('admin.buildVersion')"
        >
          {{ buildInfo.version }} · {{ buildInfo.commit }} · {{ buildInfo.buildTime }}
        </span>
        <UiButton
          variant="text"
          shape="square"
          :aria-label="t('settings.title')"
          @click="router.push('/settings')"
        >
          <template #icon><SettingIcon /></template>
        </UiButton>
        <UiButton variant="text" @click="router.push('/chat')">{{
          t('admin.backToChat')
        }}</UiButton>
      </div>
    </header>

    <!-- 概览统计 -->
    <section class="mc-admin__stats" :class="{ 'is-loading': statsLoading }">
      <div class="mc-admin__stat">
        <span class="mc-admin__stat-label">{{ t('admin.statUsers') }}</span>
        <strong class="mc-admin__stat-value num">{{ stats?.users.total ?? '—' }}</strong>
        <span class="mc-admin__stat-sub">{{
          t('admin.statToday', { n: stats?.users.today ?? 0 })
        }}</span>
      </div>
      <div class="mc-admin__stat">
        <span class="mc-admin__stat-label">{{ t('admin.statOnline') }}</span>
        <strong class="mc-admin__stat-value num">{{ stats?.online ?? '—' }}</strong>
        <span class="mc-admin__stat-sub">{{ t('admin.statOnlineSub') }}</span>
      </div>
      <div class="mc-admin__stat">
        <span class="mc-admin__stat-label">{{ t('admin.statConvs') }}</span>
        <strong class="mc-admin__stat-value num">
          {{ stats ? stats.conversations.single + stats.conversations.group : '—' }}
        </strong>
        <span class="mc-admin__stat-sub">
          {{
            t('admin.statConvSub', {
              s: stats?.conversations.single ?? 0,
              g: stats?.conversations.group ?? 0,
            })
          }}
        </span>
      </div>
      <div class="mc-admin__stat">
        <span class="mc-admin__stat-label">{{ t('admin.statMsgs') }}</span>
        <strong class="mc-admin__stat-value num">{{ stats?.messages.total ?? '—' }}</strong>
        <span class="mc-admin__stat-sub">{{
          t('admin.statToday', { n: stats?.messages.today ?? 0 })
        }}</span>
      </div>
      <div class="mc-admin__stat">
        <span class="mc-admin__stat-label">{{ t('admin.statPlugins') }}</span>
        <strong class="mc-admin__stat-value num">{{ stats?.plugins.installed ?? '—' }}</strong>
        <span class="mc-admin__stat-sub">
          {{ t('admin.statPluginSub', { n: stats?.plugins.enabled ?? 0 }) }}
        </span>
      </div>
      <div class="mc-admin__stat">
        <span class="mc-admin__stat-label">{{ t('admin.statBanned') }}</span>
        <strong class="mc-admin__stat-value num">{{ stats?.users.banned ?? '—' }}</strong>
        <span class="mc-admin__stat-sub">{{
          t('admin.statAdminSub', { n: stats?.users.admins ?? 0 })
        }}</span>
      </div>
    </section>

    <!-- 全站公告 -->
    <div class="mc-admin__announce">
      <UiInput
        v-model="announceText"
        :placeholder="t('admin.announcePlaceholder')"
        :maxlength="200"
        @enter="doAnnounce"
      />
      <UiButton theme="primary" :loading="announceSending" @click="doAnnounce">
        {{ t('admin.announceSend') }}
      </UiButton>
    </div>

    <div class="mc-admin__tabs">
      <div class="mc-admin__tabbar" role="tablist">
        <button
          v-for="item in tabItems"
          :key="item.value"
          type="button"
          role="tab"
          class="mc-admin__tab"
          :class="{ 'is-active': tab === item.value }"
          :aria-selected="tab === item.value"
          @click="switchTab(item.value)"
        >
          {{ item.label }}
        </button>
      </div>

      <div v-if="tab === 'users'" class="mc-admin__panel" role="tabpanel">
        <div class="mc-admin__filters">
          <UiInput
            v-model="keyword"
            class="mc-admin__search"
            :placeholder="t('admin.searchUser')"
            clearable
            @enter="applyFilter"
            @clear="applyFilter"
          />
          <UiSelect
            v-model="roleFilter"
            :options="filterRoleOptions()"
            :placeholder="t('admin.filterRole')"
            style="width: 130px"
            @change="applyFilter"
          />
          <UiSelect
            v-model="statusFilter"
            :options="filterStatusOptions()"
            :placeholder="t('admin.filterStatus')"
            style="width: 130px"
            @change="applyFilter"
          />
          <UiButton size="small" variant="outline" @click="resetFilter">
            {{ t('admin.resetFilter') }}
          </UiButton>
        </div>

        <TTable
          row-key="id"
          :data="users"
          :columns="columns"
          :loading="loading"
          :pagination="pagination"
          @page-change="onPageChange"
        >
          <template #user="{ row }">
            <div class="mc-admin__user">
              <AvatarBadge
                :name="row.nickname"
                :src="row.avatar"
                :color="row.avatarColor"
                :size="30"
              />
              <span>{{ row.nickname }}</span>
            </div>
          </template>

          <template #role="{ row }">
            {{ row.role === 'admin' ? t('admin.roleAdmin') : t('admin.roleUser') }}
          </template>

          <template #status="{ row }">
            <UiTag v-if="isBanned(row)" theme="danger" variant="light" size="small">
              {{
                row.banReason
                  ? t('admin.banned', { reason: row.banReason })
                  : t('admin.bannedShort')
              }}
            </UiTag>
            <UiTag v-else theme="success" variant="light" size="small">{{
              t('admin.normal')
            }}</UiTag>
          </template>

          <template #ops="{ row }">
            <div class="mc-admin__ops">
              <a @click="openDetail(row)">{{ t('admin.detail') }}</a>
              <a v-if="!isBanned(row)" @click="openBan(row)">{{ t('admin.ban') }}</a>
              <a v-else @click="unban(row)">{{ t('admin.unban') }}</a>
              <a @click="kick(row)">{{ t('admin.kick') }}</a>
              <a @click="setRole(row)">
                {{ row.role === 'admin' ? t('admin.revokeAdmin') : t('admin.grantAdmin') }}
              </a>
              <a @click="resetPassword(row)">{{ t('admin.resetPassword') }}</a>
            </div>
          </template>
        </TTable>
      </div>

      <div v-if="tab === 'plugins'" class="mc-admin__panel" role="tabpanel">
        <p class="mc-admin__hint text-placeholder">{{ t('admin.pluginHint') }}</p>
        <TTable
          row-key="name"
          :data="plugins"
          :columns="pluginColumns"
          :loading="pluginsLoading"
          :pagination="pluginPagination"
          @page-change="onPluginPageChange"
        >
          <template #name="{ row }">
            <span class="mc-admin__plugin-name">{{ row.name }}</span>
          </template>
          <template #installCount="{ row }">
            <span class="mc-admin__num">{{ row.installCount }}</span>
          </template>
          <template #enabledCount="{ row }">
            <span class="mc-admin__num">{{ row.enabledCount }}</span>
          </template>
          <template #globalStatus="{ row }">
            <UiTag v-if="row.globalDisabled" theme="danger" variant="light" size="small">
              {{ t('admin.disabledTag') }}
            </UiTag>
            <UiTag v-else theme="success" variant="light" size="small">{{
              t('admin.globalNormal')
            }}</UiTag>
          </template>
          <template #ops="{ row }">
            <div class="mc-admin__ops">
              <a @click="toggleGlobalDisabled(row)">
                {{ row.globalDisabled ? t('admin.globalEnable') : t('admin.globalDisable') }}
              </a>
              <a class="mc-admin__danger" @click="purgePlugin(row)">{{ t('admin.purge') }}</a>
            </div>
          </template>
        </TTable>
      </div>

      <div v-if="tab === 'audits'" class="mc-admin__panel" role="tabpanel">
        <p class="mc-admin__hint text-placeholder">{{ t('admin.auditHint') }}</p>
        <TTable
          row-key="id"
          :data="audits"
          :columns="auditColumns"
          :loading="auditsLoading"
          :pagination="auditPagination"
          @page-change="onAuditPageChange"
        >
          <template #createdAt="{ row }">{{ formatDate(row.createdAt) }}</template>
          <template #action="{ row }">
            <UiTag variant="outline" size="small">{{ actionLabel(row.action) }}</UiTag>
          </template>
          <template #targetName="{ row }">
            {{ row.targetName || '—' }}
          </template>
          <template #detail="{ row }">
            <span class="mc-admin__audit-detail">{{ row.detail || '—' }}</span>
          </template>
          <template #ip="{ row }">
            <span class="text-placeholder num">{{ row.ip || '—' }}</span>
          </template>
        </TTable>
      </div>

      <div v-if="tab === 'invites'" class="mc-admin__panel" role="tabpanel">
        <p class="mc-admin__hint text-placeholder">{{ t('admin.invitesHint') }}</p>

        <div class="mc-invites__create">
          <label class="mc-settings__field">
            <span>{{ t('admin.inviteMaxUses') }}</span>
            <UiInputNumber v-model="inviteForm.maxUses" :min="1" :max="1000" :step="1" />
          </label>
          <label class="mc-settings__field">
            <span>{{ t('admin.inviteExpiresDays') }}</span>
            <UiInputNumber v-model="inviteForm.expiresDays" :min="0" :max="365" :step="1" />
          </label>
          <label class="mc-settings__field">
            <span>{{ t('admin.inviteCount') }}</span>
            <UiInputNumber v-model="inviteForm.count" :min="1" :max="20" :step="1" />
          </label>
          <UiButton theme="primary" :loading="invitesLoading" @click="createInvites">
            {{ t('admin.inviteGenerate') }}
          </UiButton>
        </div>
        <p class="mc-settings__warn">{{ t('admin.inviteExpiresHint') }}</p>

        <div v-if="invites.length" class="mc-invites__list">
          <div v-for="row in invites" :key="row.id" class="mc-invites__row">
            <button
              type="button"
              class="mc-invites__code num"
              :title="t('admin.inviteCopy')"
              @click="copyInvite(row.code)"
            >
              {{ row.code }}
            </button>
            <span class="mc-invites__uses text-placeholder num">{{ row.uses }}/{{ row.maxUses }}</span>
            <span class="mc-invites__date text-placeholder">{{ fmtInviteDate(row.expiresAt) }}</span>
            <span class="mc-invites__creator text-placeholder">{{ row.creator }}</span>
            <UiTag
              v-if="row.disabled"
              theme="default"
              variant="light"
            >{{ t('admin.inviteDisabled') }}</UiTag>
            <UiTag
              v-else-if="inviteExpired(row) || row.uses >= row.maxUses"
              theme="warning"
              variant="light"
            >{{ t('admin.inviteExhausted') }}</UiTag>
            <UiTag v-else theme="success" variant="light">{{ t('admin.inviteActive') }}</UiTag>
            <UiButton size="small" variant="text" @click="toggleInvite(row)">
              {{ row.disabled ? t('admin.inviteEnable') : t('admin.inviteDisable') }}
            </UiButton>
          </div>
        </div>
        <p v-else-if="!invitesLoading" class="mc-invites__empty text-placeholder">
          {{ t('admin.invitesEmpty') }}
        </p>
      </div>

      <div v-if="tab === 'settings'" class="mc-admin__panel" role="tabpanel">
        <p class="mc-admin__hint text-placeholder">{{ t('admin.settingsHint') }}</p>

        <!-- 加载失败：表单是默认值，保存会覆盖线上配置，所以先锁住并给重试 -->
        <p v-if="settingsLoadFailed" class="mc-settings__warn">
          {{ t('admin.settingsLoadFailed') }}
          <a role="button" tabindex="0" @click="fetchSettings" @keydown.enter="fetchSettings">
            {{ t('common.retry') }}
          </a>
        </p>

        <!-- 站点信息：改了即时生效，前端拉 /api/site -->
        <section class="mc-settings">
          <div class="mc-settings__head">
            <h3 class="mc-settings__title">{{ t('admin.siteTitle') }}</h3>
            <UiTag theme="success" variant="light">{{ t('admin.cfgLive') }}</UiTag>
          </div>
          <p class="mc-settings__desc text-placeholder">{{ t('admin.siteDesc') }}</p>
          <div class="mc-settings__grid">
            <label class="mc-settings__field">
              <span>{{ t('admin.siteName') }}</span>
              <UiInput
                v-model="settingsForm.site.name"
                :maxlength="40"
                :placeholder="t('admin.siteNameHint')"
              />
            </label>
            <label class="mc-settings__field">
              <span>{{ t('admin.siteAllowRegister') }}</span>
              <div class="mc-settings__switch-row">
                <UiSwitch
                  :value="settingsForm.site.allowRegister"
                  @change="(v) => (settingsForm.site.allowRegister = v)"
                />
                <span class="text-placeholder">
                  {{
                    settingsForm.site.allowRegister
                      ? t('admin.allowRegisterOn')
                      : t('admin.allowRegisterOff')
                  }}
                </span>
              </div>
            </label>
            <label class="mc-settings__field">
              <span>{{ t('admin.maintenanceMode') }}</span>
              <div class="mc-settings__switch-row">
                <UiSwitch
                  :value="settingsForm.site.maintenanceMode"
                  @change="(v) => (settingsForm.site.maintenanceMode = v)"
                />
                <span class="text-placeholder">
                  {{
                    settingsForm.site.maintenanceMode
                      ? t('admin.maintenanceOn')
                      : t('admin.maintenanceOff')
                  }}
                </span>
              </div>
            </label>
            <label class="mc-settings__field">
              <span>{{ t('admin.requireInviteCode') }}</span>
              <div class="mc-settings__switch-row">
                <UiSwitch
                  :value="settingsForm.site.requireInviteCode"
                  @change="(v) => (settingsForm.site.requireInviteCode = v)"
                />
                <span class="text-placeholder">{{ t('admin.inviteHint') }}</span>
              </div>
            </label>
            <label class="mc-settings__field">
              <span>{{ t('admin.requireEmailVerify') }}</span>
              <div class="mc-settings__switch-row">
                <UiSwitch
                  :value="settingsForm.site.requireEmailVerify"
                  @change="(v) => (settingsForm.site.requireEmailVerify = v)"
                />
                <span class="text-placeholder">{{ t('admin.emailVerifyHint') }}</span>
              </div>
            </label>
          </div>

          <p v-if="!settingsMeta.smtpEnabled && settingsForm.site.requireEmailVerify" class="mc-settings__warn">
            {{ t('admin.emailVerifyNoSmtp') }}
          </p>

          <label class="mc-settings__field mc-settings__field--wide">
            <span>{{ t('admin.maintenanceMessage') }}</span>
            <UiInput
              v-model="settingsForm.site.maintenanceMessage"
              :maxlength="200"
              :placeholder="t('admin.maintenanceMessageHint')"
            />
          </label>

          <p v-if="!settingsForm.site.allowRegister" class="mc-settings__warn">
            {{ t('admin.registerOffHint') }}
          </p>
          <p v-if="settingsForm.site.maintenanceMode" class="mc-settings__warn">
            {{ t('admin.maintenanceOnHint') }}
          </p>
        </section>

        <!-- 聊天规则：服务端每次请求现读，无需重启 -->
        <section class="mc-settings">
          <div class="mc-settings__head">
            <h3 class="mc-settings__title">{{ t('admin.chatTitle') }}</h3>
            <UiTag theme="success" variant="light">{{ t('admin.cfgLive') }}</UiTag>
          </div>
          <p class="mc-settings__desc text-placeholder">{{ t('admin.chatDesc') }}</p>
          <div class="mc-settings__grid">
            <label class="mc-settings__field">
              <span>{{ t('admin.maxMessageLength') }}</span>
              <UiInputNumber
                v-model="settingsForm.chat.maxMessageLength"
                :min="100"
                :max="5000"
                :step="100"
              />
            </label>
            <label class="mc-settings__field">
              <span>{{ t('admin.recallMinutes') }}</span>
              <UiInputNumber
                v-model="settingsForm.chat.recallMinutes"
                :min="0"
                :max="60"
                :step="1"
              />
            </label>
            <label class="mc-settings__field">
              <span>{{ t('admin.maxGroupMembers') }}</span>
              <UiInputNumber
                v-model="settingsForm.chat.maxGroupMembers"
                :min="2"
                :max="2000"
                :step="10"
              />
            </label>
            <label class="mc-settings__field mc-settings__field--wide">
              <span>{{ t('admin.blockedWords') }}</span>
              <textarea
                v-model="settingsForm.chat.blockedWords"
                class="mc-settings__textarea"
                rows="5"
                :maxlength="2000"
                :placeholder="t('admin.blockedWordsHint')"
              />
            </label>
          </div>
          <p class="mc-settings__warn">{{ t('admin.blockedWordsNote') }}</p>
          <p class="mc-settings__warn">{{ t('admin.recallZeroHint') }}</p>
        </section>

        <!-- 上传上限 -->
        <section class="mc-settings">
          <div class="mc-settings__head">
            <h3 class="mc-settings__title">{{ t('admin.uploadTitle') }}</h3>
            <UiTag theme="success" variant="light">{{ t('admin.cfgLive') }}</UiTag>
          </div>
          <p class="mc-settings__desc text-placeholder">{{ t('admin.uploadDesc') }}</p>
          <div class="mc-settings__grid">
            <label class="mc-settings__field">
              <span>{{ t('admin.maxImageMB') }}</span>
              <UiInputNumber v-model="settingsForm.upload.maxImageMB" :min="1" :max="32" :step="1" />
            </label>
            <label class="mc-settings__field">
              <span>{{ t('admin.maxAudioMB') }}</span>
              <UiInputNumber v-model="settingsForm.upload.maxAudioMB" :min="1" :max="32" :step="1" />
            </label>
            <label class="mc-settings__field">
              <span>{{ t('admin.maxFileMB') }}</span>
              <UiInputNumber v-model="settingsForm.upload.maxFileMB" :min="1" :max="200" :step="1" />
            </label>
            <label class="mc-settings__field">
              <span>{{ t('admin.maxStorageMB') }}</span>
              <UiInputNumber
                v-model="settingsForm.upload.maxStorageMB"
                :min="0"
                :max="102400"
                :step="1024"
              />
            </label>
          </div>
          <p class="mc-settings__warn">{{ t('admin.maxStorageHint') }}</p>
          <p class="mc-settings__warn">{{ t('admin.uploadNginxHint', { mb: nginxBodyHint }) }}</p>
        </section>

        <!-- 运维：附件治理与数据库备份 -->
        <section class="mc-settings">
          <div class="mc-settings__head">
            <h3 class="mc-settings__title">{{ t('admin.opsTitle') }}</h3>
          </div>
          <p class="mc-settings__desc text-placeholder">{{ t('admin.opsDesc') }}</p>
          <div v-if="storageStats" class="mc-ops__stats">
            <div class="mc-ops__stat">
              <span>{{ t('admin.opsFiles') }}</span><b>{{ storageStats.files }}</b>
            </div>
            <div class="mc-ops__stat">
              <span>{{ t('admin.opsSize') }}</span><b>{{ fmtBytes(storageStats.bytes) }}</b>
            </div>
            <div class="mc-ops__stat">
              <span>{{ t('admin.opsReferenced') }}</span><b>{{ storageStats.referenced }}</b>
            </div>
            <div class="mc-ops__stat">
              <span>{{ t('admin.opsOrphans') }}</span>
              <b>{{ storageStats.orphans }} · {{ fmtBytes(storageStats.orphanBytes) }}</b>
            </div>
          </div>
          <div class="mc-settings__actions">
            <UiButton variant="outline" size="small" :loading="storageBusy" @click="gcStorage(true)">
              {{ t('admin.gcDryRun') }}
            </UiButton>
            <UiButton
              variant="outline"
              theme="danger"
              size="small"
              :loading="storageBusy"
              @click="gcStorage(false)"
            >
              {{ t('admin.gcRun') }}
            </UiButton>
            <UiButton variant="outline" size="small" :loading="backupBusy" @click="doBackup">
              {{ t('admin.backupNow') }}
            </UiButton>
          </div>
          <p v-if="backupDir" class="text-placeholder mc-ops__dir">
            {{ t('admin.backupDir', { dir: backupDir }) }}
          </p>
          <div v-if="backups.length" class="mc-ops__backups">
            <div v-for="b in backups.slice(0, 5)" :key="b.name" class="mc-ops__backup">
              <span class="mc-ops__backup-name">{{ b.name }}</span>
              <span class="num text-placeholder">{{ fmtBytes(b.size) }}</span>
            </div>
          </div>
        </section>

        <!-- 登录安全 -->
        <section class="mc-settings">
          <div class="mc-settings__head">
            <h3 class="mc-settings__title">{{ t('admin.securityTitle') }}</h3>
            <UiTag theme="success" variant="light">{{ t('admin.cfgLive') }}</UiTag>
          </div>
          <p class="mc-settings__desc text-placeholder">{{ t('admin.securityDesc') }}</p>
          <div class="mc-settings__grid">
            <label class="mc-settings__field">
              <span>{{ t('admin.loginMaxAttempts') }}</span>
              <UiInputNumber
                v-model="settingsForm.security.loginMaxAttempts"
                :min="0"
                :max="20"
                :step="1"
              />
            </label>
            <label class="mc-settings__field">
              <span>{{ t('admin.loginLockMinutes') }}</span>
              <UiInputNumber
                v-model="settingsForm.security.loginLockMinutes"
                :min="1"
                :max="1440"
                :step="5"
              />
            </label>
          </div>
          <p class="mc-settings__warn">{{ t('admin.loginLockHint') }}</p>
        </section>

        <!-- 邮件（SMTP）：找回密码依赖它 -->
        <section class="mc-settings">
          <div class="mc-settings__head">
            <h3 class="mc-settings__title">{{ t('admin.smtpTitle') }}</h3>
            <UiTag :theme="settingsMeta.smtpEnabled ? 'success' : 'default'" variant="light">
              {{ settingsMeta.smtpEnabled ? t('admin.cfgActive') : t('admin.cfgInactive') }}
            </UiTag>
          </div>
          <p class="mc-settings__desc text-placeholder">{{ t('admin.smtpDesc') }}</p>
          <div class="mc-settings__grid">
            <label class="mc-settings__field">
              <span>{{ t('admin.smtpHost') }}</span>
              <UiInput v-model="settingsForm.smtp.host" placeholder="smtp.qq.com" />
            </label>
            <label class="mc-settings__field">
              <span>{{ t('admin.smtpPort') }}</span>
              <UiInput v-model="settingsForm.smtp.port" placeholder="465" />
            </label>
            <label class="mc-settings__field">
              <span>{{ t('admin.smtpUser') }}</span>
              <UiInput v-model="settingsForm.smtp.user" placeholder="you@qq.com" />
            </label>
            <label class="mc-settings__field">
              <span>{{ t('admin.smtpPass') }}</span>
              <UiInput
                v-model="settingsForm.smtp.pass"
                type="password"
                :placeholder="t('admin.secretPlaceholder')"
              />
              <span v-if="secretMeta.smtpPass" class="mc-settings__hint">
                {{ t('admin.secretSaved') }}
              </span>
            </label>
            <label class="mc-settings__field">
              <span>{{ t('admin.smtpFrom') }}</span>
              <UiInput v-model="settingsForm.smtp.from" :placeholder="t('admin.smtpFromHint')" />
            </label>
          </div>
          <div class="mc-settings__actions">
            <UiInput
              v-model="testEmail"
              class="mc-settings__test-to"
              :placeholder="t('admin.testMailPlaceholder')"
            />
            <UiButton variant="outline" :loading="testingSMTP" @click="sendTestMail">
              {{ t('admin.sendTestMail') }}
            </UiButton>
          </div>
        </section>

        <!-- Redis：限流共享配额 -->
        <section class="mc-settings">
          <div class="mc-settings__head">
            <h3 class="mc-settings__title">{{ t('admin.redisTitle') }}</h3>
            <UiTag :theme="settingsMeta.redisEnabled ? 'success' : 'default'" variant="light">
              {{ settingsMeta.redisEnabled ? t('admin.cfgActive') : t('admin.cfgInactive') }}
            </UiTag>
          </div>
          <p class="mc-settings__desc text-placeholder">{{ t('admin.redisDesc') }}</p>
          <div class="mc-settings__grid">
            <label class="mc-settings__field mc-settings__field--wide">
              <span>{{ t('admin.redisAddr') }}</span>
              <UiInput v-model="settingsForm.redis.addr" placeholder="127.0.0.1:6379" />
            </label>
            <label class="mc-settings__field">
              <span>{{ t('admin.redisDB') }}</span>
              <UiInput v-model="settingsForm.redis.db" placeholder="0" />
            </label>
            <label class="mc-settings__field">
              <span>{{ t('admin.redisPass') }}</span>
              <UiInput
                v-model="settingsForm.redis.pass"
                type="password"
                :placeholder="t('admin.secretPlaceholder')"
              />
              <span v-if="secretMeta.redisPass" class="mc-settings__hint">
                {{ t('admin.secretSaved') }}
              </span>
            </label>
          </div>
          <p class="mc-settings__warn">{{ t('admin.redisRestartHint') }}</p>
        </section>

        <!-- 对象存储：暂无消费方 -->
        <section class="mc-settings">
          <div class="mc-settings__head">
            <h3 class="mc-settings__title">{{ t('admin.storageTitle') }}</h3>
            <UiTag :theme="settingsMeta.storageApplied ? 'success' : 'default'" variant="light">
              {{ settingsMeta.storageApplied ? t('admin.cfgActive') : t('admin.cfgInactive') }}
            </UiTag>
          </div>
          <p class="mc-settings__desc text-placeholder">{{ t('admin.storageDesc') }}</p>
          <div class="mc-settings__grid">
            <label class="mc-settings__field mc-settings__field--wide">
              <span>{{ t('admin.storageEndpoint') }}</span>
              <UiInput
                v-model="settingsForm.storage.endpoint"
                placeholder="https://s3.example.com"
              />
            </label>
            <label class="mc-settings__field">
              <span>{{ t('admin.storageRegion') }}</span>
              <UiInput v-model="settingsForm.storage.region" placeholder="us-east-1" />
            </label>
            <label class="mc-settings__field">
              <span>{{ t('admin.storageBucket') }}</span>
              <UiInput v-model="settingsForm.storage.bucket" placeholder="modernchat" />
            </label>
            <label class="mc-settings__field">
              <span>{{ t('admin.storageAK') }}</span>
              <UiInput v-model="settingsForm.storage.accessKey" placeholder="AKIA..." />
            </label>
            <label class="mc-settings__field">
              <span>{{ t('admin.storageSK') }}</span>
              <UiInput
                v-model="settingsForm.storage.secretKey"
                type="password"
                :placeholder="t('admin.secretPlaceholder')"
              />
              <span v-if="secretMeta.storageKey" class="mc-settings__hint">
                {{ t('admin.secretSaved') }}
              </span>
            </label>
            <label class="mc-settings__field mc-settings__field--wide">
              <span>{{ t('admin.storagePublicBase') }}</span>
              <UiInput
                v-model="settingsForm.storage.publicBase"
                :placeholder="t('admin.storagePublicBaseHint')"
              />
            </label>
          </div>
          <p class="mc-settings__warn">{{ t('admin.storageRestartHint') }}</p>
        </section>

        <div class="mc-settings__save">
          <UiButton
            theme="primary"
            :loading="savingSettings"
            :disabled="!settingsLoaded || settingsLoading"
            @click="saveSettings"
          >
            {{ t('admin.saveSettings') }}
          </UiButton>
        </div>
      </div>
    </div>

    <!-- 封禁弹窗 -->
    <UiDialog v-model:visible="banVisible" :header="t('admin.banTitle')" @confirm="doBan">
      <div class="mc-admin__ban">
        <UiInputNumber
          v-model="banDays"
          :min="0"
          :suffix="t('admin.daysSuffix')"
          style="width: 160px"
        />
        <p class="text-placeholder">{{ t('admin.banHint') }}</p>
        <UiInput v-model="banReason" :placeholder="t('admin.banReason')" clearable />
      </div>
    </UiDialog>

    <!-- 用户详情 -->
    <UiDialog
      v-model:visible="detailVisible"
      :header="t('admin.detailTitle')"
      width="520px"
      :footer="false"
    >
      <p v-if="detailLoading" class="text-placeholder">{{ t('admin.loading') }}</p>
      <div v-else-if="detail" class="mc-admin__detail">
        <div class="mc-admin__detail-head">
          <AvatarBadge
            :name="detail.user.nickname"
            :src="detail.user.avatar"
            :color="detail.user.avatarColor"
            :size="48"
          />
          <div class="mc-admin__detail-id">
            <strong>{{ detail.user.nickname }}</strong>
            <span class="text-placeholder">@{{ detail.user.username }}</span>
          </div>
          <UiTag :theme="detail.online ? 'success' : 'default'" variant="light" size="small">
            {{ detail.online ? t('admin.online') : t('admin.offline') }}
          </UiTag>
        </div>

        <dl class="mc-admin__detail-grid">
          <div>
            <dt>{{ t('admin.detailConvs') }}</dt>
            <dd class="num">{{ detail.stats.conversations }}</dd>
          </div>
          <div>
            <dt>{{ t('admin.detailMsgs') }}</dt>
            <dd class="num">{{ detail.stats.messages }}</dd>
          </div>
          <div>
            <dt>{{ t('admin.detailPlugins') }}</dt>
            <dd class="num">{{ detail.stats.plugins }}</dd>
          </div>
          <div>
            <dt>{{ t('admin.detailJoined') }}</dt>
            <dd>{{ formatDate(detail.user.createdAt) }}</dd>
          </div>
        </dl>

        <p v-if="detail.user.bannedUntil" class="mc-admin__detail-ban">
          {{ t('admin.detailBanned', { reason: detail.user.banReason || '—' }) }}
        </p>
      </div>
    </UiDialog>
  </div>
</template>

<style scoped>
.mc-admin {
  max-width: 980px;
  margin: 0 auto;
  padding: 32px 24px 48px;
}

.mc-admin__header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  margin-bottom: 18px;
}

.mc-admin__header h2 {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
  letter-spacing: -0.01em;
}

.mc-admin__header-right {
  display: flex;
  align-items: center;
  gap: 12px;
}

/* 构建版本做成一枚元信息胶囊，不再是一条游离的长文本 */
.mc-admin__build {
  padding: 4px 8px;
  border-radius: 999px;
  background: var(--td-bg-color-secondarycontainer);
  font-size: var(--mc-fs-sm);
  white-space: nowrap;
}

.mc-admin__user {
  display: flex;
  align-items: center;
  gap: 8px;
}

.mc-admin__ops {
  display: flex;
  flex-wrap: wrap;
  /* 5 个操作挤在一行时，12px 间距会让它们看起来连成一片文字；
     16px + hover 底色之后每一档都能单独识别 */
  gap: 6px 16px;
}

.mc-admin__ops a {
  font-size: 13px;
  white-space: nowrap;
  cursor: pointer;
  /* 负 margin 让 hover 底色外扩，但文字基线不动 */
  padding: 2px 5px;
  margin: -2px -5px;
  border-radius: var(--mc-radius-sm);
  transition: background-color var(--mc-dur-instant) var(--mc-ease-out);
}

.mc-admin__ops a:hover {
  background: var(--td-bg-color-container-hover);
}

.mc-admin__ops a:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 1px;
}

.mc-admin__tabs {
  margin-top: 4px;
}

/* 轻量页签：视觉与 TDesign 下划线页签一致，但不渲染隐藏面板 */
.mc-admin__tabbar {
  display: flex;
  gap: 4px;
  margin-bottom: 14px;
  border-bottom: 1px solid var(--mc-hairline);
}

.mc-admin__tab {
  position: relative;
  padding: 10px 12px 9px;
  border: none;
  background: transparent;
  font: inherit;
  font-size: var(--mc-fs-emphasis);
  color: var(--td-text-color-secondary);
  cursor: pointer;
  border-radius: var(--mc-radius-sm) var(--mc-radius-sm) 0 0;
  transition:
    color var(--mc-dur-instant) var(--mc-ease-out),
    background-color var(--mc-dur-instant) var(--mc-ease-out);
}

.mc-admin__tab:hover {
  color: var(--td-text-color-primary);
  background: var(--td-bg-color-secondarycontainer);
}

.mc-admin__tab.is-active {
  color: var(--td-brand-color);
  font-weight: 600;
}

.mc-admin__tab.is-active::after {
  content: '';
  position: absolute;
  left: 12px;
  right: 12px;
  bottom: -1px;
  height: 2px;
  border-radius: 2px;
  background: var(--td-brand-color);
}

.mc-admin__tab:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 2px;
}

.mc-admin__hint {
  margin: 0 0 12px;
  font-size: 12px;
  line-height: 1.5;
}

/* 每个页签的内容收进一块卡片，页面背景变化时仍有稳定的阅读面 */
.mc-admin__panel {
  padding: 16px;
  background: var(--td-bg-color-container);
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-lg);
  box-shadow: var(--mc-shadow-1);
}

.mc-admin__plugin-name {
  font-weight: 500;
}

/* 数字列右对齐 + 等宽 */
.mc-admin__num {
  display: block;
  text-align: right;
  font-variant-numeric: tabular-nums;
}

.mc-admin__ban {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

/* 表头回归数据本身：无色带、12px 常规灰字 */
.mc-admin :deep(.t-table__header th) {
  background: transparent;
  color: var(--mc-text-faint);
  font-weight: 500;
  font-size: 12px;
  letter-spacing: 0.02em;
}

.mc-admin :deep(.t-table td) {
  font-variant-numeric: tabular-nums;
}

.mc-admin :deep(.t-table__row--hover:hover > td) {
  background: var(--td-bg-color-secondarycontainer);
}

/* ===== 概览统计 ===== */
.mc-admin__stats {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 18px;
  transition: opacity var(--mc-dur-fast) var(--mc-ease-out);
}

.mc-admin__stats.is-loading {
  opacity: 0.55;
}

.mc-admin__stat {
  display: flex;
  flex-direction: column;
  gap: 3px;
  padding: 16px 16px;
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-lg);
  background: var(--td-bg-color-container);
  transition:
    border-color var(--mc-dur-fast) var(--mc-ease-out),
    box-shadow var(--mc-dur-fast) var(--mc-ease-out);
}

.mc-admin__stat:hover {
  border-color: var(--td-component-border);
  box-shadow: var(--mc-shadow-1);
}

.mc-admin__stat-label {
  font-size: 12px;
  color: var(--mc-text-faint);
}

.mc-admin__stat-value {
  font-size: var(--mc-fs-display);
  line-height: 1.2;
  letter-spacing: -0.02em;
  font-variant-numeric: tabular-nums;
}

.mc-admin__stat-sub {
  font-size: var(--mc-fs-sm);
  color: var(--mc-text-faint);
}

/* ===== 筛选栏 ===== */
.mc-admin__filters {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  margin-bottom: 14px;
}

.mc-admin__search {
  flex: 1;
  min-width: 180px;
  max-width: 320px;
}

/* ===== 系统设置 ===== */
.mc-settings {
  padding: 16px 0;
  border-bottom: 1px solid var(--mc-hairline);
}

.mc-settings__head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;
}

.mc-settings__title {
  margin: 0;
  font-size: var(--mc-fs-emphasis);
  font-weight: 600;
}

.mc-settings__desc {
  margin: 0 0 12px;
  font-size: var(--mc-fs-sm);
  line-height: 1.5;
}

.mc-settings__grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 10px 16px;
}

.mc-settings__field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: var(--mc-fs-sm);
  color: var(--td-text-color-secondary);
}

/* UiInput 根元素会带上本组件的 scope id，可直接命中。
   设置页的输入框密排在卡片上，bg-surface 与卡片同色、8% 白的边框几乎看不见，
   改成"凹下"的底色 + 更明显的描边，边界才清楚。 */
.mc-settings__field > div {
  background: var(--td-bg-color-page);
  border-color: var(--td-component-border);
}

.mc-settings__textarea {
  width: 100%;
  min-height: 100px;
  resize: vertical;
  padding: 8px 10px;
  border: 1px solid var(--td-component-border);
  border-radius: var(--mc-radius-sm);
  background: var(--td-bg-color-container);
  color: var(--td-text-color-primary);
  font: inherit;
  font-size: 13px;
  line-height: 1.5;
  outline: none;
}

.mc-settings__textarea:focus {
  border-color: var(--td-brand-color);
}

.mc-settings__field--wide {
  grid-column: 1 / -1;
}

/* 运维：附件统计与备份列表 */
.mc-ops__stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 10px;
  margin-top: 4px;
}

.mc-ops__stat {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 10px 12px;
  border: 1px solid var(--td-component-border);
  border-radius: var(--mc-radius-sm);
  background: var(--td-bg-color-container);
}

.mc-ops__stat span {
  font-size: 12px;
  color: var(--td-text-color-secondary);
}

.mc-ops__stat b {
  font-size: 15px;
  font-weight: 600;
  color: var(--td-text-color-primary);
}

.mc-ops__dir {
  margin: 10px 0 0;
  font-size: 12px;
  word-break: break-all;
}

.mc-ops__backups {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-top: 8px;
}

.mc-ops__backup {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 6px 10px;
  border-radius: var(--mc-radius-sm);
  background: var(--td-bg-color-container);
  font-size: 13px;
}

.mc-ops__backup-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mc-settings__actions {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
}

.mc-settings__test-to {
  width: 260px;
}

.mc-settings__warn {
  margin: 10px 0 0;
  font-size: var(--mc-fs-sm);
  color: var(--td-warning-color);
}

/* 密钥已设置：浅色小字，避免与「必填」类提示混淆 */
.mc-settings__hint {
  margin-top: 4px;
  font-size: 12px;
  color: var(--td-success-color);
}

/* 邀请码管理 */
.mc-invites__create {
  display: grid;
  grid-template-columns: repeat(3, minmax(120px, 1fr)) auto;
  align-items: end;
  gap: 12px;
  margin-bottom: 12px;
}

.mc-invites__list {
  display: flex;
  flex-direction: column;
  gap: 2px;
  margin-top: 16px;
}

.mc-invites__row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border-bottom: 1px solid var(--mc-hairline);
  font-size: var(--mc-fs-sm);
}

.mc-invites__code {
  flex: 1;
  min-width: 0;
  padding: 2px 0;
  border: none;
  background: transparent;
  font: inherit;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  color: var(--td-brand-color);
  text-align: left;
  cursor: pointer;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mc-invites__code:hover {
  text-decoration: underline;
}

.mc-invites__uses,
.mc-invites__date,
.mc-invites__creator {
  flex-shrink: 0;
  font-size: var(--mc-fs-caption);
}

.mc-invites__empty {
  margin: 40px 0;
  text-align: center;
}

/* 开关 + 说明文字同行 */
.mc-settings__switch-row {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 32px;
  font-size: var(--mc-fs-sm);
}

.mc-settings__save {
  padding-top: 16px;
}

/* ===== 全站公告 ===== */
.mc-admin__announce {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px;
  margin-bottom: 18px;
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-lg);
  background: var(--td-bg-color-container);
}

.mc-admin__announce > :first-child {
  flex: 1;
  min-width: 0;
}

/* ===== 危险操作链接 ===== */
.mc-admin__danger {
  color: var(--td-error-color, #d54941);
}

/* ===== 审计详情列（超长省略） ===== */
.mc-admin__audit-detail {
  display: inline-block;
  max-width: 360px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
}

/* ===== 用户详情 ===== */
.mc-admin__detail-head {
  display: flex;
  align-items: center;
  gap: 12px;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--mc-hairline);
}

.mc-admin__detail-id {
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1;
  min-width: 0;
}

.mc-admin__detail-id strong {
  font-size: 16px;
}

.mc-admin__detail-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px 20px;
  margin: 16px 0 0;
}

.mc-admin__detail-grid dt {
  font-size: 12px;
  color: var(--mc-text-faint);
  margin-bottom: 2px;
}

.mc-admin__detail-grid dd {
  margin: 0;
  font-size: 16px;
  font-weight: 500;
}

.mc-admin__detail-ban {
  margin: 16px 0 0;
  padding: 10px 12px;
  border-radius: var(--mc-radius-sm);
  background: var(--td-error-color-1, #fff0ed);
  color: var(--td-error-color, #d54941);
  font-size: 13px;
  line-height: 1.6;
}

@media (max-width: 900px) {
  .mc-admin__stats {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}

@media (max-width: 560px) {
  .mc-admin__stats {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .mc-admin__panel {
    padding: 12px;
  }

  .mc-admin__detail-grid {
    grid-template-columns: 1fr;
  }
}
</style>
