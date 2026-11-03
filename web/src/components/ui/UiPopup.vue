<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

/**
 * 浮层（UnoCSS 实现，替代 TDesign Popup）。
 *
 * - 受控：`v-model:visible`；非受控：不传 visible 时内部自己管理
 * - 定位：Teleport 到 body + fixed，按触发元素 rect 计算，自动做视口内翻转
 * - 关闭：点外部 / Esc / 滚动 / 窗口尺寸变化
 */
const props = withDefaults(
  defineProps<{
    visible?: boolean
    /** 相对触发元素的位置 */
    placement?: 'right-top' | 'right-bottom' | 'bottom-right' | 'bottom-left' | 'top-right'
    minWidth?: number
  }>(),
  { placement: 'bottom-right', minWidth: 0 },
)

const emit = defineEmits<{ 'update:visible': [value: boolean] }>()

const anchorEl = ref<HTMLElement | null>(null)
const panelEl = ref<HTMLElement | null>(null)
const innerOpen = ref(false)

const open = computed(() => (props.visible === undefined ? innerOpen.value : props.visible))

const pos = ref({ left: 0, top: 0 })

function setOpen(v: boolean) {
  innerOpen.value = v
  emit('update:visible', v)
}

function toggle() {
  setOpen(!open.value)
}

function close() {
  if (open.value) setOpen(false)
}

async function place() {
  await nextTick()
  const a = anchorEl.value?.getBoundingClientRect()
  if (!a) return
  const pw = panelEl.value?.offsetWidth ?? 0
  const ph = panelEl.value?.offsetHeight ?? 0
  const gap = 6
  const pad = 8
  let left = 0
  let top = 0

  switch (props.placement) {
    case 'right-top':
      left = a.right + gap
      top = a.top
      break
    case 'right-bottom':
      left = a.right + gap
      top = a.bottom - ph
      break
    case 'bottom-left':
      left = a.left
      top = a.bottom + gap
      break
    case 'top-right':
      left = a.right - pw
      top = a.top - ph - gap
      break
    default: // bottom-right
      left = a.right - pw
      top = a.bottom + gap
  }

  // 视口内翻转，避免被裁掉
  if (left + pw > window.innerWidth - pad) left = Math.max(pad, window.innerWidth - pw - pad)
  if (left < pad) left = pad
  if (top + ph > window.innerHeight - pad) top = Math.max(pad, window.innerHeight - ph - pad)
  if (top < pad) top = pad

  pos.value = { left, top }
}

watch(open, async (v) => {
  if (v) {
    // 滚动/尺寸监听只在浮层打开期间挂载：关闭时不该为每个 UiPopup
    // 都留一对全局监听（页面上十几个气泡菜单，累积起来是真实开销）
    window.addEventListener('resize', close)
    window.addEventListener('scroll', close, { capture: true, passive: true })
    await place()
  } else {
    window.removeEventListener('resize', close)
    window.removeEventListener('scroll', close, true)
  }
})

function onDocPointerDown(e: MouseEvent) {
  if (!open.value) return
  const target = e.target as Node
  if (panelEl.value?.contains(target) || anchorEl.value?.contains(target)) return
  close()
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') close()
}

onMounted(() => {
  // pointerdown 必须常驻：它负责判断「点击是否落在浮层外」
  document.addEventListener('pointerdown', onDocPointerDown, true)
  document.addEventListener('keydown', onKeydown)
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocPointerDown, true)
  document.removeEventListener('keydown', onKeydown)
  window.removeEventListener('resize', close)
  window.removeEventListener('scroll', close, true)
})
</script>

<template>
  <span ref="anchorEl" class="ui-popup__anchor" @click="toggle">
    <slot />
  </span>

  <Teleport to="body">
    <Transition name="ui-pop">
      <div
        v-if="open"
        ref="panelEl"
        class="ui-popup__panel"
        role="dialog"
        :style="{
          left: `${pos.left}px`,
          top: `${pos.top}px`,
          minWidth: minWidth ? `${minWidth}px` : undefined,
        }"
        @pointerdown.stop
      >
        <slot name="content" :close="close" />
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.ui-popup__anchor {
  display: inline-flex;
  align-items: center;
}

.ui-popup__panel {
  position: fixed;
  z-index: 2000;
  /* 浮层材质：半透明 + 模糊，让下方内容透出来，层次比纯色更真 */
  background: var(--mc-material);
  backdrop-filter: var(--mc-blur);
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-md);
  box-shadow: var(--mc-elev-2);
}

.ui-pop-enter-active,
.ui-pop-leave-active {
  transition:
    opacity 120ms var(--mc-ease-in-out),
    transform 120ms var(--mc-ease-in-out);
}

.ui-pop-enter-from,
.ui-pop-leave-to {
  opacity: 0;
  transform: scale(0.97);
}

@media (prefers-reduced-motion: reduce) {
  .ui-pop-enter-active,
  .ui-pop-leave-active {
    transition: none;
  }
}
</style>
