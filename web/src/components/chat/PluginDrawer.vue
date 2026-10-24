<script setup lang="ts">
import { ref } from 'vue'
import {
  DialogPlugin,
  Drawer as TDrawer,
  Input as TInput,
  MessagePlugin,
  Switch as TSwitch,
  Tag as TTag,
} from 'tdesign-vue-next'

import { pluginApi } from '@/api'
import { usePluginHost } from '@/composables/usePluginHost'
import { usePluginsStore } from '@/stores/plugins'
import type { Plugin } from '@/types/proto'

const visible = defineModel<boolean>({ default: false })
const pluginsStore = usePluginsStore()
const host = usePluginHost()

const urlInput = ref('')
const installing = ref(false)

// ===== 安装 =====

async function onPickFile(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  await installConfirm(() => pluginApi.upload(file))
  input.value = ''
}

async function installFromUrl() {
  const url = urlInput.value.trim()
  if (!url) return
  await installConfirm(async () => {
    const r = await pluginApi.fromUrl(url)
    urlInput.value = ''
    return r
  })
}

/** 安装 / 升级 → 弹权限清单，用户确认后才生效（§8.4） */
async function installConfirm(action: () => Promise<{ plugin: Plugin; updated: boolean }>) {
  installing.value = true
  let plugin: Plugin
  let updated = false
  try {
    const r = await action()
    plugin = r.plugin
    updated = r.updated
  } catch (e) {
    void MessagePlugin.error((e as Error).message || '安装失败')
    installing.value = false
    return
  }
  installing.value = false
  await pluginsStore.load()

  const permText = plugin.permissions.length ? plugin.permissions.join('、') : '无'
  const hostText = plugin.allowedHosts.length ? plugin.allowedHosts.join('、') : '无'
  const dialog = DialogPlugin.confirm({
    header: updated ? `更新「${plugin.name}」到 v${plugin.version}` : `安装「${plugin.name}」v${plugin.version}`,
    body: `该插件申请以下权限：\n${permText}\n\n网络访问白名单：${hostText}\n\n${plugin.description || ''}`,
    confirmBtn: updated ? '更新并重载' : '确认并启用',
    cancelBtn: updated ? '稍后生效' : '暂不启用',
    onConfirm: async () => {
      try {
        if (updated) {
          // 升级：保持原启停状态；已启用则立即重载新代码
          if (plugin.enabled) {
            await host.reloadPlugin(plugin)
            void MessagePlugin.success('已更新并重载')
          } else {
            void MessagePlugin.success('已更新，启用后生效')
          }
        } else {
          await pluginsStore.setEnabled(plugin.id, true)
          await host.reloadPlugin({ ...plugin, enabled: true })
          void MessagePlugin.success('插件已启用')
        }
      } catch (e) {
        void MessagePlugin.error((e as Error).message)
      }
      dialog.destroy()
    },
  })
}

// ===== 启停 / 卸载 =====

async function toggle(plugin: Plugin, enabled: boolean) {
  try {
    await pluginsStore.setEnabled(plugin.id, enabled)
    await host.reloadPlugin({ ...plugin, enabled })
  } catch (e) {
    void MessagePlugin.error((e as Error).message)
  }
}

async function uninstall(plugin: Plugin) {
  const dialog = DialogPlugin.confirm({
    header: '卸载插件',
    body: `确定卸载「${plugin.name}」吗？其存储数据将保留。`,
    confirmBtn: { content: '卸载', theme: 'danger' },
    onConfirm: async () => {
      try {
        await pluginsStore.remove(plugin.id)
        await host.reloadPlugin({ ...plugin, enabled: false })
        void MessagePlugin.success('已卸载')
      } catch (e) {
        void MessagePlugin.error((e as Error).message)
      }
      dialog.destroy()
    },
  })
}

const PERM_LABELS: Record<string, string> = {
  'message:read': '读取新消息',
  'message:send': '发送消息',
  'message:render': '自定义消息渲染',
  'ui:panel': '侧边栏面板',
  storage: '本地存储',
  net: '网络请求',
  'ui:theme': '修改主题',
}
</script>

<template>
  <TDrawer v-model="visible" header="插件管理" size="420px" :footer="false">
    <div class="mc-plugins">
      <!-- 安装 -->
      <div class="mc-plugins__install">
        <label class="mc-plugins__upload">
          <input type="file" accept=".js,application/javascript,text/javascript" @change="onPickFile" />
          <TButton variant="outline" size="small" :loading="installing">上传 JS 文件安装</TButton>
        </label>
        <div class="mc-plugins__url">
          <TInput v-model="urlInput" placeholder="插件 URL（http/https）" clearable @enter="installFromUrl" />
          <TButton variant="outline" size="small" :loading="installing" @click="installFromUrl">URL 安装</TButton>
        </div>
      </div>

      <!-- 列表 -->
      <div v-for="p in pluginsStore.plugins" :key="p.id" class="mc-plugins__item">
        <div class="mc-plugins__head">
          <span class="mc-plugins__name">
            {{ p.name }}
            <span class="text-placeholder">v{{ p.version }}</span>
          </span>
          <TSwitch
            :value="p.enabled && !p.globalDisabled"
            :disabled="p.globalDisabled"
            @change="(v: unknown) => toggle(p, v as boolean)"
          />
        </div>
        <div class="mc-plugins__desc text-secondary">{{ p.description || '暂无描述' }}</div>
        <div class="mc-plugins__tags">
          <TTag v-for="perm in p.permissions" :key="perm" size="small" theme="primary" variant="light">
            {{ PERM_LABELS[perm] ?? perm }}
          </TTag>
          <TTag v-for="h in p.allowedHosts" :key="h" size="small" variant="outline">{{ h }}</TTag>
          <TTag v-if="p.globalDisabled" size="small" theme="danger" variant="light">已被站长下架</TTag>
        </div>
        <div class="mc-plugins__ops">
          <a class="mc-plugins__remove" @click="uninstall(p)">卸载</a>
        </div>
      </div>

      <div v-if="!pluginsStore.plugins.length" class="text-placeholder mc-plugins__empty">
        还没有安装插件。上传一个 JS 文件或用 URL 安装试试。
      </div>
    </div>
  </TDrawer>
</template>

<style scoped>
.mc-plugins {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.mc-plugins__install {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px;
  border: 1px dashed var(--td-component-border);
  border-radius: 8px;
}

.mc-plugins__upload input {
  display: none;
}

.mc-plugins__url {
  display: flex;
  gap: 8px;
}

.mc-plugins__item {
  border: 1px solid var(--td-component-border);
  border-radius: 8px;
  padding: 12px;
}

.mc-plugins__head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.mc-plugins__name {
  font-weight: 600;
}

.mc-plugins__desc {
  font-size: 13px;
  margin: 6px 0;
  white-space: pre-wrap;
}

.mc-plugins__tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.mc-plugins__ops {
  margin-top: 8px;
  text-align: right;
}

.mc-plugins__remove {
  font-size: 12px;
  color: var(--td-error-color);
  cursor: pointer;
}

.mc-plugins__empty {
  text-align: center;
  padding: 32px 0;
}
</style>
