# Phase 2 Plan - 外部 Agent 应用

## Status

- ✅ Completed on 2026-02-28
- ✅ Tests: `go test ./internal/cron ./internal/agent ./internal/router` and `go test ./...`

## Scope

- 支持 `cron.type=external`
- 定义外部应用调用协议（HTTP + bearer）
- 落地传话模式与来源标记隔离

## Exit Criteria

- 外部应用可被定时调度并返回结果
- `source: external-agent` 消息不会污染系统行为
- `spawn_agent` 最小可用（任务下发 + 状态回报）

## Execution Loop

### 1. Test First

- 增加外部 cron 配置解析测试
- 增加 external HTTP 调用超时/鉴权失败测试
- 增加传话模式隔离测试

### 2. Develop

- 实现 external cron 执行器
- 实现来源标记与 relay-pass-through 策略
- 增加 `spawn_agent` 协议层

### 3. Test

- 运行 `go test ./internal/cron ./internal/agent ./internal/router`
- 运行外部应用模拟端到端测试

### 4. Docs

- 更新外部应用协议文档
- 更新 `VISION.md` Phase 2 状态
