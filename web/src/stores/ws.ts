import { ref } from 'vue'
import { defineStore } from 'pinia'

import { MessagePlugin } from 'tdesign-vue-next'

import { authApi } from '@/api'
import { ApiError } from '@/api/client'
import type { ClientFrame, ServerFrame } from '@/types/proto'
import { useAuthStore } from './auth'
import { useConversationsStore } from './conversations'
import { useFriendsStore } from './friends'

const HEARTBEAT_INTERVAL = 25_000
const MAX_RECONNECT_DELAY = 30_000
/** 连续 N 次握手都未能建立时，用一次 REST 探测区分「会话失效」与「网络故障」 */
const HANDSHAKE_FAIL_LIMIT = 3

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
  /** 连续「握手未建立就断开」的次数：成功 onopen 或用户主动断开时归零 */
  let handshakeFails = 0
  let probingSession = false
  /**
   * 是否成功建立过连接：用于区分首连与重连（重连后需补数据），
   * 界面也据此决定要不要显示「连接已断开」提示（首连中不闪）。
   */
  const hasConnected = ref(false)
  /**
   * 页面生命周期内是否成功连过一次。与 hasConnected 的区别：
   * 离开聊天页 disconnect 会把 hasConnected 归零（避免回到聊天页时闪断线横幅），
   * 但重连后的补数据（resync）必须照常做——从设置页回到聊天页的重连
   * 若被当成首连，聊天区就停留在离开前的缓存，红点与正文不一致。
   */
  const everConnected = ref(false)

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

    // 本次连接是否成功建立过：onclose 用它区分「握手被拒」与「已建立后断开」
    let opened = false
    const ws = new WebSocket(wsUrl())
    socket = ws

    // 每个回调都先判断自己是否仍是当前连接：快速切页时旧 socket 处于 CLOSING，
    // connect() 会新建一条；旧连接的 onclose 若继续改状态，会把已连上的新连接
    // 标成「已断开」，断线横幅将永远不消失（且会再排一次无意义的重连）
    ws.onopen = () => {
      if (ws !== socket) return
      opened = true
      connected.value = true
      reconnectDelay = 1000
      handshakeFails = 0
      startHeartbeat()
      // 重连（含从设置页回到聊天页后的重连）：断线期间的消息不会补发帧，
      // 必须重新对齐服务端状态，否则聊天区停留在旧缓存而侧栏红点照涨
      if (everConnected.value) void conversations.resync()
      hasConnected.value = true
      everConnected.value = true
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
      if (!closedByUser) {
        // 从未建立成功的断开按握手失败计：连续多次通常是 token 失效/被封禁/维护模式。
        // WebSocket 拿不到握手的 HTTP 状态码，达阈值后用一次 REST 探测分辨，
        // 避免对失效的会话无限重连；瞬时断线一两次就能连上，不会走到探测
        if (!opened) handshakeFails++
        if (handshakeFails >= HANDSHAKE_FAIL_LIMIT) void probeSession()
        scheduleReconnect()
      }
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
        // 部署在 /app/ 下：站内跳转必须带 BASE_URL，否则会落到官网根路径
        location.href = `${import.meta.env.BASE_URL}login`
        break
      }
      case 'error':
        // 带 clientMsgId：这条消息的发送被服务端拒绝（禁言/敏感词/拉黑等），
        // 立即把对应的乐观消息标为失败，不让气泡空转 8 秒等超时；原因仍用 toast 告知
        if (frame.clientMsgId) {
          if (conversations.markSendFailed(frame.clientMsgId) && frame.reason) {
            void MessagePlugin.error(frame.reason)
          }
          break
        }
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

  /**
   * 握手连续失败后的会话探测：借 GET /api/me 复用 REST 的 401 统一处理——
   * token 失效时 client.ts 会清登录态、跳登录页并触发 disconnect（重连随之停止）；
   * 403 说明 token 有效但被服务端拒绝（封禁），按收到 kicked 帧同等处理；
   * 网络故障 / 503 维护 / 其他则保留 token 继续退避重连，不破坏正常断线恢复。
   */
  async function probeSession() {
    if (probingSession || !auth.token) return
    probingSession = true
    try {
      await authApi.me()
    } catch (e) {
      const status = e instanceof ApiError ? e.status : 0
      if (status === 401) return // client.ts 已统一处理：清登录态 + 跳登录页 + 停止重连
      if (status === 403) {
        closedByUser = true
        socket?.close()
        auth.logout()
        conversations.reset()
        onlineUsers.value.clear()
        // 部署在 /app/ 下：站内跳转必须带 BASE_URL，否则会落到官网根路径
        location.href = `${import.meta.env.BASE_URL}login`
        return
      }
      // 网络故障 / 维护模式：交还给退避重连，之后仍连续失败会再次探测
    } finally {
      probingSession = false
      handshakeFails = 0
    }
  }

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
    handshakeFails = 0
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
