<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  Dialog as TDialog,
  DialogPlugin,
  Input as TInput,
  InputNumber as TInputNumber,
  MessagePlugin,
  Table as TTable,
  Tag as TTag,
} from 'tdesign-vue-next'
import type { TableRowData, TableProps } from 'tdesign-vue-next'

import AvatarBadge from '@/components/chat/AvatarBadge.vue'
import { adminApi, type AdminUserRow } from '@/api'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const auth = useAuthStore()

const loading = ref(false)
const users = ref<AdminUserRow[]>([])
const total = ref(0)
const pagination = ref({ current: 1, pageSize: 20, total: 0 })

onMounted(() => {
  if (auth.user?.role !== 'admin') {
    void router.replace('/chat')
    return
  }
  void fetchUsers()
})

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
</script>

<template>
  <div class="mc-admin">
    <header class="mc-admin__header">
      <h2>站长后台</h2>
      <TButton variant="text" @click="router.push('/chat')">返回聊天</TButton>
    </header>

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

.mc-admin__ops a {
  cursor: pointer;
  font-size: 13px;
}

.mc-admin__ban {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
</style>
