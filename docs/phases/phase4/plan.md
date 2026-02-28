# Phase 4 Plan - Skills 安全与扩展

## Status

- ✅ Completed on 2026-02-28
- ✅ Tests: `go test ./internal/skills ./cmd` and `go test ./...`

## Scope

- 增加 skills 安装前安全评估
- 实现 `coco skill install/search`
- 建立技能包发布与本地管理流程

## Exit Criteria

- skill 安装前自动输出安全评级与原因
- CLI 支持搜索、安装、启停、升级
- 技能来源、版本、签名元数据可追踪

## Execution Loop

### 1. Test First

- 增加 skills 元数据校验测试
- 增加安全评估结果解析测试
- 增加 install/search CLI 行为测试

### 2. Develop

- 实现技能仓库接口与本地缓存
- 实现安装前评估流水线
- 扩展技能管理命令

### 3. Test

- 运行 `go test ./internal/skills ./cmd`
- 运行 skills 安装回归场景

### 4. Docs

- 更新 skills 使用文档与安全说明
- 更新 `VISION.md` Phase 4 状态
