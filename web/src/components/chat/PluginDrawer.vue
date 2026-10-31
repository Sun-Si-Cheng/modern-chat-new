<script setup lang="ts">
import { ref, watch } from 'vue'
import {
  Button as TButton,
  DialogPlugin,
  Drawer as TDrawer,
  Input as TInput,
  MessagePlugin,
  Switch as TSwitch,
  Tag as TTag,
} from 'tdesign-vue-next'

import { useAppI18n } from '@/composables/useAppI18n'
import { pluginApi } from '@/api'
import { usePluginHost } from '@/composables/usePluginHost'
import { usePluginsStore } from '@/stores/plugins'
import type { Plugin } from '@/types/proto'

const visible = defineModel<boolean>({ default: false })
const pluginsStore = usePluginsStore()
const host = usePluginHost()
const { t, te } = useAppI18n()

const urlInput = ref('')
const installing = ref(false)

/**
 * 抽屉打开时拉全量清单（含已停用）：宿主的 active 列表只有启用项，
 * 若沿用它，停用后的插件将从抽屉里消失、再也无法重新启用。
 */
watch(
  visible,
  (v) => {
    if (!v) return
    pluginsStore.load().catch((e) => {
      void MessagePlugin.error(te(e))
    })
  },
  { immediate: true },
)

// ===== 安装 / 升级 =====

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
    void MessagePlugin.error(te(e) || t('plugin.installFailed'))
    installing.value = false
    return
  }
  installing.value = false
  await pluginsStore.load()

  const permText = plugin.permissions.length
    ? plugin.permissions.map((p) => permLabel(p)).join(', ')
    : t('plugin.noPerm')
  const hostText = plugin.allowedHosts.length ? plugin.allowedHosts.join(', ') : t('plugin.noPerm')
  const dialog = DialogPlugin.confirm({
    header: updated
      ? t('plugin.updateTitle', { name: plugin.name, version: plugin.version })
      : t('plugin.installTitle', { name: plugin.name, version: plugin.version }),
    body: t('plugin.permBody', {
      perms: permText,
      hosts: hostText,
      desc: plugin.description || '',
    }),
    confirmBtn: updated ? t('plugin.confirmUpdate') : t('plugin.confirmEnable'),
    cancelBtn: updated ? t('plugin.laterReload') : t('plugin.laterEnable'),
    onConfirm: async () => {
      try {
        if (updated) {
          // 升级：保持原启停状态；已启用则立即重载新代码
          if (plugin.enabled) {
            await host.reloadPlugin(plugin)
            void MessagePlugin.success(t('plugin.updatedReloadOk'))
          } else {
            void MessagePlugin.success(t('plugin.updatedSavedOk'))
          }
        } else {
          await pluginsStore.setEnabled(plugin.id, true)
          await host.reloadPlugin({ ...plugin, enabled: true })
          void MessagePlugin.success(t('plugin.enabledOk'))
        }
      } catch (e) {
        void MessagePlugin.error(te(e))
      }
      dialog.destroy()
    },
  })
}

/** 权限标签（未知权限原样显示） */
function permLabel(perm: string): string {
  const key = `plugin.perms.${perm}`
  const label = t(key)
  return label === key ? perm : label
}

// ===== 启停 / 卸载 =====

async function toggle(plugin: Plugin, enabled: boolean) {
  try {
    await pluginsStore.setEnabled(plugin.id, enabled)
    await host.reloadPlugin({ ...plugin, enabled })
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
}

async function uninstall(plugin: Plugin) {
  const dialog = DialogPlugin.confirm({
    header: t('plugin.uninstallTitle'),
    body: t('plugin.uninstallBody', { name: plugin.name }),
    confirmBtn: { content: t('plugin.uninstallBtn'), theme: 'danger' },
    onConfirm: async () => {
      try {
        await pluginsStore.remove(plugin.id)
        await host.reloadPlugin({ ...plugin, enabled: false })
        void MessagePlugin.success(t('plugin.uninstallOk'))
      } catch (e) {
        void MessagePlugin.error(te(e))
      }
      dialog.destroy()
    },
  })
}
</script>

<template>
  <!-- TDesign 的 Drawer 只有 visible prop（没有 modelValue），必须写全 v-model:visible，
       否则值会被当作 HTML 属性落到 DOM 上，抽屉永远停在屏幕外 -->
  <TDrawer v-model:visible="visible" :header="t('plugin.title')" size="420px" :footer="false">
    <div class="mc-plugins">
      <!-- 安装 -->
      <div class="mc-plugins__install">
        <label class="mc-plugins__upload">
          <input type="file" accept=".js,application/javascript,text/javascript" @change="onPickFile" />
          <TButton variant="outline" size="small" :loading="installing">{{ t('plugin.upload') }}</TButton>
        </label>
        <div class="mc-plugins__url">
          <TInput v-model="urlInput" :placeholder="t('plugin.urlPlaceholder')" clearable @enter="installFromUrl" />
          <TButton variant="outline" size="small" :loading="installing" @click="installFromUrl">
            {{ t('plugin.urlInstall') }}
          </TButton>
        </div>
      </div>

      <!-- 列表 -->
      <div v-for="p in pluginsStore.plugins" :key="p.id" class="mc-plugins__item">
        <div class="mc-plugins__head">
          <span class="mc-plugins__name">
            {{ p.name }}
            <span class="text-placeholder num">v{{ p.version }}</span>
          </span>
          <TSwitch
            :value="p.enabled && !p.globalDisabled"
            :disabled="p.globalDisabled"
            @change="(v: unknown) => toggle(p, v as boolean)"
          />
        </div>
        <div class="mc-plugins__desc text-secondary">{{ p.description || t('plugin.noDesc') }}</div>
        <div class="mc-plugins__tags">
          <TTag v-for="perm in p.permissions" :key="perm" size="small" theme="primary" variant="light">
            {{ permLabel(perm) }}
          </TTag>
          <TTag v-for="h in p.allowedHosts" :key="h" size="small" variant="outline">{{ h }}</TTag>
          <TTag v-if="p.globalDisabled" size="small" theme="danger" variant="light">
            {{ t('plugin.globalDisabled') }}
          </TTag>
        </div>
        <div class="mc-plugins__ops">
          <a class="mc-plugins__remove" @click="uninstall(p)">{{ t('plugin.uninstall') }}</a>
        </div>
      </div>

      <div v-if="!pluginsStore.plugins.length" class="text-placeholder mc-plugins__empty">
        {{ t('plugin.empty') }}
      </div>
    </div>
  </TDrawer>
</template>

<style scoped>
.mc-plugins {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.mc-plugins__install {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 14px;
  border: 1px dashed var(--td-component-border);
  border-radius: var(--mc-radius-lg);
  background: var(--td-bg-color-secondarycontainer);
}

.mc-plugins__upload input {
  display: none;
}

.mc-plugins__url {
  display: flex;
  gap: 8px;
}

.mc-plugins__item {
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-md);
  padding: 14px;
  transition:
    border-color var(--mc-dur-fast) var(--mc-ease-out),
    box-shadow var(--mc-dur-fast) var(--mc-ease-out);
}

.mc-plugins__item:hover {
  border-color: var(--td-component-border);
  box-shadow: var(--mc-shadow-1);
}

.mc-plugins__head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}

.mc-plugins__name {
  min-width: 0;
  display: flex;
  align-items: baseline;
  gap: 6px;
  font-size: 14px;
  font-weight: 600;
}

.mc-plugins__desc {
  font-size: 12.5px;
  line-height: 1.6;
  margin: 8px 0;
  white-space: pre-wrap;
}

.mc-plugins__tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.mc-plugins__ops {
  margin-top: 10px;
  text-align: right;
}

.mc-plugins__remove {
  font-size: 12px;
  color: var(--td-error-color);
  cursor: pointer;
}

.mc-plugins__remove:hover {
  text-decoration: underline;
}

.mc-plugins__empty {
  text-align: center;
  padding: 48px 0;
  font-size: 13px;
}
</style>
