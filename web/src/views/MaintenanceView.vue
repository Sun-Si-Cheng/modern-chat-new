<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'

import UiButton from '@/components/ui/UiButton.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import { useAuthStore } from '@/stores/auth'
import { useSiteStore } from '@/stores/site'

const site = useSiteStore()
const auth = useAuthStore()
const router = useRouter()
const { t } = useAppI18n()

const message = computed(
  () => site.config.maintenanceMessage || t('maintenance.defaultMessage'),
)
const siteName = computed(() => site.config.name)

function retry() {
  window.location.reload()
}

function goAdminLogin() {
  auth.logout()
  void router.push('/login')
}
</script>

<template>
  <main class="mc-maint">
    <div class="mc-maint__card">
      <div class="mc-maint__icon" aria-hidden="true">
        <svg width="40" height="40" viewBox="0 0 24 24" fill="none">
          <path
            d="M12 3.2 2.8 5.4v6.1c0 4.4 3.6 7.9 9.2 9.3 5.6-1.4 9.2-4.9 9.2-9.3V5.4L12 3.2Z"
            stroke="currentColor"
            stroke-width="1.4"
            stroke-linejoin="round"
          />
          <path
            d="M12 8v5M12 16.2h.01"
            stroke="currentColor"
            stroke-width="1.7"
            stroke-linecap="round"
          />
        </svg>
      </div>

      <p class="mc-maint__eyebrow">{{ siteName }}</p>
      <h1 class="mc-maint__title">{{ t('maintenance.title') }}</h1>
      <p class="mc-maint__message">{{ message }}</p>
      <p class="mc-maint__hint text-placeholder">{{ t('maintenance.hint') }}</p>

      <div class="mc-maint__actions">
        <UiButton variant="outline" size="small" @click="retry">
          {{ t('maintenance.retry') }}
        </UiButton>
        <UiButton variant="text" size="small" @click="goAdminLogin">
          {{ t('maintenance.adminLogin') }}
        </UiButton>
      </div>
    </div>
  </main>
</template>

<style scoped>
.mc-maint {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: var(--td-bg-color-page);
}

.mc-maint__card {
  width: 100%;
  max-width: 420px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  padding: 40px 32px;
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-lg);
  background: var(--td-bg-color-container);
  box-shadow: var(--mc-elev-2);
  text-align: center;
}

.mc-maint__icon {
  color: var(--td-brand-color);
  margin-bottom: 4px;
}

.mc-maint__eyebrow {
  margin: 0;
  font-size: var(--mc-fs-caption);
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--td-text-color-placeholder);
}

.mc-maint__title {
  margin: 0;
  font-size: var(--mc-fs-title);
  font-weight: 600;
  color: var(--td-text-color-primary);
}

.mc-maint__message {
  margin: 0;
  max-width: 320px;
  font-size: var(--mc-fs-body);
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--td-text-color-secondary);
  user-select: text;
}

.mc-maint__hint {
  margin: 4px 0 0;
  font-size: var(--mc-fs-sm);
  line-height: 1.5;
}

.mc-maint__actions {
  display: flex;
  gap: 8px;
  margin-top: 12px;
}
</style>
