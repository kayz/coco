# lingti-bot 开发路线图

> 参考 [OpenClaw](https://github.com/openclaw/openclaw) 功能对照，规划 lingti-bot 后续开发方向。

## 功能对照表

| 功能分类 | OpenClaw | lingti-bot | 状态 |
|---------|----------|------------|------|
| **消息平台** | | | |
| Slack | ✅ | ✅ | 已实现 |
| Discord | ✅ | ✅ | 已实现 |
| Telegram | ✅ | ✅ | 已实现 |
| 飞书/Lark | ❌ | ✅ | 已实现 |
| 微信 | ❌ | ✅ | 已实现（云中继）|
| WhatsApp | ✅ | ❌ | 待开发 |
| iMessage | ✅ | ❌ | 待开发 |
| Signal | ✅ | ❌ | 待开发 |
| Microsoft Teams | ✅ | ❌ | 待开发 |
| Matrix | ✅ | ❌ | 待开发 |
| Google Chat | ✅ | ❌ | 待开发 |
| 钉钉 | ❌ | 🚧 | 开发中 |
| 企业微信 | ❌ | ✅ | 已实现 |
| **语音功能** | | | |
| 语音输入 (STT) | ✅ | ✅ | 已实现 |
| 语音输出 (TTS) | ✅ ElevenLabs | ✅ macOS say | 已实现 |
| Voice Wake (唤醒词) | ✅ | ❌ | 待开发 |
| PTT 悬浮窗 | ✅ | ❌ | 待开发 |
| **可视化交互** | | | |
| Live Canvas | ✅ | ❌ | 待开发 |
| 浏览器控制 | ✅ | ❌ | 待开发 |
| WebChat UI | ✅ | ❌ | 待开发 |
| **客户端应用** | | | |
| macOS 菜单栏应用 | ✅ | ❌ | 待开发 |
| iOS 应用 | ✅ | ❌ | 待开发 |
| Android 应用 | ✅ | ❌ | 待开发 |
| **自动化** | | | |
| 定时任务 (Cron) | ✅ | ❌ | 待开发 |
| Webhooks | ✅ | ❌ | 待开发 |
| Gmail 集成 | ✅ | ❌ | 待开发 |
| 主动唤醒 (Heartbeat) | ✅ | ❌ | 待开发 |
| **AI 功能** | | | |
| 多模型支持 | ✅ | ✅ | 已实现 |
| 模型 Failover | ✅ | ❌ | 待开发 |
| Extended Thinking | ✅ | ❌ | 待开发 |
| Agent 间通信 | ✅ | ❌ | 待开发 |
| 对话记忆 | ✅ | ✅ | 已实现 |
| **技能系统** | | | |
| 技能注册中心 | ✅ ClawHub | ❌ | 待开发 |
| 技能安装/管理 | ✅ | ❌ | 待开发 |
| 自定义技能 | ✅ | ❌ | 待开发 |
| **安全功能** | | | |
| DM 配对验证 | ✅ | ❌ | 待开发 |
| Docker 沙箱 | ✅ | ❌ | 待开发 |
| **媒体处理** | | | |
| 截图 | ✅ | ✅ | 已实现 |
| 屏幕录制 | ✅ | ❌ | 待开发 |
| 摄像头 | ✅ | ❌ | 待开发 |
| 位置服务 | ✅ | ❌ | 待开发 |
| **效率工具** | | | |
| Apple Notes | ✅ | ✅ | 已实现 |
| Apple Reminders | ✅ | ✅ | 已实现 |
| Apple Calendar | ✅ | ✅ | 已实现 |
| Apple Music | ✅ | ✅ | 已实现 |
| GitHub | ✅ | ✅ | 已实现 |
| Things 3 | ✅ | ❌ | 待开发 |
| Notion | ✅ | ❌ | 待开发 |
| Obsidian | ✅ | ❌ | 待开发 |
| Trello | ✅ | ❌ | 待开发 |

---

## 待开发功能清单

### 高优先级

- [ ] **钉钉集成** - 国内企业用户需求
- [ ] **企业微信集成** - 国内企业用户需求
- [ ] **定时任务 (Cron)** - 支持定时执行任务
- [ ] **Webhooks** - 支持外部事件触发
- [ ] **模型 Failover** - 主模型失败时自动切换备用模型
- [ ] **WebChat UI** - 浏览器端聊天界面

### 中优先级

- [ ] **Voice Wake (唤醒词)** - "Hey Lingti" 语音唤醒
- [ ] **ElevenLabs TTS** - 更自然的语音合成
- [ ] **浏览器控制** - Playwright/Puppeteer 集成
- [ ] **macOS 菜单栏应用** - SwiftUI 原生应用
- [ ] **DM 配对验证** - 未知发送者需验证码配对
- [ ] **Extended Thinking** - 支持 Claude 深度思考模式

### 低优先级

- [ ] **WhatsApp 集成** - 海外用户需求
- [ ] **iMessage 集成** - 需 BlueBubbles/AppleScript
- [ ] **Signal 集成** - 隐私优先用户
- [ ] **Microsoft Teams** - 企业用户
- [ ] **Matrix 集成** - 开源社区
- [ ] **Google Chat** - G Suite 用户
- [ ] **Things 3 集成** - macOS 任务管理
- [ ] **Notion 集成** - 笔记/知识库
- [ ] **Obsidian 集成** - 本地 Markdown 笔记
- [ ] **Trello 集成** - 看板任务管理
- [ ] **屏幕录制** - 录制屏幕操作
- [ ] **摄像头集成** - 拍照/视频
- [ ] **位置服务** - GPS 定位
- [ ] **iOS 应用** - Swift/SwiftUI
- [ ] **Android 应用** - Kotlin
- [ ] **Live Canvas** - Agent 驱动的可视化画布
- [ ] **Docker 沙箱** - 隔离执行环境
- [ ] **Agent 间通信** - 多 Agent 协作
- [ ] **技能注册中心** - 类似 ClawHub 的技能市场
- [ ] **Gmail 集成** - 邮件触发工作流

---

## lingti-bot 独有功能

以下是 lingti-bot 相比 OpenClaw 的差异化功能：

| 功能 | 说明 |
|------|------|
| 飞书/Lark 原生支持 | OpenClaw 不支持 |
| 钉钉支持 (开发中) | OpenClaw 不支持 |
| 企业微信支持 (开发中) | OpenClaw 不支持 |
| 微信公众号接入 | 通过云中继实现 |
| 中文语音默认 | whisper `-l zh` |
| 云中继模式 | 无需公网 IP |
| 数据完全本地化 | 云端不存储任何数据 |

---

## 贡献指南

欢迎为以上功能提交 PR！请参考：

1. Fork 本仓库
2. 创建功能分支: `git checkout -b feature/xxx`
3. 提交代码: `git commit -m "feat: add xxx"`
4. 推送分支: `git push origin feature/xxx`
5. 创建 Pull Request

如有问题，请在 [Issues](https://github.com/ruilisi/lingti-bot/issues) 中讨论。
