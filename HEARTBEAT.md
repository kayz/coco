---
enabled: true
interval: 6h
checks:
 - name: memory-consistency
    prompt: |
      你正在执行心跳巡检（默认不主动打扰用户）。
      请检查最近记忆是否存在冲突、过期假设、未闭环事项。
      输出三段：
      1) 状态摘要
      2) 风险点
      3) 建议动作（最多 3 条）
    notify: never
---
# HEARTBEAT

说明：
- HEARTBEAT 主要用于“巡检”，不是每个心跳都主动对话
- `notify` 支持：`never`（默认）、`always`、`on_change`、`auto`
- 其中 `on_change` 仅在巡检结果出现变化时提醒；`auto` 由 coco 决定是否提醒
- HEARTBEAT 不负责人格成长写入；SOUL 变更必须由用户在对话中显式触发
- 若需要主动关怀，可添加一条独立任务并单独设置 schedule + notify
