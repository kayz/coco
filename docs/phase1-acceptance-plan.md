# Phase 1 验收方案（当前分支版）

> 最后更新：2026-02-27
> 目标：按当前代码结构执行 Phase 1 多模型路由验收
> 说明：当前命令面已精简为 `relay / keeper / both / onboard`

---

## 一、验收前准备

### 1.1 环境与构建

```bash
go build -o coco.exe
```

### 1.2 使用 onboard 生成配置（推荐）

交互式：

```bash
coco onboard
```

非交互式（示例）：

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

### 1.3 最低配置要求

- `.coco/providers.yaml` 至少 2 个 provider
- `.coco/models.yaml` 至少 3 个模型
- 若测试 `failover`，至少保证 2 个模型在失败时可被切换

---

## 二、验收项

### 2.1 Onboard 验收

| 验收项 | 验收标准 | 验收方式 |
|--------|----------|----------|
| 交互式向导 | `coco onboard` 可完成配置 | 运行 `coco onboard` |
| 非交互式向导 | `--non-interactive --set` 可完成配置 | 运行非交互示例命令 |
| 写文件正确 | 生成 `.coco.yaml/.coco/providers.yaml/.coco/models.yaml` | 检查文件存在和内容 |
| 模式分支正确 | `relay/keeper/both` 条件提问正确 | 分别执行三种模式 |

### 2.2 配置加载验收

| 验收项 | 验收标准 | 验收方式 |
|--------|----------|----------|
| providers.yaml 加载 | 能读取 providers.yaml | 用有效参数运行 `coco relay ...`，不出现 registry 读取错误 |
| models.yaml 加载 | 能读取 models.yaml | 同上 |
| 配置缺失报错 | 缺文件/字段时有清晰错误 | 临时删除/改坏配置并运行 `coco relay ...` |

### 2.3 系统提示注入验收

| 验收项 | 验收标准 | 验收方式 |
|--------|----------|----------|
| 模型清单注入 | 系统提示包含所有可用模型 | 开启 debug 日志并抓取系统提示 |
| 模型能力显示 | 每个模型含智力/速度/费用/技能 | 检查系统提示文本 |
| 选择原则注入 | 包含模型选择原则 | 检查系统提示文本 |

### 2.4 AI 工具验收

| 验收项 | 验收标准 | 验收方式 |
|--------|----------|----------|
| `ai.list_models` | 返回所有可用模型 | 在对话中触发工具调用 |
| `ai.get_current_model` | 返回当前模型 | 在对话中触发工具调用 |
| `ai.switch_model` | 可切换模型 | 切换后再次查询当前模型 |
| `ai.switch_model(force)` | `force=true` 可忽略冷却切换 | 对冷却模型执行强制切换 |

### 2.5 模型切换与 Failover 验收

| 验收项 | 验收标准 | 验收方式 |
|--------|----------|----------|
| 切换成功 | 指定模型被实际使用 | 切换后连续发起对话验证 |
| 切换失败提示 | 不存在模型时提示清晰 | 切到不存在模型 |
| 自动 failover | 当前模型失败自动切换 | 让当前模型 API key 失效后发消息 |
| failover 范围 | 仅在 `models.yaml` 内切换 | 检查日志中的模型名 |
| 失败记录 | 连续失败后优先跳过失败模型 | 连续触发失败并观察顺序 |

### 2.6 冷却机制验收

| 验收项 | 验收标准 | 验收方式 |
|--------|----------|----------|
| 冷却生效 | 失败模型在冷却期内不再被选 | 触发失败后重复请求 |
| 冷却过期 | 冷却期后模型恢复可选 | 等待冷却时间后重试 |
| 冷却可配置 | 读取 `model_cooldown` 配置 | 调整配置后验证行为变化 |

### 2.7 命令入口验收（当前命令面）

| 验收项 | 验收标准 | 验收方式 |
|--------|----------|----------|
| 默认入口 | `coco` 等价执行 relay | 运行 `coco`，应进入 relay 流程 |
| `coco relay` | 能启动 relay 流程 | 运行 `coco relay` |
| `coco keeper` | 能启动 keeper 流程 | 运行 `coco keeper` |
| `coco both` | 能启动 keeper+relay 流程 | 运行 `coco both` |
| 旧入口已移除 | `router/gateway/talk/service/verify` 不再可用 | 运行旧命令，应报 unknown command |

### 2.8 服务化验收（三模式）

| 验收项 | 验收标准 | 验收方式 |
|--------|----------|----------|
| relay 服务化 | `relay --service install/start/status` 可用 | 逐项执行 |
| keeper 服务化 | `keeper --service install/start/status` 可用 | 逐项执行 |
| both 服务化 | `both --service install/start/status` 可用 | 逐项执行 |
| service 标识隔离 | 三模式 service id/配置路径互不冲突 | 检查生成的 launchd/systemd 条目 |
| Windows 常驻化 | 守护脚本 + 登录自启可保持 relay 在线 | `scripts/relay-service.ps1` + `HKCU\\...\\Run` |

### 2.9 集成与稳定性验收

| 验收项 | 验收标准 | 验收方式 |
|--------|----------|----------|
| 完整对话流程 | 完成多轮对话并可触发工具 | 在实际渠道进行多轮会话 |
| 模型自主选择 | 能依据任务类型选择模型 | 提交不同复杂度任务观察模型 |
| 稳定性 | 连续 10 次请求无崩溃 | 连续发送 10 条消息 |

---

## 三、验收通过标准

### 3.1 必须通过（P0）

- [ ] onboard 可生成可用配置（交互/非交互至少各通过 1 次）
- [ ] providers/models 配置加载正常
- [ ] `ai.list_models` / `ai.switch_model` / `ai.get_current_model` 可用
- [ ] 模型切换与 failover 正常
- [ ] `coco`、`relay`、`keeper`、`both` 命令入口正常
- [ ] 三模式服务化接口正常（至少 install + status 通过）
- [ ] 完整对话流程正常

### 3.2 应该通过（P1）

- [ ] 冷却机制正常
- [ ] 模型自主选择符合预期
- [ ] 连续 10 次请求无崩溃

---

## 四、验收执行顺序（建议）

1. 执行 `onboard` 生成配置  
2. 运行单条 `relay` 启动验证配置加载  
3. 执行 AI 工具 + 模型切换 + failover/cooldown 用例  
4. 进行 `relay/keeper/both` 命令入口验证  
5. 进行三模式服务化验证  
6. 做完整链路与稳定性测试  

---

## 五、验收记录模板

```text
Phase 1 验收记录（当前分支）
===========================

验收日期：___________
验收人：___________

P0 验收项：
[ ] onboard 交互/非交互通过
[ ] 配置文件加载通过
[ ] ai.list_models / ai.switch_model / ai.get_current_model 通过
[ ] 模型切换 + failover 通过
[ ] coco / relay / keeper / both 入口通过
[ ] relay/keeper/both 服务化通过
[ ] 完整对话流程通过

P1 验收项：
[ ] 冷却机制通过
[ ] 模型自主选择符合预期
[ ] 连续 10 次请求稳定

问题记录：
1. _________________________
2. _________________________

验收结论：[ ] 通过 / [ ] 不通过
```

---

## 六、回滚方案

```bash
git checkout v1.9.0
```

---

## 七、2026-02-27 联调结果补充

1. Keeper 已在 `47.100.66.40` 完成升级并改为 `systemd --user` 托管
2. 本地 relay 已采用 Windows 用户级常驻方案并验证在线
3. 企业微信多轮消息、模型查询与切换均通过
