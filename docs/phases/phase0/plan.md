# Phase 0 Plan - 双 Agent 架构骨架

## Status

- ✅ Completed on 2026-02-28 (baseline verification)
- ✅ Tests: `go test ./...`

## Scope

本阶段已交付，当前目标是保持稳定并补齐回归测试。

## Exit Criteria

- keeper/relay/both 三种模式启动路径可用
- WebSocket 转发链路回归通过
- 离线兜底逻辑可验证

## Execution Loop

### 1. Test First

- 增加启动参数与配置读取测试
- 增加 keeper 与 relay 链路最小集成测试脚本

### 2. Develop

- 仅修复回归问题，不引入新特性

### 3. Test

- 执行 phase0 验收脚本
- 执行 `go test ./...`

### 4. Docs

- 更新 `docs/phase0-verification.md`
- 更新 `VISION.md` Phase 0 状态日期
