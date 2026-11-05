/**
 * 功能开关：只隐藏不删码。
 * 置 false 时对应 UI 不渲染（逻辑与样式保留），要重新启用改回 true 即可。
 */
export const FEATURES = {
  /**
   * 表情回应（消息气泡下的 emoji 表态）。
   * 暂未开放：设计文档 §1.2 把「表情回应」划在二期，本期只做自定义表情发送。
   */
  reactions: false,
} as const
