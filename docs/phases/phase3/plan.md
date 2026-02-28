# Phase 3 Plan - 长程记忆增强

## Status

- ✅ Completed on 2026-02-28
- ✅ Tests: `go test ./internal/agent` and `go test ./...`

## Scope

- 完成 Obsidian 文件监视与增量索引
- 实现上下文自动压缩（compaction）
- 维持现有时间衰减与 MMR 排序

## Exit Criteria

- vault 文件变更后可自动触发索引刷新
- 长对话超阈值时自动产出压缩摘要并继续对话
- 压缩后关键上下文可恢复

## Execution Loop

### 1. Test First

- 增加 fsnotify 文件变更索引测试
- 增加 compaction 触发阈值测试
- 增加 compaction 前后回答连续性测试

### 2. Develop

- 实现 Obsidian watch + 增量更新管线
- 引入消息压缩摘要存储与注入机制
- 增加压缩策略配置项

### 3. Test

- 运行 `go test ./internal/agent`
- 运行长会话回归脚本（含工具调用历史）

### 4. Docs

- 更新 memory 设计与配置文档
- 更新 `VISION.md` Phase 3 状态
