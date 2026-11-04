<script setup lang="ts">
import UiButton from '@/components/ui/UiButton.vue'
import UiDialog from '@/components/ui/UiDialog.vue'
import { useAppI18n } from '@/composables/useAppI18n'

const visible = defineModel<boolean>('visible', { default: false })
defineProps<{ content: string }>()

const { t } = useAppI18n()
</script>

<template>
  <UiDialog
    v-model:visible="visible"
    :header="t('announcement.title')"
    width="440px"
    :footer="false"
  >
    <p class="mc-announce__content">{{ content }}</p>
    <div class="mc-announce__actions">
      <UiButton theme="primary" @click="visible = false">{{ t('announcement.gotIt') }}</UiButton>
    </div>
  </UiDialog>
</template>

<style scoped>
.mc-announce__content {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-word;
  line-height: 1.7;
  font-size: var(--mc-fs-body);
  color: var(--td-text-color-primary);
}

.mc-announce__actions {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>
