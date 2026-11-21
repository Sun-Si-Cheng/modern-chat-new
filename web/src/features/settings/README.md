# settings 模块（设置）

个人偏好与站点设置：外观（主题/背景）、通知偏好、账号安全、字体与语言。

## 归属
- 视图：SettingsView
- 状态：stores/prefs
- 组件：QuickPrefs（悬浮入口，现居 components/common/）、AppBackgroundPicker

## 边界规则
1. 不 import `features/chat`、`features/contacts` 内部；被其他模块使用的
   入口组件（如 QuickPrefs）保持在共享层（components/common/）。
2. 外观令牌只写 `--td-*` / `--mc-*`，禁止页面散落硬编码颜色（见
   scripts/audit-tokens.mjs）。
