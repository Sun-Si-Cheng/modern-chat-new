import { ref } from 'vue'
import { defineStore } from 'pinia'

import { MessagePlugin } from 'tdesign-vue-next'

import type { ClientFrame, ServerFrame } from '@/types/proto'
import { useAuthStore } from './auth'
import { useConversationsStore } from './conversations'
import { useFriendsStore } from './friends'

const HEARTBEAT_INTERVAL = 25_000
const MAX_RECONNECT_DELAY = 30_000

/** WebSocket 连接生命周期：心跳 + 指数退避重连 + 帧路由（§3 / §7） */
export const useWsStore = defineStore('ws', () => {
  const auth = useAuthStore()
  const conversations = useConversationsStore()
  const friends = useFriendsStore()

  const connected = ref(false)
  /** 在线用户 id 集合（含自己） */
  const onlineUsers = ref<Set<number>>(new Set())

  let socket: WebSocket | null = null
  let heartbeatTimer: ReturnType<typeof setInterval> | null = null
  let pongTimeout: ReturnType<typeof setTimeout> | null = null
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null
  let reconnectDelay = 1000
  let closedByUser = false
  /**
   * 是否成功建立过连接：用于区分首连与重连（重连后需补数据），
   * 界面也据此决定要不要显示「连接已断开」提示（首连中不闪）。
   */
  const hasConnected = ref(false)

  /** 旁路帧监听（插件宿主等），不参与状态机 */
  const frameListeners = new Set<(frame: ServerFrame) => void>()

  function onFrame(fn: (frame: ServerFrame) => void): () => void {
    frameListeners.add(fn)
    return () => frameListeners.delete(fn)
  }

  function wsUrl(): string {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    return `${proto}://${location.host}/ws?token=${encodeURIComponent(auth.token)}`
  }

  function connect() {
    if (!auth.token) return
    closedByUser = false
    if (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) return

    const ws = new WebSocket(wsUrl())
    socket = ws

    // 每个回调都先判断自己是否仍是当前连接：快速切页时旧 socket 处于 CLOSING，
    // connect() 会新建一条；旧连接的 onclose 若继续改状态，会把已连上的新连接
    // 标成「已断开」，断线横幅将永远不消失（且会再排一次无意义的重连）
    ws.onopen = () => {
      if (ws !== socket) return
      connected.value = true
      reconnectDelay = 1000
      startHeartbeat()
      // 重连：断线期间的消息不会补发帧，必须重新对齐服务端状态
      if (hasConnected.value) void conversations.resync()
      hasConnected.value = true
    }

    ws.onmessage = (ev) => {
      if (ws !== socket) return
      let frame: ServerFrame
      try {
        frame = JSON.parse(ev.data as string) as ServerFrame
      } catch {
        return
      }
      handleFrame(frame)
    }

    ws.onclose = () => {
      if (ws !== socket) return
      connected.value = false
      // 断开后无法再得知他人在线状态，清空避免继续显示过期的"在线"
      onlineUsers.value = new Set()
      stopHeartbeat()
      if (!closedByUser) scheduleReconnect()
    }

    ws.onerror = () => {
      if (ws !== socket) return
      ws.close()
    }
  }

  function handleFrame(frame: ServerFrame) {
    frameListeners.forEach((fn) => fn(frame))
    switch (frame.type) {
      case 'pong':
        if (pongTimeout) {
          clearTimeout(pongTimeout)
          pongTimeout = null
        }
        break
      case 'presence':
        if (frame.online) onlineUsers.value.add(frame.userId)
        else onlineUsers.value.delete(frame.userId)
        break
      case 'user_change': {
        // 昵称 / 头像变更：好友列表、会话 peer、群成员与本人其他端就地更新。
        // 服务端一直会广播该帧（含本人多端），此前前端没有分支处理，
        // 导致文档里承诺的「实时同步」实际要刷新页面才生效。
        conversations.applyUserProfile(frame)
        friends.applyUserProfile(frame)
        if (auth.user && auth.user.id === frame.userId) {
          auth.user = {
            ...auth.user,
            nickname: frame.nickname,
            avatar: frame.avatar,
            avatarColor: frame.avatarColor,
          }
        }
        break
      }
      case 'kicked': {
        // 封禁/失效强制下线
        closedByUser = true
        socket?.close()
        auth.logout()
        conversations.reset()
        onlineUsers.value.clear()
        location.href = '/login'
        break
      }
      case 'error':
        // 服务端返回的业务错误（撤回/编辑/表情校验失败等）：直接提示用户
        if (frame.reason) void MessagePlugin.error(frame.reason)
        break
      default:
        conversations.handleServerFrame(frame)
    }
  }

  /** 返回是否真的写出：连接未就绪时调用方需要把消息标记为失败而不是静默丢弃 */
  function send(frame: ClientFrame): boolean {
    if (socket?.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify(frame))
      return true
    }
    return false
  }

  // ===== 心跳 =====

  function startHeartbeat() {
    stopHeartbeat()
    heartbeatTimer = setInterval(() => {
      if (socket?.readyState !== WebSocket.OPEN) return
      send({ type: 'ping' })
      // 8s 未收到 pong 视为僵死，强制断开触发重连
      pongTimeout = setTimeout(() => socket?.close(), 8000)
    }, HEARTBEAT_INTERVAL)
  }

  function stopHeartbeat() {
    if (heartbeatTimer) {
      clearInterval(heartbeatTimer)
      heartbeatTimer = null
    }
    if (pongTimeout) {
      clearTimeout(pongTimeout)
      pongTimeout = null
    }
  }

  // ===== 重连 =====

  function scheduleReconnect() {
    if (reconnectTimer) return
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null
      reconnectDelay = Math.min(reconnectDelay * 2, MAX_RECONNECT_DELAY)
      if (auth.token) connect()
    }, reconnectDelay)
  }

  function disconnect() {
    closedByUser = true
    hasConnected.value = false
    if (reconnectTimer) {
      clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
    stopHeartbeat()
    // 先摘掉引用再关闭：旧连接的 onclose 会因 ws !== socket 直接返回，
    // 不会覆盖新连接的状态，也不会再排重连
    const old = socket
    socket = null
    old?.close()
    connected.value = false
    onlineUsers.value = new Set()
  }

  function resetPresence() {
    onlineUsers.value = new Set()
  }

  return {
    connected,
    hasConnected,
    onlineUsers,
    connect,
    send,
    disconnect,
    onFrame,
    resetPresence,
  }
})
