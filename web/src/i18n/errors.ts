/**
 * 服务端错误消息翻译。
 * 后端错误体是中文文案（{error:"..."}），英文界面下按已知消息映射；
 * 未收录的消息原样透传（宁可显示中文也不要吞掉信息）。
 */
import type { Lang } from '.'

const exact: Record<string, string> = {
  '登录已失效，请重新登录': 'Session expired. Please sign in again.',
  '用户名已被占用': 'This username is already taken.',
  '邮箱已被绑定': 'This email is already in use.',
  '用户名或密码错误': 'Incorrect username or password.',
  '密码至少 6 位': 'Password must be at least 6 characters.',
  '密码最长 72 位': 'Password must be at most 72 characters.',
  '用户名需为 3-20 位字母、数字、下划线或连字符': 'Username must be 3-20 letters, digits, underscores or hyphens.',
  '昵称需为 1-20 个字符': 'Display name must be 1-20 characters.',
  '邮箱格式不正确': 'Invalid email address.',
  '请求格式错误': 'Malformed request.',
  '操作过于频繁，请稍后再试': 'Too many requests. Please try again later.',
  '服务器开小差了，请稍后再试': 'Something went wrong. Please try again.',
  '无权执行此操作': 'You are not allowed to do this.',
  '无权访问该会话': 'You do not have access to this conversation.',
  '会话不存在': 'Conversation not found.',
  '用户不存在': 'User not found.',
  '插件不存在': 'Plugin not found.',
  '对方已是好友或申请中': 'You are already friends or a request is pending.',
  '不能添加自己为好友': 'You cannot add yourself as a friend.',
  '不能删除自己': 'You cannot remove yourself.',
  '你已在群中': 'You are already in this group.',
  '群成员已满（200 人上限）': 'The group is full (200 members max).',
  '群主不能退群，请先转让': 'Transfer ownership before leaving the group.',
  '超过 2 分钟，无法撤回': 'Messages can only be recalled within 2 minutes.',
  '不能移除自己，请使用转让或退群': 'Use transfer or leave instead of removing yourself.',
  '该用户不在群中': 'This user is not in the group.',
  '重置令牌无效或已过期': 'The reset token is invalid or expired.',
  '插件文件超过 1MB': 'Plugin file exceeds 1MB.',
  '插件已被站长下架': 'This plugin was disabled by the admin.',
  '消息需为 1-1000 个字符': 'Messages must be 1-1000 characters.',
  '申请不存在或已处理': 'Request not found or already handled.',
  '只能与好友发起单聊': 'You can only start a direct chat with friends.',
  '不能和自己发起单聊': 'You cannot start a direct chat with yourself.',
  '单聊无需加入': 'Direct chats need no joining.',
  '群名称需为 1-30 个字符': 'Group name must be 1-30 characters.',
  '不能封禁站长账号': 'The owner account cannot be banned.',
  'days 取值 0-3650（0 表示永久）': 'days must be 0-3650 (0 means permanent).',
  'upToId 不合法': 'Invalid upToId.',
  '关键词需为 1-50 个字符': 'Keyword must be 1-50 characters.',
  '路径参数不合法': 'Invalid path parameter.',
  '会话 id 不合法': 'Invalid conversation id.',
  '用户 id 不合法': 'Invalid user id.',
  '插件 id 不合法': 'Invalid plugin id.',
  '插件名不合法': 'Invalid plugin name.',
  '读取上传内容失败': 'Failed to read the upload.',
  '插件拉取失败': 'Failed to fetch the plugin.',
}

const prefixes: Array<[string, string]> = [['账号已被封禁', 'Account banned']]

export function translateError(msg: string, lang: Lang): string {
  if (!msg || lang !== 'en-US') return msg
  const hit = exact[msg]
  if (hit) return hit
  for (const [zh, en] of prefixes) {
    if (msg.startsWith(zh)) return msg.replace(zh, en)
  }
  return msg
}
