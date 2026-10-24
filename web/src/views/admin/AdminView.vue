<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  Dialog as TDialog,
  DialogPlugin,
  Input as TInput,
  InputNumber as TInputNumber,
  MessagePlugin,
  TabPanel as TTabPanel,
  Tabs as TTabss,
  Table as TTable,
  Tag as TTag,
  Button as TButton,
} from 'tdesign-vue-next'


import AvatarBadge from '@/components/chat/AvatarBadge.vue'
import PreferencesMenu from '@/components/common/PreferencesMenu.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import { adminApi, systemApi, type AdminPluginRow, type AdminUserRow, type VersionInfo } from '@/api'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const auth = useAuthStore()
const { t, te } = useAppI18n()

const tab = ref<'users' | 'plugins'>('users')

const loading = ref(false)
const users = ref<AdminUserRow[]>([])
const total = ref(0)
const pagination = ref({ current: 1, pageSize: 20, total: 0 })

// ---- 插件聚合管理 ----
const pluginsLoading = ref(false)
const plugins = ref<AdminPluginRow[]>([])
const pluginPagination = ref({ current: 1, pageSize: 20, total: 0 })

// 当前构建版本（升级后刷新本页即可确认是否生效）
const buildInfo = ref<VersionInfo | null>(null)

// TDesign 的 TableProps['columns'] 递归类型过深（TS2589），此处放宽为宽松列定义
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const columns = ref<any[]>([])
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const pluginColumns = ref<any[]>([])

function buildColumns() {
  columns.value = [
    { colKey: 'user', title: t('admin.colUser'), width: 200 },
    { colKey: 'username', title: t('admin.colUsername') },
    { colKey: 'role', title: t('admin.colRole'), width: 90 },
    { colKey: 'status', title: t('admin.colStatus'), width: 160 },
    { colKey: 'ops', title: t('admin.colOps'), width: 220 },
  ]
  pluginColumns.value = [
    { colKey: 'name', title: t('admin.colPlugin'), width: 220 },
    { colKey: 'version', title: t('admin.colVersion'), width: 100 },
    { colKey: 'installCount', title: t('admin.colInstalls'), width: 100 },
    { colKey: 'enabledCount', title: t('admin.colEnabled'), width: 100 },
    { colKey: 'globalStatus', title: t('admin.colGlobal'), width: 120 },
    { colKey: 'ops', title: t('admin.colOps'), width: 120 },
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
    const page = await adminApi.users(pagination.value.current, pagination.value.pageSize)
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

async function fetchPlugins() {
  pluginsLoading.value = true
  try {
    const page = await adminApi.plugins(pluginPagination.value.current, pluginPagination.value.pageSize)
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
        void MessagePlugin.success(next ? t('admin.disabledOk', { n: r.affected }) : t('admin.enabledOk'))
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
        <span v-if="buildInfo" class="mc-admin__build text-placeholder num" :title="t('admin.buildVersion')">
          {{ buildInfo.version }} · {{ buildInfo.commit }} · {{ buildInfo.buildTime }}
        </span>
        <PreferencesMenu />
        <TButton variant="text" @click="router.push('/chat')">{{ t('admin.backToChat') }}</TButton>
      </div>
    </header>

    <TTabss v-model="tab" class="mc-admin__tabs">
      <TTabPanel value="users" :label="t('admin.tabUsers')">
        <TTable
          row-key="id"
          :data="users"
          :columns="columns"
          :loading="loading"
          :pagination="{ ...pagination, total }"
          @page-change="onPageChange"
        >
          <template #user="{ row }">
            <div class="mc-admin__user">
              <AvatarBadge :name="row.nickname" :color="row.avatarColor" :size="30" />
              <span>{{ row.nickname }}</span>
            </div>
          </template>

          <template #role="{ row }">
            {{ row.role === 'admin' ? t('admin.roleAdmin') : t('admin.roleUser') }}
          </template>

          <template #status="{ row }">
            <TTag v-if="isBanned(row)" theme="danger" variant="light" size="small">
              {{ row.banReason ? t('admin.banned', { reason: row.banReason }) : t('admin.bannedShort') }}
            </TTag>
            <TTag v-else theme="success" variant="light" size="small">{{ t('admin.normal') }}</TTag>
          </template>

          <template #ops="{ row }">
            <div class="mc-admin__ops">
              <a v-if="!isBanned(row)" @click="openBan(row)">{{ t('admin.ban') }}</a>
              <a v-else @click="unban(row)">{{ t('admin.unban') }}</a>
              <a @click="resetPassword(row)">{{ t('admin.resetPassword') }}</a>
            </div>
          </template>
        </TTable>
      </TTabPanel>

      <TTabPanel value="plugins" :label="t('admin.tabPlugins')">
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
            <TTag v-else theme="success" variant="light" size="small">{{ t('admin.globalNormal') }}</TTag>
          </template>
          <template #ops="{ row }">
            <div class="mc-admin__ops">
              <a @click="toggleGlobalDisabled(row)">
                {{ row.globalDisabled ? t('admin.globalEnable') : t('admin.globalDisable') }}
              </a>
            </div>
          </template>
        </TTable>
      </TTabPanel>
    </TTabss>

    <!-- 封禁弹窗 -->
    <TDialog v-model:visible="banVisible" :header="t('admin.banTitle')" @confirm="doBan">
      <div class="mc-admin__ban">
        <TInputNumber v-model="banDays" :min="0" theme="column" :suffix="t('admin.daysSuffix')" style="width: 160px" />
        <p class="text-placeholder">{{ t('admin.banHint') }}</p>
        <TInput v-model="banReason" :placeholder="t('admin.banReason')" clearable />
      </div>
    </TDialog>
  </div>
</template>

<style scoped>
.mc-admin {
  max-width: 960px;
  margin: 0 auto;
  padding: 24px;
}

.mc-admin__header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.mc-admin__header-right {
  display: flex;
  align-items: center;
  gap: 12px;
}

.mc-admin__build {
  font-size: 12px;
}

.mc-admin__user {
  display: flex;
  align-items: center;
  gap: 8px;
}

.mc-admin__ops {
  display: flex;
  gap: 12px;
}

.mc-admin__tabs {
  margin-top: 4px;
}

.mc-admin__hint {
  margin: 0 0 12px;
  font-size: 12px;
  line-height: 1.5;
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
  gap: 10px;
}

/* 表头回归数据本身：无色带、12px 常规灰字 */
.mc-admin :deep(.t-table__header th) {
  background: transparent;
  color: var(--mc-text-faint);
  font-weight: 400;
  font-size: 12px;
}

.mc-admin :deep(.t-table td) {
  font-variant-numeric: tabular-nums;
}
</style>
