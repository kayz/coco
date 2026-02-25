# Keeper 部署指南

> 本文档指导你从零完成 Phase 0 的完整部署：
> 企业微信 → Keeper（公网）→ coco（本地）→ AI → 回复

---

## 一、架构概览

```
企业微信用户
    │ HTTP Webhook
    ▼
Keeper（公网服务器，coco keeper）
    │ WebSocket（收消息）
    │ HTTP /webhook（收回复）
    ▼
coco（本地机器，coco relay）
    │
    ▼
AI（Claude / DeepSeek / ...）
```

---

## 二、前置条件

| 项目 | 要求 |
|------|------|
| 公网服务器 | 有固定公网 IP 或域名 |
| HTTPS | 企业微信要求回调 URL 必须是 HTTPS |
| 端口 | 8080（或自定义），防火墙/安全组放行 |
| 企业微信 | 管理员权限，能配置自建应用 |

**HTTPS 推荐方案**（任选一）：
- **Caddy**（最简单，自动申请证书）：
  ```bash
  caddy reverse-proxy --from your-domain.com --to localhost:8080
  ```
- **Nginx + Certbot**：标准反向代理配置

---

## 三、Keeper 配置（公网服务器）

在服务器上创建配置文件 `.coco.yaml`（与 `coco` 可执行文件同目录）：

```yaml
keeper:
  port: 8080
  token: "your-secret-token"      # 自定义，coco 连接时用此 token 认证

  # 企业微信应用凭证（在企业微信管理后台获取）
  wecom_corp_id:  "ww1234567890abcdef"   # 企业 ID
  wecom_agent_id: "1000001"              # 应用 AgentID
  wecom_secret:   "your-app-secret"      # 应用 Secret

  # 企业微信回调验证（在「接收消息」配置页获取/设置）
  wecom_token:    "your-callback-token"  # 随机字符串，自己设定
  wecom_aes_key:  "your-43-char-aes-key" # 43位，随机生成或自动填充
```

**token 安全建议**：
- 使用随机字符串，长度 ≥ 32 位
- 生成方法：`openssl rand -hex 32`
- Keeper 和 coco 两端必须配置相同的 token

启动 Keeper：

```bash
./coco keeper --port 8080
```

启动后日志应显示：
```
[Keeper] Listening on :8080
[Keeper] WebSocket:      ws://0.0.0.0:8080/ws
[Keeper] WeCom callback: http://0.0.0.0:8080/wecom
[Keeper] Webhook:        http://0.0.0.0:8080/webhook
[Keeper] Health check:   http://0.0.0.0:8080/health
```

验证健康检查：
```bash
curl https://your-domain.com/health
# 预期返回：{"status":"ok","coco":"offline"}
```

---

## 四、企业微信后台配置

1. 登录[企业微信管理后台](https://work.weixin.qq.com/wework_admin/loginpage_wx) → **应用管理** → 选择自建应用

2. 进入应用详情 → **接收消息** → 点击「设置API接收」

3. 填写以下信息：

   | 字段 | 值 |
   |------|-----|
   | URL | `https://your-domain.com/wecom` |
   | Token | 与 `.coco.yaml` 中 `wecom_token` 一致 |
   | EncodingAESKey | 与 `.coco.yaml` 中 `wecom_aes_key` 一致 |

4. 点击**保存**，企业微信会立即向 URL 发 GET 请求验证

   - 验证成功：Keeper 日志显示 `[Keeper] WeCom URL verification passed`
   - 验证失败：检查 Keeper 是否正在运行，HTTPS 是否配置正确

---

## 五、coco 配置（本地机器）

在本地机器的 `.coco.yaml` 中配置：

```yaml
mode: relay

relay:
  user_id:     "my-user"                          # 任意标识符
  platform:    "wecom"
  token:       "your-secret-token"                # 与 Keeper 配置一致
  server_url:  "wss://your-domain.com/ws"         # Keeper WebSocket 地址
  webhook_url: "https://your-domain.com/webhook"  # Keeper Webhook 地址

ai:
  provider: "anthropic"
  api_key:  "sk-ant-..."
  model:    "claude-sonnet-4-5"
```

启动 coco：

```bash
./coco relay
```

启动后 Keeper 日志应显示：
```
[Keeper] New WebSocket connection from x.x.x.x:xxxxx
[Keeper] coco connected: user=my-user, platform=wecom, session=keeper-my-user-...
```

此时健康检查返回：
```json
{"status":"ok","coco":"online"}
```

---

## 六、验证完整链路

1. 在企业微信中向应用发送消息「你好」
2. 观察日志链路：

   **Keeper 日志：**
   ```
   [Keeper] WeCom message from UserXXX: 你好
   [Keeper] Forwarded message to coco: UserXXX -> 你好
   [Keeper] Sending coco reply to WeCom user UserXXX: ...
   ```

   **coco 日志：**
   ```
   [Relay] Received message from Keeper
   [Agent] Processing...
   [Relay] Sending response via webhook
   ```

3. 企业微信收到 AI 回复 ✓

---

## 七、离线兜底验证

1. 停止本地 coco
2. 在企业微信发送消息
3. 预期收到回复：**「coco 暂时不在线，请稍后再试。」**
4. 重启 coco，发送消息，恢复正常回复 ✓

---

## 八、常见问题

**企业微信 URL 验证失败**
- 确认 Keeper 已启动：`curl https://your-domain.com/health`
- 确认 HTTPS 证书有效：`curl -v https://your-domain.com/health`
- 确认防火墙/安全组放行了对应端口

**coco 连接 Keeper 失败（token 错误）**
- Keeper 日志：`[Keeper] Auth rejected: invalid token`
- 检查两端 `.coco.yaml` 中的 `token` 是否完全一致（区分大小写）

**coco 连接 Keeper 失败（网络问题）**
- 确认 `server_url` 使用 `wss://`（HTTPS 环境）或 `ws://`（仅内网测试）
- 确认防火墙未拦截 WebSocket 升级请求

**消息收到但 AI 无回复**
- 检查 coco 的 AI 配置（`api_key`、`model`）
- 查看 coco 本地日志是否有报错

**`wecom_aes_key` 格式错误**
- 必须是 43 位字符（企业微信管理后台可随机生成）

---

## 九、配置参数速查

### Keeper（`.coco.yaml`）

| 字段 | 说明 | 必填 |
|------|------|------|
| `keeper.port` | HTTP 监听端口，默认 8080 | 否 |
| `keeper.token` | coco 连接认证 token | **强烈建议** |
| `keeper.wecom_corp_id` | 企业微信企业 ID | 是 |
| `keeper.wecom_agent_id` | 应用 AgentID | 是 |
| `keeper.wecom_secret` | 应用 Secret | 是 |
| `keeper.wecom_token` | 回调验证 Token | 是 |
| `keeper.wecom_aes_key` | 回调 EncodingAESKey（43位）| 是 |

### coco（`.coco.yaml`）

| 字段 | 说明 | 必填 |
|------|------|------|
| `relay.user_id` | 任意标识符 | 是 |
| `relay.platform` | 固定填 `wecom` | 是 |
| `relay.token` | 与 Keeper 一致的 token | **强烈建议** |
| `relay.server_url` | Keeper WebSocket 地址（`wss://`）| 是 |
| `relay.webhook_url` | Keeper Webhook 地址（`https://`）| 是 |
