import { onMounted, onUnmounted } from 'vue'

import { useWsStore } from '@/stores/ws'
import { useConversationsStore } from '@/stores/conversations'

/**
 * 组件层 WS 生命周期封装：
 * 挂载时连接并把上行出口绑定到 conversations store，卸载时断开。
 */
export function useWebSocket() {
  const ws = useWsStore()
  const conversations = useConversationsStore()

  conversations.bindSender((frame) => ws.send(frame))

  onMounted(() => {
    ws.connect()
  })

  onUnmounted(() => {
    ws.disconnect()
  })
}
