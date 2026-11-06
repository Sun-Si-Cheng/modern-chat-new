import { onMounted, onUnmounted } from 'vue'

import { useAuthStore } from '@/stores/auth'
import { useWsStore } from '@/stores/ws'
import { useConversationsStore } from '@/stores/conversations'

/**
 * 组件层 WS 生命周期封装：
 * 挂载时连接并把上行出口绑定到 conversations store，卸载 / 退出登录时断开。
 */
export function useWebSocket() {
  const ws = useWsStore()
  const auth = useAuthStore()
  const conversations = useConversationsStore()

  conversations.bindSender((frame) => ws.send(frame))

  // 退出登录（主动或 401）时立即断开：否则 token 已清，socket 仍在收消息
  const offSessionEnd = auth.onSessionEnd(() => ws.disconnect())

  onMounted(() => {
    ws.connect()
  })

  onUnmounted(() => {
    offSessionEnd()
    ws.disconnect()
  })
}
