# coco 项目愿景

> 本文档记录 coco 项目的长期愿景、架构决策和实现路线图。
> 所有功能的实现状态、每阶段目标和架构设计均在此维护。
>
> 最后更新：2026-02-25

---

## 一、用户原始需求

以下是用户对理想系统的完整描述，作为所有设计决策的原始依据。

---

> **1. 双 Agent 架构**
>
> 我理想的工作中有两个 agent：
>
> - **Keeper**：部署在公网服务器，负责渠道对接（企业微信、飞书等 IM）、Cron/心跳管理、保证 24 小时在线。接入简单 LLM，提供本地 Whisper 语音理解，保持在线服务。
> - **coco**（cowork & copilot）：部署在个人机器，理解用户行为/性格，响应用户需求，能力类似 OpenClaw。

---

> **2. 多模型动态选择**
>
> 主模型支持多模态、响应快、成本低；根据任务难度动态选择模型；主模型可调用其他专业模型。

---

> **3. 扩展性**
>
> 倾向于让 coco 调用 claude code/codex 等开发工具组装独立应用，部署到公网服务器，由 cron 驱动或 coco 主动调用，cron 里需要标记外部应用。

---

> **4. 主动发起对话**
>
> cron 或事件驱动，coco 或 keeper 均可发起。其他 agent 应用发出的消息通过 keeper（cron 驱动）或 coco（coco 调用）传递给用户。keeper/coco 知道自己只是传话，不受 agent 内容影响。

---

> **5. 内置功能精简**
>
> 参考 OpenClaw 的 pi 核心，复杂任务由用户和 coco 讨论后，coco 监督 claude code 开发 agent 应用再加载执行。

---

> **6. 外部 skills 安全评估**
>
> coco 对外部 skills 进行安全评估，可调用带思维链的 LLM 模型辅助理解。

---

> **7. 渠道管理**
>
> 主要在 keeper 层管理；coco 在线时由 coco 回答，coco 不在线时 keeper 简单答复（keeper 保持"傻"和公式化）；初期只需企业微信（keeper 自实现云中继能力）。

---

> **8. LLM 分类管理**
>
> 接入多种 LLM，按能力（思维链、多模态、OCR）和费用（每百万 token 费用）分类，coco 知道这些信息并按需调用。

---

> **9. 长程记忆**
>
> coco 与用户越来越熟悉，支持长程记忆；可使用用户 Obsidian 文件夹作为知识来源；coco 可操作用户电脑（上网、发帖、发邮件、交流、完成工作）。

---

> **10. CI / 长期愿景**
>
> 维护长期愿景文档（所有工作和实现状态）、实现计划、每阶段上线 features、预留扩展能力；keeper 和 coco 编译为同一二进制文件（`coco.exe`），通过参数区分（默认=coco，`--keeper`=keeper）。

---

> **11. 交付形式**
>
> 单一 `coco.exe`，首次运行通过 `--onboard` 引导用户完成所有配置并生成配置文件。

---

## 二、项目定位

**coco**（cowork & copilot）是一个**个人 AI 助手系统**，核心理念：

- **自托管**：运行在用户自己的设备和服务器上，数据自控
- **双 Agent**：Keeper（公网，24 小时在线）+ coco（个人机器，主力 AI）
- **中国友好**：原生支持企业微信、飞书、钉钉、微信公众号，中文模型全覆盖
- **单一二进制**：Go 编译，`coco.exe`，无运行时依赖
- **渐进扩展**：内置功能精简，复杂能力通过外部 Agent 应用扩展

---

## 三、系统架构

### 3.1 双 Agent 架构

```
┌─────────────────────────────────────────────────────────┐
│                   公网服务器                              │
│                                                          │
│  ┌──────────────────────────────────────────────────┐   │
│  │                   Keeper                         │   │
│  │  coco.exe --keeper                               │   │
│  │                                                  │   │
│  │  • 企业微信 Webhook 接收                          │   │
│  │  • WebSocket 服务端（等待 coco 连接）              │   │
│  │  • Cron 调度（含外部 Agent 应用）                  │   │
│  │  • 简单 LLM（coco 离线时兜底答复）                 │   │
│  │  • Whisper 语音识别                               │   │
│  └──────────────────┬───────────────────────────────┘   │
│                     │ WebSocket 长连接                    │
└─────────────────────┼───────────────────────────────────┘
                      │
┌─────────────────────┼───────────────────────────────────┐
│                个人机器│                                  │
│                     ▼                                    │
│  ┌──────────────────────────────────────────────────┐   │
│  │                    coco                          │   │
│  │  coco.exe                                        │   │
│  │                                                  │   │
│  │  • 主力 AI（多模型路由）                           │   │
│  │  • 长程记忆（Obsidian + RAG）                     │   │
│  │  • 工具执行（文件/Shell/浏览器/邮件）              │   │
│  │  • 外部 Agent 应用调用                            │   │
│  │  • Skills 安全评估                               │   │
│  └──────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
```

### 3.2 消息流

```
用户（企业微信）
    │
    ▼
Keeper（接收 Webhook）
    │
    ├── coco 在线？ ──是──► 转发给 coco ──► coco AI 处理 ──► 回复
    │
    └── coco 离线？ ──────► Keeper 简单 LLM 兜底答复

外部 Agent 应用
    │
    ├── Cron 驱动��Keeper 调度）──► 结果转发给用户（传话模式）
    │
    └── coco 主动调用 ──────────► 结果转发给用户（传话模式）
```

### 3.3 传话模式

Keeper 和 coco 知道自己只是"传话人"：
- 外部 Agent 消息带 `source: "external-agent"` 标记
- 系统提示明确指令：收到此标记时直接转发，不生成自己的回复
- 防止外部内容影响 Keeper/coco 的行为（防 prompt injection）

### 3.4 单一二进制

```
coco.exe              # 默认：运行 coco 模式（个人机器）
coco.exe --keeper     # 运行 Keeper 模式（公网服务器）
coco.exe --onboard    # 首次运行引导配置
```

---

## 四、核心能力规划

### 4.1 多模型路由

```yaml
# 配置示例
ai:
  providers:
    - name: claude-sonnet
      type: anthropic
      capabilities: [multimodal, thinking, fast]
      cost_per_1m_input: 3.0
      cost_per_1m_output: 15.0
    - name: deepseek-r1
      type: deepseek
      capabilities: [thinking]
      cost_per_1m_input: 0.55
      cost_per_1m_output: 2.19
    - name: qwen-vl
      type: qwen
      capabilities: [multimodal, ocr]
      cost_per_1m_input: 0.8
      cost_per_1m_output: 0.8
```

coco 在系统提示中知晓所有模型的能力和费用，可通过工具调用动态切换。

### 4.2 外部 Agent 应用

```yaml
# Cron 配置示例
cron:
  - id: "daily-news"
    type: "external"              # 区别于内置 cron
    endpoint: "https://news-bot.example.com/run"
    schedule: "0 8 * * *"
    auth: "bearer ${NEWS_BOT_TOKEN}"
    result_channel: "wecom"
  - id: "market-monitor"
    type: "external"
    endpoint: "https://market-bot.example.com/check"
    schedule: "*/30 9-15 * * 1-5"
    result_channel: "wecom"
    relay_mode: true              # 传话模式，不经 AI 处理
```

### 4.3 长程记忆

- **工作记忆**：当前对话上下文（SQLite，最近 200 条）
- **情节记忆**：历史事件摘要（RAG，chromem-go）
- **语义记忆**：Obsidian vault 知识库（fsnotify 监视，自动索引）

```yaml
memory:
  obsidian_vault: "~/Documents/Obsidian/MyVault"
  auto_index: true
  index_patterns: ["*.md", "!.obsidian/**"]
```

### 4.4 Skills 安全评估

```
coco skill install <name>
    │
    ▼
读取 skill 定义（shell/http/mcp 动作）
    │
    ▼
调用带思维链的 LLM 分析（claude-3-5-sonnet extended thinking）
    │
    ▼
输出：safe ✅ / warning ⚠️ / dangerous ❌ + 原因说明
    │
    ▼
用户确认后安装
```

---

## 五、实现状态

### 现有能力（✅ 已实现）

| 功能 | 状态 | 说明 |
|------|------|------|
| 多 AI 提供商 | ✅ | Claude/DeepSeek/Kimi/Qwen/OpenAI 及 15+ 兼容提供商 |
| Telegram | ✅ | 完整实现 |
| Discord | ✅ | 完整实现 |
| Slack | ✅ | 完整实现 |
| 飞书/Lark | ✅ | 完整实现 |
| 钉钉 | ✅ | 完整实现 |
| 企业微信（WeCom） | ✅ | 完整实现 |
| 微信公众号（云中继） | ✅ | 通过 keeper.kayz.com 中继 |
| MCP Server 内置 | ✅ | stdio/SSE 双模式 |
| 文件系统工具 | ✅ | read/write/list/search/delete |
| Shell 执行 | ✅ | 基础 exec |
| 系统信息工具 | ✅ | system_info/disk/env/process |
| 网络工具 | ✅ | ping/dns/interfaces/connections |
| 浏览器自动化 | ✅ | go-rod（CDP） |
| 日历工具（macOS） | ✅ | AppleScript |
| 截图/通知/剪贴板 | ✅ | macOS 系统工具 |
| 语音 STT | ✅ | Whisper（ggml-base.bin 内置） |
| 语音 TTS | ✅ | ElevenLabs/系统TTS/OpenAI |
| Cron 调度 | ✅ | robfig/cron，秒级精度 |
| RAG 长程记忆 | ✅ | chromem-go，向量搜索 |
| 用户偏好自动学习 | ✅ | 从对话中提取并记忆 |
| 日报生成 | ✅ | 每日对话/工作报告 |
| SQLite 持久化 | ✅ | WAL 模式 |
| WebSocket Gateway | ✅ | :18789 端口 |
| 云中继客户端 | ✅ | 连接 keeper.kayz.com |
| Skills 系统 | ✅ | JSON 定义，8 个内置 skills |
| `--onboard` 向导 | ✅ | 基础框架（1240行） |
| 单一二进制 | ✅ | Go 编译 |

### 待实现（🚧 规划中）

#### Phase 0：双 Agent 架构骨架

| 功能 | 优先级 | 说明 |
|------|--------|------|
| `--keeper` 模式入口 | 🔴 高 | cmd/ 入口拆分 |
| Keeper WebSocket 服务端 | 🔴 高 | 接受 coco 连接，替代 keeper.kayz.com |
| 企业微信 Webhook 自托管 | 🔴 高 | Keeper 自建，不依赖第三方 |
| coco 连接 Keeper | 🔴 高 | 替代连接 keeper.kayz.com |
| 消息透传链路 | 🔴 高 | 企业微信 → Keeper → coco → AI → 回复 |

#### Phase 1：多模型路由

| 功能 | 优先级 | 说明 |
|------|--------|------|
| 模型能力配置（capabilities/cost） | 🔴 高 | YAML 新增字段 |
| ModelRouter 实现 | 🔴 高 | 按任务类型选择模型 |
| AI 工具调用切换模型 | 🟡 中 | 系统提示注入模型清单 |
| 模型 failover | 🔴 高 | 多 profile 轮换 + 冷却 |

#### Phase 2：外部 Agent 应用

| 功能 | 优先级 | 说明 |
|------|--------|------|
| Cron `type: external` | 🔴 高 | 外部应用调度 |
| 外部应用调用协议 | 🔴 高 | HTTP POST + bearer token |
| 传话模式（source 标记） | 🔴 高 | 防 prompt injection |
| `spawn_agent` 工具 | 🟡 中 | coco 监督 claude code 开发 |

#### Phase 3：长程记忆增强

| 功能 | 优先级 | 说明 |
|------|--------|------|
| Obsidian vault 文件监视 | 🟡 中 | fsnotify + 自动索引 |
| 记忆分层（工作/情节/语义） | 🟡 中 | 三层记忆架构 |
| 时间衰减 | 🟡 中 | 旧记忆权重降低 |
| MMR 多样性排序 | 🟡 中 | 搜索结果多样性 |
| 上下文自动压缩 | 🔴 高 | 长对话必需 |

#### Phase 4：Skills 安全 + 扩展

| 功能 | 优先级 | 说明 |
|------|--------|------|
| Skills 安全评估（思维链 LLM） | 🟡 中 | 安装前自动分析 |
| `coco skill install/search` 命令 | 🟡 中 | CLI 技能管理 |
| 更多内置 skills | 🟢 低 | 扩充 8 → 20+ |

#### Phase 5：安全与完善

| 功能 | 优先级 | 说明 |
|------|--------|------|
| DM 配对安全机制 | 🔴 高 | 防陌生人滥用 |
| allowFrom 白名单 | 🔴 高 | 基础安全控制 |
| 群组 mention gating | 🔴 高 | 群组场景必需 |
| exec 审批流程 | 🟡 中 | 高危命令人工确认 |
| SSRF 防护 | 🟡 中 | web_fetch 安全 |
| 打字指示器 | 🟡 中 | 用户体验 |
| 配置热重载 | 🟡 中 | 无需重启 |

#### Phase 6：生态与部署

| 功能 | 优先级 | 说明 |
|------|--------|------|
| Web UI（基础 WebChat） | 🟢 低 | 浏览器访问 |
| Docker 支持 | 🟢 低 | 容器化部署 |
| 子 Agent 系统 | 🟢 低 | sessions_spawn |
| Agent 间通信 | 🟢 低 | sessions_send |
| Keeper 离线时兜底 LLM 完善 | 🟡 中 | 更自然的离线答复 |

---

## 六、架构决策记录（ADR）

### ADR-001：双 Agent 通信协议

**决策**：复用现有 relay WebSocket 协议，角色对调（Keeper 成为服务端）

**原因**：
- 避免重写协议层，现有 `internal/platforms/relay/` 代码可复用
- 消息格式已稳定，减少风险

**替代方案**：gRPC（更强类型）、HTTP 轮询（更简单）

---

### ADR-002：企业微信接入方式

**决策**：Keeper 自建 Webhook 接收端，不依赖 `keeper.kayz.com`

**原因**：
- 数据自控，不经过第三方服务器
- 减少外部依赖，提高稳定性
- 企业微信 Webhook 模式已在 `internal/platforms/wecom/` 实现

---

### ADR-003：模型路由策略

**决策**：配置驱动 + AI 自主通过工具调用切换模型

**原因**：
- 避免硬编码路由规则（规则会随模型演进过时）
- AI 比规则更能理解任务复杂度
- 配置文件描述能力和费用，AI 做决策

---

### ADR-004：外部 Agent 隔离

**决策**：`source: "external-agent"` 标记 + 系统提示指令实现传话模式

**原因**：
- 防止外部 agent 内容影响 Keeper/coco 行为（prompt injection 防护）
- 实现简单，不需要额外沙箱

---

### ADR-005：记忆系统扩展方向

**决策**：在现有 chromem-go 基础上扩展，加 Obsidian 文件监视，不引入新向量库

**原因**：
- 现有 chromem-go 嵌入式，无额外依赖
- 减少技术栈复杂度
- 可后续迁移到 sqlite-vec（如性能需要）

---

## 七、参考项目

| 项目 | 语言 | 参考价值 |
|------|------|----------|
| OpenClaw | TypeScript | 能力基准，架构参考（Gateway/Plugin/Skills） |
| coco（当前） | Go | 基础代码，中国平台支持 |

详细对比见：`docs/openclaw-feature-comparison.md`

---

*文档维护原则：每个 Phase 完成后更新实现状态；架构变更时同步更新 ADR。*
