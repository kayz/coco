# lingti-bot vs OpenClaw

> lingti-bot 与 OpenClaw 的设计理念、技术特性、集成方式全面对比

## 概览

| 指标 | **lingti-bot** | **OpenClaw** |
|------|---------------|--------------|
| 语言 | Go | TypeScript |
| 安装大小 | ~15MB 单文件 | 100MB+（含 node_modules）|
| 分发方式 | 单一可执行文件，复制即用 | npm 安装，依赖 Node.js |
| 消息平台 | 7 个（含国内平台） | 30+（海外为主）|
| 工具数 | 70+ MCP 工具 | 54 技能 + 工具 |
| 设计哲学 | 极简主义，够用就好 | 功能全面，灵活优先 |
| 目标用户 | 所有人（包括非技术用户）| 开发者、技术爱好者 |

## 核心差异

### 设计理念

| | **lingti-bot** | **OpenClaw** |
|---|---|---|
| **代码方向** | 克制增长，保持精简 | 越来越多，越来越复杂 |
| **接入方式** | 云中继 + 自建双模式 | 主要依赖自建服务器 |
| **入门门槛** | 3 条命令即可完成 | 需要服务器运维知识 |
| **国内平台** | 原生支持飞书/企微/钉钉/公众号 | 不原生支持 |
| **运行依赖** | 无（单一二进制）| 需要 Node.js 运行时 |
| **嵌入式设备** | 可部署到 ARM/MIPS | 需要 Node.js 环境 |

### 功能矩阵

| 功能 | **lingti-bot** | **OpenClaw** |
|------|:---:|:---:|
| CLI 工具 | ✅ | ✅ |
| MCP Server | ✅ | ✅ |
| 云中继（免服务器接入）| ✅ | ❌ |
| 单一二进制部署 | ✅ | ❌ |
| 浏览器自动化 | ✅ | ✅ |
| Web UI | ❌ | ✅ |
| macOS/iOS/Android 应用 | ❌ | ✅ |
| 技能/插件系统 | ❌ | ✅ |
| 持久化记忆 (RAG) | ❌ | ✅ |
| Terminal UI (TUI) | ❌ | ✅ |
| Hooks 系统 | ❌ | ✅ |

## 消息平台支持

### lingti-bot 支持的平台（7 个）

| 平台 | 协议 | 需要公网服务器 |
|------|------|:-:|
| 飞书/Lark | WebSocket | ❌ |
| 企业微信 | 回调 API / 云中继 | ❌ |
| 微信公众号 | 云中继 | ❌ |
| 钉钉 | Stream Mode | ❌ |
| Slack | Socket Mode | ❌ |
| Telegram | Bot API | ❌ |
| Discord | Gateway | ❌ |

### OpenClaw 额外支持的平台（24+）

iMessage、Signal、WhatsApp、LINE、Matrix、Microsoft Teams、Google Chat、Mattermost、Nextcloud Talk、Twitch、NOSTR、Zalo 等。

## 企业微信接入对比

这是两个项目差异最明显的场景：

| 步骤 | **OpenClaw** | **lingti-bot** |
|------|------------|--------------|
| 公网服务器 | 需要购买和配置 | 不需要（云中继）|
| 域名 / DNS | 需要 | 不需要 |
| SSL 证书 | 推荐配置 | 不需要 |
| 回调 URL 验证 | 手动编写验证逻辑 | `relay` 命令自动完成 |
| 消息加解密 | 自行集成 SDK | 内置，零配置 |
| Access Token 管理 | 自行实现刷新逻辑 | 内置自动刷新 |
| **预计耗时** | **数小时到数天** | **5 分钟** |

**lingti-bot 云中继流程：**

```bash
# 1. 安装
curl -fsSL https://cli.lingti.com/install.sh | bash -s -- --bot

# 2. 一条命令搞定验证和消息
lingti-bot relay --platform wecom \
  --wecom-corp-id ... --wecom-token ... --wecom-aes-key ... \
  --provider deepseek --api-key sk-xxx

# 3. 配置回调 URL: https://bot.lingti.com/wecom
```

### 消息流转对比

**传统方式（OpenClaw 等）：**

```
企业微信 → 公网服务器（用户准备）→ AI API → 响应
```

**lingti-bot 云中继：**

```
企业微信 → bot.lingti.com → WebSocket → 本地客户端（用户本地）→ AI API
```

### 凭据安全对比

| | 传统方式 | lingti-bot 云中继 |
|---|---|---|
| AI API Key | 存放在服务器 | 存放在本地 |
| 企业微信凭据 | 持久化在服务器 | 动态传输，不持久化 |
| 消息内容 | 服务器端处理 | 本地处理，云端仅转发 |

## MCP 工具对比

两个项目共享核心工具集：

| 分类 | 工具 |
|------|------|
| 文件操作 | file_read, file_write, file_list, file_search, file_info 等 |
| Shell | shell_execute, shell_which |
| 系统 | system_info, disk_usage, env_get, env_list |
| 进程 | process_list, process_info, process_kill |
| 网络 | network_interfaces, network_connections, network_ping, network_dns_lookup |
| 日历 (macOS) | calendar_today, calendar_list_events, calendar_create_event 等 |
| 提醒/备忘录 (macOS) | reminders_*, notes_* |
| 音乐 (macOS) | music_play, music_pause, music_next 等 |
| GitHub | github_pr_list, github_issue_list 等 |

### lingti-bot 独有

| 功能 | 说明 |
|------|------|
| 浏览器自动化（纯 Go） | 基于 go-rod，快照-操作模式 |
| 文件管理工具 | file_list_old, file_delete_old, file_trash |

### OpenClaw 独有

| 功能 | 说明 |
|------|------|
| 技能系统 (54 个) | Bear Notes, Notion, Obsidian, Trello, 1Password 等 |
| 媒体处理 | 图像/视频/音频处理，视觉模型集成 |
| 持久化记忆 | 向量数据库 (LanceDB)，跨会话知识 |
| 自动回复/定时任务 | Cron 调度，Heartbeat 主动唤醒 |
| 设备配对 | 多设备同步 |

## OpenClaw 架构参考

OpenClaw 是由 Peter Steinberger 于 2025 年底创建的开源自主 AI 个人助手，曾在一周内获得 100k+ GitHub stars。

```
┌────────────────────────────────────────────┐
│           OpenClaw Runtime (Node.js)        │
├────────────────────────────────────────────┤
│  Message Router  │  Agentic Loop  │ Memory │
└────────┬─────────┴───────┬────────┴───┬────┘
         │                 │            │
    Chat Platforms    AI Models    Local Machine
    (30+ 平台)       (Claude/GPT)  (文件/应用)
```

**三大支柱：**
1. **跨会话持久记忆** — 记住上下文，学习使用模式
2. **深度本地机器访问** — 无限制访问本地应用和文件
3. **自主代理循环** — 主动执行任务，而非仅提供建议

## 适用场景

| 场景 | 推荐方案 |
|------|---------|
| 国内平台快速接入（飞书/企微/钉钉/微信）| **lingti-bot** |
| 无运维经验、快速体验 AI Bot | **lingti-bot** |
| 嵌入式/IoT 设备部署 | **lingti-bot** |
| 数据完全本地化要求 | **lingti-bot** |
| 需要 WhatsApp/iMessage/Signal 等平台 | **OpenClaw** |
| 需要 Web UI / 原生应用 | **OpenClaw** |
| 需要持久化记忆和技能系统 | **OpenClaw** |
| 已有成熟服务器基础设施 | **OpenClaw** |

## 参考链接

- [OpenClaw 官网](https://openclaw.ai/)
- [OpenClaw 文档](https://docs.openclaw.ai/)
- [OpenClaw GitHub](https://github.com/openclaw/openclaw)
- [lingti-bot 开发路线图](roadmap.md)
