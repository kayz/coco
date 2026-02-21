# OpenClaw vs Lingti-Bot: Cron 系统对比分析

> 完整对比两个项目的定时任务系统差异

---

## 📊 总体对比

| 维度 | OpenClaw | Lingti-Bot | 结论 |
|------|----------|------------|------|
| **实现语言** | TypeScript | Go | ✅ Lingti 性能更好 |
| **持久化** | 未知（可能文件系统）| SQLite | ✅ Lingti 更可靠 |
| **精度** | 未知 | 秒级 | ✅ Lingti 更精细 |
| **单一二进制** | ❌ 需要 Node.js | ✅ Go 编译 | ✅ Lingti 更易部署 |

---

## 🔧 功能特性对比

### 1. 调度方式

| 功能 | OpenClaw | Lingti-Bot |
|------|----------|------------|
| **Cron 表达式** | ✅ | ✅ |
| **Heartbeat 短周期巡检** | ✅ | ❌ |
| **秒级精度** | ❓ | ✅ |
| **5 字段/6 字段兼容** | ❓ | ✅ |

**Lingti 实现**：
- 使用 `github.com/robfig/cron/v3` 库
- 支持秒级精度（`cron.WithSeconds()`）
- 自动兼容 5 字段和 6 字段 cron 表达式
- `internal/cron/scheduler.go:56-62` 实现了自动补秒功能

### 2. 任务类型

| 任务类型 | OpenClaw | Lingti-Bot |
|---------|----------|------------|
| **发送固定消息** | ❓ | ✅ |
| **执行 AI 对话（Prompt）** | ❓ | ✅ |
| **执行 MCP 工具** | ❓ | ✅ |
| **Heartbeat 主动唤醒** | ✅ | ❌ |

**Lingti 实现**：
- **Message-based job**：直接发送固定消息给用户
- **Prompt-based job**：执行完整的 AI 对话，每次生成新内容
- **Tool-based job**：执行 MCP 工具
- `internal/agent/cron_tools.go` 提供了完整的工具接口

### 3. 任务管理

| 功能 | OpenClaw | Lingti-Bot |
|------|----------|------------|
| **创建任务** | ✅ | ✅ |
| **删除任务** | ✅ | ✅ |
| **暂停任务** | ✅ | ✅ |
| **恢复任务** | ✅ | ✅ |
| **列出任务** | ✅ | ✅ |
| **查看任务状态** | ✅ | ✅ |
| **查看运行历史** | ❓ | ✅ (LastRun) |
| **错误记录** | ❓ | ✅ (LastError) |

**Lingti 实现**：
- `executeCronCreate` - 创建任务
- `executeCronList` - 列出所有任务
- `executeCronDelete` - 删除任务
- `executeCronPause` - 暂停任务
- `executeCronResume` - 恢复任务
- 每个任务记录：LastRun、LastError、Enabled 状态

### 4. 与 AI 集成

| 功能 | OpenClaw | Lingti-Bot |
|------|----------|------------|
| **通过对话创建任务** | ✅ | ✅ |
| **自然语言解析 cron** | ✅ | ❌ (需 AI 生成) |
| **任务执行结果通知** | ✅ | ✅ |
| **任务失败通知** | ✅ | ✅ |

**Lingti 实现**：
- AI 可以通过工具调用创建 cron 任务
- 提供了完整的工具 Schema（在 `internal/agent/tools.go` 中）
- 任务执行成功/失败都会通知用户

### 5. 持久化与恢复

| 功能 | OpenClaw | Lingti-Bot |
|------|----------|------------|
| **任务持久化** | ✅ | ✅ |
| **重启后自动恢复** | ✅ | ✅ |
| **存储介质** | ❓ | SQLite |
| **事务支持** | ❓ | ✅ |

**Lingti 实现**：
- `internal/cron/store.go` - SQLite 存储
- `internal/persist/` - 持久化层
- `Scheduler.Start()` - 启动时从数据库加载所有任务
- 每次任务执行后自动更新状态（LastRun、LastError）

---

## 🏗️ 架构对比

### OpenClaw 架构（推测）
```
OpenClaw Cron System
├── src/cron/              # Cron 调度器
├── src/heartbeat/         # Heartbeat 机制
├── HEARTBEAT.md           # 心跳配置文件
└── 集成到 Gateway/Agent 层
```

### Lingti-Bot 架构（实际）
```
Lingti-Bot Cron System
├── internal/
│   ├── cron/
│   │   ├── scheduler.go    # 调度器核心
│   │   ├── job.go          # Job 模型
│   │   └── store.go        # 持久化存储
│   ├── agent/
│   │   ├── cron_tools.go   # AI 工具接口
│   │   └── tools.go        # 工具定义
│   └── persist/            # 通用持久化层
└── 集成到 Agent 层（通过 Scheduler 接口）
```

---

## ❌ Lingti-Bot 缺失的功能

### 1. Heartbeat 短周期巡检
**OpenClaw 功能**：
- 短周期（如每分钟）唤醒 AI
- 检查是否有定时任务
- 类似 cron 但更灵活

**Lingti 当前状态**：❌ 缺失

**实现建议**：
- 可以用现有的 cron 系统模拟
- 或者添加简单的 ticker 机制
- 优先级：低（cron 已覆盖大多数场景）

### 2. 自然语言 Cron 解析
**OpenClaw 功能**：
- 用户说"每天早上 9 点"，自动解析为 cron
- 无需手动写 cron 表达式

**Lingti 当前状态**：⚠️ 部分支持（AI 生成，但可能不准确）

**实现建议**：
- 添加自然语言到 cron 的转换库
- 或增强 AI 的提示词，让它更准确地生成 cron
- 优先级：中

### 3. 任务执行日志历史
**OpenClaw 功能**：
- 记录每次任务的执行历史
- 可以查看过去 N 次的执行结果

**Lingti 当前状态**：⚠️ 只有 LastRun 和 LastError

**实现建议**：
- 添加 `JobExecution` 表记录历史
- 保留最近 100 次执行
- 优先级：低

---

## ✅ Lingti-Bot 已具备且优秀的功能

### 1. 三种任务类型
- Message-based：简单消息
- Prompt-based：AI 对话（每次生成新内容）
- Tool-based：执行 MCP 工具

### 2. 完整的任务管理
- 创建、删除、暂停、恢复、列出
- 状态跟踪（Enabled、LastRun、LastError）

### 3. 可靠的持久化
- SQLite 存储
- 重启后自动恢复
- 事务支持

### 4. 与 AI 无缝集成
- AI 可以通过工具创建任务
- 任务执行结果通知用户
- 失败时自动报错

### 5. 灵活的调度
- 秒级精度
- 兼容 5 字段和 6 字段 cron
- 标准 cron 表达式

---

## 🎯 结论与建议

### Lingti-Bot Cron 系统现状：✅ 基本完整！

**好消息**：Lingti-Bot 的 cron 系统已经非常完整，覆盖了 OpenClaw 的核心功能。

**缺失的功能**都是锦上添花，不是必需的：
- Heartbeat（可以用 cron 替代）
- 自然语言 cron 解析（AI 已经可以生成）
- 执行历史日志（LastRun/LastError 已够用）

### 建议优先做的改进：

1. **测试和文档**（最高优先级）
   - 测试 cron 系统的各种场景
   - 完善用户文档

2. **增强 AI 的 cron 生成能力**（中优先级）
   - 优化提示词，让 AI 更准确地生成 cron
   - 添加示例到提示词中

3. **添加简单的自然语言解析**（低优先级）
   - 可选，不是必需的

---

## 📝 附录：Lingti-Bot Cron 系统代码结构

### 核心文件

| 文件 | 功能 |
|------|------|
| `internal/cron/scheduler.go` | 调度器核心 |
| `internal/cron/job.go` | Job 数据模型 |
| `internal/cron/store.go` | SQLite 持久化 |
| `internal/agent/cron_tools.go` | AI 工具实现 |
| `internal/agent/tools.go` | 工具 Schema 定义 |

### 关键代码位置

- Cron 表达式兼容：`internal/cron/scheduler.go:56-62`
- 任务执行：`internal/cron/scheduler.go:294-422`
- AI 工具接口：`internal/agent/cron_tools.go`
- 启动/停止：`internal/cron/scheduler.go:64-102`

---

*最后更新：2026-02-21*
