<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

import UiIcon from './UiIcon.vue'

/**
 * 代码块：高亮由调用方传 HTML（站点里只有 2-3 段固定代码，
 * 引 highlight.js 会让体积翻倍，不划算）；复制走 clipboard。
 */
const props = withDefaults(
  defineProps<{
    name?: string
    html: string
    /** 复制到剪贴板的纯文本；不传则不显示复制按钮 */
    raw?: string
  }>(),
  { name: '', raw: '' },
)

const { t } = useI18n()
const done = ref(false)
let timer: ReturnType<typeof setTimeout> | undefined

async function copy() {
  if (!props.raw) return
  try {
    await navigator.clipboard.writeText(props.raw)
    done.value = true
    clearTimeout(timer)
    timer = setTimeout(() => (done.value = false), 1600)
  } catch {
    /* 非安全上下文 / 无权限：静默失败，用户可手动选中 */
  }
}
</script>

<template>
  <div class="code">
    <div v-if="name || raw" class="code__head">
      <span class="code__name">{{ name }}</span>
      <button v-if="raw" class="code__copy" type="button" @click="copy">
        <UiIcon :name="done ? 'check' : 'copy'" :size="13" />
        {{ done ? t('copy.done') : t('copy.action') }}
      </button>
    </div>
    <!-- eslint-disable-next-line vue/no-v-html -->
    <pre><code v-html="html" /></pre>
  </div>
</template>
