# Lingti-Bot 云中继使用指南

## 什么是云中继？

云中继是 Lingti-Bot 的一种部署模式，它通过一个中间服务器来转发消息和 API 请求，解决了以下问题：

1. **动态 IP 问题**：家庭网络的公网 IP 经常变化，云中继使用固定的服务器 IP 作为信任 IP
2. **媒体文件访问**：企业微信等平台要求客户端 IP 在白名单中才能下载/上传媒体文件
3. **无需公网 IP**：本地客户端通过 WebSocket 连接到服务器，不需要公网 IP 或端口映射

## 架构说明

```
┌─────────────┐
│  企业微信    │
│  (固定信任IP)│
└──────┬──────┘
       │
       ▼
┌─────────────────────────┐
│   云中继服务器          │
│   (固定公网IP)          │
│  - WebSocket 服务      │
│  - 企业微信回调        │
│  - 媒体文件代理        │
└──────┬──────────────────┘
       │ WebSocket
       ▼
┌─────────────────────────┐
│   本地 Lingti-Bot      │
│   (动态家庭网络)        │
│  - AI 消息处理         │
│  - 本地工具执行        │
└─────────────────────────┘
```

## 快速开始

### 方案一：使用官方云中继（最简单）

Lingti-Bot 官方提供了免费的云中继服务，你可以直接使用：

```bash
lingti-bot relay \
  --platform wecom \
  --wecom-corp-id YOUR_CORP_ID \
  --wecom-agent-id YOUR_AGENT_ID \
  --wecom-secret YOUR_SECRET \
  --wecom-token YOUR_TOKEN \
  --wecom-aes-key YOUR_AES_KEY \
  --use-media-proxy \
  --provider deepseek \
  --api-key YOUR_API_KEY
```

官方云中继地址：
- WebSocket: `wss://bot.lingti.com/ws`
- Webhook: `https://bot.lingti.com/webhook`
- 信任 IP: `106.52.166.51`

### 方案二：自托管云中继（推荐）

如果你希望完全控制，可以部署自己的云中继服务器。云中继服务器现在集成在 lingti-bot 主程序中！

#### 1. 准备服务器

需要一台有固定公网 IP 的服务器（VPS、云服务器等）：
- 推荐配置：1核2GB 以上
- 操作系统：Linux（Ubuntu/CentOS/Debian 均可）或 Windows

#### 2. 部署云中继服务器

```bash
# 克隆项目
git clone https://github.com/pltanton/lingti-bot.git
cd lingti-bot

# 编译
go build -o lingti-bot main.go

# 运行服务器（默认端口 8080）
./lingti-bot relay-server
```

详细部署指南请参考 [relay-server-deploy.md](./relay-server-deploy.md)

#### 3. 配置企业微信

1. **添加信任 IP**：在企业微信管理后台，将你的服务器 IP 添加到应用的「IP 白名单」
2. **配置回调 URL**：设置为 `https://your-domain.com/wecom`

#### 4. 启动本地客户端

```bash
lingti-bot relay \
  --platform wecom \
  --wecom-corp-id YOUR_CORP_ID \
  --wecom-agent-id YOUR_AGENT_ID \
  --wecom-secret YOUR_SECRET \
  --wecom-token YOUR_TOKEN \
  --wecom-aes-key YOUR_AES_KEY \
  --server wss://your-domain.com/ws \
  --webhook https://your-domain.com/webhook \
  --use-media-proxy \
  --provider deepseek \
  --api-key YOUR_API_KEY
```

## 配置方式

Lingti-Bot 支持三种配置方式：

### 1. 命令行参数

```bash
lingti-bot relay \
  --user-id YOUR_USER_ID \
  --platform wecom \
  --server wss://your-domain.com/ws \
  --webhook https://your-domain.com/webhook \
  --use-media-proxy \
  --provider deepseek \
  --api-key YOUR_API_KEY
```

### 2. 配置文件

在 `~/.lingti.yaml` 中配置：

```yaml
mode: relay
relay:
  user_id: wecom-YOUR_CORP_ID
  platform: wecom
  server_url: wss://your-domain.com/ws
  webhook_url: https://your-domain.com/webhook
  use_media_proxy: true

platforms:
  wecom:
    corp_id: YOUR_CORP_ID
    agent_id: YOUR_AGENT_ID
    secret: YOUR_SECRET
    token: YOUR_TOKEN
    aes_key: YOUR_AES_KEY

ai:
  provider: deepseek
  api_key: YOUR_API_KEY
```

然后直接运行：

```bash
lingti-bot relay
```

### 3. 环境变量

```bash
export RELAY_PLATFORM=wecom
export RELAY_SERVER_URL=wss://your-domain.com/ws
export RELAY_WEBHOOK_URL=https://your-domain.com/webhook
export RELAY_USE_MEDIA_PROXY=true
export WECOM_CORP_ID=YOUR_CORP_ID
export WECOM_AGENT_ID=YOUR_AGENT_ID
export WECOM_SECRET=YOUR_SECRET
export WECOM_TOKEN=YOUR_TOKEN
export WECOM_AES_KEY=YOUR_AES_KEY
export AI_PROVIDER=deepseek
export AI_API_KEY=YOUR_API_KEY

lingti-bot relay
```

## 配置优先级

配置按以下优先级加载（从高到低）：

1. **命令行参数**（最高优先级）
2. **环境变量**
3. **配置文件**（`~/.lingti.yaml`）
4. **默认值**（最低优先级）

## 完整参数说明

### 通用参数

| 参数 | 环境变量 | 说明 |
|------|---------|------|
| `--user-id` | `RELAY_USER_ID` | 用户 ID（从 /whoami 获取） |
| `--platform` | `RELAY_PLATFORM` | 平台类型：feishu, slack, wechat, wecom |
| `--server` | `RELAY_SERVER_URL` | WebSocket 服务器地址 |
| `--webhook` | `RELAY_WEBHOOK_URL` | Webhook 地址 |
| `--use-media-proxy` | `RELAY_USE_MEDIA_PROXY` | 启用媒体文件代理 |

### AI 相关参数

| 参数 | 环境变量 | 说明 |
|------|---------|------|
| `--provider` | `AI_PROVIDER` | AI 提供商：claude, deepseek, kimi, qwen |
| `--api-key` | `AI_API_KEY` | AI API Key |
| `--base-url` | `AI_BASE_URL` | 自定义 API 基础 URL |
| `--model` | `AI_MODEL` | 模型名称 |

### 企业微信参数

| 参数 | 环境变量 | 说明 |
|------|---------|------|
| `--wecom-corp-id` | `WECOM_CORP_ID` | 企业微信 Corp ID |
| `--wecom-agent-id` | `WECOM_AGENT_ID` | 企业微信 Agent ID |
| `--wecom-secret` | `WECOM_SECRET` | 企业微信 Secret |
| `--wecom-token` | `WECOM_TOKEN` | 企业微信回调 Token |
| `--wecom-aes-key` | `WECOM_AES_KEY` | 企业微信 EncodingAESKey |

## 企业微信配置步骤

### 1. 创建应用

1. 登录 [企业微信管理后台](https://work.weixin.qq.com/)
2. 进入「应用管理」→「应用」→「创建应用」
3. 填写应用名称和简介，上传应用logo
4. 创建后记录以下信息：
   - AgentId
   - Secret（需要点击「获取」按钮）

### 2. 配置接收消息

1. 在应用详情页找到「接收消息」
2. 点击「设置接收消息」
3. 配置以下内容：
   - URL: `https://your-domain.com/wecom`（或官方地址 `https://bot.lingti.com/wecom`）
   - Token: 自定义一个字符串，记住它
   - EncodingAESKey: 点击「随机生成」，记住它

**注意**：先不要点击「保存」，需要先启动本地客户端进行验证。

### 3. 配置 IP 白名单

1. 在应用详情页找到「开发者接口」
2. 点击「IP 白名单」
3. 添加云中继服务器的 IP 地址：
   - 官方云中继：`106.52.166.51`
   - 自托管：你的服务器公网 IP

### 4. 启动客户端并验证

1. 启动本地 Lingti-Bot relay 客户端
2. 在企业微信管理后台点击「保存」
3. 如果配置正确，会提示验证成功

## 常见问题

### 1. 回调验证失败

**可能原因**：
- 回调 URL 不正确
- Token 或 EncodingAESKey 不匹配
- 本地客户端未启动
- 防火墙阻止了连接

**解决方法**：
- 检查回调 URL 是否可访问
- 确认 Token 和 EncodingAESKey 一致
- 查看本地客户端日志
- 检查服务器防火墙设置

### 2. 媒体文件下载失败

**可能原因**：
- 未启用 `--use-media-proxy`
- 服务器 IP 未添加到企业微信信任 IP
- 媒体文件已过期（企业微信媒体文件只保留 3 天）

**解决方法**：
- 确保使用 `--use-media-proxy` 参数
- 检查企业微信 IP 白名单配置
- 重新发送媒体文件

### 3. WebSocket 连接断开

**可能原因**：
- 网络不稳定
- 服务器重启
- 防火墙超时设置

**解决方法**：
- 客户端会自动重连，等待即可
- 检查服务器稳定性
- 调整防火墙超时设置

### 4. 消息发送失败

**可能原因**：
- Webhook URL 配置错误
- 服务器未正常运行
- 企业微信应用权限不足

**解决方法**：
- 检查 Webhook URL 配置
- 确认服务器正常运行
- 检查企业微信应用权限

## 安全建议

1. **使用 HTTPS/WSS**：始终使用加密连接，避免明文传输
2. **保护 API Key**：不要将 API Key 提交到代码仓库
3. **定期更新**：保持服务器和客户端最新
4. **防火墙配置**：只开放必要的端口
5. **访问日志**：启用并监控访问日志

## 下一步

- 查看 [自托管云中继部署指南](./relay-server-deploy.md)
- 了解 [系统架构](../ARCHITECTURE.md)
- 探索 [更多功能](../README.md)
