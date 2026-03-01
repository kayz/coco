# Coco 人格化与记忆系统设计

## 目标

- 人格基线由 `SOUL.md` 驱动。
- 用户画像由 `USER.md` 驱动。
- 工作秘书职责由 `JD.md` 驱动。
- 记忆主库来自用户 Obsidian vault，可读可写。
- `HEARTBEAT.md` 负责周期巡检，不默认等价“每次主动对话”。

## 设计原则

1. 人格先于技巧  
- 系统提示优先加载 `SOUL.md`，确保语气、价值观、行为边界稳定。

2. 用户先于临时上下文  
- `USER.md` 记录稳定偏好、边界与长期背景，优先级高于碎片记忆。

3. 记忆即工作区  
- Obsidian 是事实与过程的主存储；用户与 coco 共用同一知识基座。

4. 时间加权 + 历史回响  
- 最近信息优先（衰减加权）。
- 对高相关历史信息保留“回响权重”，避免关键旧经验被淹没。

5. 心跳以巡检为主  
- 默认 `notify: never`，只做后台检查。
- `notify: on_change` 仅在结果变化时提醒用户。
- `notify: auto` 由 coco 在每次心跳后自行决策是否提醒。

6. 灵魂可成长，但需用户显式触发  
- `SOUL.md` 允许增长，但运行时仅追加，不覆盖历史条目。
- 用户可在系统外直接编辑。
- coco 在系统内仅可在“用户当前消息明确要求”时执行 `soul_append`。
- HEARTBEAT/cron 不得自动写入 SOUL。

## HEARTBEAT 约定

推荐使用 YAML frontmatter：

```yaml
---
enabled: true
interval: 6h
checks:
  - name: memory-consistency
    prompt: |
      检查近期记忆冲突、过期假设、未闭环事项。
    notify: never
  - name: key-change-alert
    prompt: |
      巡检是否有关键风险变化；有变化再提醒用户。
    notify: on_change
  - name: weekly-checkin
    schedule: "0 30 10 * * 1"
    prompt: |
      主动问候并确认本周最重要任务。
    notify: always
---
```

字段说明：

- `interval`: 全局默认间隔（会转为 `@every <interval>`）。
- `schedule`: 单任务 cron 表达式（优先于 interval）。
- `notify`: `never`（默认）/ `always` / `on_change` / `auto`。

## 已落地能力

- 新增 `memory_write` 工具：可在 Obsidian/核心记忆范围内 `append/overwrite`。
- 新增 `soul_append` 工具：仅追加人格成长记录，禁止运行时覆盖 SOUL；且必须用户显式触发。
- HEARTBEAT 任务自动注册为 cron prompt job。
- 记忆检索加入“历史回响”评分项，平衡近期优先与历史价值。
