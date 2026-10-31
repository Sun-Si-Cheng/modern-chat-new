<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

export interface ContextMenuItem {
  key: string
  label: string
  /** 危险操作（如撤回）用红色 */
  danger?: boolean
  disabled?: boolean
  /** 前面加一条分隔线 */
  dividerBefore?: boolean
}

const props = defineProps<{
  visible: boolean
  x: number
  y: number
  items: ContextMenuItem[]
}>()

const emit = defineEmits<{
  (e: 'update:visible', value: boolean): void
  (e: 'select', key: string): void
}>()

const menuRef = ref<HTMLElement | null>(null)
const pos = ref({ x: 0, y: 0 })

/** 定位 + 边界翻转：贴到视口右侧/底部时向内翻转，避免被裁掉 */
watch(
  () => props.visible,
  async (v) => {
    if (!v) return
    pos.value = { x: props.x, y: props.y }
    await nextTick()
    const el = menuRef.value
    if (!el) return
    const pad = 8
    const rect = el.getBoundingClientRect()
    let { x, y } = props
    if (x + rect.width > window.innerWidth - pad) x = Math.max(pad, x - rect.width)
    if (y + rect.height > window.innerHeight - pad) y = Math.max(pad, y - rect.height)
    pos.value = { x, y }
  },
)

function close() {
  if (props.visible) emit('update:visible', false)
}

function choose(item: ContextMenuItem) {
  if (item.disabled) return
  emit('select', item.key)
  close()
}

function onPointerDown(e: MouseEvent) {
  if (!props.visible) return
  if (menuRef.value && !menuRef.value.contains(e.target as Node)) close()
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') close()
}

onMounted(() => {
  // 捕获阶段监听，点菜单外的任何地方都能关闭
  document.addEventListener('pointerdown', onPointerDown, true)
  document.addEventListener('keydown', onKeydown)
  window.addEventListener('scroll', close, true)
  window.addEventListener('resize', close)
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onPointerDown, true)
  document.removeEventListener('keydown', onKeydown)
  window.removeEventListener('scroll', close, true)
  window.removeEventListener('resize', close)
})
</script>

<template>
  <Teleport to="body">
    <Transition name="mc-ctx">
      <div
        v-if="visible"
        ref="menuRef"
        class="mc-ctx"
        role="menu"
        :style="{ left: `${pos.x}px`, top: `${pos.y}px` }"
        @contextmenu.prevent
      >
        <button
          v-for="item in items"
          :key="item.key"
          type="button"
          role="menuitem"
          class="mc-ctx__item"
          :class="{ 'is-danger': item.danger, 'is-disabled': item.disabled, 'has-divider': item.dividerBefore }"
          :disabled="item.disabled"
          @click="choose(item)"
        >
          {{ item.label }}
        </button>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.mc-ctx {
  position: fixed;
  z-index: 3000;
  min-width: 148px;
  padding: 6px;
  border-radius: var(--mc-radius-md);
  border: 1px solid var(--mc-hairline, rgba(0, 0, 0, 0.08));
  background: var(--td-bg-color-container);
  box-shadow: var(--mc-shadow-2);
  transform-origin: top left;
}

.mc-ctx__item {
  display: block;
  width: 100%;
  padding: 9px 12px;
  border: none;
  border-radius: var(--mc-radius-sm);
  background: transparent;
  color: var(--td-text-color-primary);
  font: inherit;
  font-size: 13px;
  text-align: left;
  white-space: nowrap;
  cursor: pointer;
  transition:
    background-color var(--mc-dur-fast) var(--mc-ease-out),
    color var(--mc-dur-fast) var(--mc-ease-out);
}

.mc-ctx__item:hover:not(.is-disabled) {
  background: var(--td-bg-color-secondarycontainer);
}

.mc-ctx__item.is-danger {
  color: var(--td-error-color);
}

.mc-ctx__item.is-danger:hover:not(.is-disabled) {
  background: var(--td-error-color-1, rgba(213, 73, 65, 0.08));
}

.mc-ctx__item.is-disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.mc-ctx__item.has-divider {
  margin-top: 6px;
  border-top: 1px solid var(--mc-hairline, rgba(0, 0, 0, 0.08));
  padding-top: 10px;
  border-top-left-radius: 0;
  border-top-right-radius: 0;
}

.mc-ctx:focus-visible,
.mc-ctx__item:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: -2px;
}

.mc-ctx-enter-active,
.mc-ctx-leave-active {
  transition:
    opacity var(--mc-dur-fast) var(--mc-ease-out),
    transform var(--mc-dur-fast) var(--mc-ease-out);
}

.mc-ctx-enter-from,
.mc-ctx-leave-to {
  opacity: 0;
  transform: scale(0.96);
}

@media (prefers-reduced-motion: reduce) {
  .mc-ctx-enter-active,
  .mc-ctx-leave-active {
    transition: none;
  }
}
</style>
