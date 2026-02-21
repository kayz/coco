# 配置优先级

lingti-bot 采用三层配置解析机制，优先级从高到低：

```
命令行参数  >  环境变量  >  配置文件 (~/.lingti.yaml)
```

每个配置项按此顺序查找，找到即停止。这意味着：

- **命令行参数**始终优先，适合临时覆盖或运行多个实例
- **环境变量**适合 CI/CD 或容器化部署
- **配置文件**适合日常使用，通过 `lingti-bot onboard` 生成

## 示例

以 AI Provider 为例，解析顺序为：

| 优先级 | 来源 | 示例 |
|--------|------|------|
| 1 | `--provider deepseek` | 命令行参数 |
| 2 | `AI_PROVIDER=deepseek` | 环境变量 |
| 3 | `ai.provider: deepseek` | ~/.lingti.yaml |

```bash
# 配置文件中设置了 provider: qwen
# 环境变量设置了 AI_PROVIDER=deepseek
# 命令行指定了 --provider claude
# 最终使用: claude（命令行参数最高优先）
```

## 配置文件

默认路径：`~/.lingti.yaml`

通过交互式向导生成：

```bash
lingti-bot onboard
```

### 完整结构

```yaml
mode: relay  # "relay" 或 "router"

ai:
  provider: deepseek  # 单个模型配置（向后兼容）
  api_key: sk-xxx
  base_url: ""        # 自定义 API 地址（可选）
  model: ""           # 自定义模型名（可选，留空使用 provider 默认值）
  
  # 多模型配置（推荐，支持自动切换）
  models:
    - provider: claude
      api_key: sk-ant-xxx
      enabled: true
      priority: 1
    - provider: deepseek
      api_key: sk-xxx
      enabled: true
      priority: 2
    - provider: qwen
      api_key: sk-xxx
      enabled: true
      priority: 3

relay:
  platform: wecom    # "feishu", "slack", "wechat", "wecom"
  user_id: ""        # 从 /whoami 获取（WeCom 不需要）

platforms:
  wecom:
    corp_id: ""
    agent_id: ""
    secret: ""
    token: ""
    aes_key: ""
  wechat:
    app_id: ""
    app_secret: ""
  feishu:
    app_id: ""
    app_secret: ""
  slack:
    bot_token: ""
    app_token: ""
  dingtalk:
    client_id: ""
    client_secret: ""
  telegram:
    token: ""
  discord:
    token: ""

browser:
  screen_size: fullscreen  # "fullscreen" 或 "宽x高"（如 "1024x768"），默认 fullscreen

search:
  primary_engine: metaso    # 主搜索引擎：metaso, tavily
  secondary_engine: tavily  # 副搜索引擎
  auto_search: true         # 自动搜索：当无法回答时自动搜索
  engines:
    - name: metaso
      type: metaso
      api_key: ""           # 秘塔搜索 API Key
      enabled: true
      priority: 1
    - name: tavily
      type: tavily
      api_key: ""           # Tavily 搜索 API Key
      enabled: true
      priority: 2
    # 自定义搜索引擎示例
    # - name: myengine
    #   type: custom_http
    #   api_key: ""
    #   base_url: "https://api.myengine.com"
    #   enabled: true
    #   priority: 3

security:
  allowed_paths:             # 限制文件操作的目录白名单（空=不限制）
    - ~/Documents
    - ~/Downloads
  blocked_commands:          # 禁止执行的命令前缀
    - "rm -rf /"
    - "mkfs"
    - "dd if="
  require_confirmation: []   # 需要用户确认的命令（预留）
```

## 安全配置

通过 `security` 配置项限制 bot 的文件系统访问和命令执行范围。

### allowed_paths — 目录白名单

限制 `file_read`、`file_write`、`file_list`、`file_trash` 和 `shell_execute` 只能访问指定目录：

```yaml
security:
  allowed_paths:
    - ~/Documents/work
    - ~/Downloads
```

- 空列表 `[]`（默认）= 不限制，可访问所有路径
- 设置后，所有文件操作必须在白名单目录内，否则返回权限错误
- 路径支持 `~` 展开为用户 home 目录

### blocked_commands — 命令黑名单

阻止 `shell_execute` 执行包含指定前缀的命令：

```yaml
security:
  blocked_commands:
    - "rm -rf /"
    - "mkfs"
    - "dd if="
```

## 环境变量

### AI 配置

| 环境变量 | 对应参数 | 说明 |
|----------|----------|------|
| `AI_PROVIDER` | `--provider` | AI 服务商 |
| `AI_API_KEY` | `--api-key` | API 密钥 |
| `AI_BASE_URL` | `--base-url` | 自定义 API 地址 |
| `AI_MODEL` | `--model` | 模型名称 |
| - | `--instructions` | 自定义指令文件路径（追加到系统提示词） |
| `ANTHROPIC_API_KEY` | `--api-key` | API 密钥（fallback） |
| `ANTHROPIC_BASE_URL` | `--base-url` | API 地址（fallback） |
| `ANTHROPIC_MODEL` | `--model` | 模型名称（fallback） |

### Relay 配置

| 环境变量 | 对应参数 | 说明 |
|----------|----------|------|
| `RELAY_USER_ID` | `--user-id` | 用户 ID |
| `RELAY_PLATFORM` | `--platform` | 平台类型 |
| `RELAY_SERVER_URL` | `--server` | WebSocket 服务器地址 |
| `RELAY_WEBHOOK_URL` | `--webhook` | Webhook 地址 |

### 平台凭证

| 环境变量 | 对应参数 | 说明 |
|----------|----------|------|
| `WECOM_CORP_ID` | `--wecom-corp-id` | 企业微信 Corp ID |
| `WECOM_AGENT_ID` | `--wecom-agent-id` | 企业微信 Agent ID |
| `WECOM_SECRET` | `--wecom-secret` | 企业微信 Secret |
| `WECOM_TOKEN` | `--wecom-token` | 企业微信回调 Token |
| `WECOM_AES_KEY` | `--wecom-aes-key` | 企业微信 AES Key |
| `WECHAT_APP_ID` | `--wechat-app-id` | 微信公众号 App ID |
| `WECHAT_APP_SECRET` | `--wechat-app-secret` | 微信公众号 App Secret |
| `SLACK_BOT_TOKEN` | - | Slack Bot Token |
| `SLACK_APP_TOKEN` | - | Slack App Token |
| `FEISHU_APP_ID` | - | 飞书 App ID |
| `FEISHU_APP_SECRET` | - | 飞书 App Secret |
| `DINGTALK_CLIENT_ID` | - | 钉钉 Client ID |
| `DINGTALK_CLIENT_SECRET` | - | 钉钉 Client Secret |

### 搜索引擎配置

| 环境变量 | 对应参数 | 说明 |
|----------|----------|------|
| `METASO_API_KEY` | `--metaso-api-key` | 秘塔搜索 API Key |
| `TAVILY_API_KEY` | `--tavily-api-key` | Tavily 搜索 API Key |
| `SEARCH_ENGINE` | `--search-engine` | 主搜索引擎 |
| `AUTO_SEARCH` | `--auto-search` | 自动搜索（true/false） |

## 多模型配置

lingti-bot 支持配置多个 AI 模型，当主模型不可用时会自动切换到备用模型，提高服务稳定性。

### 配置说明

| 字段 | 说明 | 示例 |
|------|------|------|
| `provider` | AI 提供商 | `claude`, `deepseek`, `qwen` |
| `api_key` | API 密钥 | `sk-xxx` |
| `base_url` | 自定义 API 地址（可选） | `https://api.example.com` |
| `model` | 模型名称（可选） | `claude-sonnet-4-20250514` |
| `enabled` | 是否启用 | `true` / `false` |
| `priority` | 优先级（数字越小越优先） | `1`, `2`, `3` |

### 工作原理

1. 按 `priority` 排序尝试所有启用的模型
2. 当前模型失败时自动尝试下一个
3. 成功后记住当前模型，下次从这里开始
4. 所有模型都失败时返回错误

### 完整示例

```yaml
ai:
  models:
    - provider: claude
      api_key: sk-ant-xxx
      base_url: ""
      model: claude-sonnet-4-20250514
      enabled: true
      priority: 1
    - provider: deepseek
      api_key: sk-xxx
      base_url: ""
      model: deepseek-chat
      enabled: true
      priority: 2
    - provider: qwen
      api_key: sk-xxx
      base_url: ""
      model: qwen-plus
      enabled: true
      priority: 3
```

### 向后兼容

如果只配置了单个模型（`provider`, `api_key` 等），系统会自动将其作为唯一模型使用，保持完全兼容。

## 搜索引擎

项目支持多个搜索引擎，默认配置了 **秘塔搜索**（中文）和 **Tavily**（全球）。

### 配置方式

#### 1. 交互式配置（推荐）
```bash
lingti-bot onboard
```
向导会引导你完成搜索引擎配置（可选）。

#### 2. 命令行参数
```bash
# 配置秘塔搜索
lingti-bot --metaso-api-key=sk-xxx

# 配置 Tavily
lingti-bot --tavily-api-key=tvly-xxx

# 同时配置多个引擎
lingti-bot --metaso-api-key=sk-xxx --tavily-api-key=tvly-xxx --search-engine=metaso
```

#### 3. 手动编辑配置文件
编辑 `~/.lingti.yaml` 文件中的 `search` 部分。

### 搜索引擎说明

| 引擎 | 说明 | 适用场景 |
|------|------|----------|
| **metaso** | 秘塔搜索 | 中文内容搜索，中国大陆访问稳定 |
| **tavily** | Tavily 搜索 | 全球内容搜索，支持英文查询 |
| **custom_http** | 自定义 HTTP 搜索引擎 | 支持添加任意搜索引擎 |

### 搜索特性

- **智能引擎选择**：英文查询自动使用 Tavily，中文查询自动使用秘塔
- **多引擎搜索**：以"搜索"或"search"开头触发所有引擎搜索
- **自动搜索**：当无法回答时自动搜索（可配置）

### 使用示例

```bash
# 普通搜索（单引擎）
web_search: { "query": "Go 语言入门" }

# 多引擎综合搜索（触发词）
web_search: { "query": "搜索 AI 最新进展" }
web_search: { "query": "search latest AI news" }
```

## 典型用法

### 日常使用：配置文件

```bash
lingti-bot onboard        # 首次配置
lingti-bot relay           # 之后无需任何参数
```

### 临时覆盖：命令行参数

```bash
# 配置文件用 deepseek，临时切换到 qwen 测试
lingti-bot relay --provider qwen --model qwen-plus
```

### 容器部署：环境变量

```bash
docker run -e AI_PROVIDER=deepseek -e AI_API_KEY=sk-xxx lingti-bot relay
```

### 多实例运行：命令行参数覆盖

```bash
# 实例 1: 企业微信
lingti-bot relay --platform wecom --provider deepseek --api-key sk-aaa

# 实例 2: 飞书（不同 provider）
lingti-bot relay --platform feishu --user-id xxx --provider claude --api-key sk-bbb
```
