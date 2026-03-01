# 2026-03-01 能力缺口收敛记录

本次按“先不改 onboard 策略，其余缺口全部收敛”执行。

## 已完成

1. keeper 离线兜底升级  
- 原固定文案兜底升级为：优先低价 LLM 代答，失败或无 key 时自动回落固定文案。

2. 模型治理命令闭环  
- 新增：
  - `coco doctor models`
  - `coco doctor models --bench`
  - `coco models bench --disable-failures --disable-for 24h`
  - `coco models status`
  - `coco models disable|enable`

3. 模型临时下架能力  
- `models.yaml` 支持：
  - `enabled`
  - `disabled_until`
  - `disabled_reason`
- 路由阶段会跳过不可用模型。

4. API key 池  
- `providers.yaml` 新增可选 `api_keys`。
- 主对话/cron 默认稳定首 key，专家任务可轮换 key。

5. keeper 侧 cron 能力补齐  
- keeper 新增 `/api/cron/*`。
- relay/coco 侧新增 `relay.cron_on_keeper`，开启后 cron 管理工具优先走 keeper API。

6. phase2~phase7 验收文档补齐  
- 新增 `docs/phases/phase{2..7}/acceptance.md`。
