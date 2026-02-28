# Phase 6 Plan - 生态与部署

## Status

- ✅ Completed on 2026-02-28
- ✅ Tests: `go test ./internal/agent ./internal/webui ./internal/deploy ./cmd` and `go test ./...`

## Scope

- Web UI 最小可用
- Docker 化部署
- 子 Agent 与 Agent 间通信基础能力

## Exit Criteria

- Web UI 可完成基础对话与状态查看
- Docker 镜像可一键启动 relay/keeper
- 子 Agent 会话可创建与发送消息

## Execution Loop

### 1. Test First

- 增加 Web API 与 UI 集成测试
- 增加容器启动健康检查测试
- 增加 sessions 通信单元测试

### 2. Develop

- 实现 WebChat 与状态接口
- 编写 Dockerfile 与 compose 模板
- 扩展 sessions_spawn/sessions_send

### 3. Test

- 运行 UI/API 自动化测试
- 运行容器化部署回归
- 运行 `go test ./...`

### 4. Docs

- 更新部署文档与 FAQ
- 更新 `VISION.md` Phase 6 状态
