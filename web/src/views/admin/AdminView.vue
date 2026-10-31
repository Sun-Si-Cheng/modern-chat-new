<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  Dialog as TDialog,
  DialogPlugin,
  Input as TInput,
  InputNumber as TInputNumber,
  MessagePlugin,
  Select as TSelect,
  TabPanel as TTabPanel,
  Tabs as TTabss,
  Table as TTable,
  Tag as TTag,
  Button as TButton,
} from 'tdesign-vue-next'
import { SettingIcon } from 'tdesign-icons-vue-next'

import AvatarBadge from '@/components/chat/AvatarBadge.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import {
  adminApi,
  systemApi,
  type AdminAudit,
  type AdminPluginRow,
  type AdminStats,
  type AdminUserDetail,
  type AdminUserRow,
  type VersionInfo,
} from '@/api'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const auth = useAuthStore()
const { t, te } = useAppI18n()

const tab = ref<'users' | 'plugins' | 'audits'>('users')

// ---- 全站公告 ----
const announceText = ref('')
const announceSending = ref(false)

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
  void fetchPlugins()
  void fetchStats()
  void fetchAudits()
  systemApi
    .version()
    .then((v) => {
      buildInfo.value = v
    })
    .catch(() => undefined)
})

async function fetchUsers() {
  loading.value = true
  try {
    const page = await adminApi.users(pagination.value.current, pagination.value.pageSize, {
      kw: keyword.value.trim(),
      role: roleFilter.value,
      status: statusFilter.value,
    })
    users.value = page.users
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
    void fetchUsers()
    void fetchStats()
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
    void MessagePlugin.success(t('admin.announceOk', { n: r.recipients }))
    void fetchStats()
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
    void fetchUsers()
    void fetchStats()
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
    void fetchPlugins()
    void fetchStats()
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
    audits.value = page.audits
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
    plugins.value = page.plugins
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
    await fetchUsers()
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
}

async function unban(row: AdminUserRow) {
  try {
    await adminApi.unban(row.id)
    void MessagePlugin.success(t('admin.unbanOk'))
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
        <TButton
          variant="text"
          shape="square"
          :aria-label="t('settings.title')"
          @click="router.push('/settings')"
        >
          <template #icon><SettingIcon /></template>
        </TButton>
        <TButton variant="text" @click="router.push('/chat')">{{ t('admin.backToChat') }}</TButton>
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
      <TInput
        v-model="announceText"
        :placeholder="t('admin.announcePlaceholder')"
        :maxlength="200"
        @enter="doAnnounce"
      />
      <TButton theme="primary" :loading="announceSending" @click="doAnnounce">
        {{ t('admin.announceSend') }}
      </TButton>
    </div>

    <TTabss v-model="tab" class="mc-admin__tabs">
      <TTabPanel value="users" :label="t('admin.tabUsers')">
        <div class="mc-admin__panel">
          <div class="mc-admin__filters">
            <TInput
              v-model="keyword"
              class="mc-admin__search"
              :placeholder="t('admin.searchUser')"
              clearable
              @enter="applyFilter"
              @clear="applyFilter"
            />
            <TSelect
              v-model="roleFilter"
              :options="filterRoleOptions()"
              :placeholder="t('admin.filterRole')"
              style="width: 130px"
              @change="applyFilter"
            />
            <TSelect
              v-model="statusFilter"
              :options="filterStatusOptions()"
              :placeholder="t('admin.filterStatus')"
              style="width: 130px"
              @change="applyFilter"
            />
            <TButton size="small" variant="outline" @click="resetFilter">
              {{ t('admin.resetFilter') }}
            </TButton>
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
              <TTag v-if="isBanned(row)" theme="danger" variant="light" size="small">
                {{
                  row.banReason
                    ? t('admin.banned', { reason: row.banReason })
                    : t('admin.bannedShort')
                }}
              </TTag>
              <TTag v-else theme="success" variant="light" size="small">{{
                t('admin.normal')
              }}</TTag>
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
      </TTabPanel>

      <TTabPanel value="plugins" :label="t('admin.tabPlugins')">
        <div class="mc-admin__panel">
          <p class="mc-admin__hint text-placeholder">{{ t('admin.pluginHint') }}</p>
          <TTable
            row-key="name"
            :data="plugins"
            :columns="pluginColumns"
            :loading="pluginsLoading"
            :pagination="{ ...pluginPagination }"
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
              <TTag v-if="row.globalDisabled" theme="danger" variant="light" size="small">
                {{ t('admin.disabledTag') }}
              </TTag>
              <TTag v-else theme="success" variant="light" size="small">{{
                t('admin.globalNormal')
              }}</TTag>
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
      </TTabPanel>

      <TTabPanel value="audits" :label="t('admin.tabAudits')">
        <div class="mc-admin__panel">
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
              <TTag variant="outline" size="small">{{ actionLabel(row.action) }}</TTag>
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
      </TTabPanel>
    </TTabss>

    <!-- 封禁弹窗 -->
    <TDialog v-model:visible="banVisible" :header="t('admin.banTitle')" @confirm="doBan">
      <div class="mc-admin__ban">
        <TInputNumber
          v-model="banDays"
          :min="0"
          theme="column"
          :suffix="t('admin.daysSuffix')"
          style="width: 160px"
        />
        <p class="text-placeholder">{{ t('admin.banHint') }}</p>
        <TInput v-model="banReason" :placeholder="t('admin.banReason')" clearable />
      </div>
    </TDialog>

    <!-- 用户详情 -->
    <TDialog
      v-model:visible="detailVisible"
      :header="t('admin.detailTitle')"
      :footer="false"
      width="520px"
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
          <TTag :theme="detail.online ? 'success' : 'default'" variant="light" size="small">
            {{ detail.online ? t('admin.online') : t('admin.offline') }}
          </TTag>
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
    </TDialog>
  </div>
</template>

<style scoped>
.mc-admin {
  max-width: 980px;
  margin: 0 auto;
  padding: 28px 24px 48px;
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
  padding: 3px 10px;
  border-radius: 999px;
  background: var(--td-bg-color-secondarycontainer);
  font-size: 11.5px;
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
  gap: 4px 12px;
}

.mc-admin__ops a {
  font-size: 13px;
  white-space: nowrap;
}

.mc-admin__tabs {
  margin-top: 4px;
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
  padding: 14px 16px;
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
  font-size: 24px;
  line-height: 1.2;
  letter-spacing: -0.02em;
  font-variant-numeric: tabular-nums;
}

.mc-admin__stat-sub {
  font-size: 11.5px;
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
