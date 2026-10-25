/* ============================================================
   ModernChat 官网脚本：双语切换 / 交互演示 / 快速开始 Tabs / 文档渲染
   ============================================================ */
(function () {
  'use strict';

  /* ---------------- i18n ---------------- */
  var I18N = {
    en: {
      'site.title': 'ModernChat — The plugin-first web chat platform',
      'site.desc': 'Out of the box: accounts and plain-text chat. Images, emoji, themes, bots and mini-games all come from pluggable JS plugins. Go + Vue 3, open source, self-hosted.',
      'nav.demo': 'Live demo',
      'nav.features': 'Features', 'nav.plugins': 'Plugins', 'nav.quickstart': 'Quick start', 'nav.docs': 'Docs',
      'hero.badge-open': 'Open source · Self-hosted',
      'hero.h1': 'The plugin-first<br>web chat platform',
      'hero.sub': 'Out of the box you get plain accounts + plain-text chat. Images, emoji, Markdown, dark themes, bots, mini-games — every extra comes from pluggable JS plugins. Uninstall them and you are back to plain text.',
      'hero.cta-start': 'Quick start', 'hero.cta-docs': 'Read the docs',
      'hero.p1': 'Sandboxed plugin runtime: no DOM, no JWT, enabled only after you confirm the permission list',
      'hero.p2': '10,000+ concurrent WebSocket connections per instance, end-to-end P99 < 100ms',
      'hero.p3': 'One command with Docker Compose, one command to upgrade from Gitee',
      'demo.title': 'Product Chat', 'demo.subtitle': '12 members · 3 online',
      'demo.composer': 'Type a message…', 'demo.send': 'Send',
      'demo.caption': '▶ Demo: plain-text messages · plugin custom message card · plugin switches dark theme',
      'features.title': 'Features',
      'features.sub': 'A thin kernel, everything else is a plugin. These are the out-of-the-box kernel capabilities.',
      'f1.t': 'Secure accounts',
      'f1.d': 'Register / login / password recovery, bcrypt hashing, JWT with token_version for instant revocation, ban & force logout, custom avatars.',
      'f2.t': 'Realtime pipeline',
      'f2.d': 'WebSocket heartbeat + exponential-backoff reconnect, read receipts, 2-minute recall, typing indicator, presence, auto resync after reconnect.',
      'f3.t': 'Unified conversations',
      'f3.d': 'One conversation model: idempotent direct chats, group create / search / join / leave, kick & owner transfer, unread badges and list filters.',
      'f4.t': 'Plugin system',
      'f4.d': 'Sandboxed iframes, declarative UI rendered by whitelist components, permission confirmation on install, same-name upload upgrades, admin global takedown.',
      'f5.t': 'Admin console',
      'f5.d': 'Ban / unban / random password reset, plugins aggregated by name with global takedown, build version at a glance.',
      'f6.t': 'Ops friendly',
      'f6.d': 'Structured daily-rotated logs, three-tier rate limiting (optional shared Redis quota), health & version endpoints, one-command upgrade from Gitee.',
      'plugin.title': 'Plugins: one JS file extends the whole site',
      'plugin.sub': 'Each plugin is a JS file (≤1MB) running in a sandbox iframe, talking to the host via postMessage. All UI is described as JSON and rendered by whitelist components.',
      'plugin.security-t': 'Security model',
      'plugin.s1': 'Plugins never touch the DOM, cannot read the JWT or host storage',
      'plugin.s2': 'Permission list shown at install; enabled only after user confirmation',
      'plugin.s3': 'Network requests go through the host bridge, only allowedHosts domains pass',
      'plugin.s4': 'URL install has SSRF protection: private / loopback / non-http(s) rejected',
      'plugin.ui-t': 'Declarative UI: 8 whitelist components',
      'plugin.ui-note': 'Components outside the whitelist and javascript: links are always rejected — plugins have no arbitrary HTML injection.',
      'sdk.api': 'API', 'sdk.role': 'Purpose', 'sdk.perm': 'Permission',
      'sdk.r1': 'Subscribe to message:new / recall / presence / conv:open / ui:action',
      'sdk.r2': 'Send messages; objects are JSON-serialized automatically',
      'sdk.r3': 'Custom message type → UI description',
      'sdk.r4': 'Submit side panel UI description',
      'sdk.r5': 'Per-plugin namespaced storage (≤64KB per value)',
      'sdk.r6': 'Host-mediated fetch, only allowedHosts domains pass',
      'sdk.r7': 'Modify TDesign theme tokens, site-wide instant reskin',
      'qs.title': 'Quick start',
      'qs.sub': 'Two terminals, four commands, one complete ModernChat. The first registered account becomes the admin.',
      'qs.docker': 'Docker (recommended)', 'qs.manual': 'Manual', 'qs.verify': 'Verify',
      'qs.stack': 'Tech stack',
      'footer.line': 'An open-source, frontend-backend separated, plugin-first web chat platform.',
      'docs.title': 'Docs — ModernChat',
      'docs.zhNote': 'Docs are maintained in the repository and kept in sync with the code. English translations live in assets/docs/en/.',
      'docs.loading': 'Loading…',
      'docs.notfound': 'Document not found.',
      'docs.prev': '← Previous', 'docs.next': 'Next →',
      'docs.zhOriginal': 'English version not available yet — showing the Chinese original.',
      // demo script (English)
      'demo.m1': 'The plugin system just shipped 🎉',
      'demo.m2': 'Let me install the weather plugin.',
      'demo.via': 'via Weather Plugin',
      'demo.city': 'Hangzhou', 'demo.sky': 'Sunny', 'demo.refresh': 'Refresh',
      'demo.m3': 'And a theme plugin — dark mode in one click ✨',
      'demo.names': ['Alice', 'Bob', 'Bob', 'Carol'],
      'pg.title': 'Live demo',
      'pg.sub': 'No signup, no deployment — below is an interactive mini ModernChat: switch conversations, send messages, get auto-replies with read receipts, open the plugin side panel, and flip light/dark themes.',
      'pg.dark': 'Dark', 'pg.light': 'Light', 'pg.reset': 'Reset',
      'pg.note': 'Frontend-only simulation · nothing leaves your browser',
      'pg.input': 'Type a message and press Enter…',
      'pg.send': 'Send', 'pg.read': 'Read', 'pg.me': 'Me', 'pg.typing': 'typing…',
      'pg.groupName': 'Product Chat', 'pg.groupSub': '12 members',
      'pg.directName': 'Carol', 'pg.directSub': 'online',
      'pg.pluginTitle': 'Weather',
      'pg.pluginBy': 'Powered by the weather plugin · rendered via declarative UI',
      'pg.names': ['Alice', 'Bob', 'Carol'],
      'pg.bot': [
        'Got it! The plugin system is really handy 🎉',
        'Try installing the weather plugin!',
        'Dark theme feels much better, right?',
        'This demo is simulated entirely in your browser',
        'Right — read receipts shipped in v1',
        'All frames go through the same pipeline as the real app'
      ],
      'pg.seed1': 'The plugin system just shipped 🎉',
      'pg.seed2': 'Just installed the weather plugin, works great.',
      'pg.seed3': 'Dark mode in one click, love it ✨',
      'pg.seedD1': 'Are you joining the weekend sync?',
      'pg.seedD2': 'Yes, I will. See you Saturday!',
      'langNext': '中文'
    },
    zh: {
      'site.title': 'ModernChat — 插件优先的网页聊天平台',
      'site.desc': '开箱只有账号与纯文本聊天，图片、表情、主题、机器人、小游戏全部由可插拔的 JS 插件实现。前后端分离，Go + Vue 3，开源自托管。',
      'nav.demo': '在线演示',
      'nav.features': '功能特性', 'nav.plugins': '插件系统', 'nav.quickstart': '快速开始', 'nav.docs': '文档',
      'hero.badge-open': '开源自托管',
      'hero.h1': '插件优先的<br>网页聊天平台',
      'hero.sub': '开箱只有最朴素的账号 + 纯文本聊天；图片、表情、Markdown、暗色主题、机器人、小游戏——一切增强能力都来自可插拔的 JS 插件，卸掉即回到纯文本。',
      'hero.cta-start': '快速开始', 'hero.cta-docs': '阅读文档',
      'hero.p1': '沙箱插件运行时：不碰 DOM、拿不到 JWT，权限清单确认后才启用',
      'hero.p2': '单实例支持 1 万+ WebSocket 在线，消息端到端 P99 < 100ms',
      'hero.p3': 'Docker Compose 一键起，一条命令从 Gitee 升级',
      'demo.title': '产品交流群', 'demo.subtitle': '12 人 · 3 人在线',
      'demo.composer': '输入消息…', 'demo.send': '发送',
      'demo.caption': '▶ 演示：纯文本消息 · 插件自定义消息卡片 · 插件一键切换暗色主题',
      'features.title': '功能特性',
      'features.sub': '内核尽量薄，一切增强皆插件。以下为开箱即用的内核能力。',
      'f1.t': '安全账号体系',
      'f1.d': '注册 / 登录 / 找回密码，bcrypt 散列，JWT 带 token_version 支持主动失效，封禁强制全端下线，自定义头像。',
      'f2.t': '实时消息管道',
      'f2.d': 'WebSocket 心跳 + 指数退避重连，已读回执、2 分钟撤回、"对方正在输入"、在线状态，断线重连自动补齐数据。',
      'f3.t': '单聊群聊同构',
      'f3.d': '统一会话模型：单聊幂等复用，群聊建 / 搜 / 加 / 退、踢人与群主转让，会话列表未读红点与分组筛选。',
      'f4.t': '插件系统',
      'f4.d': '沙箱 iframe 运行、描述式 UI 白名单渲染、权限清单确认、同名上传即升级，站长可全局下架任意插件。',
      'f5.t': '站长后台',
      'f5.d': '用户封禁 / 解封 / 随机重置密码，插件按名称聚合视图与全局下架，构建版本信息一目了然。',
      'f6.t': '运维友好',
      'f6.d': '结构化日志按天轮转，三档限流（Redis 可选共享配额），健康检查与版本接口，一条命令从 Gitee 升级。',
      'plugin.title': '插件系统：写一个 JS 文件，扩展整个网站',
      'plugin.sub': '每个插件是一个 ≤1MB 的 JS 文件，运行在 sandbox iframe 中，通过 postMessage 与主站通信，UI 一律以描述式 JSON 交由白名单组件渲染。',
      'plugin.security-t': '安全模型',
      'plugin.s1': '插件不接触 DOM、拿不到 JWT、读不到主站存储',
      'plugin.s2': '安装时展示权限清单，用户确认后才启用',
      'plugin.s3': '网络请求经主站桥接，仅放行 allowedHosts 白名单域名',
      'plugin.s4': 'URL 安装有 SSRF 防护：拒绝私网 / 回环 / 非 http(s)',
      'plugin.ui-t': '描述式 UI：8 个白名单组件',
      'plugin.ui-note': '白名单外的组件与 javascript: 链接一律拒绝渲染——插件拿不到任意 HTML 注入能力。',
      'sdk.api': 'API', 'sdk.role': '作用', 'sdk.perm': '所需权限',
      'sdk.r1': '订阅 message:new / recall / presence / conv:open / ui:action',
      'sdk.r2': '发消息，对象自动 JSON 序列化',
      'sdk.r3': '自定义消息类型 → UI 描述',
      'sdk.r4': '提交侧边栏面板 UI 描述',
      'sdk.r5': '每插件独立命名空间存储（单值 ≤64KB）',
      'sdk.r6': '主站代为 fetch，仅放行 allowedHosts 域名',
      'sdk.r7': '修改 TDesign 主题令牌，全站即时换肤',
      'qs.title': '快速开始',
      'qs.sub': '两个终端、四条命令，跑起一个完整的 ModernChat。首个注册的账号自动成为站长。',
      'qs.docker': 'Docker 一键（推荐）', 'qs.manual': '手动运行', 'qs.verify': '验证',
      'qs.stack': '技术栈',
      'footer.line': '前后端分离、插件优先的开源网页聊天平台。',
      'docs.title': '文档 — ModernChat',
      'docs.zhNote': '文档与代码同仓库维护、保持同步；英文译本位于 assets/docs/en/。',
      'docs.loading': '加载中…',
      'docs.notfound': '文档不存在。',
      'docs.prev': '← 上一篇', 'docs.next': '下一篇 →',
      'docs.zhOriginal': '本文档暂无英文版，当前展示中文原文。',
      'demo.m1': '插件系统上线了 🎉',
      'demo.m2': '先装个天气插件试试。',
      'demo.via': '天气插件',
      'demo.city': '杭州', 'demo.sky': '晴', 'demo.refresh': '刷新',
      'demo.m3': '主题插件也装了——一键换暗色 ✨',
      'demo.names': ['小明', '阿凯', '阿凯', '小柔'],
      'pg.title': '在线演示',
      'pg.sub': '不用注册、不用部署——下面是一个可交互的迷你 ModernChat：切换会话、发消息、收到自动回复与已读回执、打开插件侧栏面板、一键切换明暗主题。',
      'pg.dark': '暗色', 'pg.light': '亮色', 'pg.reset': '重置',
      'pg.note': '纯前端模拟 · 数据不离开浏览器',
      'pg.input': '输入消息，回车发送…',
      'pg.send': '发送', 'pg.read': '已读', 'pg.me': '我', 'pg.typing': '对方正在输入…',
      'pg.groupName': '产品交流群', 'pg.groupSub': '12 人',
      'pg.directName': '小柔', 'pg.directSub': '在线',
      'pg.pluginTitle': '天气',
      'pg.pluginBy': '由天气插件提供 · 描述式 UI 渲染',
      'pg.names': ['小明', '阿凯', '小柔'],
      'pg.bot': [
        '收到！插件系统确实好用 🎉',
        '要不要装个天气插件试试？',
        '暗色主题看着舒服多了～',
        '这个演示是纯前端模拟的哦',
        '没错，一期就做了已读回执',
        '所有帧都走和真实应用一样的管道'
      ],
      'pg.seed1': '插件系统上线了 🎉',
      'pg.seed2': '刚装了天气插件，挺好用。',
      'pg.seed3': '一键暗色，爱了 ✨',
      'pg.seedD1': '周末同步会你来吗？',
      'pg.seedD2': '来的，周六见！',
      'langNext': 'EN'
    }
  };

  var LANG_KEY = 'mc_site_lang';
  var lang = 'zh';
  try {
    var saved = localStorage.getItem(LANG_KEY);
    if (saved === 'zh' || saved === 'en') lang = saved;
    else lang = /^zh/i.test(navigator.language || '') ? 'zh' : 'en';
  } catch (e) { /* ignore */ }

  function t(key) { return (I18N[lang] && I18N[lang][key]) || I18N.zh[key] || key; }

  function applyLang() {
    document.documentElement.lang = lang === 'zh' ? 'zh-CN' : 'en';
    document.title = t('site.title');
    var nodes = document.querySelectorAll('[data-i18n]');
    for (var i = 0; i < nodes.length; i++) nodes[i].innerHTML = t(nodes[i].getAttribute('data-i18n'));
    var meta = document.querySelector('meta[name="description"]');
    if (meta) meta.setAttribute('content', t('site.desc'));
    var btn = document.getElementById('langToggle');
    if (btn) btn.textContent = t('langNext');
  }

  function setLang(next) {
    lang = next;
    try { localStorage.setItem(LANG_KEY, lang); } catch (e) { /* ignore */ }
    applyLang();
    if (window.__mcDocsRerender) window.__mcDocsRerender();
    if (window.__mcDemoRerender) window.__mcDemoRerender();
    if (window.__mcPgRerender) window.__mcPgRerender();
  }

  var toggle = document.getElementById('langToggle');
  if (toggle) toggle.addEventListener('click', function () { setLang(lang === 'zh' ? 'en' : 'zh'); });

  /* ---------------- 快速开始 Tabs ---------------- */
  var tabs = document.querySelectorAll('.qs-tab');
  if (tabs.length) {
    tabs.forEach(function (tab) {
      tab.addEventListener('click', function () {
        document.querySelectorAll('.qs-tab').forEach(function (x) { x.classList.remove('is-active'); });
        document.querySelectorAll('.qs-panel').forEach(function (x) { x.classList.remove('is-active'); });
        tab.classList.add('is-active');
        var panel = document.querySelector('.qs-panel[data-panel="' + tab.dataset.tab + '"]');
        if (panel) panel.classList.add('is-active');
      });
    });
  }

  /* ---------------- 交互式聊天演示 ---------------- */
  var demoBody = document.getElementById('demoBody');
  var demoChat = document.getElementById('demoChat');
  var demoTimers = [];

  function el(tag, cls, html) {
    var node = document.createElement(tag);
    if (cls) node.className = cls;
    if (html !== undefined) node.innerHTML = html;
    return node;
  }

  function avatarNode(name, hue) {
    var av = el('span', 'demo-msg-avatar', name.charAt(0));
    av.style.background = 'hsl(' + hue + ', 62%, 52%)';
    return av;
  }

  function addMsg(name, hue, html, isMe) {
    var msg = el('div', 'demo-msg' + (isMe ? ' is-me' : ''));
    msg.appendChild(avatarNode(name, hue));
    msg.appendChild(el('div', 'demo-bubble', html));
    demoBody.appendChild(msg);
    demoBody.scrollTop = demoBody.scrollHeight;
    return msg;
  }

  function addTyping(hue) {
    var wrap = el('div', 'demo-typing');
    var av = el('span', 'demo-msg-avatar');
    av.style.background = 'hsl(' + hue + ', 62%, 52%)';
    var bub = el('div', 'demo-typing-bubble');
    bub.appendChild(el('i')); bub.appendChild(el('i')); bub.appendChild(el('i'));
    wrap.appendChild(av); wrap.appendChild(bub);
    demoBody.appendChild(wrap);
    demoBody.scrollTop = demoBody.scrollHeight;
    return wrap;
  }

  function makeWeatherCard() {
    var temps = ['22', '21', '23', '20', '24'];
    var idx = 0;
    var card = el('div', 'demo-weather');
    card.innerHTML =
      '<div class="demo-weather-top">' +
      '  <span class="demo-weather-icon">🌤️</span>' +
      '  <span><span class="demo-weather-city" data-w="city"></span><br>' +
      '  <span class="demo-weather-sky" data-w="sky"></span></span>' +
      '</div>' +
      '<div class="demo-weather-temp"><span data-w="temp">22</span>°C</div>' +
      '<button class="demo-weather-refresh" type="button" data-w="btn"></button>';
    function fill() {
      card.querySelector('[data-w="city"]').textContent = t('demo.city');
      card.querySelector('[data-w="sky"]').textContent = t('demo.sky');
      card.querySelector('[data-w="temp"]').textContent = temps[idx];
      card.querySelector('[data-w="btn"]').textContent = t('demo.refresh');
    }
    fill();
    card.querySelector('[data-w="btn"]').addEventListener('click', function () {
      idx = (idx + 1) % temps.length;
      card.querySelector('[data-w="temp"]').textContent = temps[idx];
    });
    return card;
  }

  function clearTimers() { demoTimers.forEach(clearTimeout); demoTimers = []; }

  function runDemo() {
    if (!demoBody) return;
    clearTimers();
    demoBody.innerHTML = '';
    demoChat.classList.remove('is-dark');

    var names = t('demo.names');
    var via = '<span class="demo-via">' + t('demo.via') + '</span>';

    demoTimers.push(setTimeout(function () {
      addMsg(names[0], 210, t('demo.m1'), false);
    }, 500));
    demoTimers.push(setTimeout(function () { demoBody.appendChild(addTyping(150)); }, 1600));
    demoTimers.push(setTimeout(function () {
      var typing = demoBody.querySelector('.demo-typing');
      if (typing) typing.remove();
      addMsg(names[2], 150, t('demo.m2'), false);
    }, 2800));
    demoTimers.push(setTimeout(function () {
      var msg = addMsg(names[2], 150, via, false);
      var bubble = msg.querySelector('.demo-bubble');
      bubble.style.padding = '6px';
      bubble.style.background = 'transparent';
      bubble.appendChild(makeWeatherCard());
      demoBody.scrollTop = demoBody.scrollHeight;
    }, 3900));
    demoTimers.push(setTimeout(function () { demoBody.appendChild(addTyping(330)); }, 5200));
    demoTimers.push(setTimeout(function () {
      var typing = demoBody.querySelector('.demo-typing');
      if (typing) typing.remove();
      addMsg(names[3], 330, t('demo.m3'), false);
    }, 6400));
    demoTimers.push(setTimeout(function () {
      demoChat.classList.add('is-dark'); // "主题插件" 生效：全窗口换暗色
    }, 7800));
  }

  window.__mcDemoRerender = function () {
    // 语言切换后重播演示，保证气泡文案同步
    if (demoBody) runDemo();
  };

  var replayBtn = document.getElementById('demoReplay');
  if (replayBtn) {
    replayBtn.addEventListener('click', function () {
      replayBtn.classList.remove('is-spinning');
      void replayBtn.offsetWidth; // 重新触发旋转动画
      replayBtn.classList.add('is-spinning');
      runDemo();
    });
  }

  // 进入视口后自动播放一次
  if (demoBody && 'IntersectionObserver' in window) {
    var played = false;
    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (entry) {
        if (entry.isIntersecting && !played) { played = true; runDemo(); io.disconnect(); }
      });
    }, { threshold: .35 });
    io.observe(document.getElementById('demoChat'));
  } else if (demoBody) {
    runDemo();
  }

  /* ---------------- 文档页 ---------------- */
  var DOCS = [
    { group: true, zh: '入门', en: 'Getting started' },
    { file: 'getting-started.md', zh: '项目总览（README）', en: 'Project overview', gzh: '入门', gen: 'Getting started', dzh: '定位、快速开始、插件开发与升级部署', den: 'Positioning, quick start, plugin dev, upgrades' },
    { file: 'changelog.md', zh: '更新日志', en: 'Changelog', gzh: '入门', gen: 'Getting started', dzh: '各版本新增、修复与性能变更记录', den: 'Features, fixes and perf changes per release' },
    { group: true, zh: '设计与架构', en: 'Design & architecture' },
    { file: 'design.md', zh: '设计文档', en: 'Design doc', gzh: '设计与架构', gen: 'Design & architecture', dzh: '产品定位、架构、数据库、WS 协议、插件系统', den: 'Product, architecture, DB, WS protocol, plugins' },
    { file: 'api-spec.md', zh: '后端需求规格', en: 'Backend API spec', gzh: '设计与架构', gen: 'Design & architecture', dzh: '接口契约、校验规则、错误码、验收标准', den: 'Contracts, validation, error codes, acceptance' },
    { file: 'frontend-guide.md', zh: '前端技术方案', en: 'Frontend guide', gzh: '设计与架构', gen: 'Design & architecture', dzh: '结构、状态管理、WS 链路、性能约定与踩坑', den: 'Structure, state, WS, performance notes & pitfalls' },
    { group: true, zh: '运维与子项目', en: 'Ops & subprojects' },
    { file: 'ops-guide.md', zh: '运维手册', en: 'Ops manual', gzh: '运维与子项目', gen: 'Ops & subprojects', dzh: '环境变量全表、部署、备份升级、排障 FAQ', den: 'Env vars, deploy, backup & upgrade, troubleshooting' },
    { file: 'server-readme.md', zh: '后端运行手册', en: 'Server runbook', gzh: '运维与子项目', gen: 'Ops & subprojects', dzh: '配置、迁移、测试、目录结构与覆盖率', den: 'Config, migrations, tests, structure & coverage' },
    { file: 'web-readme.md', zh: '前端结构速览', en: 'Web structure', gzh: '运维与子项目', gen: 'Ops & subprojects', dzh: '目录、命令、聊天界面与设计约定', den: 'Layout, commands, chat UI & design conventions' }
  ];

  // 仓库内相对链接 → 本站文档路由（按文件名映射）
  var LINK_MAP = {
    'README.md': 'getting-started.md', 'CHANGELOG.md': 'changelog.md',
    '设计文档.md': 'design.md', '后端需求规格.md': 'api-spec.md',
    '前端技术方案.md': 'frontend-guide.md', '运维手册.md': 'ops-guide.md',
    'getting-started.md': 'getting-started.md', 'changelog.md': 'changelog.md',
    'design.md': 'design.md', 'api-spec.md': 'api-spec.md',
    'frontend-guide.md': 'frontend-guide.md', 'ops-guide.md': 'ops-guide.md',
    'server-readme.md': 'server-readme.md', 'web-readme.md': 'web-readme.md'
  };

  var docsNav = document.getElementById('docsNav');
  var docsBody = document.getElementById('docsBody');
  var docsTitle = document.getElementById('docsTitle');
  var docsDesc = document.getElementById('docsDesc');
  var docsCrumb = document.getElementById('docsCrumb');
  var docsPrev = document.getElementById('docsPrev');
  var docsNext = document.getElementById('docsNext');

  function docItems() { return DOCS.filter(function (d) { return d.file; }); }

  function currentFile() {
    var m = (location.hash || '').match(/^#\/(.+\.md)$/);
    if (m && LINK_MAP[m[1]]) return LINK_MAP[m[1]];
    return null;
  }

  function buildNav(activeFile) {
    if (!docsNav) return;
    docsNav.innerHTML = '';
    DOCS.forEach(function (item) {
      if (item.group) {
        docsNav.appendChild(el('div', 'docs-group', lang === 'zh' ? item.zh : item.en));
        return;
      }
      var a = document.createElement('a');
      a.href = '#/' + item.file;
      a.textContent = lang === 'zh' ? item.zh : item.en;
      if (item.file === activeFile) a.classList.add('is-active');
      docsNav.appendChild(a);
    });
  }

  function loadDoc(file) {
    var item = docItems().filter(function (d) { return d.file === file; })[0];
    if (!item) return;
    buildNav(file);
    docsTitle.textContent = lang === 'zh' ? item.zh : item.en;
    docsDesc.textContent = lang === 'zh' ? item.dzh : item.den;
    docsCrumb.textContent = lang === 'zh' ? item.gzh : item.gen;
    var old = document.getElementById('docsLangNotice');
    if (old) old.remove();

    // 英文模式优先加载英文版文档；缺失时回退中文原文并提示
    var zhPath = 'assets/docs/' + file;
    var enPath = 'assets/docs/en/' + file;

    function render(text, isFallback) {
      text = text.replace(/^#\s+.+\r?\n/, ''); // 去掉文档自身的 H1（页面已有标题）
      docsBody.innerHTML = marked.parse(text);
      docsBody.querySelectorAll('a[href]').forEach(function (a) {
        var href = a.getAttribute('href') || '';
        if (/^https?:/i.test(href)) { a.target = '_blank'; a.rel = 'noopener'; return; }
        var base = href.split('/').pop().split('#')[0];
        if (LINK_MAP[base]) a.href = '#/' + LINK_MAP[base];
        else {
          a.href = 'javascript:void(0)';
          a.style.opacity = '.55';
          a.title = lang === 'zh' ? '该文件在仓库内，请前往 Gitee 查看' : 'Repository file — see Gitee';
        }
      });
      if (isFallback) {
        var notice = el('p', 'docs-desc');
        notice.id = 'docsLangNotice';
        notice.style.color = 'var(--ink-3)';
        notice.style.fontSize = '13px';
        notice.textContent = t('docs.zhOriginal');
        docsDesc.insertAdjacentElement('afterend', notice);
      }
      docsBody.scrollTop = 0;
      window.scrollTo({ top: 0 });
      setPager(file);
    }

    function fetchText(path) {
      return fetch(path).then(function (res) {
        if (!res.ok) throw new Error('HTTP ' + res.status);
        return res.text();
      });
    }

    var promise;
    if (lang === 'en') {
      promise = fetchText(enPath).then(function (text) { return { text: text, fallback: false }; })
        .catch(function () {
          return fetchText(zhPath).then(function (text) { return { text: text, fallback: true }; });
        });
    } else {
      promise = fetchText(zhPath).then(function (text) { return { text: text, fallback: false }; });
    }

    promise.then(function (r) { render(r.text, r.fallback); })
      .catch(function () {
        docsBody.innerHTML = '<p class="docs-loading">' + t('docs.notfound') + '</p>';
      });
  }

  function setPager(file) {
    var items = docItems();
    var i = items.findIndex(function (d) { return d.file === file; });
    var prev = items[i - 1], next = items[i + 1];
    docsPrev.textContent = prev ? (t('docs.prev') + ' ' + (lang === 'zh' ? prev.zh : prev.en)) : '';
    docsPrev.href = prev ? '#/' + prev.file : '#';
    docsNext.textContent = next ? ((lang === 'zh' ? next.zh : next.en) + ' ' + t('docs.next')) : '';
    docsNext.href = next ? '#/' + next.file : '#';
  }

  function routeDocs() {
    if (!docsBody) return;
    var file = currentFile() || docItems()[0].file;
    if (!location.hash) history.replaceState(null, '', '#/' + file);
    loadDoc(file);
  }

  window.__mcDocsRerender = function () {
    if (!docsBody) return;
    var file = currentFile() || docItems()[0].file;
    buildNav(file);
    loadDoc(file);
  };

  window.addEventListener('hashchange', routeDocs);

  /* ---------------- 在线演示（Playground：可交互迷你聊天） ---------------- */
  var pgAppEl = document.getElementById('pgApp');
  var pgListEl = document.getElementById('pgList');
  var pgMsgsEl = document.getElementById('pgMsgs');
  var pgChatName = document.getElementById('pgChatName');
  var pgChatSub = document.getElementById('pgChatSub');
  var pgText = document.getElementById('pgText');
  var pgPanelEl = document.getElementById('pgPanel');
  var pgState = null;
  var pgTimers = [];

  function pgSeed() {
    var names = t('pg.names');
    return {
      open: 0,
      unread: [0, 1],
      botCount: 0,
      convs: [
        { name: t('pg.groupName'), sub: t('pg.groupSub'),
          msgs: [
            { me: false, name: names[0], hue: 210, text: t('pg.seed1') },
            { me: false, name: names[1], hue: 150, text: t('pg.seed2') },
            { me: false, name: names[2], hue: 330, text: t('pg.seed3') }
          ] },
        { name: t('pg.directName'), sub: t('pg.directSub'),
          msgs: [
            { me: false, name: t('pg.directName'), hue: 330, text: t('pg.seedD1') },
            { me: true, text: t('pg.seedD2') }
          ] }
      ]
    };
  }

  function pgMsgNode(m, showRead) {
    var row = el('div', 'pg-msg' + (m.me ? ' is-me' : ''));
    var av = el('span', 'pg-msg-avatar' + (m.me ? ' is-me' : ''), m.me ? t('pg.me') : m.name.charAt(0));
    if (!m.me) av.style.background = 'hsl(' + m.hue + ', 58%, 48%)';
    var col = el('div', 'pg-msg-col');
    if (!m.me) col.appendChild(el('span', 'pg-msg-name', m.name));
    col.appendChild(el('div', 'pg-msg-bubble', m.text));
    if (m.me && showRead) col.appendChild(el('span', 'pg-msg-read', t('pg.read')));
    row.appendChild(av); row.appendChild(col);
    return row;
  }

  function pgRenderMsgs() {
    pgMsgsEl.innerHTML = '';
    var msgs = pgState.convs[pgState.open].msgs;
    var lastRead = -1;
    for (var i = msgs.length - 1; i >= 0; i--) {
      if (msgs[i].me && msgs[i].read) { lastRead = i; break; }
    }
    msgs.forEach(function (m, i) { pgMsgsEl.appendChild(pgMsgNode(m, i === lastRead)); });
    pgMsgsEl.scrollTop = pgMsgsEl.scrollHeight;
  }

  function pgRenderList() {
    pgListEl.innerHTML = '';
    pgState.convs.forEach(function (c, i) {
      var item = el('div', 'pg-conv' + (i === pgState.open ? ' is-open' : ''));
      var av = el('span', 'pg-conv-avatar', c.name.charAt(0));
      av.style.background = i === 0
        ? 'linear-gradient(135deg, #2b6fff, var(--brand))'
        : 'hsl(330, 55%, 52%)';
      var meta = el('div', 'pg-conv-meta');
      meta.appendChild(el('strong', null, c.name));
      meta.appendChild(el('span', 'pg-conv-sub', c.sub));
      item.appendChild(av); item.appendChild(meta);
      if (pgState.unread[i] > 0) item.appendChild(el('span', 'pg-badge', String(pgState.unread[i])));
      item.addEventListener('click', function () { pgOpenConv(i); });
      pgListEl.appendChild(item);
    });
  }

  function pgRenderHead() {
    var c = pgState.convs[pgState.open];
    pgChatName.textContent = c.name;
    pgChatSub.textContent = c.sub;
  }

  function pgRenderAll() { pgRenderList(); pgRenderHead(); pgRenderMsgs(); }

  function pgOpenConv(i) {
    pgState.open = i;
    pgState.unread[i] = 0;
    pgRenderAll();
  }

  function pgSendMsg() {
    if (!pgState) return;
    var text = pgText.value.trim();
    if (!text) return;
    pgText.value = '';
    pgTimers.forEach(clearTimeout); pgTimers = [];
    var c = pgState.convs[pgState.open];
    c.msgs.push({ me: true, text: text });
    pgRenderMsgs();

    // "对方正在输入…"
    var typing = el('div', 'pg-typing');
    var bub = el('div', 'pg-typing-bubble');
    bub.appendChild(el('i')); bub.appendChild(el('i')); bub.appendChild(el('i'));
    typing.appendChild(bub);
    typing.appendChild(el('span', 'pg-typing-label', t('pg.typing')));
    pgMsgsEl.appendChild(typing);
    pgMsgsEl.scrollTop = pgMsgsEl.scrollHeight;

    pgTimers.push(setTimeout(function () {
      var pool = t('pg.bot');
      var i = pgState.botCount++ % pool.length;
      var names = t('pg.names');
      var hues = [210, 150, 330];
      var sender = pgState.open === 0
        ? { name: names[i % names.length], hue: hues[i % hues.length] }
        : { name: t('pg.directName'), hue: 330 };
      c.msgs.push({ me: false, name: sender.name, hue: sender.hue, text: pool[i] });
      for (var k = 0; k < c.msgs.length; k++) { if (c.msgs[k].me) c.msgs[k].read = true; }
      pgRenderMsgs();
    }, 1500));
  }

  function pgSetPanel(open) {
    pgPanelEl.hidden = !open;
    pgAppEl.classList.toggle('has-panel', open);
    var btn = document.getElementById('pgBtnPlugins');
    if (btn) btn.classList.toggle('is-active', open);
  }

  function pgApplyThemeBtn() {
    var dark = pgAppEl.classList.contains('is-dark');
    var btn = document.getElementById('pgTheme');
    if (btn) btn.innerHTML = (dark ? '☀️ ' : '🌙 ') + t(dark ? 'pg.light' : 'pg.dark');
  }

  function pgSetPlaceholder() { pgText.placeholder = t('pg.input'); }

  if (pgAppEl) {
    pgState = pgSeed();
    pgRenderAll();
    pgSetPlaceholder();
    document.getElementById('pgMe').textContent = t('pg.me');

    document.getElementById('pgSendBtn').addEventListener('click', pgSendMsg);
    pgText.addEventListener('keydown', function (e) { if (e.key === 'Enter') pgSendMsg(); });

    document.getElementById('pgBtnPlugins').addEventListener('click', function () {
      pgSetPanel(!pgAppEl.classList.contains('has-panel'));
    });
    document.getElementById('pgPanelClose').addEventListener('click', function () { pgSetPanel(false); });
    document.getElementById('pgPanelBody').appendChild(makeWeatherCard());

    document.getElementById('pgTheme').addEventListener('click', function () {
      pgAppEl.classList.toggle('is-dark');
      pgApplyThemeBtn();
    });
    document.getElementById('pgReset').addEventListener('click', function () {
      pgTimers.forEach(clearTimeout); pgTimers = [];
      pgState = pgSeed();
      pgRenderAll();
      pgSetPlaceholder();
    });
  }

  window.__mcPgRerender = function () {
    if (!pgAppEl) return;
    pgState = pgSeed();
    pgRenderAll();
    pgSetPlaceholder();
    pgApplyThemeBtn();
    var meEl = document.getElementById('pgMe');
    if (meEl) meEl.textContent = t('pg.me');
  };

  /* ---------------- 启动 ---------------- */
  applyLang();
  routeDocs();
})();
