# Phase 1 Plan - 多模型路由

## Status

- ✅ Completed on 2026-02-28 (current code baseline)
- ✅ Tests: `go test ./internal/ai ./internal/agent` and `go test ./...`

## Scope

- 完善模型能力配置字段与应用级模型池
- 保持现有 ModelRouter/failover 能力
- 打通 coco/keeper/cron 维度的模型选择边界

## Exit Criteria

- 配置可声明各应用可用模型列表
- failover 仅在对应应用模型池内发生
- AI 切模工具与系统提示一致

## Execution Loop

### 1. Test First

- 为应用级模型池增加单元测试
- 增加 failover 边界测试（不得越池）
- 增加模型配置解析与校验测试

### 2. Develop

- 扩展 `models.yaml` 与运行时路由映射
- 增加应用维度模型约束
- 保留现有冷却策略与统计逻辑

### 3. Test

- 运行 `go test ./internal/ai ./internal/agent`
- 执行端到端切模回归（包含工具调用）

### 4. Docs

- 更新 `VISION.md` Phase 1 状态
- 补充模型配置示例与迁移说明
