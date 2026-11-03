import { defineConfig, presetWind3 } from 'unocss'

/**
 * UnoCSS 配置
 *
 * 颜色值全部指向产品的 CSS 变量（--td-* / --mc-*），因此：
 * - 亮/暗主题切换自动生效（变量由 TDesign 主题体系维护）
 * - 原子类与存量手写样式取同一套色值，迁移期间不会出现两套色板
 *
 * 用法示例：`bg-surface text-ink-2 border-line rounded-lg px-3 py-2`
 */
export default defineConfig({
  presets: [presetWind3()],
  theme: {
    colors: {
      // 品牌
      brand: 'var(--td-brand-color)',
      'brand-hover': 'var(--td-brand-color-hover)',
      'brand-active': 'var(--td-brand-color-active)',
      'brand-light': 'var(--td-brand-color-light)',
      // 文本
      ink: 'var(--td-text-color-primary)',
      'ink-2': 'var(--td-text-color-secondary)',
      'ink-3': 'var(--td-text-color-placeholder)',
      'ink-4': 'var(--td-text-color-disabled)',
      // 表面
      surface: 'var(--td-bg-color-container)',
      page: 'var(--td-bg-color-page)',
      hover: 'var(--td-bg-color-container-hover)',
      active: 'var(--td-bg-color-container-active)',
      line: 'var(--mc-hairline)',
      // 语义
      danger: 'var(--td-error-color)',
      success: 'var(--td-success-color)',
      warning: 'var(--td-warning-color)',
      info: 'var(--td-brand-color)',

      // ===== 带透明度的衍生色 =====
      // 上面的色值都是 CSS 变量，UnoCSS 的 `/12` 透明度修饰对 var() 无效：
      // `bg-success/12` 会退化成实心色，配合同色文字就变成「看不见的字」。
      // 所以这里用 color-mix 预先把常用档位算好。
      'brand-soft': 'color-mix(in srgb, var(--td-brand-color) 10%, transparent)',
      'brand-line': 'color-mix(in srgb, var(--td-brand-color) 40%, transparent)',
      'brand-line-strong': 'color-mix(in srgb, var(--td-brand-color) 50%, transparent)',
      'brand-ring': 'color-mix(in srgb, var(--td-brand-color) 45%, transparent)',
      'danger-soft': 'color-mix(in srgb, var(--td-error-color) 12%, transparent)',
      'danger-line': 'color-mix(in srgb, var(--td-error-color) 40%, transparent)',
      'success-soft': 'color-mix(in srgb, var(--td-success-color) 12%, transparent)',
      'success-line': 'color-mix(in srgb, var(--td-success-color) 40%, transparent)',
      'warning-soft': 'color-mix(in srgb, var(--td-warning-color) 12%, transparent)',
      'warning-line': 'color-mix(in srgb, var(--td-warning-color) 40%, transparent)',
    },
    // 圆角统一走 main.css 的 --mc-radius-*：
    // 之前这里写死 6/8/10/14，与 --mc-radius-md(10px) 冲突，
    // 于是「rounded-md」和「var(--mc-radius-md)」给出两种圆角。
    borderRadius: {
      sm: 'var(--mc-radius-sm)',
      DEFAULT: 'var(--mc-radius-md)',
      md: 'var(--mc-radius-md)',
      lg: 'var(--mc-radius-lg)',
      xl: 'var(--mc-radius-xl)',
      pill: '999px',
    },
    // 与产品既有的节奏对齐（4 的倍数）
    spacing: {
      px: '1px',
    },
  },
  shortcuts: {
    // 面板（卡片/浮层）统一观感
    'ui-panel': 'bg-surface border border-line rounded-lg',
    // 可点击列表行
    'ui-row': 'flex items-center gap-2 px-2 py-1.5 rounded-md cursor-pointer transition-colors',
    'ui-row-hover': 'hover:bg-hover',
    // 次要说明文字
    'ui-hint': 'text-[12px] leading-[1.45] text-ink-3',
    // 表单字段标签
    'ui-label': 'text-[13px] text-ink-2',
  },
  // 扫描范围（默认已覆盖 src，这里显式声明避免 node_modules 误扫）
  content: {
    pipeline: {
      include: [/\.(vue|ts|html)($|\?)/],
    },
  },
})
