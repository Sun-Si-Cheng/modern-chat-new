<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'
import { EditIcon } from 'tdesign-icons-vue-next'

import AvatarBadge from './AvatarBadge.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import { useAuthStore } from '@/stores/auth'

const emit = defineEmits<{ logout: [] }>()

const router = useRouter()
const auth = useAuthStore()
const { t, te } = useAppI18n()

const fileInput = ref<HTMLInputElement | null>(null)
const uploading = ref(false)

function pickFile() {
  fileInput.value?.click()
}

/** 居中裁切成正方形并压到 256px，输出 webp，控制传输与存储体积 */
async function toSquareDataURL(file: File, size = 256): Promise<string> {
  const bitmap = await createImageBitmap(file)
  const canvas = document.createElement('canvas')
  canvas.width = size
  canvas.height = size
  const ctx = canvas.getContext('2d')
  if (!ctx) throw new Error('canvas unavailable')
  const edge = Math.min(bitmap.width, bitmap.height)
  ctx.drawImage(
    bitmap,
    (bitmap.width - edge) / 2,
    (bitmap.height - edge) / 2,
    edge,
    edge,
    0,
    0,
    size,
    size,
  )
  bitmap.close?.()
  const url = canvas.toDataURL('image/webp', 0.86)
  // 浏览器不支持 webp 时会回退 png，二者服务端都接受
  return url
}

async function onFileChange(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  if (!/^image\/(png|jpeg|webp)$/.test(file.type)) {
    void MessagePlugin.warning(t('profile.avatarInvalid'))
    return
  }
  uploading.value = true
  try {
    const dataUrl = await toSquareDataURL(file)
    await auth.updateMe({ avatar: dataUrl })
    void MessagePlugin.success(t('profile.avatarUpdated'))
  } catch (e) {
    void MessagePlugin.error(te(e) || t('profile.avatarFailed'))
  } finally {
    uploading.value = false
  }
}

function goSettings() {
  void router.push('/settings')
}
</script>

<template>
  <div class="profile-card">
    <div class="profile-card__head">
      <!-- 头像：hover 出现相机，点击选图上传 -->
      <button
        type="button"
        class="profile-card__avatar"
        :aria-label="t('profile.changeAvatar')"
        :disabled="uploading"
        @click="pickFile"
      >
        <AvatarBadge
          :name="auth.user?.nickname ?? '?'"
          :src="auth.user?.avatar"
          :color="auth.user?.avatarColor ?? '#0052d9'"
          :size="72"
        />
        <span class="profile-card__cam">
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" aria-hidden="true">
            <path
              d="M3 8.6A2.6 2.6 0 0 1 5.6 6h1.1l.9-1.5A1.6 1.6 0 0 1 9 3.8h6a1.6 1.6 0 0 1 1.4.7l.9 1.5h1.1A2.6 2.6 0 0 1 21 8.6v8.8A2.6 2.6 0 0 1 18.4 20H5.6A2.6 2.6 0 0 1 3 17.4V8.6Z"
              stroke="currentColor"
              stroke-width="1.7"
              stroke-linejoin="round"
            />
            <circle cx="12" cy="13" r="3.5" stroke="currentColor" stroke-width="1.7" />
          </svg>
        </span>
      </button>

      <div class="profile-card__info">
        <div class="profile-card__name-row">
          <span class="profile-card__name">{{ auth.user?.nickname }}</span>
          <button type="button" class="profile-card__edit" :aria-label="t('settings.account')" @click="goSettings">
            <EditIcon />
          </button>
        </div>
        <span class="profile-card__username">@{{ auth.user?.username }}</span>
      </div>
    </div>

    <input
      ref="fileInput"
      class="profile-card__file"
      type="file"
      accept="image/png,image/jpeg,image/webp"
      @change="onFileChange"
    />

    <ul class="profile-card__menu">
      <li>
        <button type="button" @click="goSettings">{{ t('settings.title') }}</button>
      </li>
      <li v-if="auth.isAdmin">
        <button type="button" @click="router.push('/admin')">{{ t('admin.title') }}</button>
      </li>
      <li>
        <button type="button" class="is-danger" @click="emit('logout')">
          {{ t('settings.logout') }}
        </button>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.profile-card {
  width: 240px;
  padding: 16px 14px 8px;
}

.profile-card__head {
  display: flex;
  align-items: center;
  gap: 12px;
}

/* 头像按钮：hover / 键盘聚焦时显示相机遮罩 */
.profile-card__avatar {
  position: relative;
  padding: 0;
  border: none;
  background: none;
  border-radius: 50%;
  cursor: pointer;
  line-height: 0;
}

.profile-card__avatar:disabled {
  cursor: progress;
}

.profile-card__cam {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: rgba(0, 0, 0, 0.5);
  color: #fff;
  opacity: 0;
  transition: opacity 0.14s ease-out;
}

.profile-card__avatar:hover .profile-card__cam,
.profile-card__avatar:focus-visible .profile-card__cam {
  opacity: 1;
}

.profile-card__avatar:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 2px;
}

.profile-card__info {
  min-width: 0;
  flex: 1;
}

.profile-card__name-row {
  display: flex;
  align-items: center;
  gap: 4px;
}

.profile-card__name {
  font-size: 15px;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.profile-card__edit {
  flex-shrink: 0;
  padding: 2px;
  border: none;
  border-radius: 4px;
  background: none;
  color: var(--mc-text-faint);
  cursor: pointer;
  line-height: 0;
}

.profile-card__edit:hover {
  color: var(--td-brand-color);
}

.profile-card__username {
  display: block;
  margin-top: 2px;
  font-size: 12px;
  color: var(--mc-text-faint);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.profile-card__file {
  display: none;
}

.profile-card__menu {
  list-style: none;
  margin: 12px 0 0;
  padding: 8px 0 0;
  border-top: 1px solid var(--mc-hairline);
}

.profile-card__menu button {
  display: block;
  width: 100%;
  padding: 8px 6px;
  border: none;
  border-radius: 6px;
  background: none;
  font: inherit;
  font-size: 13px;
  text-align: left;
  color: var(--td-text-color-primary);
  cursor: pointer;
}

.profile-card__menu button:hover {
  background: var(--td-bg-color-secondarycontainer);
}

.profile-card__menu button.is-danger {
  color: var(--td-error-color, #d54941);
}
</style>
