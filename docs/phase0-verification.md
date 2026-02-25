# Phase 0 验证手册

> 手工操作步骤，逐步验证企业微信 → Keeper → coco → AI 完整链路。
> 每一步都有预期结果，遇到问题先对照「预期结果」排查。

---

## 准备工作

**需要的信息（提前收集好）：**

| 项目 | 说明 | 你的值 |
|------|------|--------|
| 公网服务器 IP/域名 | Keeper 运行的机器 | |
| HTTPS 域名 | 企业微信要求 | |
| 企业 ID（CorpID） | 企业微信管理后台首页 | |
| 应用 AgentID | 应用详情页 | |
| 应用 Secret | 应用详情页 | |
| 回调 Token | 自己设定，任意字符串 | |
| 回调 EncodingAESKey | 管理后台随机生成，43位 | |
| Keeper Token | 自己生成，用于 coco 认证 | |

**生成 Keeper Token（在任意机器上运行）：**
```powershell
# PowerShell
-join ((65..90) + (97..122) + (48..57) | Get-Random -Count 32 | % {[char]$_})
```
记录生成的字符串，两端都要用。

---

## Step 1：验证 Keeper 启动

### 1.1 在公网服务器上创建配置文件

将 `coco.exe` 复制到公网服务器，在同一目录创建 `.coco.yaml`：

```yaml
keeper:
  port: 8080
  token: "YOUR_KEEPER_TOKEN"

  wecom_corp_id:  "wwXXXXXXXXXXXXXXXX"
  wecom_agent_id: "1000001"
  wecom_secret:   "YOUR_APP_SECRET"
  wecom_token:    "YOUR_CALLBACK_TOKEN"
  wecom_aes_key:  "YOUR_43_CHAR_AES_KEY"

logging:
  level: info
```

### 1.2 启动 Keeper

```cmd
coco.exe keeper --port 8080
```

### 1.3 预期日志

```
[Keeper] Listening on :8080
[Keeper] WebSocket:      ws://0.0.0.0:8080/ws
[Keeper] WeCom callback: http://0.0.0.0:8080/wecom
[Keeper] Webhook:        http://0.0.0.0:8080/webhook
[Keeper] Health check:   http://0.0.0.0:8080/health
```

### 1.4 验证健康检查

在另一台机器（或本机）执行：

```powershell
Invoke-WebRequest -Uri "https://your-domain.com/health" | Select-Object -ExpandProperty Content
```

**预期返回：**
```json
{"status":"ok","coco":"offline"}
```

✅ **Step 1 通过条件：** 健康检查返回 200，内容包含 `"status":"ok"`

---

## Step 2：验证企业微信 URL 验证

### 2.1 在企业微信管理后台配置

1. 登录 [企业微信管理后台](https://work.weixin.qq.com/wework_admin/loginpage_wx)
2. **应用管理** → 选择你的自建应用
3. **接收消息** → 点击「设置API接收」
4. 填写：
   - **URL**：`https://your-domain.com/wecom`
   - **Token**：与 `.coco.yaml` 中 `wecom_token` 完全一致
   - **EncodingAESKey**：与 `.coco.yaml` 中 `wecom_aes_key` 完全一致
5. 点击**保存**

### 2.2 预期 Keeper 日志

```
[Keeper] WeCom URL verification passed
```

### 2.3 如果验证失败

- 检查 HTTPS 是否正常：`Invoke-WebRequest -Uri "https://your-domain.com/health"`
- 检查端口是否放行：服务器防火墙/安全组开放 8080（或反代端口 443）
- 检查 Token 和 AESKey 是否与配置文件完全一致（注意空格、换行）

✅ **Step 2 通过条件：** 企业微信后台保存成功，Keeper 日志出现 `URL verification passed`

---

## Step 3：验证 coco 连接 Keeper

### 3.1 在本地机器创建配置文件

在本地 `coco.exe` 同目录创建 `.coco.yaml`：

```yaml
mode: relay

relay:
  user_id:     "my-user"
  platform:    "wecom"
  token:       "YOUR_KEEPER_TOKEN"
  server_url:  "wss://your-domain.com/ws"
  webhook_url: "https://your-domain.com/webhook"

ai:
  provider: "anthropic"        # 或 deepseek、openai 等
  api_key:  "YOUR_API_KEY"
  model:    "claude-sonnet-4-5"

logging:
  level: info
```

### 3.2 启动 coco

```cmd
coco.exe relay
```

### 3.3 预期：Keeper 日志出现

```
[Keeper] New WebSocket connection from x.x.x.x:xxxxx
[Keeper] coco connected: user=my-user, platform=wecom, session=keeper-my-user-...
```

### 3.4 验证健康检查变为 online

```powershell
Invoke-WebRequest -Uri "https://your-domain.com/health" | Select-Object -ExpandProperty Content
```

**预期返回：**
```json
{"status":"ok","coco":"online"}
```

### 3.5 如果连接被拒绝

- Keeper 日志出现 `Auth rejected: invalid token`
  → 检查两端 `token` 是否完全一致
- coco 日志出现连接超时
  → 检查 `server_url` 是否正确（`wss://` 不是 `ws://`）
  → 检查防火墙是否拦截了 WebSocket 升级

✅ **Step 3 通过条件：** 健康检查返回 `"coco":"online"`

---

## Step 4：验证完整消息链路

### 4.1 发送测试消息

在企业微信中向应用发送：**「你好」**

### 4.2 预期 Keeper 日志

```
[Keeper] WeCom message from UserXXX: 你好
[Keeper] Forwarded message to coco: UserXXX -> 你好
[Keeper] Sending coco reply to WeCom user UserXXX: ...
```

### 4.3 预期 coco 日志

```
[Relay] Received message ...
[Relay] Sending response via webhook
```

### 4.4 预期结果

企业微信收到 AI 回复。

### 4.5 如果有消息但无回复

- 检查 coco 日志是否有 AI 报错（API Key 无效、余额不足等）
- 检查 `webhook_url` 是否能从本地机器访问公网 Keeper：
  ```powershell
  Invoke-WebRequest -Uri "https://your-domain.com/health"
  ```

✅ **Step 4 通过条件：** 企业微信收到 AI 回复

---

## Step 5：验证离线兜底

### 5.1 停止本地 coco（Ctrl+C）

### 5.2 在企业微信发送消息

### 5.3 预期 Keeper 日志

```
[Keeper] coco offline, sending fallback reply to UserXXX
```

### 5.4 预期结果

企业微信收到回复：**「coco 暂时不在线，请稍后再试。」**

✅ **Step 5 通过条件：** 收到离线提示

---

## Step 6：验证自动重连

### 6.1 重新启动本地 coco

```cmd
coco.exe relay
```

### 6.2 预期 Keeper 日志

```
[Keeper] coco connected: user=my-user, platform=wecom, session=keeper-my-user-...
```

### 6.3 在企业微信发送消息

**预期：** 恢复正常 AI 回复

✅ **Step 6 通过条件：** 重启后自动重连，AI 回复恢复正常

---

## 验收总结

| Step | 验证项 | 状态 |
|------|--------|------|
| 1 | Keeper 启动，`/health` 返回 200 | ⬜ |
| 2 | 企业微信 URL 验证通过 | ⬜ |
| 3 | coco 连接 Keeper，health 显示 online | ⬜ |
| 4 | 企业微信发消息，AI 正常回复 | ⬜ |
| 5 | coco 离线时收到兜底回复 | ⬜ |
| 6 | 重启 coco 自动重连，回复恢复 | ⬜ |

全部 ✅ → **Phase 0 验收通过**

---

## 附：日志级别调整

如需看更详细的调试信息，将配置文件中 `logging.level` 改为 `debug`：

```yaml
logging:
  level: debug
```

重启后可以看到 WebSocket 帧、AI 请求等详细日志。
