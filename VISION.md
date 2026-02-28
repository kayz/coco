# coco 项目愿景

> 本文档记录 coco 项目的长期愿景、架构决策和实现路线图。
> 所有功能的实现状态、每阶段目标和架构设计均在此维护。
>
> 最后更新：2026-02-28

---

## 0. 实施方法（本轮新增）

为保证愿景持续落地，本项目统一采用以下执行闭环（每个 Phase 都执行）：

1. 先写测试（单元/集成/验收脚本），明确通过标准。
2. 按测试驱动开发实现功能，不跳过安全与可观测性。
3. 运行阶段测试 + 全量回归测试，必须通过后才进入文档阶段。
4. 更新文档（VISION + phase plan + runbook + 变更日志），保证“代码即状态”。

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
    ├── Cron 驱动（Keeper 调度）──► 结果转发给用户（传话模式）
    │
    └── coco 主动调用 ───────────► 结果转发给用户（传话模式）
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

### 4.1 多模型路由（Phase 1 目标方案）

**配置分层：**
- `providers.yaml`：仅本地，含 API URL + API Key（每个 provider 一个 key）
- `models.yaml`：可公开，描述模型能力/费用/速度/智力分档
- 运行期路由（应用级）在主配置中维护：每个应用都有独立的“可用模型列表”

**配置示例：**

```yaml
# providers.yaml（本地，不进 Git）
providers:
  - name: openai
    type: openai
    base_url: "https://api.openai.com/v1"
    api_key: "${OPENAI_API_KEY}"
  - name: openai-codex
    type: openai
    base_url: "https://api.openai.com/v1"
    api_key: "${OPENAI_CODEX_API_KEY}"

# models.yaml（可公开）
models:
  - name: gpt-4o
    code: gpt-4o
    provider: openai
    intellect: full        # 满分 / 优秀 / 良好 / 可用
    speed: fast            # 快 / 中 / 慢
    cost: high             # 贵 / 高 / 中 / 低 / 免费
    skills: [multimodal, thinking]
```

**路由原则：**
- 按应用维度维护可用模型列表（coco/keeper/agent/cron/心跳各自独立）
- failover 只在该应用列表内进行，优先同一智力等级，必要时再降档
- cron/心跳必须使用 `speed: fast` 的模型
- 大量 token 场景：先小模型拉取/规范，再交给高智力模型解读
- 特殊能力（文生图/视频等）由 agent 指定模型

coco 在系统提示中知晓可用模型清单，通过工具调用切换模型。

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

### 4.5 个人系统工作区契约（新增）

针对“长期个人使用”场景，coco 采用工作区 Markdown 契约作为上层产品接口，减少人格/偏好/运行规则散落在代码和配置里的问题：

- `AGENTS.md`：运行规范与工具调用约束（必需）
- `SOUL.md`：价值观、人格边界、行为准则（必需）
- `PROFILE.md`：用户画像与 agent 身份（可读写）
- `MEMORY.md`：长期沉淀记忆（可读写）
- `HEARTBEAT.md`：后台巡检与周期任务意图（可读写）
- `BOOTSTRAP.md`：首次人格引导流程（一次性）

配套能力要求：

- PromptBuild 支持读取上述工作区文件并组装系统提示。
- 运行时支持配置与安全策略热更新（无需重启）。
- 长对话支持自动上下文压缩（避免 token 衰减导致“失忆”）。

---

## 五、实现状态

### 现有能力（✅ 已实现）

| 功能 | 状态 | 说明 |
|------|------|------|
| 多 AI 提供商 | ✅ | Claude/DeepSeek/Kimi/Qwen/OpenAI 及 15+ 兼容提供商 |
| ModelRouter + failover | ✅ | 已实现模型切换、失败降级、冷却机制 |
| AI 模型工具调用 | ✅ | `ai.list_models` / `ai.switch_model` / `ai.get_current_model` |
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
| Markdown 记忆检索 | ✅ | Core files + Obsidian 检索、语义融合、MMR 重排 |
| 用户偏好自动学习 | ✅ | 从对话中提取并记忆 |
| 日报生成 | ✅ | 每日对话/工作报告 |
| SQLite 持久化 | ✅ | WAL 模式 |
| WebSocket Gateway | ✅ | :18789 端口 |
| 云中继客户端 | ✅ | 连接 keeper.kayz.com |
| Skills 系统 | ✅ | JSON 定义，8 个内置 skills |
| `--onboard` 向导 | ✅ | 基础框架（1240行） |
| 单一二进制 | ✅ | Go 编译 |
| Keeper 模式 | ✅ | v1.9.0 — 自建公网服务端，企业微信 Webhook + WebSocket 转发 |
| coco 连接自建 Keeper | ✅ | v1.9.0 — relay 模式指向自建 Keeper，跳过本地 WeCom 凭证 |
| 离线兜底 | ✅ | v1.9.0 — coco 离线时 Keeper 返回固定文本 |
| PromptBuild 模块 | ✅ | v1.9.0 — 无状态 Prompt 组装（SQLite + Markdown 模板） |
| Shell 安全策略配置贯通 | ✅ | `security.blocked_commands` / `security.require_confirmation` 已接入运行时执行链路 |
| 安全策略热更新 | ✅ | 消息处理前按配置文件 mtime 自动重载，无需重启 |

### Phase 0：双 Agent 架构骨架（✅ 已完成 — 2026-02-26，v1.9.0）

| 功能 | 状态 | 说明 |
|------|------|------|
| `coco keeper` 模式入口 | ✅ 完成 | `cmd/keeper.go`，540 行 |
| Keeper WebSocket 服务端 | ✅ 完成 | 接受 coco 连接，token 认证，ping/pong 保活 |
| 企业微信 Webhook 自托管 | ✅ 完成 | URL 验证 + 消息解密转发 |
| coco 连接自建 Keeper | ✅ 完成 | relay 模式，自建 Keeper 跳过 WeCom 凭证校验 |
| 消息透传链路 | ✅ 完成 | 企业微信 → Keeper → coco → AI → 回复，全链路验证通过 |
| 离线兜底 | ✅ 完成 | coco 离线时返回固定文本 |
| 断线自动重连 | ✅ 完成 | coco 重启 / Keeper 重启均自动恢复 |
| 部署文档 | ✅ 完成 | `docs/keeper-setup.md` + `docs/phase0-verification.md` |

### 待实现（🚧 规划中）

#### Phase 1：多模型路由（✅ 已完成）

| 功能 | 状态 | 优先级 | 说明 |
|------|------|--------|------|
| ModelRouter 实现 | ✅ 已完成 | 🔴 高 | 已支持切换、failover、冷却 |
| AI 工具调用切换模型 | ✅ 已完成 | 🟡 中 | 已有模型管理工具 |
| 模型能力配置（capabilities/cost）完善 | ✅ 已完成 | 🔴 高 | 支持能力/费用/速度分层配置 |
| 按应用可用模型池隔离 | ✅ 已完成 | 🔴 高 | 运行时按 agent/cron/search 维度生效 |

#### Phase 2：外部 Agent 应用（✅ 已完成）

| 功能 | 状态 | 优先级 | 说明 |
|------|------|--------|------|
| Cron `type: external` | ✅ 已完成 | 🔴 高 | 已支持 external job 调度与配置校验 |
| 外部应用调用协议 | ✅ 已完成 | 🔴 高 | HTTP POST + bearer token + timeout |
| 传话模式（source 标记） | ✅ 已完成 | 🔴 高 | source/relay_mode 已进入调度与工具链路 |
| `spawn_agent` 工具 | ✅ 已完成 | 🟡 中 | 已支持 coco 主动调用外部 agent endpoint |

#### Phase 3：长程记忆增强（✅ 已完成）

| 功能 | 状态 | 优先级 | 说明 |
|------|------|--------|------|
| 记忆分层（工作/情节/语义） | ✅ 已完成 | 🟡 中 | SQLite + RAG + Markdown/Obsidian |
| 时间衰减 | ✅ 已完成 | 🟡 中 | 检索评分已包含时间因子 |
| MMR 多样性排序 | ✅ 已完成 | 🟡 中 | Markdown 记忆已启用 MMR |
| Obsidian vault 文件监视 | ✅ 已完成 | 🟡 中 | 轮询 watcher + 缓存/embedding 增量回收 |
| 上下文自动压缩 | ✅ 已完成 | 🔴 高 | 超阈值自动摘要并保留最近消息 |

#### Phase 4：Skills 安全 + 扩展（✅ 已完成）

| 功能 | 状态 | 优先级 | 说明 |
|------|------|--------|------|
| skills 分层发现优先级 | ✅ 已完成 | 🟡 中 | workspace > managed > bundled |
| Skills 安全评估（思维链 LLM） | ✅ 已完成 | 🟡 中 | 安装前自动评级（safe/warning/dangerous） |
| `coco skill install/search` 命令 | ✅ 已完成 | 🟡 中 | 已支持 search/install/list/download CLI |
| 更多内置 skills | 🟢 低 | 🟢 低 | 扩充 8 → 20+ |

#### Phase 5：安全与完善（✅ 已完成）

| 功能 | 状态 | 优先级 | 说明 |
|------|------|--------|------|
| exec 阻断策略 | ✅ 已完成 | 🟡 中 | blocked_commands 已接入 agent + tool 执行链路 |
| exec 审批流程 | ✅ 已完成 | 🟡 中 | require_confirmation + `--yes` 已生效 |
| 安全策略热更新 | ✅ 已完成 | 🟡 中 | 配置变更后按消息自动重载 |
| DM 配对安全机制 | ✅ 已完成 | 🔴 高 | sender allow_from 白名单机制 |
| allowFrom 白名单 | ✅ 已完成 | 🔴 高 | security.allow_from 支持 user/platform:user 粒度 |
| 群组 mention gating | ✅ 已完成 | 🔴 高 | security.require_mention_in_group + 平台 mentioned 元数据 |
| SSRF 防护 | ✅ 已完成 | 🟡 中 | web_fetch 增加本地/私网地址拦截 |
| 打字指示器 | 🟢 延后 | 🟡 中 | 延后到交互体验专题阶段 |
| 全局配置热重载（channels/model/search） | ✅ 已完成 | 🟡 中 | security + model/search 在运行时重载 |

#### Phase 6：生态与部署（✅ 已完成）

| 功能 | 状态 | 优先级 | 说明 |
|------|------|--------|------|
| Web UI（基础 WebChat） | ✅ 已完成 | 🟢 低 | `coco web` + `/api/chat` + 内置页面 |
| Docker 支持 | ✅ 已完成 | 🟢 低 | Dockerfile + docker-compose + healthcheck |
| 子 Agent 系统 | ✅ 已完成 | 🟢 低 | sessions_spawn |
| Agent 间通信 | ✅ 已完成 | 🟢 低 | sessions_send |
| Keeper 离线时兜底 LLM 完善 | 🟡 规划中 | 🟡 中 | 继续优化离线答复质量 |

#### Phase 7：工作区产品化（✅ 已完成）

| 功能 | 状态 | 优先级 | 说明 |
|------|------|--------|------|
| 工作区文件契约（AGENTS/SOUL/PROFILE） | ✅ 已完成 | 🔴 高 | 启动时自动初始化/补齐模板文件 |
| HEARTBEAT.md 后台意图入口 | ✅ 已完成 | 🟡 中 | HEARTBEAT 已进入工作区提示组装链路 |
| BOOTSTRAP.md 人格引导 | ✅ 已完成 | 🟡 中 | 首次会话一次性注入 BOOTSTRAP 指令 |
| PromptBuild 集成工作区文件 | ✅ 已完成 | 🔴 高 | workspace_contract/bootstrap_instruction 输入已接入 |

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

### ADR-006：Shell 安全策略以配置为准、默认兜底

**决策**：`security.blocked_commands` 与 `security.require_confirmation` 必须进入执行链路；同时保留内置危险命令默认兜底规则。

**原因**：
- 配置是用户可维护的安全边界，不应只停留在文档层
- 默认阻断用于防止配置缺失导致高危命令放行
- 支持个人系统长期演进（可按场景增量加规则）

---

### ADR-007：配置热更新先从安全策略开始

**决策**：先实现“按配置文件 mtime 的运行时重载（security 维度）”，后续再扩展至 channels/model/search/memory 等全局热更新。

**原因**：
- 安全策略变更时效性最高，应优先无需重启生效
- 分层交付能快速降低风险，同时避免一次性改动过大
- 与 TDD 流程匹配，便于逐 phase 验证

---

## 七、参考项目

| 项目 | 语言 | 参考价值 |
|------|------|----------|
| OpenClaw | TypeScript | 能力基准，架构参考（Gateway/Plugin/Skills） |
| CoPaw | Python | OpenClaw 跟随者，工作区契约/热更新/压缩机制参考 |
| coco（当前） | Go | 基础代码，中国平台支持 |

阶段实施计划见：`docs/phases/README.md`

---

*文档维护原则：每个 Phase 完成后更新实现状态；架构变更时同步更新 ADR。*
