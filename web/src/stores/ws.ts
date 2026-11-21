import { ref } from 'vue'
import { defineStore } from 'pinia'

import { MessagePlugin } from 'tdesign-vue-next'

import { authApi } from '@/core/network/endpoints'
import { ApiError } from '@/core/network/http'
import { WsChannel } from '@/core/network/ws'
import type { ServerFrame } from '@/types/proto'
import { useAuthStore } from './auth'
import { useConversationsStore } from './conversations'
import { useFriendsStore } from './friends'

/**
 * WS 业务状态机：在线集合、断线横幅、帧的业务分发与会话探测。
 * 连接/心跳/退避重连等纯传输逻辑在 core/network/ws.ts（WsChannel），
 * 这里只回答"收到帧之后做什么"与"要不要继续连"。
 */
export const useWsStore = defineStore('ws', () => {
  const auth = useAuthStore()
  const conversations = useConversationsStore()
  const friends = useFriendsStore()

  const connected = ref(false)
  /** 在线用户 id 集合（含自己） */
  const onlineUsers = ref<Set<number>>(new Set())
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

  const channel = new WsChannel({
    url: () => {
      const proto = location.protocol === 'https:' ? 'wss' : 'ws'
      return `${proto}://${location.host}/ws?token=${encodeURIComponent(auth.token)}`
    },
    canConnect: () => !!auth.token,
    onOpen: () => {
      connected.value = true
      // 重连（含从设置页回到聊天页后的重连）：断线期间的消息不会补发帧，
      // 必须重新对齐服务端状态，否则聊天区停留在旧缓存而侧栏红点照涨
      if (everConnected.value) void conversations.resync()
      hasConnected.value = true
      everConnected.value = true
    },
    onClose: () => {
      connected.value = false
      // 断开后无法再得知他人在线状态，清空避免继续显示过期的"在线"
      onlineUsers.value = new Set()
    },
    onFrame: handleFrame,
    onHandshakeFailThreshold: () => void probeSession(),
  })

  function handleFrame(frame: ServerFrame) {
    frameListeners.forEach((fn) => fn(frame))
    switch (frame.type) {
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
        // 封禁/失效强制下线：主动断开（通道不再重连）并清空业务态
        channel.disconnect()
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
  function send(frame: Parameters<typeof channel.send>[0]): boolean {
    return channel.send(frame)
  }

  /**
   * 握手连续失败后的会话探测：借 GET /api/me 复用 REST 的 401 统一处理——
   * token 失效时 http 层会清登录态、跳登录页并触发 disconnect（重连随之停止）；
   * 403 说明 token 有效但被服务端拒绝（封禁），按收到 kicked 帧同等处理；
   * 网络故障 / 503 维护 / 其他则保留 token 继续退避重连，不破坏正常断线恢复。
   */
  let probingSession = false
  async function probeSession() {
    if (probingSession || !auth.token) return
    probingSession = true
    try {
      await authApi.me()
    } catch (e) {
      const status = e instanceof ApiError ? e.status : 0
      if (status === 401) return // http 层已统一处理：清登录态 + 跳登录页 + 停止重连
      if (status === 403) {
        channel.disconnect()
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
      channel.resetHandshakeFails()
    }
  }

  function connect() {
    channel.connect()
  }

  function disconnect() {
    hasConnected.value = false
    channel.disconnect()
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
