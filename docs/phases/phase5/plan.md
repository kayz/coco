# Phase 5 Plan - 安全与完善

## Status

- ✅ Completed on 2026-02-28
- ✅ Tests: `go test ./internal/security ./internal/config ./internal/tools ./internal/agent` and `go test ./...`

## Scope

- 完成 exec 审批交互闭环
- 完成 DM 白名单与群组 mention gating
- 补齐 SSRF 防护与全局配置热重载

## Exit Criteria

- 高风险命令需显式确认后执行
- 非白名单来源与无 mention 的群消息可控
- `web_fetch` 具备基础 SSRF 拦截
- config 变更可热生效（security/channels/model/search）

## Execution Loop

### 1. Test First

- 增加命令审批流程测试
- 增加 allowFrom / mention gating 测试
- 增加 SSRF 拦截单测与集成测试
- 增加配置热重载测试

### 2. Develop

- 扩展确认 token/一次性确认机制
- 接入平台侧来源校验
- 实现 URL 安全检查
- 扩展热重载 watcher 覆盖范围

### 3. Test

- 运行 `go test ./internal/...`
- 运行平台回归脚本（relay/keeper）

### 4. Docs

- 更新安全策略文档与运维 runbook
- 更新 `VISION.md` Phase 5 状态
