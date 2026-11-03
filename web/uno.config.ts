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
    },
    borderRadius: {
      sm: '6px',
      DEFAULT: '8px',
      md: '8px',
      lg: '10px',
      xl: '14px',
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
