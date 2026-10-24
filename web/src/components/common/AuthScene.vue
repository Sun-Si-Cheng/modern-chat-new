<script setup lang="ts">
/**
 * 认证页背景场景：两段纯色（天 / 水）+ 地平线，可选自定义图片覆盖。
 * 图片放在 web/public/login-bg.jpg 即自动生效（文件不存在时保留纯色底）。
 */
</script>

<template>
  <div class="auth-scene" aria-hidden="true">
    <div class="auth-scene__water" />
    <div class="auth-scene__img" />
  </div>
</template>

<style scoped>
.auth-scene {
  position: fixed;
  inset: 0;
  z-index: 0;
  background: #123a5c; /* 天 */
}

.auth-scene__water {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  height: 34%;
  background: #0b2436; /* 水 */
  border-top: 1px solid rgba(255, 255, 255, 0.1); /* 地平线 */
}

/* 可选自定义背景图：放置 web/public/login-bg.jpg 即覆盖纯色（404 时无副作用） */
.auth-scene__img {
  position: absolute;
  inset: 0;
  background: url('/login-bg.jpg') center / cover no-repeat;
}

/* 图片蒙版：任何图都保证卡片与文字可读；暗色主题压得更沉 */
.auth-scene__img::after {
  content: '';
  position: absolute;
  inset: 0;
  background: rgba(8, 19, 31, 0.28);
}

:root[theme-mode='dark'] .auth-scene {
  background: #08131f;
}

:root[theme-mode='dark'] .auth-scene__water {
  background: #050d16;
  border-top-color: rgba(255, 255, 255, 0.06);
}

:root[theme-mode='dark'] .auth-scene__img::after {
  background: rgba(3, 8, 14, 0.52);
}
</style>
