/**
 * 描述式 UI 白名单渲染器（§8.2）。
 * 只渲染 8 个白名单组件：text/image/button/input/card/list/divider/link；
 * props 仅接受原始值；链接/图片仅 http(s)（双保险，桥接层已校验过）。
 */
import { defineComponent, h, type PropType, type VNode } from 'vue'
import { Button as TButton, Divider as TDivider, Input as TInput } from 'tdesign-vue-next'

import type { UIDescNode } from '@/types/proto'

function safeProps(raw: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(raw)) {
    if (['string', 'number', 'boolean'].includes(typeof v) || v === null) out[k] = v
  }
  return out
}

function httpUrl(v: unknown, fallback: string): string {
  const s = typeof v === 'string' ? v : ''
  return /^https?:\/\//i.test(s) ? s : fallback
}

type ActionFn = (action: string, value?: unknown) => void

export const DeclarativeNodes = defineComponent({
  name: 'DeclarativeNodes',
  props: {
    nodes: { type: Array as PropType<readonly UIDescNode[]>, required: true },
    onAction: { type: Function as PropType<ActionFn>, default: undefined },
  },
  setup(props) {
    function renderNode(node: UIDescNode): VNode {
      const p = safeProps(node.props ?? {})
      const str = (k: string, d = '') => String(p[k] ?? d)

      switch (node.component) {
        case 'text':
          return h('span', { class: 'mc-dtext' }, str('content'))
        case 'image':
          return h('img', {
            class: 'mc-dimage',
            src: httpUrl(p.src, ''),
            alt: str('alt'),
            style: { maxWidth: '100%', borderRadius: '4px', display: 'block' },
          })
        case 'button':
          return h(
            TButton,
            {
              size: 'small',
              variant: 'outline',
              theme: p.theme === 'primary' ? 'primary' : 'default',
              onClick: () => props.onAction?.(String(node.action ?? '')),
            },
            { default: () => str('text', '按钮') },
          )
        case 'input':
          return h(TInput, {
            size: 'small',
            clearable: true,
            placeholder: str('placeholder'),
            value: str('value'),
            onChange: (v: unknown) => props.onAction?.(String(node.action ?? 'input'), v),
          })
        case 'card':
          return h(
            'div',
            {
              class: 'mc-dcard',
              style: {
                border: '1px solid var(--td-component-border)',
                borderRadius: '6px',
                padding: '8px 10px',
                margin: '4px 0',
                background: 'var(--td-bg-color-container)',
              },
            },
            h(DeclarativeNodes, { nodes: node.children ?? [], onAction: props.onAction }),
          )
        case 'list':
          return h(
            'ul',
            { class: 'mc-dlist', style: { margin: '4px 0', paddingLeft: '18px' } },
            (node.children ?? []).map((c, i) => h('li', { key: i }, [renderNode(c)])),
          )
        case 'divider':
          return h(TDivider, { style: { margin: '6px 0' } })
        case 'link':
          return h(
            'a',
            {
              href: httpUrl(p.href, '#'),
              target: '_blank',
              rel: 'noopener noreferrer',
            },
            str('text', str('href', '链接')),
          )
        default:
          // 白名单外：拒绝渲染
          return h('span', { class: 'text-placeholder' }, '[已拒绝渲染未知组件]')
      }
    }

    return () => h('div', { class: 'mc-declarative' }, props.nodes.map(renderNode))
  },
})

export default DeclarativeNodes
