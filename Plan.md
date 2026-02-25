# Phase 0 开发计划：双 Agent 骨架 + 企业微信接入

> 本文档是 Phase 0 的详细开发和测试指导。
> 目标：在现有 coco 代码基础上，实现 Keeper 模式，完成企业微信消息的端到端回复。
>
> 参考：`VISION.md` Phase 0 章节
> 更新时间：2026-02-25

---

## 一、验收目标

**Phase 0 完成的标志：**

> 用户在企业微信中发送一条消息，coco 用 AI 回复，消息回到企业微信。
> 整个链路：企业微信 → Keeper（公网）→ coco（本地）→ AI → coco → Keeper → 企业微信

具体验收场景：

| # | 场景 | 预期结果 |
|---|------|----------|
| 1 | 用户在企业微信发送"你好" | coco AI 正常回复 |
| 2 | 关闭本地 coco，用户发送消息 | Keeper 返回"coco 暂时不在线，稍后回复" |
| 3 | 重新启动 coco | coco 自动重连 Keeper，恢复正常 |
| 4 | Keeper 重启 | coco 自动重连，不需要人工干预 |
| 5 | 发送语音消息 | 转文字后交给 coco 处理（如已配置 Whisper） |

---

## 二、现状分析

### 已有的代码（可直接复用）

| 文件 | 状态 | 说明 |
|------|------|------|
| `internal/platforms/wecom/wecom.go` | ✅ 完整 | 企业微信 Webhook 接收、消息解密/加密、发送文本/媒体、KF 客服消息、Token 刷新 |
| `internal/platforms/wecom/msg_crypt.go` | ✅ 完整 | 企业微信消息加解密（AES） |
| `internal/platforms/wecom/media.go` | ✅ 完整 | 媒体上传/下载 |
| `internal/platforms/relay/relay.go` | ✅ 完整 | coco 作为客户端连接中继服务器的完整实现（WebSocket 重连、消息协议、认证） |
| `internal/agent/agent.go` | ✅ 完整 | Agent 核心（处理消息、调用 AI、工具执行） |
| `internal/router/router.go` | ✅ 完整 | Platform 接口、Message/Response 结构体 |
| `cmd/relay-server.go` | ❌ 弃用 | 空壳（156 行无真实逻辑），将删除并替换为 `cmd/keeper.go` |
| `cmd/router.go` | ✅ 完整 | coco 模式的完整启动逻辑（含 wecom 平台初始化） |

### 需要新增/修改的内容

1. **删除 `cmd/relay-server.go`**：空壳程序，无技术资产，命名不符合系统概念
2. **新建 `cmd/keeper.go`**：Keeper 子命令（`coco keeper`）
3. **`internal/config/config.go`**：新增 Keeper 配置字段
4. **配置文件**：新增 Keeper 配置示例

### 关于命令结构的决策

**决策：弃用 `relay-server`，使用 `coco keeper` 子命令**，理由：
- `relay-server.go` 是空壳（156 行，无真实逻辑），没有值得保留的技术资产
- 在空壳上填充会制造命名债务——未来所有文档/日志都要解释"relay-server 就是 keeper"
- `keeper` 贴近人类常识（"守护者"），而非技术术语（"中继服务器"）
- 一步到位，所有日志前缀、配置字段、文档统一用 `keeper` 命名

---

## 三、架构设计

### 3.1 Keeper 与 coco 的通信协议

复用现有 `internal/platforms/relay/relay.go` 中已定义的消息协议（JSON over WebSocket）：

```
Keeper（服务端）
  ├── /ws          ← coco 连接此端点（WebSocket）
  ├── /wecom       ← 企业微信回调（HTTP）
  ├── /health      ← 健康检查
  └── /webhook     ← 预留（外部触发）

coco（客户端）
  └── 连接 ws://keeper-host:8080/ws
      使用现有 relay.Platform 代码（修改 ServerURL 指向自建 Keeper）
```

### 3.2 消息流

```
企业微信用户发消息
    │
    ▼ HTTP POST /wecom（企业微信回调）
Keeper
    ├── 解密消息（复用 wecom.MsgCrypt）
    ├── coco 在线？
    │     是 → 通过 WebSocket 转发给 coco
    │     否 → 直接调用简单 LLM 或返回固定回复
    │
    ▼（coco 在线时）
coco（本地）
    ├── 收到消息
    ├── 交给 agent.Agent 处理
    ├── AI 生成回复
    └── 通过 WebSocket 发回 Keeper
         │
         ▼
Keeper → 调用企业微信 API 发送回复
```

### 3.3 Keeper 的 coco 离线兜底

Phase 0 的兜底策略：**返回固定文本**（不接入 LLM）。

```
coco 离线时收到消息 → 回复："coco 暂时不在线，请稍后再试。"
```

原因：
- Phase 0 目标是验证链路通，不引入额外复杂度
- 简单 LLM 兜底在 Phase 5 实现

---

## 四、详细任务分解

### Task 1：Keeper 配置结构

**文件**：`internal/config/config.go`

新增 `KeeperConfig` 结构体，并加入 `Config`：

```go
type KeeperConfig struct {
    Port           int    `yaml:"port"`            // Keeper HTTP 服务端口，默认 8080
    Token          string `yaml:"token"`           // coco 连接 Keeper 的认证 token
    // WeCom 配置（Keeper 直接持有企业微信凭证）
    WeComCorpID    string `yaml:"wecom_corp_id"`
    WeComAgentID   string `yaml:"wecom_agent_id"`
    WeComSecret    string `yaml:"wecom_secret"`
    WeComToken     string `yaml:"wecom_token"`
    WeComAESKey    string `yaml:"wecom_aes_key"`
}
```

`Config` 中新增：
```go
Keeper KeeperConfig `yaml:"keeper,omitempty"`
```

**验收**：`config.Load()` 能正确读取 keeper 配置字段。

---

### Task 2：实现 Keeper 服务端

**文件**：`cmd/keeper.go`（新建），删除 `cmd/relay-server.go`

这是 Phase 0 的核心工作，需要实现：

#### 2a. WebSocket 服务端（接受 coco 连接）

```
功能：
- 接受 coco 的 WebSocket 连接
- 验证 token（与配置中的 keeper.token 对比）
- 维护 coco 连接状态（在线/离线）
- 收到 coco 发来的回复消息 → 调用企业微信 API 发送给用户
- coco 断开时更新状态

消息格式：复用现有 relay.go 中的 JSON 协议
```

#### 2b. 企业微信 Webhook 处理（/wecom）

```
功能：
- 复用 wecom.Platform 的 handleCallback 逻辑
- GET 请求：URL 验证（echostr 回显）
- POST 请求：解密消息 → 判断 coco 是否在线
    - 在线：通过 WebSocket 转发给 coco
    - 离线：直接回复固定文本
```

#### 2c. Keeper 启动流程

```go
func runKeeper(cmd *cobra.Command, args []string) {
    // 1. 加载配置
    // 2. 初始化 wecom.Platform（CallbackPort=-1，不启动独立 HTTP 服务）
    // 3. 启动 HTTP 服务（统一处理 /ws、/wecom、/health）
    // 4. 等待退出信号
}
```

**验收**：
- `coco keeper` 启动，日志显示 `[Keeper] Listening on :8080`
- 企业微信后台配置回调 URL 后，URL 验证通过（GET /wecom 返回 echostr）
- coco 客户端能连接到 Keeper（日志显示连接成功）

---

### Task 3：coco 客户端连接 Keeper

**文件**：`internal/config/config.go`（RelayConfig 扩展）、`cmd/router.go`

现有 `RelayConfig` 已有 `ServerURL` 字段，coco 只需将 `ServerURL` 指向自建 Keeper 即可。

需要确认：
- 现有 relay 客户端（`internal/platforms/relay/relay.go`）的认证机制是否兼容 Keeper 的 token 验证
- 如不兼容，需在 Keeper 侧适配现有协议（不改 relay.go 客户端代码）

**配置示例**：

```yaml
# coco 本地配置（.coco.yaml）
mode: relay
relay:
  user_id: "my-user"
  platform: "wecom"
  server_url: "wss://your-keeper-domain.com/ws"  # 指向自建 Keeper
  # webhook_url 不再需要（Keeper 直接持有 wecom 凭证）
```

**验收**：
- coco 启动后自动连接 Keeper
- Keeper 日志显示 coco 已连接

---

### Task 4：端到端消息链路测试

在 Task 1-3 完成后，进行完整链路验证。

**测试步骤**：

1. 启动 Keeper（公网服务器）：
   ```
   coco keeper --port 8080
   ```

2. 启动 coco（本地）：
   ```
   coco router --mode relay --relay-server wss://your-domain.com/ws
   ```
   （或通过配置文件）

3. 在企业微信发送"你好"

4. 观察日志链路：
   ```
   [Keeper] WeCom callback received from user_xxx
   [Keeper] Forwarding to coco via WebSocket
   [coco]   Received message from Keeper: "你好"
   [coco]   Agent processing...
   [coco]   Sending response to Keeper
   [Keeper] Received response from coco, sending to WeCom
   [WeCom]  Message sent to user_xxx
   ```

5. 企业微信收到 AI 回复

**验收**：完整走通上述日志链路，用户收到回复。

---

### Task 5：配置文档

**文件**：`docs/keeper-setup.md`（新建）

内容包括：
- 企业微信后台配置步骤（截图说明）
- Keeper 配置文件示例（完整 `.coco.yaml`）
- coco 配置文件示例
- 部署建议（推荐使用什么公网服务器）
- 常见问题（URL 验证失败、连接超时等）

---

## 五、部署环境要求

### Keeper（公网服务器）

| 项目 | 要求 |
|------|------|
| 操作系统 | Linux（推荐 Ubuntu 22.04）或 Windows Server |
| 公网 IP | 必须有，或通过域名解析 |
| 端口 | 8080（或自定义），需在防火墙开放 |
| HTTPS | 企业微信要求回调 URL 为 HTTPS；可用 Nginx/Caddy 反代 |
| 配置文件 | `.coco.yaml`，包含 wecom 凭证和 keeper token |

**HTTPS 说明**：企业微信回调 URL 必须是 HTTPS。推荐方案：
- 使用 Caddy（自动 HTTPS）：`caddy reverse-proxy --from your-domain.com --to localhost:8080`
- 或 Nginx + Let's Encrypt

### coco（本地机器）

| 项目 | 要求 |
|------|------|
| 操作系统 | Windows/macOS/Linux |
| 网络 | 能访问公网（连接 Keeper） |
| 配置 | `.coco.yaml` 中 `relay.server_url` 指向 Keeper |
| AI 配置 | 至少配置一个 AI 提供商（如 claude/deepseek） |

---

## 六、企业微信后台配置步骤

1. 登录企业微信管理后台 → 应用管理 → 自建应用
2. 在「接收消息」中配置：
   - **URL**：`https://your-domain.com/wecom`（Keeper 的公网地址）
   - **Token**：与 Keeper 配置中 `wecom_token` 一致
   - **EncodingAESKey**：与 Keeper 配置中 `wecom_aes_key` 一致
3. 点击「保存」，企业微信会发 GET 请求验证 URL（Keeper 需已启动）
4. 验证通过后，消息推送即生效

---

## 七、配置文件示例

### Keeper 配置（公网服务器，`.coco.yaml`）

```yaml
# Keeper 模式配置
keeper:
  port: 8080
  token: "your-secret-token-here"   # coco 连接时用此 token 认证
  wecom_corp_id: "ww1234567890abcdef"
  wecom_agent_id: "1000001"
  wecom_secret: "your-app-secret"
  wecom_token: "your-callback-token"
  wecom_aes_key: "your-43-char-aes-key"
```

### coco 配置（本地机器，`.coco.yaml`）

```yaml
# coco 模式配置（连接自建 Keeper）
mode: relay
relay:
  user_id: "my-user"
  platform: "wecom"
  server_url: "wss://your-domain.com/ws"
  # token 用于向 Keeper 认证
  token: "your-secret-token-here"

# AI 配置（至少一个）
ai:
  providers:
    - name: claude
      type: anthropic
      api_key: "sk-ant-..."
      model: "claude-sonnet-4-5"
```

---

## 八、风险与注意事项

| 风险 | 说明 | 缓解措施 |
|------|------|----------|
| 企业微信 URL 验证失败 | Keeper 未启动或端口不通 | 先测试 `curl https://your-domain.com/health` |
| WebSocket 连接被防火墙拦截 | 部分云服务商限制 WebSocket | 确认安全组/防火墙开放对应端口 |
| 消息重复处理 | coco 断线重连期间消息丢失或重复 | Phase 0 接受此问题，Phase 1 加消息去重 |
| 企业微信 access_token 过期 | Token 每 2 小时过期 | 现有 wecom.go 已实现自动刷新，无需处理 |
| relay.go 协议兼容性 | Keeper 需兼容现有 relay 客户端协议 | 优先阅读 relay.go 的消息格式，Keeper 侧适配 |

---

## 九、不在 Phase 0 范围内

以下功能**明确不做**，避免范围蔓延：

- ❌ Keeper 接入 LLM（兜底只用固定文本）
- ❌ 多用户支持（Phase 0 只支持单个 coco 连接）
- ❌ 消息持久化/重试（断线期间消息丢失可接受）
- ❌ HTTPS 自动配置（由用户自行处理 Nginx/Caddy）
- ❌ 飞书/钉钉等其他渠道
- ❌ 多模型路由（Phase 1）
- ❌ 外部 Agent 应用（Phase 2）
- ❌ `--onboard` 向导扩展（后续）

---

## 十、完成标志

Phase 0 完成，当且仅当：

- [x] `coco keeper` 启动正常，健康检查 `/health` 返回 200
- [x] 企业微信后台 URL 验证通过
- [x] coco 本地启动后自动连接 Keeper，Keeper 日志显示连接成功
- [x] 用户在企业微信发消息，coco AI 回复正常到达
- [x] 关闭 coco，企业微信发消息，Keeper 返回离线提示
- [x] 重启 coco，自动重连，恢复正常回复
- [x] `docs/keeper-setup.md` 文档完成，能指导新用户从零配置
- [x] `cmd/relay-server.go` 已删除

---

*计划制定：2026-02-25*
*预计涉及文件：`cmd/keeper.go`（新建）、`cmd/relay-server.go`（删除）、`internal/config/config.go`、`docs/keeper-setup.md`*
