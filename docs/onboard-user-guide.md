# onboard 用户手册

## 1. 命令概览

`onboard` 用于初始化 `relay / keeper / both` 模式配置，并可选安装长期服务。

```bash
coco onboard [flags]
```

常用参数：

- `--mode relay|keeper|both`：指定模式
- `--non-interactive`：非交互模式（缺失必填直接失败）
- `--set key=value`：预填配置（可重复）
- `--skip-service`：跳过服务安装/启动提问

## 2. 交互式快速开始

```bash
coco onboard
```

向导会按顺序询问：

1. 运行模式
2. AI provider / API key / 模型
3. 模式相关配置（relay 或 keeper 或 both）
4. 是否安装并启动服务

完成后会写入：

- `.coco.yaml`
- `.coco/providers.yaml`
- `.coco/models.yaml`

## 3. 模式说明

### 3.1 relay

适用于本地 relay 客户端运行。

最小关键项：
- `relay.platform`
- `relay.user_id`
- `relay.server_url`
- `relay.webhook_url`
- AI 配置

### 3.2 keeper

适用于公网 keeper 服务端运行（WeCom 回调）。

最小关键项：
- `keeper.port`
- `keeper.token`
- `keeper.wecom_corp_id`
- `keeper.wecom_agent_id`
- `keeper.wecom_secret`
- `keeper.wecom_token`
- `keeper.wecom_aes_key`（43 位）

### 3.3 both

适用于单机同时跑 keeper+relay。

默认会自动设置：
- `relay.server_url = ws://127.0.0.1:<keeper.port>/ws`
- `relay.webhook_url = http://127.0.0.1:<keeper.port>/webhook`
- `relay.token` 继承 `keeper.token`（如 relay token 留空）

## 4. 非交互模式示例

## 4.1 relay 示例

```bash
coco onboard \
  --mode relay \
  --non-interactive \
  --skip-service \
  --set ai.provider=deepseek \
  --set ai.api_key=YOUR_API_KEY \
  --set ai.base_url=https://api.deepseek.com/v1 \
  --set ai.model.primary=deepseek-chat \
  --set ai.model.fallback=deepseek-reasoner \
  --set relay.platform=wecom \
  --set relay.user_id=wecom-xxx \
  --set relay.server_url=wss://your-keeper.example.com/ws \
  --set relay.webhook_url=https://your-keeper.example.com/webhook \
  --set relay.use_media_proxy=no
```

## 4.2 keeper 示例

```bash
coco onboard \
  --mode keeper \
  --non-interactive \
  --skip-service \
  --set ai.provider=deepseek \
  --set ai.api_key=YOUR_API_KEY \
  --set ai.base_url=https://api.deepseek.com/v1 \
  --set ai.model.primary=deepseek-chat \
  --set keeper.port=8080 \
  --set keeper.token=YOUR_KEEPER_TOKEN \
  --set keeper.wecom_corp_id=wwxxxx \
  --set keeper.wecom_agent_id=1000001 \
  --set keeper.wecom_secret=xxxx \
  --set keeper.wecom_token=xxxx \
  --set keeper.wecom_aes_key=YOUR_43_CHAR_AES_KEY
```

## 4.3 both 示例

```bash
coco onboard \
  --mode both \
  --non-interactive \
  --skip-service \
  --set ai.provider=deepseek \
  --set ai.api_key=YOUR_API_KEY \
  --set ai.base_url=https://api.deepseek.com/v1 \
  --set ai.model.primary=deepseek-chat \
  --set keeper.port=8080 \
  --set keeper.token=YOUR_KEEPER_TOKEN \
  --set keeper.wecom_corp_id=wwxxxx \
  --set keeper.wecom_agent_id=1000001 \
  --set keeper.wecom_secret=xxxx \
  --set keeper.wecom_token=xxxx \
  --set keeper.wecom_aes_key=YOUR_43_CHAR_AES_KEY \
  --set relay.platform=wecom \
  --set relay.user_id=wecom-wwxxxx
```

## 5. `--set` 常用键

AI 相关：
- `ai.provider`
- `ai.provider_name`（custom）
- `ai.provider_type`（custom）
- `ai.api_key`
- `ai.base_url`
- `ai.model.primary`
- `ai.model.fallback`

relay 相关：
- `relay.platform`
- `relay.user_id`
- `relay.token`
- `relay.server_url`
- `relay.webhook_url`
- `relay.use_media_proxy`
- `relay.wecom_corp_id`（云 relay wecom 场景）
- `relay.wecom_agent_id`
- `relay.wecom_secret`
- `relay.wecom_token`
- `relay.wecom_aes_key`

keeper 相关：
- `keeper.port`
- `keeper.token`
- `keeper.wecom_corp_id`
- `keeper.wecom_agent_id`
- `keeper.wecom_secret`
- `keeper.wecom_token`
- `keeper.wecom_aes_key`

## 6. 服务化相关

向导默认会询问是否安装服务并可选立即启动。  
如果不希望在向导中处理，使用 `--skip-service`，之后手工执行：

```bash
coco relay  --service install
coco keeper --service install
coco both   --service install
```

其它动作：

```bash
coco <mode> --service start
coco <mode> --service stop
coco <mode> --service restart
coco <mode> --service status
coco <mode> --service uninstall
```

说明：当前服务管理实现仅支持 Darwin/Linux；Windows 会提示不支持。

## 7. 常见问题

### Q1: `wecom_aes_key` 校验失败

必须是 43 个字符，直接使用企业微信后台生成值。

### Q2: relay URL 校验失败

- `relay.server_url` 必须 `ws://` 或 `wss://`
- `relay.webhook_url` 必须 `http://` 或 `https://`

### Q3: `go run . onboard` 后找不到配置文件

`go run` 运行时可执行文件位于临时目录，配置会写入该临时目录。  
建议使用已构建二进制执行 `onboard`，如 `./coco onboard`。

### Q4: 非交互模式提示某字段缺失

给出对应 `--set key=value`，或去掉 `--non-interactive` 改为交互填充。
