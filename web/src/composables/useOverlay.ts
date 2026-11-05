/**
 * 浮层（对话框 / 抽屉）的公共行为：body 滚动锁 + Tab 焦点环。
 *
 * 为什么需要：
 * - 不锁滚动时，鼠标滚轮滚遮罩会把背后的页面滚走，弹层看起来「飘」；
 * - 不环住焦点时，Tab 会直接走到背景页的按钮上，键盘用户会误触后面的列表。
 */

let lockCount = 0
let prevOverflow = ''

/** 打开浮层时锁住页面滚动（可嵌套：多个浮层同时打开只在最后一个关闭时解锁） */
export function lockBodyScroll() {
  if (lockCount === 0) {
    prevOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
  }
  lockCount += 1
}

export function unlockBodyScroll() {
  lockCount = Math.max(0, lockCount - 1)
  if (lockCount === 0) document.body.style.overflow = prevOverflow
}

const FOCUSABLE = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

/** 容器内可聚焦元素（过滤不可见项） */
export function focusablesIn(container: HTMLElement | null): HTMLElement[] {
  if (!container) return []
  return Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE)).filter((el) => {
    if (el.hasAttribute('disabled') || el.getAttribute('aria-hidden') === 'true') return false
    return el.offsetParent !== null || el === document.activeElement
  })
}

/**
 * Tab / Shift+Tab 焦点环：在容器首尾之间循环。
 * 返回 true 表示已接管本次按键（调用方无需再处理）。
 */
export function handleTabKey(e: KeyboardEvent, container: HTMLElement | null): boolean {
  if (e.key !== 'Tab' || !container) return false
  const list = focusablesIn(container)
  if (!list.length) {
    // 容器内没有可聚焦元素：把焦点留在容器上，别让它跑到背景页
    e.preventDefault()
    container.focus()
    return true
  }
  const first = list[0]!
  const last = list[list.length - 1]!
  const active = document.activeElement as HTMLElement | null

  if (!e.shiftKey && (active === last || !container.contains(active))) {
    e.preventDefault()
    first.focus()
    return true
  }
  if (e.shiftKey && (active === first || !container.contains(active))) {
    e.preventDefault()
    last.focus()
    return true
  }
  return false
}
