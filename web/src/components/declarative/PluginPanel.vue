<script setup lang="ts">
import DeclarativeNodes from '.'
import { usePluginHost } from '@/composables/usePluginHost'
import type { UIDescNode } from '@/types/proto'

const host = usePluginHost()

function onPanelAction(pluginId: number) {
  return (action: string, value?: unknown) => host.panelAction(pluginId, action, value)
}

/** readonly store 数据 → 可变副本（渲染组件 prop 类型要求） */
function toMutable(body: unknown): UIDescNode[] {
  return JSON.parse(JSON.stringify(body)) as UIDescNode[]
}
</script>

<template>
  <div v-if="Object.keys(host.state.panels).length" class="mc-plugin-panel">
    <template v-for="(panel, pluginId) in host.state.panels" :key="pluginId">
      <div class="mc-plugin-panel__card">
        <div class="mc-plugin-panel__title">{{ panel.title }}</div>
        <DeclarativeNodes :nodes="toMutable(panel.body)" :on-action="onPanelAction(Number(pluginId))" />
      </div>
    </template>
  </div>
</template>

<style scoped>
.mc-plugin-panel {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px;
  min-width: 0;
  overflow-y: auto;
}

.mc-plugin-panel__card {
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-border);
  border-radius: 8px;
  padding: 10px 12px;
}

.mc-plugin-panel__title {
  font-weight: 600;
  margin-bottom: 6px;
  color: var(--td-text-color-primary);
}
</style>
