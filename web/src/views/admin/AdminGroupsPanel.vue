<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { MessagePlugin, Table as TTable } from 'tdesign-vue-next'

import UiButton from '@/components/ui/UiButton.vue'
import UiDrawer from '@/components/ui/UiDrawer.vue'
import UiInput from '@/components/ui/UiInput.vue'
import UiTag from '@/components/ui/UiTag.vue'
import { adminApi } from '@/api'
import { useAppI18n } from '@/composables/useAppI18n'
import type {
  AdminGroupDetail,
  AdminGroupMember,
  AdminGroupMessage,
  AdminGroupRow,
} from '@/types/proto'

/**
 * 站长后台 · 群聊管理（旧站迁移 §8，只读）。
 *
 * 结构：
 * - 上半部分：群列表（按群名搜索 + 分页），点行打开右侧抽屉；
 * - 抽屉：群详情（群主 / 成员 / 消息数 / 创建时间 / 公告）+ 成员列表（按角色排序）
 *   + 群消息（向上翻页「加载更多」、按发送者/内容搜索、撤回/管理员删除置灰标记）。
 *
 * 接口返回顺序不做假设：消息统一按 id 升序（旧 → 新）展示，
 * 「加载更多」把更早的消息插到列表顶部，before 取当前最早一条的 id。
 */
const { t, te } = useAppI18n()

// ---- 群列表 ----
const keyword = ref('')
const loading = ref(false)
const groups = ref<AdminGroupRow[]>([])
const pagination = ref({ current: 1, pageSize: 20, total: 0 })

// TDesign 的 TableProps['columns'] 递归类型过深（TS2589），与后台其它表格一致放宽为宽松列定义
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const columns = computed<any[]>(() => [
  { colKey: 'name', title: t('admin.groupColName') },
  { colKey: 'ownerName', title: t('admin.groupColOwner'), width: 150 },
  { colKey: 'memberCount', title: t('admin.groupColMembers'), width: 100 },
  { colKey: 'messageCount', title: t('admin.groupColMessages'), width: 100 },
  { colKey: 'createdAt', title: t('admin.groupColCreatedAt'), width: 180 },
  { colKey: 'ops', title: t('admin.groupColOps'), width: 90 },
])

async function fetchGroups() {
  loading.value = true
  try {
    const page = await adminApi.groups(
      keyword.value.trim(),
      pagination.value.current,
      pagination.value.pageSize,
    )
    groups.value = page.items ?? []
    pagination.value.total = page.total
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    loading.value = false
  }
}

/** 点「搜索」/回车/清空：回到第一页 */
function applySearch() {
  pagination.value.current = 1
  void fetchGroups()
}

function onPageChange(info: { current: number; pageSize: number }) {
  pagination.value.current = info.current
  pagination.value.pageSize = info.pageSize
  void fetchGroups()
}

// TDesign 的 row-click 回调参数是 RowEventContext（含 row/event/index），表格泛型默认是宽松
// 的 TableRowData，这里只取 row，再窄化为群列表行类型
function onRowClick(ctx: { row: unknown }) {
  openGroup(ctx.row as AdminGroupRow)
}

// ---- 群详情抽屉 ----
const detailVisible = ref(false)
const detailLoading = ref(false)
const detail = ref<AdminGroupDetail | null>(null)
const activeGroup = ref<AdminGroupRow | null>(null)

const drawerHeader = computed(() =>
  activeGroup.value
    ? `${t('admin.groupDetailTitle')} · ${activeGroup.value.name}`
    : t('admin.groupDetailTitle'),
)

const roleOrder: Record<AdminGroupMember['role'], number> = { owner: 0, admin: 1, member: 2 }

/** 成员按角色排序：群主 → 管理员 → 普通成员（同角色保持接口顺序） */
const sortedMembers = computed(() =>
  [...(detail.value?.members ?? [])].sort(
    (a, b) => (roleOrder[a.role] ?? 9) - (roleOrder[b.role] ?? 9),
  ),
)

function roleLabel(role: AdminGroupMember['role']): string {
  if (role === 'owner') return t('admin.groupRoleOwner')
  if (role === 'admin') return t('admin.groupRoleAdmin')
  return t('admin.groupRoleMember')
}

function roleTheme(role: AdminGroupMember['role']): 'primary' | 'success' | 'default' {
  if (role === 'owner') return 'primary'
  if (role === 'admin') return 'success'
  return 'default'
}

async function openGroup(row: AdminGroupRow) {
  activeGroup.value = row
  detail.value = null
  detailVisible.value = true
  // 详情与消息并行拉取；任一失败只提示不关抽屉
  detailLoading.value = true
  void adminApi
    .groupDetail(row.id)
    .then((d) => {
      // 抽屉可能已经切到别的群，避免旧响应覆盖
      if (activeGroup.value?.id === row.id) detail.value = d
    })
    .catch((e) => {
      void MessagePlugin.error(te(e))
    })
    .finally(() => {
      if (activeGroup.value?.id === row.id) detailLoading.value = false
    })
  resetMessages()
  void fetchMessages(false)
}

// ---- 群消息 ----
const messages = ref<AdminGroupMessage[]>([])
const msgLoading = ref(false)
const msgHasMore = ref(false)
const msgQuery = ref('')

function resetMessages() {
  messages.value = []
  msgHasMore.value = false
  msgQuery.value = ''
}

async function fetchMessages(append: boolean) {
  const group = activeGroup.value
  if (!group) return
  // 记录本次请求所属的群：请求期间用户切到别的群时，旧响应必须丢弃，
  // 否则会把上一个群的消息混进当前抽屉。
  const reqId = group.id
  msgLoading.value = true
  try {
    const before = append ? messages.value[0]?.id : undefined
    const resp = await adminApi.groupMessages(reqId, {
      before,
      size: 50,
      q: msgQuery.value.trim(),
    })
    if (activeGroup.value?.id !== reqId) return
    // 不假设服务端顺序：按 id 升序展示；加载更多时把更早的消息合并到顶部
    const merged = append ? [...(resp.items ?? []), ...messages.value] : [...(resp.items ?? [])]
    merged.sort((a, b) => a.id - b.id)
    messages.value = merged
    msgHasMore.value = resp.hasMore
  } catch (e) {
    if (activeGroup.value?.id === reqId) void MessagePlugin.error(te(e))
  } finally {
    if (activeGroup.value?.id === reqId) msgLoading.value = false
  }
}

function applyMsgSearch() {
  resetMessages()
  void fetchMessages(false)
}

function loadMore() {
  void fetchMessages(true)
}

// ---- 其他 ----

function formatDate(iso: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString()
}

function isDeleted(m: AdminGroupMessage): boolean {
  return m.isRecalled || m.adminDeleted
}

onMounted(() => {
  void fetchGroups()
})
</script>

<template>
  <div class="mc-groups">
    <div class="mc-groups__toolbar">
      <UiInput
        v-model="keyword"
        class="mc-groups__search"
        size="small"
        clearable
        :placeholder="t('admin.groupSearchPlaceholder')"
        @enter="applySearch"
        @clear="applySearch"
      />
      <UiButton size="small" variant="outline" @click="applySearch">
        {{ t('admin.groupSearch') }}
      </UiButton>
      <span class="mc-groups__total">{{ t('admin.groupTotal', { n: pagination.total }) }}</span>
    </div>

    <TTable
      row-key="id"
      :data="groups"
      :columns="columns"
      :loading="loading"
      :pagination="pagination"
      @page-change="onPageChange"
      @row-click="onRowClick"
    >
      <template #name="{ row }">
        <span class="mc-groups__name">{{ row.name }}</span>
      </template>
      <template #memberCount="{ row }">
        <span class="mc-groups__num">{{ row.memberCount }}</span>
      </template>
      <template #messageCount="{ row }">
        <span class="mc-groups__num">{{ row.messageCount }}</span>
      </template>
      <template #createdAt="{ row }">
        <span class="mc-groups__date">{{ formatDate(row.createdAt) }}</span>
      </template>
      <template #ops="{ row }">
        <button type="button" class="mc-groups__link" @click.stop="openGroup(row)">
          {{ t('admin.groupView') }}
        </button>
      </template>
      <template #empty>
        <div class="mc-groups__empty">{{ t('admin.groupEmpty') }}</div>
      </template>
    </TTable>

    <UiDrawer v-model:visible="detailVisible" :header="drawerHeader" size="min(680px, 94vw)">
      <div v-if="detailLoading && !detail" class="mc-groups__loading">
        {{ t('common.loading') }}
      </div>
      <template v-else-if="detail">
        <section class="mc-groups__meta">
          <div class="mc-groups__meta-item">
            <span class="mc-groups__meta-label">{{ t('admin.groupColOwner') }}</span>
            <span class="mc-groups__meta-value">{{ detail.ownerName || '—' }}</span>
          </div>
          <div class="mc-groups__meta-item">
            <span class="mc-groups__meta-label">{{ t('admin.groupColMembers') }}</span>
            <span class="mc-groups__meta-value">{{ detail.memberCount }}</span>
          </div>
          <div class="mc-groups__meta-item">
            <span class="mc-groups__meta-label">{{ t('admin.groupColMessages') }}</span>
            <span class="mc-groups__meta-value">{{ detail.messageCount }}</span>
          </div>
          <div class="mc-groups__meta-item">
            <span class="mc-groups__meta-label">{{ t('admin.groupColCreatedAt') }}</span>
            <span class="mc-groups__meta-value">{{ formatDate(detail.createdAt) }}</span>
          </div>
        </section>

        <section v-if="detail.announcement" class="mc-groups__section">
          <h3 class="mc-groups__section-title">{{ t('admin.groupAnnouncement') }}</h3>
          <p class="mc-groups__announce">{{ detail.announcement }}</p>
        </section>

        <section class="mc-groups__section">
          <h3 class="mc-groups__section-title">
            {{ t('admin.groupMembersTitle') }} ({{ sortedMembers.length }})
          </h3>
          <p v-if="!sortedMembers.length" class="mc-groups__empty">
            {{ t('admin.groupMembersEmpty') }}
          </p>
          <ul v-else class="mc-groups__members">
            <li v-for="m in sortedMembers" :key="m.userId" class="mc-groups__member">
              <span class="mc-groups__member-name">{{ m.nickname || m.username }}</span>
              <span class="mc-groups__member-user">@{{ m.username }}</span>
              <UiTag :theme="roleTheme(m.role)" variant="light">{{ roleLabel(m.role) }}</UiTag>
              <span class="mc-groups__member-joined">{{ formatDate(m.joinedAt) }}</span>
            </li>
          </ul>
        </section>

        <section class="mc-groups__section">
          <div class="mc-groups__msg-head">
            <h3 class="mc-groups__section-title">{{ t('admin.groupMessagesTitle') }}</h3>
            <div class="mc-groups__msg-search">
              <UiInput
                v-model="msgQuery"
                size="small"
                clearable
                :placeholder="t('admin.groupMsgSearchPlaceholder')"
                @enter="applyMsgSearch"
                @clear="applyMsgSearch"
              />
              <UiButton size="small" variant="outline" :loading="msgLoading" @click="applyMsgSearch">
                {{ t('admin.groupSearch') }}
              </UiButton>
            </div>
          </div>

          <div v-if="msgLoading && !messages.length" class="mc-groups__loading">
            {{ t('common.loading') }}
          </div>
          <template v-else>
            <div v-if="msgHasMore" class="mc-groups__more">
              <UiButton size="small" variant="outline" :loading="msgLoading" @click="loadMore">
                {{ t('admin.groupMsgLoadMore') }}
              </UiButton>
            </div>
            <p v-else-if="messages.length" class="mc-groups__nomore">
              {{ t('admin.groupMsgNoMore') }}
            </p>
            <p v-if="!messages.length" class="mc-groups__empty">
              {{ t('admin.groupMsgEmpty') }}
            </p>
            <ul v-else class="mc-groups__msgs">
              <li
                v-for="m in messages"
                :key="m.id"
                class="mc-groups__msg"
                :class="{ 'is-deleted': isDeleted(m) }"
              >
                <div class="mc-groups__msg-meta">
                  <span class="mc-groups__msg-sender">{{ m.senderName }}</span>
                  <span class="mc-groups__msg-time">{{ formatDate(m.createdAt) }}</span>
                  <UiTag v-if="m.msgType && m.msgType !== 'text'" variant="outline">
                    {{ m.msgType }}
                  </UiTag>
                  <UiTag v-if="m.isRecalled" theme="warning" variant="light">
                    {{ t('admin.groupMsgRecalled') }}
                  </UiTag>
                  <UiTag v-if="m.adminDeleted" theme="danger" variant="light">
                    {{ t('admin.groupMsgAdminDeleted') }}
                  </UiTag>
                  <span class="mc-groups__msg-id">#{{ m.id }}</span>
                </div>
                <div class="mc-groups__msg-content">{{ m.content }}</div>
              </li>
            </ul>
          </template>
        </section>
      </template>
    </UiDrawer>
  </div>
</template>

<style scoped>
.mc-groups {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.mc-groups__toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
}

.mc-groups__search {
  max-width: 280px;
}

.mc-groups__total {
  margin-left: auto;
  font-size: 12px;
  color: var(--mc-text-faint);
  font-variant-numeric: tabular-nums;
}

.mc-groups__name {
  font-weight: 500;
}

.mc-groups__num {
  display: block;
  text-align: right;
  font-variant-numeric: tabular-nums;
}

.mc-groups__date {
  color: var(--mc-text-faint);
  font-variant-numeric: tabular-nums;
}

.mc-groups__link {
  padding: 0;
  border: none;
  background: transparent;
  font: inherit;
  font-size: 12px;
  color: var(--td-brand-color);
  cursor: pointer;
}

.mc-groups__link:hover {
  text-decoration: underline;
}

.mc-groups__empty,
.mc-groups__loading {
  padding: 24px 0;
  text-align: center;
  font-size: 13px;
  color: var(--mc-text-faint);
}

/* ===== 抽屉：群详情 ===== */

.mc-groups__meta {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px 16px;
  padding: 14px;
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-md);
  background: var(--td-bg-color-secondarycontainer);
}

.mc-groups__meta-item {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
}

.mc-groups__meta-label {
  font-size: 12px;
  color: var(--mc-text-faint);
}

.mc-groups__meta-value {
  font-size: 13px;
  color: var(--td-text-color-primary);
  overflow-wrap: anywhere;
}

.mc-groups__section {
  margin-top: 18px;
}

.mc-groups__section-title {
  margin: 0 0 8px;
  font-size: 13px;
  font-weight: 600;
  color: var(--td-text-color-primary);
}

.mc-groups__announce {
  margin: 0;
  padding: 10px 12px;
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-sm);
  background: var(--td-bg-color-secondarycontainer);
  font-size: 13px;
  line-height: 1.6;
  color: var(--td-text-color-secondary);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.mc-groups__members {
  display: flex;
  flex-direction: column;
  margin: 0;
  padding: 0;
  list-style: none;
}

.mc-groups__member {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 2px;
  border-bottom: 1px solid var(--mc-hairline);
  font-size: 13px;
  min-width: 0;
}

.mc-groups__member:last-child {
  border-bottom: none;
}

.mc-groups__member-name {
  color: var(--td-text-color-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mc-groups__member-user {
  color: var(--mc-text-faint);
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mc-groups__member-joined {
  margin-left: auto;
  flex-shrink: 0;
  font-size: 12px;
  color: var(--mc-text-faint);
  font-variant-numeric: tabular-nums;
}

/* ===== 抽屉：群消息 ===== */

.mc-groups__msg-head {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.mc-groups__msg-head .mc-groups__section-title {
  margin: 0;
}

.mc-groups__msg-search {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-left: auto;
  flex: 1 1 240px;
  max-width: 380px;
}

.mc-groups__more {
  display: flex;
  justify-content: center;
  padding: 6px 0 12px;
}

.mc-groups__nomore {
  margin: 0;
  padding: 6px 0 12px;
  text-align: center;
  font-size: 12px;
  color: var(--mc-text-faint);
}

.mc-groups__msgs {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.mc-groups__msg {
  padding: 10px 12px;
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-sm);
  background: var(--td-bg-color-container);
}

/* 撤回 / 管理员删除：整条置灰，仍保留内容供排查 */
.mc-groups__msg.is-deleted {
  opacity: 0.6;
  background: var(--td-bg-color-secondarycontainer);
}

.mc-groups__msg.is-deleted .mc-groups__msg-content {
  text-decoration: line-through;
}

.mc-groups__msg-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 6px;
}

.mc-groups__msg-sender {
  font-size: 13px;
  font-weight: 500;
  color: var(--td-text-color-primary);
}

.mc-groups__msg-time {
  font-size: 12px;
  color: var(--mc-text-faint);
  font-variant-numeric: tabular-nums;
}

.mc-groups__msg-id {
  margin-left: auto;
  font-size: 12px;
  color: var(--mc-text-faint);
  font-variant-numeric: tabular-nums;
}

.mc-groups__msg-content {
  font-size: 13px;
  line-height: 1.65;
  color: var(--td-text-color-primary);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>
