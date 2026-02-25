# Lingti-Bot 系统架构与数据流

## 目录结构

```
lingti-bot/
├── cmd/                          # CLI 入口
│   ├── root.go                   # 根命令
│   ├── router.go                 # router 命令（多平台消息路由）
│   └── onboard.go                # 交互式配置向导
├── internal/
│   ├── agent/                    # 核心 AI 代理逻辑
│   │   ├── agent.go              # 主 Agent 结构体（消息处理、工具调用）
│   │   ├── provider.go           # Provider 接口
│   │   ├── provider_*.go         # 各 AI 提供商实现（Claude、DeepSeek、Qwen 等）
│   │   ├── memory.go             # 对话记忆（SQLite）
│   │   ├── rag_memory.go         # RAG 持久化记忆（向量数据库）
│   │   ├── tools.go              # 工具调度
│   │   └── cron_tools.go         # 定时任务工具
│   ├── router/                   # 消息路由层
│   │   └── router.go             # 统一的消息路由接口
│   ├── platforms/                # 各消息平台实现
│   │   ├── wecom/                # 企业微信
│   │   ├── feishu/               # 飞书
│   │   ├── dingtalk/             # 钉钉
│   │   ├── slack/                # Slack
│   │   ├── telegram/             # Telegram
│   │   ├── discord/              # Discord
│   │   └── ...其他平台
│   ├── cron/                     # 定时任务系统
│   ├── tools/                    # 本地工具集（文件、Shell、浏览器等）
│   ├── mcp/                      # MCP (Model Context Protocol) 服务
│   ├── config/                   # 配置管理
│   ├── search/                   # 搜索引擎
│   └── voice/                    # 语音功能（STT/TTS）
└── ...
```

---

## 核心数据流

### 1. 消息接收流程（以企业微信为例）

```
用户发送消息
    │
    ▼
企业微信服务器
    │
    ▼
WeCom 平台回调 (wecom.go:handleCallback)
    │ 验证签名、解密消息
    ▼
转换为 router.Message 结构体
    │
    ▼
Router 分发 (router.go:handleMessage)
    │
    ▼
Agent.HandleMessage (agent.go)
    │
    ├─ 检查内置命令 (/status, /model 等)
    ├─ 从记忆中加载历史对话
    ├─ 从 RAG 中检索相关记忆
    ├─ 调用 AI 提供商 (带自动切换)
    │   │
    │   └─ chatWithFailover()
    │       ├─ 尝试当前 provider
    │       ├─ 失败则尝试下一个（按优先级）
    │       └─ 成功后记住当前 provider
    │
    ├─ 处理工具调用（最多 20 轮）
    │   ├─ 执行工具（文件、Shell、浏览器等）
    │   └─ 将结果返回给 AI
    │
    ├─ 保存对话到记忆
    ├─ 保存到 RAG 持久化记忆
    └─ 提取并保存用户偏好
    │
    ▼
返回 router.Response
    │
    ▼
Router 发送回对应平台
    │
    ▼
用户收到回复
```

### 2. 消息平台接口 (Platform Interface)

所有消息平台都实现统一的 `router.Platform` 接口：

```go
type Platform interface {
    Name() string                                    // 平台名称
    Start(ctx context.Context) error                 // 启动监听
    Stop() error                                     // 停止
    Send(ctx context.Context, channelID string, resp Response) error  // 发送消息
    SetMessageHandler(handler func(msg Message))    // 设置消息处理器
}
```

### 3. 消息结构

**输入消息 (Message):**
```go
type Message struct {
    ID          string              // 消息 ID
    Platform    string              // 平台名 ("wecom", "feishu", "slack" 等)
    ChannelID   string              // 频道/聊天 ID
    UserID      string              // 用户 ID
    Username    string              // 用户名
    Text        string              // 消息文本
    ThreadID    string              // 线程 ID（用于回复）
    Attachments []Attachment        // 附件（图片、文件等）
    Metadata    map[string]string   // 平台特定元数据
}
```

**输出响应 (Response):**
```go
type Response struct {
    Text     string
    Files    []FileAttachment
    ThreadID string
    Metadata map[string]string
}
```

---

## Agent 核心组件

### Agent 结构体

```go
type Agent struct {
    providers       []Provider       // 多个 AI 提供商（支持自动切换）
    currentProvider int              // 当前使用的 provider 索引
    memory          *Memory          // 对话记忆（SQLite）
    ragMemory       *RAGMemory       // RAG 持久化记忆（向量数据库）
    sessions        *SessionManager  // 会话设置（思考模式、详细模式等）
    // ... 其他字段
}
```

### 模型自动切换 (Failover)

当配置了多个模型时：

1. **初始化时**：按 `priority` 排序所有启用的模型
2. **请求时**：
   - 先尝试 `currentProvider`
   - 失败则循环尝试下一个
   - 成功后更新 `currentProvider`
3. **日志**：详细记录每次切换

---

## 定时任务 (Cron) 数据流

```
用户说："每天早上 9 点提醒我开会"
    │
    ▼
Agent 处理，识别为定时任务创建
    │
    ▼
调用 cron_create_job 工具
    │
    ▼
cron.Store 保存到 SQLite
    │
    ▼
cron.Scheduler 启动调度
    │
    ┌─────────────────┐
    │  定时触发       │
    └─────────────────┘
    │
    ▼
CronNotifier 调用 Agent.HandleMessage
    │
    ▼
发送提醒消息给用户
```

---

## 配置加载优先级

```
命令行参数 (最高)
    │
    ▼
环境变量
    │
    ▼
配置文件 (~/.coco.yaml)
    │
    ▼
默认值 (最低)
```

---

## 安全机制

1. **文件访问白名单**：`security.allowed_paths`
2. **命令黑名单**：`security.blocked_commands`
3. **文件工具禁用**：`security.disable_file_tools`
4. **自动批准**：`--yes` 标志（跳过安全检查）

---

## 扩展性设计

### 添加新的 AI 提供商

1. 在 `internal/agent/` 下创建 `provider_xxx.go`
2. 实现 `Provider` 接口：
   ```go
   type Provider interface {
       Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
       Name() string
   }
   ```
3. 在 `New` 函数中注册

### 添加新的消息平台

1. 在 `internal/platforms/` 下创建新目录
2. 实现 `router.Platform` 接口
3. 在 `cmd/router.go` 中注册

---

## 总结

Lingti-Bot 的设计哲学：
- **单一二进制**：所有功能编译为一个 Go 可执行文件
- **极简架构**：避免过度设计，保持代码简洁
- **渐进式增强**：在现有架构上逐步添加功能
- **中国优先**：优先支持国内平台和云服务
