import { ref } from 'vue'
import { defineStore } from 'pinia'

import type { ClientFrame, ServerFrame } from '@/types/proto'
import { useAuthStore } from './auth'
import { useConversationsStore } from './conversations'

const HEARTBEAT_INTERVAL = 25_000
const MAX_RECONNECT_DELAY = 30_000

/** WebSocket 连接生命周期：心跳 + 指数退避重连 + 帧路由（§3 / §7） */
export const useWsStore = defineStore('ws', () => {
  const auth = useAuthStore()
  const conversations = useConversationsStore()

  const connected = ref(false)
  /** 在线用户 id 集合（含自己） */
  const onlineUsers = ref<Set<number>>(new Set())

  let socket: WebSocket | null = null
  let heartbeatTimer: ReturnType<typeof setInterval> | null = null
  let pongTimeout: ReturnType<typeof setTimeout> | null = null
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null
  let reconnectDelay = 1000
  let closedByUser = false

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

  function send(frame: ClientFrame) {
    if (socket?.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify(frame))
    }
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
    if (reconnectTimer) {
      clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
    stopHeartbeat()
    socket?.close()
    socket = null
    connected.value = false
  }

  function resetPresence() {
    onlineUsers.value = new Set()
  }

  return { connected, onlineUsers, connect, send, disconnect, onFrame, resetPresence }
})
