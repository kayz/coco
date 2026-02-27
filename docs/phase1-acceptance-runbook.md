# Phase 1 验收执行手册

> 用途：按顺序执行 Phase 1 验收，减少遗漏
> 参考：`docs/phase1-acceptance-plan.md`

---

## 0. 验收环境信息记录

- 验收日期：`_______`
- 验收人：`_______`
- 运行环境：`Windows / Linux / macOS`
- 二进制版本：`coco ______`

---

## 1. 预检查（5 分钟）

1. 构建可执行文件：

```bash
go build -o coco.exe
```

2. 查看命令面（应包含 `relay/keeper/both/onboard`）：

```bash
coco --help
```

3. 运行单元/编译检查：

```bash
go test ./...
```

记录：
- [ ] 通过
- [ ] 失败（错误：`_______`）

---

## 2. onboard 配置验收（10-15 分钟）

### 2.1 非交互式

执行一次非交互式 onboarding（示例）：

```bash
coco onboard \
  --mode relay \
  --non-interactive \
  --skip-service \
  --set ai.provider=deepseek \
  --set ai.api_key=sk-xxx \
  --set ai.base_url=https://api.deepseek.com/v1 \
  --set ai.model.primary=deepseek-chat \
  --set ai.model.fallback=deepseek-reasoner \
  --set relay.platform=wecom \
  --set relay.user_id=wecom-xxx \
  --set relay.server_url=wss://your-keeper/ws \
  --set relay.webhook_url=https://your-keeper/webhook \
  --set relay.use_media_proxy=no
```

检查输出与文件：
- [ ] `.coco.yaml`
- [ ] `.coco/providers.yaml`
- [ ] `.coco/models.yaml`

### 2.2 交互式

```bash
coco onboard
```

- [ ] 交互流程完整
- [ ] 关键字段可输入/回显默认值

---

## 3. 配置加载与命令入口验收（10 分钟）

1. 默认入口（应进入 relay 逻辑）：

```bash
coco
```

2. 三模式入口：

```bash
coco relay  --help
coco keeper --help
coco both   --help
```

3. 旧命令应不可用：

```bash
coco router
coco gateway
coco talk
coco service
coco verify
```

记录：
- [ ] 默认入口行为正确
- [ ] 三模式入口可用
- [ ] 旧命令均已移除

---

## 4. AI 路由能力验收（20-30 分钟）

在实际消息链路中触发模型工具（建议通过你们真实渠道对话）：

1. 触发 `ai.list_models`
2. 触发 `ai.get_current_model`
3. 触发 `ai.switch_model`（正常切换）
4. 触发 `ai.switch_model` 到不存在模型（错误提示）

记录：
- [ ] `list_models` 正常
- [ ] `get_current_model` 正常
- [ ] `switch_model` 正常
- [ ] 错误场景提示清晰

---

## 5. Failover 与冷却验收（20-30 分钟）

1. 让当前模型失败（例如临时改错 API key）
2. 发起请求，观察是否自动 failover
3. 冷却期内重复请求，确认失败模型被跳过
4. 等待冷却过期后再测，确认模型恢复可选

记录：
- [ ] 自动 failover 生效
- [ ] failover 仅在 models.yaml 范围内
- [ ] 冷却期内跳过失败模型
- [ ] 冷却后恢复

---

## 6. 服务化验收（15-20 分钟）

### 6.1 Linux/macOS 原生服务化（按需）

分别验证三种模式（原生命令）：

```bash
coco relay  --service install
coco relay  --service status
coco relay  --service start
coco relay  --service stop

coco keeper --service install
coco keeper --service status

coco both   --service install
coco both   --service status
```

记录：
- [ ] relay 服务化通过
- [ ] keeper 服务化通过
- [ ] both 服务化通过

### 6.2 Windows 常驻化（当前方案）

Windows 当前不支持项目内置 `--service` manager，采用用户级常驻方案验收：

1. 启动守护脚本：

```powershell
pwsh -NoProfile -ExecutionPolicy Bypass -File scripts/relay-service.ps1 -Token <relay-token>
```

2. 验证 relay 连接与守护重启：

- [ ] relay 成功连接 keeper（日志有 `Authenticated` / `Connected`）
- [ ] 结束 relay 进程后可自动重启
- [ ] 日志文件 `relay-service.log` 可持续输出

3. 验证登录自启配置（如启用）：

```powershell
Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run' -Name CocoRelayService
```

- [ ] 自启动项存在
- [ ] 值指向 `scripts/relay-service.ps1`

---

## 7. 稳定性抽测（10-15 分钟）

- 连续发送 10 条请求
- 检查进程无崩溃、日志无致命错误

记录：
- [ ] 10 次连续请求稳定

---

## 8. 验收结论

- P0 通过项数：`___ / ___`
- P1 通过项数：`___ / ___`
- 结论：
  - [ ] 通过
  - [ ] 不通过

问题清单：
1. `_______`
2. `_______`

---

## 9. 2026-02-27 实测记录（已执行）

1. 服务器 keeper 已完成二进制升级并切换为 `systemd --user` 托管
2. 本地 relay 已完成 Windows 用户级常驻化（守护脚本 + Run 自启动）
3. 企业微信链路实测通过：
   - 问答消息成功往返
   - `ai.get_current_model`、`ai.switch_model` 实际生效
