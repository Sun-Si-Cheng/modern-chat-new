<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { MessagePlugin, Popup as TPopup } from 'tdesign-vue-next'
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
      <!-- 头像：点击弹出 TPopup 面板（大图 + 更换头像） -->
      <TPopup trigger="click" placement="right-top" :show-arrow="false">
        <AvatarBadge
          :name="auth.user?.nickname ?? '?'"
          :src="auth.user?.avatar"
          :color="auth.user?.avatarColor ?? '#0052d9'"
          :size="72"
          clickable
        />
        <template #content>
          <div class="avatar-pop">
            <img
              class="avatar-pop__img"
              :src="auth.user?.avatar || '/default-avatar.svg'"
              :alt="auth.user?.nickname"
              draggable="false"
            />
            <button type="button" class="avatar-pop__change" :disabled="uploading" @click="pickFile">
              {{ uploading ? t('profile.uploading') : t('profile.changeAvatar') }}
            </button>
          </div>
        </template>
      </TPopup>

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

/* 头像弹层（t-popup 内容）：大图 + 更换头像 */
.avatar-pop {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  padding: 14px;
}

.avatar-pop__img {
  width: 168px;
  height: 168px;
  border-radius: 50%;
  object-fit: cover;
  display: block;
  background: #e4efff;
}

.avatar-pop__change {
  padding: 6px 16px;
  border-radius: 6px;
  border: none;
  font: inherit;
  font-size: 13px;
  background: var(--td-brand-color);
  color: #fff;
  cursor: pointer;
}

.avatar-pop__change:disabled {
  opacity: 0.6;
  cursor: progress;
}

.avatar-pop__change:hover {
  filter: brightness(1.08);
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
