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
} from 'tdesign-vue-next'
import type { TableRowData, TableProps } from 'tdesign-vue-next'

import AvatarBadge from '@/components/chat/AvatarBadge.vue'
import { adminApi, type AdminPluginRow, type AdminUserRow } from '@/api'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const auth = useAuthStore()

const tab = ref<'users' | 'plugins'>('users')

const loading = ref(false)
const users = ref<AdminUserRow[]>([])
const total = ref(0)
const pagination = ref({ current: 1, pageSize: 20, total: 0 })

// ---- 插件聚合管理 ----
const pluginsLoading = ref(false)
const plugins = ref<AdminPluginRow[]>([])
const pluginPagination = ref({ current: 1, pageSize: 20, total: 0 })

onMounted(() => {
  if (auth.user?.role !== 'admin') {
    void router.replace('/chat')
    return
  }
  void fetchUsers()
  void fetchPlugins()
})

async function fetchPlugins() {
  pluginsLoading.value = true
  try {
    const page = await adminApi.plugins(pluginPagination.value.current, pluginPagination.value.pageSize)
    plugins.value = page.plugins
    pluginPagination.value.total = page.total
  } catch (e) {
    void MessagePlugin.error((e as Error).message)
  } finally {
    pluginsLoading.value = false
  }
}

function onPluginPageChange(info: { current: number; pageSize: number }) {
  pluginPagination.value.current = info.current
  pluginPagination.value.pageSize = info.pageSize
  void fetchPlugins()
}

async function toggleGlobalDisabled(row: AdminPluginRow) {
  const next = !row.globalDisabled
  const dialog = DialogPlugin.confirm({
    header: next ? '全局下架插件' : '恢复插件',
    body: next
      ? `下架「${row.name}」后，所有用户的该插件将立即停用（${row.installCount} 份安装记录受影响）。`
      : `恢复「${row.name}」后，用户可自行重新启用。`,
    confirmBtn: next ? { content: '下架', theme: 'danger' } : '恢复',
    onConfirm: async () => {
      try {
        const r = await adminApi.setPluginGlobalDisabled(row.name, next)
        void MessagePlugin.success(next ? `已下架（影响 ${r.affected} 份安装）` : '已恢复')
        await fetchPlugins()
      } catch (e) {
        void MessagePlugin.error((e as Error).message)
      }
      dialog.destroy()
    },
  })
}

async function fetchUsers() {
  loading.value = true
  try {
    const page = await adminApi.users(pagination.value.current, pagination.value.pageSize)
    users.value = page.users
    pagination.value.total = page.total
  } catch (e) {
    void MessagePlugin.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

function onPageChange(info: { current: number; pageSize: number }) {
  pagination.value.current = info.current
  pagination.value.pageSize = info.pageSize
  void fetchUsers()
}

// ===== 封禁 =====

const banVisible = ref(false)
const banTarget = ref<AdminUserRow | null>(null)
const banDays = ref(3)
const banReason = ref('')

function openBan(row: TableRowData) {
  banTarget.value = row as AdminUserRow
  banDays.value = 3
  banReason.value = ''
  banVisible.value = true
}

async function doBan() {
  if (!banTarget.value) return
  try {
    await adminApi.ban(banTarget.value.id, banDays.value, banReason.value.trim())
    banVisible.value = false
    void MessagePlugin.success('已封禁并强制下线')
    await fetchUsers()
  } catch (e) {
    void MessagePlugin.error((e as Error).message)
  }
}

async function unban(row: AdminUserRow) {
  try {
    await adminApi.unban(row.id)
    void MessagePlugin.success('已解封')
    await fetchUsers()
  } catch (e) {
    void MessagePlugin.error((e as Error).message)
  }
}

async function resetPassword(row: AdminUserRow) {
  const dialog = DialogPlugin.confirm({
    header: '重置密码',
    body: `确定为「${row.nickname}」生成新随机密码吗？`,
    onConfirm: async () => {
      try {
        const r = await adminApi.resetPassword(row.id)
        DialogPlugin.alert({ header: '新密码', body: `新密码：${r.newPassword}\n\n请立即告知用户，此提示仅显示一次。` })
        await fetchUsers()
      } catch (e) {
        void MessagePlugin.error((e as Error).message)
      }
      dialog.destroy()
    },
  })
}

function isBanned(row: AdminUserRow): boolean {
  return !!row.bannedUntil && new Date(row.bannedUntil).getTime() > Date.now()
}

const columns: TableProps['columns'] = [
  { colKey: 'user', title: '用户', width: 200 },
  { colKey: 'username', title: '用户名' },
  { colKey: 'role', title: '角色', width: 90 },
  { colKey: 'status', title: '状态', width: 160 },
  { colKey: 'ops', title: '操作', width: 220 },
]

const pluginColumns: TableProps['columns'] = [
  { colKey: 'name', title: '插件', width: 220 },
  { colKey: 'version', title: '版本', width: 100 },
  { colKey: 'installCount', title: '安装数', width: 100 },
  { colKey: 'enabledCount', title: '启用数', width: 100 },
  { colKey: 'globalStatus', title: '全局状态', width: 120 },
  { colKey: 'ops', title: '操作', width: 120 },
]
</script>

<template>
  <div class="mc-admin">
    <header class="mc-admin__header">
      <h2>站长后台</h2>
      <TButton variant="text" @click="router.push('/chat')">返回聊天</TButton>
    </header>

    <TTabss v-model="tab" class="mc-admin__tabs">
      <TTabPanel value="users" label="用户">
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
            {{ row.role === 'admin' ? '站长' : '用户' }}
          </template>

          <template #status="{ row }">
            <TTag v-if="isBanned(row)" theme="danger" variant="light" size="small">
              已封禁{{ row.banReason ? `：${row.banReason}` : '' }}
            </TTag>
            <TTag v-else theme="success" variant="light" size="small">正常</TTag>
          </template>

          <template #ops="{ row }">
            <div class="mc-admin__ops">
              <a v-if="!isBanned(row)" @click="openBan(row)">封禁</a>
              <a v-else @click="unban(row)">解封</a>
              <a @click="resetPassword(row)">重置密码</a>
            </div>
          </template>
        </TTable>
      </TTabPanel>

      <TTabPanel value="plugins" label="插件">
        <p class="mc-admin__hint text-placeholder">
          按插件名聚合所有用户的安装记录。全局下架后，所有用户的该插件立即停用。
        </p>
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
            <TTag v-if="row.globalDisabled" theme="danger" variant="light" size="small">已下架</TTag>
            <TTag v-else theme="success" variant="light" size="small">正常</TTag>
          </template>
          <template #ops="{ row }">
            <div class="mc-admin__ops">
              <a @click="toggleGlobalDisabled(row)">{{ row.globalDisabled ? '恢复' : '全局下架' }}</a>
            </div>
          </template>
        </TTable>
      </TTabPanel>
    </TTabss>

    <!-- 封禁弹窗 -->
    <TDialog v-model:visible="banVisible" header="封禁用户" @confirm="doBan">
      <div class="mc-admin__ban">
        <TInputNumber v-model="banDays" :min="0" theme="column" suffix="天" style="width: 160px" />
        <p class="text-placeholder">填 0 表示永久封禁；封禁将立即强制下线（token 失效）。</p>
        <TInput v-model="banReason" placeholder="封禁原因（可选）" clearable />
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

.mc-admin__ops a {
  cursor: pointer;
  font-size: 13px;
}

.mc-admin__ban {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

/* 表头回归数据本身：无色带、12px 常规灰字 */
.mc-admin :deep(.t-table__header th) {
  background: transparent;
  color: #6b6b6b;
  font-weight: 400;
  font-size: 12px;
}

.mc-admin :deep(.t-table td) {
  font-variant-numeric: tabular-nums;
}
</style>
