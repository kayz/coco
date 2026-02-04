# 企业微信集成指南

本指南介绍如何将 lingti-bot 接入企业微信（WeCom/WeChat Work）。

## 前置条件

1. 企业微信管理员账号
2. 公网可访问的服务器（用于接收回调）
3. HTTPS 域名（企业微信要求回调地址使用 HTTPS）

## 第一步：创建企业微信应用

### 1.1 获取企业 ID (CorpID)

1. 登录 [企业微信管理后台](https://work.weixin.qq.com/wework_admin/frame)
2. 点击「我的企业」
3. 在页面底部找到「企业ID」

### 1.2 创建自建应用

1. 进入「应用管理」→「自建」→「创建应用」
2. 填写应用信息：
   - 应用名称：如 "灵小缇 AI 助手"
   - 应用 Logo：上传应用图标
   - 可见范围：选择可以使用此应用的部门/成员
3. 创建完成后，记录以下信息：
   - **AgentId**：应用的 AgentId
   - **Secret**：应用的 Secret（点击查看）

### 1.3 配置接收消息

1. 在应用详情页，找到「接收消息」→「设置API接收」
2. 填写回调配置：
   - **URL**：`https://your-domain.com/wecom/callback`
   - **Token**：自定义字符串（32位以内，字母数字）
   - **EncodingAESKey**：点击「随机获取」或自定义（43位）
3. 点击「保存」，企业微信会验证 URL 的有效性

## 第二步：配置 lingti-bot

### 2.1 环境变量配置

```bash
export WECOM_CORP_ID="your-corp-id"
export WECOM_AGENT_ID="your-agent-id"
export WECOM_SECRET="your-secret"
export WECOM_TOKEN="your-callback-token"
export WECOM_ENCODING_AES_KEY="your-encoding-aes-key"
```

### 2.2 启动 lingti-bot

```bash
lingti-bot gateway \
  --platform wecom \
  --wecom-corp-id $WECOM_CORP_ID \
  --wecom-agent-id $WECOM_AGENT_ID \
  --wecom-secret $WECOM_SECRET \
  --wecom-token $WECOM_TOKEN \
  --wecom-aes-key $WECOM_ENCODING_AES_KEY \
  --wecom-port 8080 \
  --model deepseek-chat \
  --api-key $DEEPSEEK_API_KEY \
  --base-url "https://api.deepseek.com/v1"
```

### 2.3 配置反向代理（推荐）

使用 Nginx 配置 HTTPS 反向代理：

```nginx
server {
    listen 443 ssl;
    server_name your-domain.com;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location /wecom/callback {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

## 第三步：验证配置

### 3.1 URL 验证

保存回调配置时，企业微信会发送 GET 请求验证 URL：

```
GET /wecom/callback?msg_signature=xxx&timestamp=xxx&nonce=xxx&echostr=xxx
```

lingti-bot 会自动处理验证请求，解密 echostr 并返回明文。

### 3.2 消息测试

1. 在企业微信 App 中找到你创建的应用
2. 发送消息给应用
3. 检查 lingti-bot 日志确认消息接收

```bash
# 查看日志
lingti-bot gateway --log verbose ...
```

## 架构说明

```
┌──────────────┐     ┌─────────────────┐     ┌──────────────┐
│  企业微信     │ ──▶ │  lingti-bot     │ ──▶ │   AI 模型    │
│  用户消息     │     │  回调服务器      │     │  处理响应    │
└──────────────┘     └─────────────────┘     └──────────────┘
       ▲                     │
       └─────────────────────┘
           发送消息 API
```

### 消息流程

1. 用户在企业微信中发送消息
2. 企业微信服务器 POST 加密消息到回调 URL
3. lingti-bot 解密消息，调用 AI 处理
4. lingti-bot 通过 API 发送响应消息
5. 用户在企业微信中收到回复

## 配置参数说明

| 参数 | 环境变量 | 说明 |
|------|---------|------|
| `--wecom-corp-id` | `WECOM_CORP_ID` | 企业 ID |
| `--wecom-agent-id` | `WECOM_AGENT_ID` | 应用 AgentId |
| `--wecom-secret` | `WECOM_SECRET` | 应用 Secret |
| `--wecom-token` | `WECOM_TOKEN` | 回调 Token |
| `--wecom-aes-key` | `WECOM_ENCODING_AES_KEY` | 回调 EncodingAESKey |
| `--wecom-port` | `WECOM_PORT` | 回调服务端口 (默认 8080) |

## 常见问题

### Q: URL 验证失败？

1. 确保服务器公网可访问
2. 确保使用 HTTPS
3. 检查 Token 和 EncodingAESKey 配置是否正确
4. 查看 lingti-bot 日志排查错误

### Q: 收不到消息？

1. 检查应用的可见范围是否包含测试用户
2. 确保回调 URL 配置正确
3. 检查防火墙是否开放端口

### Q: 发送消息失败？

1. 检查 access_token 是否有效
2. 确认 Secret 配置正确
3. 确保 AgentId 正确

### Q: 如何获取用户真实姓名？

默认回调只返回 UserID，需要调用通讯录 API 获取用户信息。需要在「应用管理」中配置「通讯录同步」权限。

## 安全建议

1. **Token 保密**：不要将 Token 和 EncodingAESKey 提交到代码仓库
2. **IP 白名单**：在企业微信后台配置 IP 白名单
3. **HTTPS**：必须使用 HTTPS 保护回调通信
4. **日志脱敏**：生产环境不要记录完整消息内容

## 相关文档

- [企业微信开发者中心](https://developer.work.weixin.qq.com/)
- [回调配置文档](https://developer.work.weixin.qq.com/document/path/90930)
- [获取 access_token](https://developer.work.weixin.qq.com/document/path/91039)
- [发送应用消息](https://developer.work.weixin.qq.com/document/path/90236)
