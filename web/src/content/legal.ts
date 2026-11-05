/**
 * 法律文档内容源（唯一维护点）。
 *
 * 部署者须知：
 * 1. legalMeta 中的占位符（方括号内容）必须替换为你自己的真实信息；
 * 2. 本文本为项目自带模板，不构成法律意见，正式对外运营前请由法务审阅；
 * 3. 英文版本仅为阅读便利，与中文版本冲突时以中文版本为准（已在页面标注）。
 */

export interface LegalMeta {
  /** 生效日期（YYYY-MM-DD） */
  effectiveDate: string
  /** 运营主体名称（部署者填写） */
  operator: string
  /** 联系邮箱（部署者填写） */
  contact: string
  /** 服务域名（部署者填写） */
  domain: string
}

export const legalMeta: LegalMeta = {
  effectiveDate: '2026-10-24',
  operator: '[运营主体名称]',
  contact: '[联系邮箱]',
  domain: '[部署域名]',
}

/** blocks 中以 "- " 开头的行渲染为列表项，其余渲染为段落。 */
export interface LegalSection {
  heading: string
  blocks: string[]
}

export interface LegalDoc {
  title: string
  updatedAt: string
  intro: string[]
  sections: LegalSection[]
}

// ===================== 用户协议 =====================

export const termsZh: LegalDoc = {
  title: '用户协议',
  updatedAt: '2026-11-05',
  intro: [
    `本协议是你与${legalMeta.operator}（以下简称"运营者"）之间就使用 ${legalMeta.domain}（以下简称"本服务"）所订立的协议。`,
    '请在注册或使用本服务前完整阅读本协议。你注册账号、登录或使用本服务的任一功能，即表示你已阅读并同意本协议全部内容。若你不同意，请停止使用。',
  ],
  sections: [
    {
      heading: '一、服务说明',
      blocks: [
        '本服务是一个自托管的即时通讯（聊天）平台。运营者基于开源项目 ModernChat 部署并运营本实例，提供账号、好友、单聊与群聊、消息收发、插件扩展等功能。',
        '本服务的基础能力（账号、纯文本聊天）由平台内核提供；图片、表情、主题、机器人等扩展能力由第三方插件提供，插件由用户自行安装、自行承担使用风险。',
        '本服务的站长（首个注册账号或被指定为管理员的账号）拥有用户管理与插件下架权限，但不因此获得阅读你私人消息内容的产品功能。',
      ],
    },
    {
      heading: '二、账号',
      blocks: [
        '你需注册账号后方可使用聊天功能。注册时应提供真实、合法的信息，不得冒用他人身份或使用引人误解的用户名、昵称。',
        '你应妥善保管账号密码。通过你的账号进行的操作视为你本人的行为，由此产生的后果由你承担。发现账号被盗用应立即修改密码并通知运营者。',
        '账号仅限你本人使用，不得出借、出租、出售或转让。',
        '你可以在个人资料中修改昵称与邮箱。当前版本不提供自助注销账号功能，如需注销请联系运营者。',
      ],
    },
    {
      heading: '三、使用规范',
      blocks: [
        '你在使用本服务时，不得制作、复制、发布、传播含有下列内容的信息：',
        '- 违反法律法规、危害国家安全、破坏社会稳定的内容；',
        '- 淫秽、色情、赌博、暴力、凶杀、恐怖或者教唆犯罪的内容；',
        '- 侮辱、诽谤、威胁、骚扰他人，或侵犯他人隐私、名誉、知识产权的内容；',
        '- 垃圾广告、恶意营销、诈骗、钓鱼链接或其他误导性信息；',
        '- 其他违反公序良俗或法律法规的内容。',
        '你不得实施下列行为：',
        '- 使用自动化程序批量注册、批量发送消息、爬取用户数据；',
        '- 攻击、探测、绕过本服务的安全机制（包括限流、鉴权、插件权限白名单）；',
        '- 上传或安装用于窃取他人信息、破坏服务运行的插件；',
        '- 干扰其他用户正常使用，或使服务器承载超出正常范围的压力。',
      ],
    },
    {
      heading: '四、插件',
      blocks: [
        '插件由第三方作者开发，并非运营者提供。运营者对插件不作任何明示或默示的担保，包括但不限于功能完整性、安全性、合法性。',
        '安装插件时，平台会展示该插件申请的权限与网络访问白名单，你确认后插件才会启用。请仅安装你信任的插件。',
        '插件在浏览器沙箱中运行，无法直接读取页面内容或你的登录凭证，但可在你授权的权限范围内读取消息事件、发送消息，并向其白名单内的域名发起网络请求。你应自行评估因此产生的信息暴露风险。',
        '运营者有权对任何插件作全局下架处理，此操作对所有用户立即生效。',
      ],
    },
    {
      heading: '五、用户内容',
      blocks: [
        '你发布的消息等内容的所有权归你所有。为实现消息存储与投递，你授予运营者一项非独占的、仅为提供服务所必需的许可：存储、传输、展示你的内容，以及在必要时（如依法配合调查、处理违规）进行删除。',
        '你应确保你发布的内容不侵犯他人权益。因你的内容引发纠纷的，由你自行承担责任。',
        '你可以撤回 2 分钟内发送的消息。撤回后消息正文不再向其他用户展示，但运营者可能因备份、审计或法律要求仍保留相应记录。',
      ],
    },
    {
      heading: '六、知识产权与开源许可',
      blocks: [
        '本服务的软件本体基于 Apache License 2.0 开源。你可以在遵守该许可证的前提下使用、修改与分发该软件。',
        '本服务的名称、标识、界面设计等由运营者或项目作者保留权利，未经许可不得以混淆方式使用。',
        '本服务中由第三方提供的插件，其知识产权归相应作者所有。',
      ],
    },
    {
      heading: '七、服务变更、中断与终止',
      blocks: [
        '运营者可能因维护、升级、故障或不可抗力中断服务。运营者将尽可能提前通知，但不对中断造成的损失承担责任（法律强制规定的除外）。',
        '若你违反本协议，运营者有权视情节采取警告、删除内容、限制功能、临时或永久封禁账号等措施。封禁将立即终止你已登录的全部会话。',
        '你可以在群聊中退出，或请求运营者删除你的账号与相关数据。账号注销后，你发布的消息可能仍以匿名或原形式保留在他人会话中。',
      ],
    },
    {
      heading: '八、免责与责任限制',
      blocks: [
        '本服务按"现状"提供。在遵守法律的前提下，运营者不对服务的绝对不中断、无错误、数据绝对不丢失作出承诺。你应自行备份重要信息。',
        '因你自身设备、网络、操作不当，或第三方插件、第三方服务造成的损失，运营者不承担责任。',
        '在法律允许的最大范围内，运营者对因使用或无法使用本服务产生的间接损失、利润损失、数据损失不承担赔偿责任。',
      ],
    },
    {
      heading: '九、协议修改',
      blocks: [
        '运营者可根据业务或法律变化修改本协议，修改后将在本页面公布并更新"更新日期"。重大变更将在登录后以显著方式提示。',
        '修改生效后你继续使用本服务的，视为接受修改后的协议；若不接受，你应停止使用并可请求注销账号。',
      ],
    },
    {
      heading: '十、法律适用与争议解决',
      blocks: [
        '本协议的订立、效力、解释与争议解决适用中华人民共和国法律（不含冲突法）。',
        '因本协议产生的争议，双方应友好协商解决；协商不成的，任一方可向运营者所在地有管辖权的人民法院提起诉讼。',
      ],
    },
    {
      heading: '十一、联系我们',
      blocks: [`如对本协议有疑问，可发送邮件至 ${legalMeta.contact}。`],
    },
  ],
}

export const termsEn: LegalDoc = {
  title: 'Terms of Service',
  updatedAt: '2026-11-05',
  intro: [
    `These terms govern your use of ${legalMeta.domain} (the "Service"), operated by ${legalMeta.operator} (the "Operator").`,
    'Please read them before registering. By registering, signing in, or using any feature of the Service, you agree to these terms in full. If you do not agree, do not use the Service.',
  ],
  sections: [
    {
      heading: '1. The Service',
      blocks: [
        'The Service is a self-hosted instant messaging platform. The Operator runs this instance on top of the open-source project ModernChat and provides accounts, contacts, direct and group chats, messaging and plugin extensions.',
        'Core features (accounts, plain-text chat) are provided by the platform. Extended features (images, emoji, themes, bots) are provided by third-party plugins that users install at their own discretion and risk.',
        'The site owner (the first registered account or an account granted administrator rights) may manage users and disable plugins, but is not given a product-level ability to read your private messages.',
      ],
    },
    {
      heading: '2. Accounts',
      blocks: [
        'You must register to use chat features. Provide accurate information and do not impersonate others or use misleading usernames or display names.',
        'Keep your password secure. Actions taken through your account are deemed your own, and you are responsible for their consequences. If your account is compromised, change your password and notify the Operator immediately.',
        'Accounts are personal and may not be lent, rented, sold, or transferred.',
        'You may change your display name and email in your profile. Self-service account deletion is not available in this version; contact the Operator to request deletion.',
      ],
    },
    {
      heading: '3. Acceptable Use',
      blocks: [
        'You must not create, copy, publish, or distribute:',
        '- content that violates the law, endangers national security, or undermines public order;',
        '- obscene, pornographic, gambling, violent, or terrorism-related content;',
        '- content that insults, defames, threatens, or harasses others, or infringes privacy, reputation, or intellectual property;',
        '- spam, deceptive marketing, fraud, phishing links, or other misleading information.',
        'You must not:',
        '- use automated tools to bulk-register, bulk-send, or scrape user data;',
        '- attack, probe, or bypass security mechanisms (rate limits, authentication, plugin permission allowlists);',
        '- upload or install plugins designed to steal data or disrupt the Service;',
        '- interfere with other users or impose abnormal load on the servers.',
      ],
    },
    {
      heading: '4. Plugins',
      blocks: [
        'Plugins are developed by third parties, not the Operator. The Operator makes no warranty, express or implied, regarding their functionality, security, or legality.',
        'When you install a plugin, the platform shows the permissions and network allowlist it requests; it is enabled only after you confirm. Install only plugins you trust.',
        'Plugins run in a browser sandbox and cannot directly read page content or your credentials, but within the permissions you grant they can read message events, send messages, and make network requests to their allowlisted domains. Assess the resulting exposure yourself.',
        'The Operator may disable any plugin globally, effective immediately for all users.',
      ],
    },
    {
      heading: '5. Your Content',
      blocks: [
        'You own the content you post. To store and deliver messages, you grant the Operator a non-exclusive license, limited to what is necessary to provide the Service: storing, transmitting, and displaying your content, and deleting it where necessary (for example, to comply with law or handle violations).',
        'You are responsible for ensuring your content does not infringe the rights of others, and for any disputes arising from it.',
        'You may recall a message within 2 minutes. After recall the content is no longer shown to other users, but the Operator may retain records for backup, audit, or legal purposes.',
      ],
    },
    {
      heading: '6. Intellectual Property and Open Source',
      blocks: [
        'The software is open source under the Apache License 2.0. You may use, modify, and distribute it in compliance with that license.',
        "The Service's name, marks, and interface design remain the rights of the Operator or project authors and may not be used in a confusing manner without permission.",
        'Third-party plugins remain the property of their respective authors.',
      ],
    },
    {
      heading: '7. Changes, Suspension, and Termination',
      blocks: [
        'The Operator may suspend the Service for maintenance, upgrades, outages, or force majeure. Advance notice will be given where possible, but the Operator is not liable for losses caused by interruptions (except as required by law).',
        'If you breach these terms, the Operator may warn you, remove content, restrict features, or suspend your account temporarily or permanently. Suspension signs out all of your active sessions immediately.',
        'You may leave group chats, or ask the Operator to delete your account and related data. After deletion, messages you posted may remain in other users’ conversations.',
      ],
    },
    {
      heading: '8. Disclaimer and Limitation of Liability',
      blocks: [
        'The Service is provided "as is". To the extent permitted by law, the Operator does not warrant uninterrupted, error-free operation or absolute data integrity. Back up important information yourself.',
        'The Operator is not liable for losses caused by your devices, network, or misuse, or by third-party plugins or services.',
        'To the maximum extent permitted by law, the Operator is not liable for indirect, incidental, or consequential damages, lost profits, or lost data arising from use of the Service.',
      ],
    },
    {
      heading: '9. Changes to These Terms',
      blocks: [
        'The Operator may amend these terms for business or legal reasons. Changes are published on this page with an updated date; material changes will be prominently announced after sign-in.',
        'Continued use after changes take effect constitutes acceptance. If you do not accept them, stop using the Service and request account deletion.',
      ],
    },
    {
      heading: '10. Governing Law and Disputes',
      blocks: [
        'These terms are governed by the laws of the People’s Republic of China, excluding conflict-of-law rules.',
        'Disputes shall first be resolved through good-faith negotiation; failing that, either party may bring an action before the competent court where the Operator is located.',
      ],
    },
    {
      heading: '11. Contact',
      blocks: [`Questions about these terms: ${legalMeta.contact}.`],
    },
  ],
}

// ===================== 隐私协议 =====================

export const privacyZh: LegalDoc = {
  title: '隐私协议',
  updatedAt: '2026-11-05',
  intro: [
    `本协议说明${legalMeta.operator}在你使用 ${legalMeta.domain} 时如何收集、使用、存储与保护你的个人信息。`,
    '我们遵循最小必要原则：只收集提供服务所必需的信息。你使用本服务即表示理解并同意本协议。',
  ],
  sections: [
    {
      heading: '一、我们收集的信息',
      blocks: [
        '1. 账号信息：用户名、密码（仅保存不可逆的加密哈希，不保存明文）、昵称、头像颜色；如站点要求邮箱验证或你选择填写，还包括邮箱。',
        '2. 内容信息：你发送的消息（正文、消息类型、发送时间、所属会话）、你创建或加入的群聊、好友关系、已读状态与未读数。',
        '3. 设备与日志信息：IP 地址、浏览器类型与版本、请求路径、响应状态、耗时。这些信息用于安全防护、故障排查与频率限制。',
        '4. 在线状态：你是否在线、正在输入的状态。该状态仅保存在服务器内存中，不上报给第三方，服务重启即清除。',
        '5. 插件相关：你安装的插件名称、版本、启用的权限与网络白名单、插件代码。插件自身写入的数据保存在你的浏览器本地（见第四节）。',
        '我们不会收集你的通讯录、精确位置、相册、剪贴板内容或生物识别信息。',
      ],
    },
    {
      heading: '二、收集与使用目的',
      blocks: [
        '提供核心功能：完成账号注册登录、消息存储与投递、会话与好友管理、未读与已读状态同步。',
        '保障安全：识别异常登录与滥用行为，执行频率限制、封禁与插件下架，防止 SSRF、跨站请求伪造等攻击。',
        '改善服务：通过聚合后的日志定位故障与性能问题（不用于个性化广告，也不向第三方出售）。',
        '发送邮件：仅在你请求找回密码或注册需要邮箱验证时，向你的邮箱发送一次性重置链接或 6 位验证码。',
      ],
    },
    {
      heading: '三、本地存储（浏览器）',
      blocks: [
        '本服务不使用第三方广告或分析类 Cookie。为维持登录态与界面偏好，你的浏览器会保存以下本地存储项：',
        '- mc_token：登录凭证（JWT，有效期 7 天）。退出登录或凭证失效后清除；',
        '- mc_lang：界面语言偏好（中文/英文）；',
        '- mc_theme：亮色/暗色/跟随系统的主题偏好；',
        '- 插件专属存储：你安装的插件如需保存数据，会以插件独立命名空间写入浏览器本地存储。',
        '你可以随时在浏览器设置中清除这些数据。清除 mc_token 将导致你需要重新登录。',
      ],
    },
    {
      heading: '四、信息的共享与披露',
      blocks: [
        '我们不会出售、出租你的个人信息。仅在以下情形共享必要信息：',
        '- 邮件服务商：找回密码或注册邮箱验证时，将你的邮箱地址与链接/验证码交给为你部署实例所配置的 SMTP 服务商发送；未配置邮件服务时，重置链接仅打印在服务器日志中。',
        '- 插件网络请求：经你授权后，插件可请求其白名单内的域名。请求内容由插件决定，可能包含该插件获取到的消息事件数据。请只安装可信插件。',
        '- 法律要求：依法配合司法或行政机关的合法要求，或在必要时为保护用户与公众的人身财产安全而披露。',
        '需要说明的是：部署本服务的运营者拥有服务器与数据库的管理权限，在技术上具备访问全部存储数据的能力。我们通过权限分离（站长后台不提供阅读消息的功能）与操作留痕来限制此类访问，但无法从技术上完全排除。若你对这一点有顾虑，请勿在私密对话中分享高度敏感的信息。',
      ],
    },
    {
      heading: '五、存储与安全措施',
      blocks: [
        '数据存储于运营者自有的 MySQL 数据库中，采用 utf8mb4 字符集；不同会话与用户的数据通过服务端权限校验隔离。',
        '密码使用 bcrypt（cost 10）散列，任何人（包括运营者）无法从数据库中还原明文密码。',
        '登录凭证为签名令牌（JWT，HS256）。修改密码、被重置密码或被封禁时，服务端会立即使该账号此前签发的全部令牌失效。',
        'WebSocket 连接校验来源域名白名单；插件在无同源权限的浏览器沙箱中运行，仅可通过白名单组件渲染界面；URL 安装插件时服务端拒绝内网地址（SSRF 防护）。',
        '生产环境应启用 HTTPS（由部署者配置）。若你通过 HTTP 访问，传输内容可能被网络中间人窃取，请避免在非加密环境下发送敏感信息。',
      ],
    },
    {
      heading: '六、保留期限',
      blocks: [
        '账号信息：在账号存续期间保留；注销后按运营者流程删除或匿名化。',
        '消息内容：长期保留以支持消息漫游，直到你或运营者删除、或服务终止。撤回的消息正文不再向其他用户展示。',
        '找回密码令牌：15 分钟有效，使用后立即失效。',
        '访问日志：按运维需要保留（默认不超过 90 天），用于安全审计与故障排查。',
        '封禁记录：在封禁期内及其后必要期间保留，用于防止规避封禁。',
      ],
    },
    {
      heading: '七、你的权利',
      blocks: [
        '- 访问与更正：你可在个人资料中查看并修改昵称、邮箱。',
        '- 删除：你可以撤回 2 分钟内的消息、退出群聊；账号与全部数据的删除需联系运营者处理。',
        '- 撤回同意：你可以停止使用并请求注销账号。清除浏览器本地存储可删除本地偏好与插件数据。',
        '- 投诉与咨询：可通过下方联系方式提出，我们将在合理期限内答复。',
        '因本服务为自托管实例，数据责任主体是部署运营者而非开源项目作者；请向本实例的运营者行使上述权利。',
      ],
    },
    {
      heading: '八、未成年人保护',
      blocks: [
        '本服务不面向未满 14 周岁的儿童。若你未满 14 周岁，请在监护人陪同下阅读本协议，并在取得监护人同意后再使用。',
        '若监护人发现未成年人未经同意使用本服务，可联系运营者删除相关账号与数据。',
      ],
    },
    {
      heading: '九、第三方链接与插件',
      blocks: [
        '本服务可能包含第三方插件或外部链接。这些第三方有其独立的隐私政策，我们无法控制其数据处理行为，你应自行阅读并判断。',
        '插件被下架后，其历史数据可能仍保留在你的浏览器本地，你可通过清除浏览器存储删除。',
      ],
    },
    {
      heading: '十、协议更新',
      blocks: [
        '我们可能更新本协议。更新后将在本页面公布并修改"更新日期"；涉及收集范围或使用方式的重大变更，将在登录后以显著方式提示。',
      ],
    },
    {
      heading: '十一、联系我们',
      blocks: [`隐私相关问题、数据访问或删除请求，请联系：${legalMeta.contact}。`],
    },
  ],
}

export const privacyEn: LegalDoc = {
  title: 'Privacy Policy',
  updatedAt: '2026-11-05',
  intro: [
    `This policy explains how ${legalMeta.operator} collects, uses, stores, and protects your information when you use ${legalMeta.domain}.`,
    'We follow the principle of data minimisation: we collect only what is needed to run the Service. By using the Service you acknowledge and agree to this policy.',
  ],
  sections: [
    {
      heading: '1. Information We Collect',
      blocks: [
        'Account data: username, password (stored only as an irreversible hash, never in plain text), display name, avatar colour, and email if the site requires verification or you choose to provide one.',
        'Content data: messages you send (text, type, timestamp, conversation), groups you create or join, friend relationships, read receipts, and unread counts.',
        'Device and log data: IP address, browser type and version, request path, response status, and duration, used for security, troubleshooting, and rate limiting.',
        'Presence: whether you are online and typing. Presence is kept in server memory only, is not shared with third parties, and is cleared on restart.',
        'Plugin data: names, versions, granted permissions and network allowlists, and plugin code you install. Data written by plugins is stored locally in your browser (see section 3).',
        'We do not collect your address book, precise location, photo library, clipboard contents, or biometric data.',
      ],
    },
    {
      heading: '2. Purposes',
      blocks: [
        'Providing core features: registration and sign-in, message storage and delivery, conversation and contact management, read-state sync.',
        'Security: detecting abnormal sign-ins and abuse, enforcing rate limits, bans, and plugin takedowns, and preventing SSRF, cross-site request forgery, and similar attacks.',
        'Improving the Service: diagnosing faults and performance issues using aggregated logs. We do not use your data for ad targeting and do not sell it.',
        'Email: only when you request a password reset or when registration requires email verification, to send a one-time reset link or a 6-digit code.',
      ],
    },
    {
      heading: '3. Local Storage in Your Browser',
      blocks: [
        'We do not use third-party advertising or analytics cookies. To keep you signed in and remember your preferences, your browser stores:',
        '- mc_token: your sign-in credential (JWT, valid for 7 days), cleared on sign-out or expiry;',
        '- mc_lang: interface language preference;',
        '- mc_theme: light / dark / follow-system theme preference;',
        '- plugin storage: plugins may write data to a namespaced area of local storage.',
        'You can clear these at any time in your browser settings. Removing mc_token requires you to sign in again.',
      ],
    },
    {
      heading: '4. Sharing and Disclosure',
      blocks: [
        'We do not sell or rent your information. We share only what is necessary in these cases:',
        '- Email provider: your address plus the reset link or verification code are handed to the SMTP provider configured for this instance. If no mail service is configured, the reset link is printed to server logs only.',
        '- Plugin network requests: after you grant permission, a plugin may call domains on its allowlist. The request content is determined by the plugin and may include message data it received. Install only plugins you trust.',
        '- Legal requirements: we may disclose data to comply with lawful requests, or where necessary to protect the safety of users and the public.',
        'Please note: whoever operates this instance has administrative access to the server and database, and therefore technically can access all stored data. Operation is restricted by design (the admin console offers no message-reading feature) and by the audit trail, but this cannot be ruled out technically. If that matters to you, do not share highly sensitive information in private conversations.',
      ],
    },
    {
      heading: '5. Storage and Security',
      blocks: [
        'Data is stored in the Operator’s own MySQL database (utf8mb4). Access to conversations and users is isolated by server-side membership checks.',
        'Passwords are hashed with bcrypt (cost 10); nobody, including the Operator, can recover plain-text passwords from the database.',
        'Sign-in credentials are signed tokens (JWT, HS256). Changing your password, having it reset, or being banned immediately invalidates every token issued to the account.',
        'WebSocket connections validate an origin allowlist; plugins run in a browser sandbox without same-origin privileges and can only render through allowlisted components; plugin installation by URL blocks internal addresses (SSRF protection).',
        'HTTPS should be enabled in production (configured by the Operator). If you reach the Service over HTTP, traffic may be intercepted; avoid sending sensitive information without encryption.',
      ],
    },
    {
      heading: '6. Retention',
      blocks: [
        'Account data: kept while the account exists; deleted or anonymised after deletion at the Operator’s discretion.',
        'Messages: retained to support message history until deleted by you or the Operator, or until the Service ends. Recalled messages are no longer shown to other users.',
        'Password reset tokens: valid for 15 minutes and invalidated on use.',
        'Access logs: retained as needed for operations (normally no more than 90 days).',
        'Ban records: retained during and shortly after a ban to prevent circumvention.',
      ],
    },
    {
      heading: '7. Your Rights',
      blocks: [
        '- Access and correction: view and edit your display name and email in your profile.',
        '- Deletion: recall messages within 2 minutes and leave groups; deleting your account and all associated data requires contacting the Operator.',
        '- Withdrawing consent: stop using the Service and request deletion. Clearing browser storage removes local preferences and plugin data.',
        '- Complaints and questions: contact us using the details below; we will respond within a reasonable period.',
        'Because this is a self-hosted instance, the data controller is the Operator of this instance, not the authors of the open-source project. Please exercise these rights with the Operator.',
      ],
    },
    {
      heading: '8. Minors',
      blocks: [
        'The Service is not directed at children under 14. If you are under 14, read this policy with your guardian and use the Service only with their consent.',
        'Guardians may contact the Operator to delete an account and its data used without consent.',
      ],
    },
    {
      heading: '9. Third Parties',
      blocks: [
        'The Service may include third-party plugins or external links. Those parties have their own privacy policies; we do not control their processing, so review them yourself.',
        'After a plugin is disabled, its data may remain in your browser and can be removed by clearing browser storage.',
      ],
    },
    {
      heading: '10. Changes',
      blocks: [
        'We may update this policy. Changes are published on this page with a new updated date; material changes to what we collect or how we use it will be prominently announced after sign-in.',
      ],
    },
    {
      heading: '11. Contact',
      blocks: [`For privacy questions or data access/deletion requests: ${legalMeta.contact}.`],
    },
  ],
}
