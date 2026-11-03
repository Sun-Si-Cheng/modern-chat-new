<script setup lang="ts">
import { usePrefsStore } from '@/stores/prefs'

/**
 * 认证页背景：单色基色 + 可选动效（光晕 / 波纹 / 网格 / 无）。
 * 基色来自 prefs（用户可在右上角调色板修改），图片 web/public/login-bg.jpg 存在时覆盖基色。
 * 所有动效仅使用 transform / opacity，避免重绘；尊重 prefers-reduced-motion。
 */
const prefs = usePrefsStore()
</script>

<template>
  <div class="auth-scene" aria-hidden="true">
    <!-- 自定义图片（可选）：存在即覆盖基色 -->
    <div class="auth-scene__img" />

    <!-- 暗化层：保证任意基色/图片下卡片与文字的对比度 -->
    <div class="auth-scene__shade" />

    <!-- 特效层 -->
    <div class="auth-scene__fx" :class="`is-${prefs.sceneEffect}`">
      <template v-if="prefs.sceneEffect === 'aurora'">
        <i class="glow glow--a" />
        <i class="glow glow--b" />
        <i class="glow glow--c" />
      </template>

      <template v-else-if="prefs.sceneEffect === 'waves'">
        <i class="wave" />
        <i class="wave wave--2" />
        <i class="wave wave--3" />
      </template>

      <template v-else-if="prefs.sceneEffect === 'grid'">
        <i class="grid" />
        <i class="pulse" />
      </template>
    </div>
  </div>
</template>

<style scoped>
.auth-scene {
  position: fixed;
  inset: 0;
  z-index: 0;
  overflow: hidden;
  background-color: var(--mc-scene-color, #123a5c);
  /* 全站背景同样叠加在认证场景上（层叠由 prefs.applyAppBg 生成）；
   * 存在 login-bg.jpg 时会被自定义图片层覆盖。 */
  background-image: var(--mc-scene-bg-stack, none);
  background-size: var(--mc-scene-bg-size, auto);
  background-repeat: var(--mc-scene-bg-repeat, repeat);
  background-position: var(--mc-scene-bg-position, center);
}

/* 自定义图片：放在 web/public/login-bg.jpg 即覆盖基色（404 时无副作用） */
.auth-scene__img {
  position: absolute;
  inset: 0;
  background: url('/login-bg.jpg') center / cover no-repeat;
}

.auth-scene__shade {
  position: absolute;
  inset: 0;
  background: rgba(4, 10, 18, 0.16);
}

:root[theme-mode='dark'] .auth-scene__shade {
  background: rgba(2, 6, 12, 0.34);
}

.auth-scene__fx {
  position: absolute;
  inset: 0;
  pointer-events: none;
}

.auth-scene__fx > i {
  position: absolute;
  display: block;
  will-change: transform, opacity;
}

/* ===== 光晕：实心圆 + blur 柔化，缓慢漂移 ===== */
.glow {
  border-radius: 50%;
  filter: blur(70px);
}

.glow--a {
  width: 38vmax;
  height: 38vmax;
  left: -10vmax;
  top: -14vmax;
  background: rgba(126, 196, 255, 0.42);
  animation: drift-a 24s var(--mc-ease-in-out) infinite alternate;
}

.glow--b {
  width: 30vmax;
  height: 30vmax;
  right: -8vmax;
  top: 6vmax;
  background: rgba(96, 226, 200, 0.3);
  animation: drift-b 30s var(--mc-ease-in-out) infinite alternate;
}

.glow--c {
  width: 26vmax;
  height: 26vmax;
  left: 24%;
  bottom: -12vmax;
  background: rgba(170, 150, 255, 0.26);
  animation: drift-c 36s var(--mc-ease-in-out) infinite alternate;
}

@keyframes drift-a {
  from {
    transform: translate3d(0, 0, 0) scale(1);
  }
  to {
    transform: translate3d(7vmax, 5vmax, 0) scale(1.12);
  }
}

@keyframes drift-b {
  from {
    transform: translate3d(0, 0, 0) scale(1.05);
  }
  to {
    transform: translate3d(-8vmax, 6vmax, 0) scale(0.94);
  }
}

@keyframes drift-c {
  from {
    transform: translate3d(0, 0, 0) scale(0.96);
  }
  to {
    transform: translate3d(-5vmax, -6vmax, 0) scale(1.1);
  }
}

/* ===== 波纹：同心圆环由内向外扩散 ===== */
.wave {
  left: 50%;
  top: 48%;
  width: 42vmax;
  height: 42vmax;
  margin: -21vmax 0 0 -21vmax;
  border-radius: 50%;
  border: 1px solid rgba(150, 205, 255, 0.28);
  animation: ripple 9s var(--mc-ease-out) infinite;
}

.wave--2 {
  animation-delay: 3s;
}

.wave--3 {
  animation-delay: 6s;
}

@keyframes ripple {
  0% {
    transform: scale(0.3);
    opacity: 0;
  }
  18% {
    opacity: 0.9;
  }
  100% {
    transform: scale(1.85);
    opacity: 0;
  }
}

/* ===== 网格：极淡网格缓慢流动 + 中央光晕呼吸 ===== */
.grid {
  inset: -12%;
  background-image:
    linear-gradient(rgba(255, 255, 255, 0.06) 1px, transparent 1px),
    linear-gradient(90deg, rgba(255, 255, 255, 0.06) 1px, transparent 1px);
  background-size: 46px 46px;
  -webkit-mask-image: radial-gradient(circle at 50% 46%, #000 0%, transparent 72%);
  mask-image: radial-gradient(circle at 50% 46%, #000 0%, transparent 72%);
  animation: grid-shift 28s linear infinite;
}

@keyframes grid-shift {
  to {
    background-position:
      46px 46px,
      46px 46px;
  }
}

.pulse {
  left: 50%;
  top: 46%;
  width: 44vmax;
  height: 44vmax;
  margin: -22vmax 0 0 -22vmax;
  border-radius: 50%;
  background: rgba(126, 196, 255, 0.22);
  filter: blur(80px);
  animation: breathe 11s var(--mc-ease-in-out) infinite;
}

@keyframes breathe {
  0%,
  100% {
    transform: scale(0.9);
    opacity: 0.65;
  }
  50% {
    transform: scale(1.06);
    opacity: 1;
  }
}

/* ===== 无特效：只保留基色与暗化层 ===== */
.is-none > i {
  display: none;
}

@media (prefers-reduced-motion: reduce) {
  .auth-scene__fx > i {
    animation: none !important;
  }
}
</style>
