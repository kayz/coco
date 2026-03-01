# 模型轮换策略（主 API 稳定 + cron 低价 + 专家池）

## 目标

- 主对话模型尽量稳定，不因一次失败就频繁切换。
- cron/heartbeat 默认走低成本模型。
- 专家任务（复杂推理）走专家模型池。

## 运行规则

1. 主对话（`primary`）
   - 正常请求优先使用当前主模型。
   - 失败后会尝试同类 failover（优先同类能力，如多模态/思维链）。
   - 只有在“连续失败达到阈值”后，才会将主模型真正轮换到备用模型。

2. cron/heartbeat（`cron`）
   - 优先选择低费用、快速度模型。
   - 不影响主模型稳定性。

3. 专家任务（`expert`）
   - 优先高智力、思维链模型池。
   - 用于 planner/final 等复杂任务阶段。

## 模型池维护方式

在 `.coco/models.yaml` 的模型项中维护 `roles`：

```yaml
models:
  - name: gpt-4o
    code: gpt-4o
    provider: openai
    intellect: full
    speed: fast
    cost: high
    skills: [multimodal, thinking]
    roles: [primary, expert]

  - name: qwen-turbo
    code: qwen-turbo
    provider: qwen
    intellect: good
    speed: fast
    cost: low
    roles: [cron]
```

说明：
- `roles` 可选值：`primary`、`cron`、`expert`
- 若未配置 `roles`，系统会按启发式自动选主模型/低价模型/专家模型
- `coco onboard` 生成 `models.yaml` 时会自动标注默认 `roles`

## API Key 池（专家任务）

`providers.yaml` 现支持单 key 与 key 池两种写法：

```yaml
providers:
  - name: qwen
    type: qwen
    base_url: https://dashscope.aliyuncs.com/compatible-mode/v1
    api_key: sk-primary-only

  - name: openai
    type: openai
    base_url: https://api.openai.com/v1
    api_keys:
      - sk-primary-stable
      - sk-expert-pool-1
      - sk-expert-pool-2
```

行为说明：
- `primary` / `cron`：默认稳定使用第一个 key，避免频繁切换导致风格波动。
- `expert`：若配置了 `api_keys` 多 key，会在专家请求中轮换 key，缓解单 key 限流。
- 若只配置 `api_key`，所有角色都使用该 key。

## 模型诊断与临时下架

新增命令：

```bash
coco doctor models
coco doctor models --bench
coco models bench --disable-failures --disable-for 24h
coco models status
coco models disable --name deepseek-chat --for 12h --reason "tool-call unstable"
coco models enable --name deepseek-chat
```

`models.yaml` 新字段：
- `enabled: false`：永久下架（直到手动 enable）
- `disabled_until: <RFC3339>`：临时下架，到期自动恢复可选
- `disabled_reason: ...`：记录原因，便于回溯

## Keeper 侧低价巡检（Heartbeat）

keeper 现在可在本地执行 HEARTBEAT 的定时任务（无需 coco 在线），并直接推送到企业微信用户。

需要在 `.coco.yaml` 配置 keeper 低价模型：

```yaml
keeper:
  default_provider: qwen
  default_base_url: https://dashscope.aliyuncs.com/compatible-mode/v1
  default_model: qwen-turbo
  default_api_key: YOUR_CHEAP_API_KEY
```

行为说明：
- keeper 启动后会拉起自己的定时调度器（独立 DB：`.coco-keeper.db`）
- 当收到某用户消息时，会按 `HEARTBEAT.md` 自动为该用户注册 heartbeat 任务
- 任务由 keeper 使用上述低价模型执行并直接推送
