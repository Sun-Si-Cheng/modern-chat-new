import type { ClientFrame, ServerFrame } from '@/types/proto'

/**
 * WebSocket 传输内核（core/network）：连接、心跳、指数退避重连、握手失败统计。
 *
 * 职责边界：这里只管"把帧可靠地送到、把状态变化报出来"，不含任何业务——
 * 帧的业务分发（消息入店、kicked 下线、会话探测）在 stores/ws.ts 的状态机里，
 * 通过回调接到本通道。transport 不 import 任何 store / UI 依赖。
 *
 * 迁移自 stores/ws.ts，守卫逻辑原样保留：每个回调先确认自己是当前连接
 * （快速切页时旧 socket 处于 CLOSING，connect() 会新建一条；旧连接的 onclose
 * 若继续改状态，会把新连接标成「已断开」且断线横幅永不消失）。
 */

const HEARTBEAT_INTERVAL = 25_000
const MAX_RECONNECT_DELAY = 30_000

export interface WsChannelOptions {
  /** 返回带鉴权参数的 WS 地址（每次重连都会重新取，token 刷新后自动生效） */
  url: () => string
  /** 是否允许建立连接（通常返回 !!auth.token）；重连计时到期后据此决定连或不连 */
  canConnect: () => boolean
  /** 连接已建立（首连与重连都触发；是否需要 resync 由业务层自行判断） */
  onOpen: () => void
  /** 连接断开。opened = 本次是否成功建立过（区分握手被拒与建立后断开）；userClosed = 业务侧主动断开 */
  onClose: (info: { opened: boolean; userClosed: boolean }) => void
  /** 收到服务端业务帧（pong 心跳应答在通道内消费，不上抛） */
  onFrame: (frame: ServerFrame) => void
  /** 连续「握手未建立就断开」达到阈值：通常是 token 失效/被封禁/维护模式，业务层据此探测会话 */
  onHandshakeFailThreshold: () => void
  /** 握手失败阈值（默认 3） */
  handshakeFailLimit?: number
}

export class WsChannel {
  private readonly opts: WsChannelOptions
  private socket: WebSocket | null = null
  private heartbeatTimer: ReturnType<typeof setInterval> | null = null
  private pongTimeout: ReturnType<typeof setTimeout> | null = null
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private reconnectDelay = 1000
  private closedByUser = false
  /** 连续「握手未建立就断开」的次数：成功 onopen 或用户主动断开时归零 */
  private fails = 0

  constructor(opts: WsChannelOptions) {
    this.opts = opts
    // 页面隐藏时浏览器会节流定时器：25s 心跳可能被拖到一分钟以上，而 8s pong
    // 超时按真实时间走——超时定时器与 pong 到达的相对时序被打乱，后台标签页
    // 会把还活着的连接误判成僵死并反复重连。回到前台时清掉未决超时并立即
    // 补一帧 ping，重新对齐心跳节拍。
    if (typeof document !== 'undefined') {
      document.addEventListener('visibilitychange', () => {
        if (document.visibilityState !== 'visible') return
        if (this.pongTimeout) {
          clearTimeout(this.pongTimeout)
          this.pongTimeout = null
        }
        if (this.socket?.readyState === WebSocket.OPEN) {
          this.send({ type: 'ping' })
          this.pongTimeout = setTimeout(() => this.socket?.close(), 8000)
        }
      })
    }
  }

  get handshakeFails(): number {
    return this.fails
  }

  resetHandshakeFails(): void {
    this.fails = 0
  }

  connect(): void {
    if (!this.opts.canConnect()) return
    this.closedByUser = false
    if (this.socket && (this.socket.readyState === WebSocket.OPEN || this.socket.readyState === WebSocket.CONNECTING)) return

    // 本次连接是否成功建立过：onclose 用它区分「握手被拒」与「已建立后断开」
    let opened = false
    const ws = new WebSocket(this.opts.url())
    this.socket = ws

    ws.onopen = () => {
      if (ws !== this.socket) return
      opened = true
      this.reconnectDelay = 1000
      this.fails = 0
      this.startHeartbeat()
      this.opts.onOpen()
    }

    ws.onmessage = (ev) => {
      if (ws !== this.socket) return
      let frame: ServerFrame
      try {
        frame = JSON.parse(ev.data as string) as ServerFrame
      } catch {
        return
      }
      if (frame.type === 'pong') {
        // 心跳应答完全在本通道消费（清计时器后不上抛）——原实现里 pong 也是
        // 业务状态机的 no-op，保持 pong 不进业务层
        if (this.pongTimeout) {
          clearTimeout(this.pongTimeout)
          this.pongTimeout = null
        }
        return
      }
      this.opts.onFrame(frame)
    }

    ws.onclose = () => {
      if (ws !== this.socket) return
      this.stopHeartbeat()
      if (!this.closedByUser) {
        // 从未建立成功的断开按握手失败计：连续多次通常是 token 失效/被封禁/维护模式。
        // WebSocket 拿不到握手的 HTTP 状态码，达阈值后交给业务层探测分辨，
        // 避免对失效的会话无限重连；瞬时断线一两次就能连上，不会走到探测
        if (!opened) {
          this.fails++
          if (this.fails >= (this.opts.handshakeFailLimit ?? 3)) {
            this.resetHandshakeFails()
            this.opts.onHandshakeFailThreshold()
          }
        }
        this.scheduleReconnect()
      }
      this.opts.onClose({ opened, userClosed: this.closedByUser })
    }

    ws.onerror = () => {
      if (ws !== this.socket) return
      ws.close()
    }
  }

  /** 返回是否真的写出：连接未就绪时调用方需要把消息标记为失败而不是静默丢弃 */
  send(frame: ClientFrame): boolean {
    if (this.socket?.readyState === WebSocket.OPEN) {
      this.socket.send(JSON.stringify(frame))
      return true
    }
    return false
  }

  /** 业务侧主动断开（登出 / 被 kicked / 离开聊天页）：不再自动重连 */
  disconnect(): void {
    this.closedByUser = true
    this.fails = 0
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    this.stopHeartbeat()
    // 先摘掉引用再关闭：旧连接的 onclose 会因 ws !== socket 直接返回，
    // 不会覆盖新连接的状态，也不会再排重连
    const old = this.socket
    this.socket = null
    old?.close()
  }

  // ===== 心跳 =====

  private startHeartbeat() {
    this.stopHeartbeat()
    this.heartbeatTimer = setInterval(() => {
      if (this.socket?.readyState !== WebSocket.OPEN) return
      this.send({ type: 'ping' })
      // 8s 未收到 pong 视为僵死，强制断开触发重连
      this.pongTimeout = setTimeout(() => this.socket?.close(), 8000)
    }, HEARTBEAT_INTERVAL)
  }

  private stopHeartbeat() {
    if (this.heartbeatTimer) {
      clearInterval(this.heartbeatTimer)
      this.heartbeatTimer = null
    }
    if (this.pongTimeout) {
      clearTimeout(this.pongTimeout)
      this.pongTimeout = null
    }
  }

  // ===== 重连 =====

  private scheduleReconnect() {
    if (this.reconnectTimer) return
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null
      this.reconnectDelay = Math.min(this.reconnectDelay * 2, MAX_RECONNECT_DELAY)
      if (this.opts.canConnect()) this.connect()
    }, this.reconnectDelay)
  }
}
