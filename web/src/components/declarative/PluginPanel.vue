<script setup lang="ts">
import { computed } from 'vue'

import DeclarativeNodes from '.'
import { usePluginHost } from '@/composables/usePluginHost'
import type { UIDescNode } from '@/types/proto'

const host = usePluginHost()

/** 面板稳定 id → 可变节点副本。host.state.panels 是 readonly 的响应式对象，
 * 模板里直接 JSON.parse(JSON.stringify(body)) 会随宿主每次重渲全量深拷贝、
 * DeclarativeNodes 也因 props 引用变化被整体重建；这里按面板引用缓存副本，
 * 仅在插件下发新面板（addSidePanel 更新）时重新拷贝。 */
const mutablePanels = computed(() => {
  const out: Array<{ pluginId: number; title: string; nodes: UIDescNode[] }> = []
  for (const [id, panel] of Object.entries(host.state.panels)) {
    out.push({
      pluginId: Number(id),
      title: panel.title,
      nodes: JSON.parse(JSON.stringify(panel.body)) as UIDescNode[],
    })
  }
  return out
})

function onPanelAction(pluginId: number) {
  return (action: string, value?: unknown) => host.panelAction(pluginId, action, value)
}
</script>

<template>
  <div v-if="mutablePanels.length" class="mc-plugin-panel">
    <div v-for="p in mutablePanels" :key="p.pluginId" class="mc-plugin-panel__card">
      <div class="mc-plugin-panel__title">{{ p.title }}</div>
      <DeclarativeNodes :nodes="p.nodes" :on-action="onPanelAction(p.pluginId)" />
    </div>
  </div>
</template>

<style scoped>
.mc-plugin-panel {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 16px;
  min-width: 0;
  overflow-y: auto;
}

.mc-plugin-panel__card {
  background: var(--td-bg-color-container);
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-md);
  padding: 12px 14px;
  box-shadow: var(--mc-shadow-1);
}

.mc-plugin-panel__title {
  font-size: var(--mc-fs-body);
  font-weight: 600;
  margin-bottom: 8px;
  color: var(--td-text-color-primary);
}
</style>
