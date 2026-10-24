import { ref } from 'vue'
import { defineStore } from 'pinia'

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
  /** 是否成功建立过连接：用于区分首连与重连（重连后需补数据） */
  let hasConnected = false

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

    socket = new WebSocket(wsUrl())

    socket.onopen = () => {
      connected.value = true
      reconnectDelay = 1000
      startHeartbeat()
      // 重连：断线期间的消息/好友变动都不会补发帧，必须重新对齐服务端状态
      if (hasConnected) {
        // 服务端在连接建立后不补发完整在线集合，只能先清空、等后续 presence 帧重建；
        // TODO: 服务端补发一次 presence 快照后，这里可以不清空，避免重连后短暂全员离线
        onlineUsers.value = new Set()
        void conversations.resync()
        void friends.loadAll().catch((e) => console.warn('[ws] reload friends failed:', e))
      }
      hasConnected = true
    }

    socket.onmessage = (ev) => {
      let frame: ServerFrame
      try {
        frame = JSON.parse(ev.data as string) as ServerFrame
      } catch {
        return
      }
      handleFrame(frame)
    }

    socket.onclose = () => {
      connected.value = false
      // 断开后无法再得知他人在线状态，清空避免继续显示过期的"在线"
      onlineUsers.value = new Set()
      stopHeartbeat()
      if (!closedByUser) scheduleReconnect()
    }

    socket.onerror = () => {
      socket?.close()
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
        // 昵称/头像变更：本机、会话 peer、好友与申请列表就地更新
        const patch = {
          nickname: frame.nickname,
          avatar: frame.avatar,
          avatarColor: frame.avatarColor,
        }
        if (auth.user?.id === frame.userId) auth.patchProfile(patch)
        conversations.applyUserProfile({ userId: frame.userId, ...patch })
        friends.applyUserProfile({ userId: frame.userId, ...patch })
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
        // 协议错误暂只记录；可接入 MessagePlugin
        console.warn('[ws] error frame:', frame.reason)
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
    hasConnected = false
    if (reconnectTimer) {
      clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
    stopHeartbeat()
    socket?.close()
    socket = null
    connected.value = false
    onlineUsers.value = new Set()
  }

  function resetPresence() {
    onlineUsers.value = new Set()
  }

  return { connected, onlineUsers, connect, send, disconnect, onFrame, resetPresence }
})
