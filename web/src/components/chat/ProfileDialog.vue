<script setup lang="ts">
import { ref, watch } from 'vue'
import {
  Button as TButton,
  Dialog as TDialog,
  Input as TInput,
  MessagePlugin,
} from 'tdesign-vue-next'

import { useAuthStore } from '@/stores/auth'

const visible = defineModel<boolean>({ default: false })
const auth = useAuthStore()

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
    void MessagePlugin.warning('昵称不能为空')
    return
  }
  saving.value = true
  try {
    await auth.updateMe({ nickname: name, email: email.value.trim() })
    void MessagePlugin.success('资料已保存')
    visible.value = false
  } catch (e) {
    void MessagePlugin.error((e as Error).message || '保存失败')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <TDialog v-model:visible="visible" header="个人资料" width="420px" :footer="false">
    <div class="profile-form">
      <label class="profile-form__label">昵称</label>
      <TInput v-model="nickname" placeholder="别人看到的名字" :maxlength="20" clearable />

      <label class="profile-form__label">邮箱</label>
      <TInput v-model="email" placeholder="用于找回密码（可留空）" clearable />
      <p class="text-placeholder profile-form__hint">
        邮箱仅用于找回密码。修改后需要重新验证（本期无邮件验证流程）。
      </p>

      <div class="profile-form__actions">
        <TButton variant="outline" @click="visible = false">取消</TButton>
        <TButton theme="primary" :loading="saving" @click="save">保存</TButton>
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
