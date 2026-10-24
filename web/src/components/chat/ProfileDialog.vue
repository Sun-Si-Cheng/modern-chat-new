<script setup lang="ts">
import { ref, watch } from 'vue'
import { Button as TButton, Dialog as TDialog, Input as TInput, MessagePlugin } from 'tdesign-vue-next'

import { useAppI18n } from '@/composables/useAppI18n'
import { useAuthStore } from '@/stores/auth'

const visible = defineModel<boolean>({ default: false })
const auth = useAuthStore()
const { t, te } = useAppI18n()

const nickname = ref('')
const email = ref('')
const saving = ref(false)

watch(visible, (v) => {
  if (v && auth.user) {
    nickname.value = auth.user.nickname
    email.value = auth.user.email ?? ''
  }
})

async function save() {
  const name = nickname.value.trim()
  if (!name) {
    void MessagePlugin.warning(t('chat.nicknameEmpty'))
    return
  }
  saving.value = true
  try {
    await auth.updateMe({ nickname: name, email: email.value.trim() })
    void MessagePlugin.success(t('chat.profileSaved'))
    visible.value = false
  } catch (e) {
    void MessagePlugin.error(te(e) || t('chat.saveFailed'))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <TDialog v-model:visible="visible" :header="t('chat.profileTitle')" width="420px" :footer="false">
    <div class="profile-form">
      <label class="profile-form__label">{{ t('chat.nickname') }}</label>
      <TInput v-model="nickname" :placeholder="t('chat.nicknamePlaceholder')" :maxlength="20" clearable />

      <label class="profile-form__label">{{ t('chat.email') }}</label>
      <TInput v-model="email" :placeholder="t('chat.emailPlaceholder')" clearable />
      <p class="text-placeholder profile-form__hint">{{ t('chat.emailHint') }}</p>

      <div class="profile-form__actions">
        <TButton variant="outline" @click="visible = false">{{ t('common.cancel') }}</TButton>
        <TButton theme="primary" :loading="saving" @click="save">{{ t('common.save') }}</TButton>
      </div>
    </div>
  </TDialog>
</template>

<style scoped>
.profile-form {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.profile-form__label {
  font-size: 14px;
  font-weight: 500;
  margin-top: 6px;
}

.profile-form__hint {
  margin: 0;
  font-size: 12px;
  line-height: 1.5;
}

.profile-form__actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 12px;
}
</style>
