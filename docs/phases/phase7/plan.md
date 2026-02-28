# Phase 7 Plan - 工作区产品化

## Status

- ✅ Completed on 2026-02-28
- ✅ Tests: `go test ./internal/agent ./internal/promptbuild` and `go test ./...`

## Scope

- 引入工作区文件契约（AGENTS/SOUL/PROFILE/MEMORY/HEARTBEAT/BOOTSTRAP）
- PromptBuild 直接消费工作区文件
- 打通“基础 onboarding + 人格 bootstrap”双阶段流程

## Exit Criteria

- 新工作区可自动生成并可持续编辑
- 系统提示由工作区文件驱动并可热更新
- 首次启动可完成人格与用户画像初始化

## Execution Loop

### 1. Test First

- 增加工作区文件装配测试
- 增加缺失文件降级与必需文件校验测试
- 增加 bootstrap 生命周期测试

### 2. Develop

- 实现工作区模板初始化
- 实现 PromptBuild 工作区输入层
- 实现 HEARTBEAT 与 BOOTSTRAP 运行时钩子

### 3. Test

- 运行 `go test ./internal/agent ./internal/promptbuild`
- 运行首次启动到稳定运行的端到端脚本

### 4. Docs

- 更新用户指南（工作区文件说明）
- 更新 `VISION.md` Phase 7 状态
