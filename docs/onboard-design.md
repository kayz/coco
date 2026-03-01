# onboard 设计说明（7阶段版）

## 1. 目标
让用户在只有 `coco.exe` 的情况下，通过一次 `coco onboard` 完成：
- 运行模式与 AI 基础能力
- 人格/用户/职责/心跳/记忆/工具文件初始化
- Obsidian 记忆库接入与索引生成
- Keeper 地址登记（不在向导内做联通测试）
- 工具能力可视化与烟雾测试
- 开机自启动配置

## 2. 阶段流程
1. `phase1-bootstrap`：包含 `phase0-runtime-mode` + `phase1-ai` + `phase1-runtime-config`
2. `phase2-persona-files`：生成 `SOUL/USER/IDENTITY/JD/HEARTBEAT/MEMORY/TOOLS` 及 `memory/*` 核心文件
3. `phase3-obsidian`：绑定 vault，写入索引文件（默认 `.coco/coco-index.md`）
4. `phase4-keeper`：登记 `keeper.base_url`（仅登记，不测试）
5. `phase5-tools`：导出内置工具目录，执行工具烟雾测试并生成报告
6. `phase6-autostart`：开机启动配置
7. `phase7-finish`：最终交接提示

## 3. 文件输出
- 配置文件：
  - `.coco.yaml`
  - `.coco/providers.yaml`
  - `.coco/models.yaml`
- 工作区契约文件：
  - `AGENTS.md`
  - `SOUL.md`
  - `USER.md`
  - `IDENTITY.md`
  - `JD.md`
  - `HEARTBEAT.md`
  - `MEMORY.md`
  - `TOOLS.md`
- 记忆核心文件：
  - `memory/MEMORY.md`
  - `memory/user_profile.md`
  - `memory/response_style.md`
  - `memory/project_context.md`
- 诊断文件：
  - `.coco/onboard/tool-smoke-report.md`

## 4. Keeper 处理策略（当前）
`onboard` 仅登记 `keeper.base_url`，不做连接测试、不上传 heartbeat。  
keeper 相关在线验证改为用户后续手工执行，保持安装路径简洁稳定。

## 5. 自动化能力
支持 `--non-interactive --set key=value` 全自动初始化。  
支持 `--workspace <path>` 指定人格与记忆文件写入目录。  
支持 `--skip-service` 跳过开机启动配置。

## 6. 兼容性与默认值
- `both` 模式默认本地环回：
  - `relay.server_url=ws://127.0.0.1:<keeper.port>/ws`
  - `relay.webhook_url=http://127.0.0.1:<keeper.port>/webhook`
- `HEARTBEAT.notify` 支持：`never|always|on_change|auto`
- Windows 使用启动目录 `.bat` 做自启动；Linux/Darwin 继续复用 service install/start。
