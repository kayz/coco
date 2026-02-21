# 自托管云中继服务器部署指南

本指南说明如何部署和使用自托管的 lingti-bot 云中继服务器，以解决企业微信信任 IP 动态变化的问题。

## 问题背景

家庭网络的公网 IP 经常变化，导致企业微信的信任 IP 设置需要频繁更新。使用自托管云中继服务器可以：
- 使用固定的服务器 IP 作为信任 IP
- 所有企业微信 API 调用（包括媒体下载/上传）都通过服务器代理
- 本地客户端通过 WebSocket 连接到服务器，无需公网 IP

## 架构说明

```
企业微信 <---> 云中继服务器 <--(WebSocket)--> 本地客户端
(固定信任IP)    (固定公网IP)       (动态家庭网络)
```

## 快速开始

云中继服务器现在集成在 lingti-bot 主程序中，无需单独部署！

### 1. 准备服务器

- 需要一台有固定公网 IP 的服务器（VPS、云服务器等）
- 推荐配置：1核2GB 以上
- 操作系统：Linux（Ubuntu/CentOS/Debian 均可）或 Windows

### 2. 安装 Lingti-Bot

在服务器上安装 lingti-bot：

```bash
# 克隆项目
git clone https://github.com/pltanton/lingti-bot.git
cd lingti-bot

# 编译
go build -o lingti-bot main.go
```

### 3. 启动服务器

```bash
# 默认端口 8080
./lingti-bot relay-server

# 指定端口
./lingti-bot relay-server --port 8080
```

### 4. 配置 systemd 服务（Linux 推荐）

创建 `/etc/systemd/system/lingti-relay.service`：

```ini
[Unit]
Description=Lingti Bot Relay Server
After=network.target

[Service]
Type=simple
User=www-data
WorkingDirectory=/path/to/lingti-bot
ExecStart=/path/to/lingti-bot/lingti-bot relay-server --port 8080
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

启动服务：

```bash
sudo systemctl daemon-reload
sudo systemctl enable lingti-relay
sudo systemctl start lingti-relay
sudo systemctl status lingti-relay
```

### 5. 配置 Nginx 反向代理（可选但推荐）

为了支持 HTTPS 和更好的安全性，建议使用 Nginx 反向代理。

创建 `/etc/nginx/sites-available/lingti-relay`：

```nginx
server {
    listen 80;
    server_name your-domain.com;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

启用配置：

```bash
sudo ln -s /etc/nginx/sites-available/lingti-relay /etc/nginx/sites-enabled/
sudo nginx -t
sudo systemctl reload nginx
```

### 6. 配置 SSL 证书（推荐）

使用 Let's Encrypt 免费证书：

```bash
sudo apt install certbot python3-certbot-nginx
sudo certbot --nginx -d your-domain.com
```

## 企业微信配置

### 1. 添加信任 IP

在企业微信管理后台：
- 进入「应用管理」→ 选择你的应用
- 找到「开发者接口」→「IP 白名单」
- 添加你的云中继服务器的公网 IP

### 2. 配置回调 URL

- 在企业微信应用设置中，找到「接收消息」配置
- 设置回调 URL 为：`https://your-domain.com/wecom`
- Token 和 EncodingAESKey 使用你在本地配置中的值

## 本地客户端配置

### 1. 使用命令行参数

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

### 2. 使用配置文件

在 `.lingti.yaml` 中配置：

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

## 安全建议

1. **使用 HTTPS/WSS**：始终使用加密连接，避免明文传输
2. **防火墙配置**：只开放必要的端口（80/443）
3. **定期更新**：保持服务器和依赖库最新
4. **访问日志**：启用并监控访问日志
5. **API Key 保护**：不要将 API Key 提交到代码仓库

## API 端点

云中继服务器提供以下端点：

| 端点 | 方法 | 说明 |
|------|------|------|
| `/ws` | GET | WebSocket 连接端点 |
| `/wecom` | GET/POST | 企业微信回调处理 |
| `/webhook` | POST | Webhook 消息发送 |
| `/proxy/media/get` | POST | 媒体文件下载代理 |
| `/proxy/media/upload` | POST | 媒体文件上传代理 |
| `/health` | GET | 健康检查端点 |

## 许可证

与 lingti-bot 主项目使用相同的许可证。
