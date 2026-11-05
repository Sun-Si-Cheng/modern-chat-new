/**
 * 描述式 UI 白名单渲染器（§8.2）。
 * 白名单组件：text / image / button / input / card / list / divider / link
 * + 数据展示类 table / progress / badge；
 * props 仅接受原始值；链接/图片仅 http(s)（双保险，桥接层已校验过）。
 */
import { defineComponent, h, type PropType, type VNode } from 'vue'
import { Button as TButton, Divider as TDivider, Input as TInput } from 'tdesign-vue-next'

import i18n from '@/i18n'
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
            { class: 'mc-dcard' },
            h(DeclarativeNodes, { nodes: node.children ?? [], onAction: props.onAction }),
          )
        case 'list':
          return h(
            'ul',
            { class: 'mc-dlist' },
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
        case 'badge': {
          const tone = ['brand', 'success', 'warning', 'danger'].includes(str('tone')) ? str('tone') : 'brand'
          return h('span', { class: `mc-dbadge mc-dbadge--${tone}` }, str('text'))
        }
        case 'progress': {
          const raw = Number(p.value)
          const value = Number.isFinite(raw) ? Math.min(100, Math.max(0, raw)) : 0
          return h('div', { class: 'mc-dprogress' }, [
            h('div', { class: 'mc-dprogress__head' }, [
              h('span', { class: 'mc-dprogress__label' }, str('label')),
              h('span', { class: 'mc-dprogress__value num' }, `${value}%`),
            ]),
            h('div', { class: 'mc-dprogress__track' }, [
              h('div', { class: 'mc-dprogress__bar', style: { width: `${value}%` } }),
            ]),
          ])
        }
        case 'table': {
          // 表格数据用字符串传：headers="A,B"、rows='[["a","1"],["b","2"]]'
          // 只接受原始值，行/列数量设上限，避免插件塞一张巨表把面板顶爆
          const headers = str('headers')
            .split(',')
            .map((s) => s.trim())
            .filter(Boolean)
            .slice(0, 6)
          let rows: string[][] = []
          try {
            const parsed: unknown = JSON.parse(str('rows', '[]'))
            if (Array.isArray(parsed)) {
              rows = parsed
                .slice(0, 50)
                .map((r) =>
                  Array.isArray(r)
                    ? r.slice(0, headers.length || 6).map((c) => String(c ?? ''))
                    : [String(r ?? '')],
                )
            }
          } catch {
            rows = []
          }
          return h('table', { class: 'mc-dtable' }, [
            headers.length
              ? h('thead', {}, [
                  h('tr', {}, headers.map((c, i) => h('th', { key: i }, c))),
                ])
              : null,
            h(
              'tbody',
              {},
              rows.map((r, ri) => h('tr', { key: ri }, r.map((c, ci) => h('td', { key: ci }, c)))),
            ),
          ])
        }
        default:
          // 白名单外：拒绝渲染
          return h('span', { class: 'text-placeholder' }, i18n.global.t('message.rejectedComponent'))
      }
    }

    return () => h('div', { class: 'mc-declarative' }, props.nodes.map(renderNode))
  },
})

export default DeclarativeNodes
