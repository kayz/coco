# 启动项实现核查（coco）

核查时间：2026-02-27  
核查范围：`main.go`、`cmd/*`、`internal/{mcp,router,gateway,platforms/relay,service,voice}`、`Makefile`  
验证结果：`go test ./...`（设置 `GOCACHE=C:\git\coco\.gocache`）通过

## 启动项清单与实现状态

| 启动项 | 启动方式 | 入口代码 | 对应实现代码 | 实现结论 |
|---|---|---|---|---|
| 程序主入口（默认） | `coco` | `main.go` → `cmd.Execute()`；`cmd/root.go` | `rootCmd` 未定义 `Run/RunE`，默认行为是显示 help | **部分实现**：入口存在，但“默认本地助手模式”仅体现在文案，未进入实际会话/服务流程 |
| MCP 服务模式 | `coco serve` | `cmd/serve.go` (`serveCmd`) | `internal/mcp/server.go`（`NewServer` 注册工具、启动 cron；`ServeStdio` 提供 MCP stdio 服务） | **已实现** |
| 多平台路由模式 | `coco router` | `cmd/router.go` (`runRouter`) | `internal/router/router.go`（`Register/Start/Stop`）；各 `internal/platforms/*` 平台实例化后启动 | **已实现**（依赖平台 token/配置） |
| Keeper 服务模式 | `coco keeper` | `cmd/keeper.go` (`runKeeper`) | `newKeeperServer` + HTTP 路由（`/ws`、`/wecom`、`/webhook`、`/health`）+ WeCom 回调处理 + WebSocket 客户端管理 | **已实现**（依赖 keeper/wecom 配置） |
| 云中继客户端模式 | `coco relay` | `cmd/relay.go` (`runRelay`) | `internal/platforms/relay/relay.go`（鉴权、心跳、重连、消息收发、媒体处理）+ `router.Start` | **已实现** |
| WebSocket 网关模式 | `coco gateway` | `cmd/gateway.go` (`runGateway`) | `internal/gateway/gateway.go`（`Start` 启动 `/ws`、`/health`、`/status`，读写泵、鉴权、消息路由） | **已实现** |
| 语音对话模式 | `coco talk` | `cmd/talk.go` (`runTalk`) | `internal/voice/voice.go`（`TalkMode.Start`、`listenLoop`、STT/TTS） | **部分实现**：macOS/Linux 可运行；`voice.go` 的 `recordAudio/playAudio` 未覆盖 Windows 分支 |
| 回调验证兼容模式（已废弃） | `coco verify` | `cmd/verify.go` (`runVerify`) | 仍会创建 `relay.Platform` 并 `Start(ctx)` 执行验证流程 | **已实现（Deprecated）**：可用但官方建议改用 `relay` |
| 服务管理入口 | `coco service install/start/stop/restart/status` | `cmd/service.go` | `internal/service/manager.go`（安装、启停、状态、模板生成） | **部分实现**：仅 Darwin/Linux 支持，Windows 返回 unsupported platform |
| 系统自启动（由 service 安装生成） | `launchd/systemd` | `internal/service/manager.go` 模板 | `ProgramArguments/ExecStart` 均指向 `coco serve` | **已实现**（平台同上） |
| 开发启动入口 | `make run` | `Makefile` | `run` 目标实际执行 `go run . serve` | **已实现** |

## 结论

1. 核心启动链路（`serve/router/keeper/relay/gateway`）均有完整代码实现，并可通过编译测试。  
2. 主要差异点有 3 个：  
   - `coco` 默认模式文案与行为不一致（显示帮助，不进入“本地助手”运行态）。  
   - `coco talk` 在当前实现中对 Windows 不完整。  
   - `coco service` 仅支持 Darwin/Linux。  
3. 启动相关代码未发现明显 TODO/空壳入口（`verify` 属于“可运行但已废弃”状态）。
