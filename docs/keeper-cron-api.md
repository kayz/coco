# Keeper Cron API

用于把调度任务放到 keeper 侧执行（适合 cron/heartbeat 高频场景）。

## 前提

- keeper 已启动并可访问。
- 若配置了 `keeper.token`，调用方需携带：
  - `Authorization: Bearer <token>` 或
  - `X-Keeper-Token: <token>`

## 接口

1. `POST /api/cron/create`
2. `GET /api/cron/list`
3. `POST /api/cron/delete`
4. `POST /api/cron/pause`
5. `POST /api/cron/resume`

## create 示例

```json
{
  "name": "daily-check",
  "tag": "user-schedule",
  "type": "prompt",
  "schedule": "0 0 9 * * *",
  "prompt": "每天早上给用户一条当日重点提醒",
  "platform": "wecom",
  "channel_id": "kayz",
  "user_id": "kayz"
}
```

支持类型：
- `prompt`
- `message`
- `tool`
- `external`（传 `endpoint`）

## relay/coco 侧接入

在 `.coco.yaml` 开启：

```yaml
relay:
  cron_on_keeper: true
```

开启后，`cron_create/list/delete/pause/resume` 会优先调用 keeper API。
