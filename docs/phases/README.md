# Phase Plans

本目录维护 `VISION.md` 对应的逐阶段执行计划。所有 Phase 统一遵循同一执行闭环：

1. 先准备测试代码（单元测试/集成测试/验收脚本）
2. 再进行开发实现
3. 开发后执行阶段测试 + 回归测试
4. 测试通过后更新文档

建议执行顺序：

1. `phase0/plan.md`（已完成项对齐与回归）
2. `phase1/plan.md`
3. `phase2/plan.md`
4. `phase3/plan.md`
5. `phase4/plan.md`
6. `phase5/plan.md`
7. `phase6/plan.md`
8. `phase7/plan.md`

通用回归命令：

```bash
go test ./...
```
