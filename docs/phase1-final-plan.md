# Phase 1 最终方案：多模型路由

> 最后更新：2026-02-27
> 方案确定，开始编码
> 架构师评价：✅ 通过，详见下方
> 实现状态：✅ 已完成（含联调与部署验证）

---

## 架构师评价摘要

**整体评价**：方案设计合理、思路清晰，与项目愿景高度一致，具备良好的扩展性。

**关键改进建议**（已采纳）：
1. ✅ 冷却时间可配置（在 `config` 中增加 `ModelCooldown`）
2. ✅ Failover 策略：同智力优先 → 同速度优先 → 降级
3. ✅ `SwitchToModel` 检查冷却状态
4. ✅ 实施顺序：先 registry/router，再集成

---

## 实现记录

**完成日期**：2026-02-27

**实际实现调整**：
1. ❌ 未独立 `pool.go` - Provider 缓存逻辑集成在 `agent.go` 中
2. ❌ `NewModelRouter` 签名简化 - 不需要 `pool` 参数
3. ❌ `GetProviderForModel` 在 `Agent` 中实现，而非 `ModelRouter`
4. ✅ `ai.switch_model` 增加 `force` 参数（可选，强制切换忽略冷却）

---

## 一、核心设计原则

1. **配置分离**：`providers.yaml`（敏感信息，不进 Git）+ `models.yaml`（可公开）
2. **AI 自主决策**：系统提示注入模型清单，由 coco 自主选择模型
3. **无硬编码路由**：没有内置应用路由，完全由 AI 根据任务和模型能力决定
4. **简单工具**：提供 `list_models` / `switch_model` / `get_current_model` 工具

---

## 二、配置文件

### 2.1 `.coco/providers.yaml`（不进 Git）

管理 API URL 和 API Key。

```yaml
providers:
  - name: openai
    type: openai
    base_url: "https://api.openai.com/v1"
    api_key: "${OPENAI_API_KEY}"

  - name: deepseek
    type: deepseek
    base_url: "https://api.deepseek.com/v1"
    api_key: "${DEEPSEEK_API_KEY}"

  - name: qwen
    type: qwen
    base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1"
    api_key: "${QWEN_API_KEY}"

  - name: kimi
    type: kimi
    base_url: "https://api.moonshot.cn/v1"
    api_key: "${KIMI_API_KEY}"

  - name: claude
    type: claude
    base_url: "https://api.anthropic.com/v1"
    api_key: "${ANTHROPIC_API_KEY}"
```

### 2.2 `.coco/models.yaml`（可公开）

每个模型的能力配置。

```yaml
models:
  - name: gpt-4o
    code: gpt-4o
    provider: openai
    intellect: full
    speed: fast
    cost: high
    skills: [multimodal, thinking]

  - name: claude-sonnet-4-5
    code: claude-sonnet-4-20250514
    provider: claude
    intellect: full
    speed: fast
    cost: high
    skills: [multimodal, thinking]

  - name: deepseek-chat
    code: deepseek-chat
    provider: deepseek
    intellect: excellent
    speed: fast
    cost: medium
    skills: [thinking]

  - name: deepseek-r1
    code: deepseek-reasoner
    provider: deepseek
    intellect: full
    speed: medium
    cost: medium
    skills: [thinking]

  - name: qwen-plus
    code: qwen-plus
    provider: qwen
    intellect: excellent
    speed: fast
    cost: medium
    skills: []

  - name: qwen-vl-plus
    code: qwen-vl-plus
    provider: qwen
    intellect: excellent
    speed: fast
    cost: medium
    skills: [multimodal]

  - name: qwen2-7b-instruct
    code: qwen2-7b-instruct
    provider: qwen
    intellect: good
    speed: fast
    cost: low
    skills: []

  - name: kimi-lite
    code: moonshot-v1-8k
    provider: kimi
    intellect: good
    speed: fast
    cost: low
    skills: []
```

### 2.3 能力枚举

**智力（intellect）**
- `full`：满分
- `excellent`：优秀
- `good`：良好
- `usable`：可用

**速度（speed）**
- `fast`：快
- `medium`：中
- `slow`：慢

**费用（cost）**
- `expensive`：贵
- `high`：高
- `medium`：中
- `low`：低
- `free`：免费

**技能标签（skills）**
- `thinking`：思维链
- `multimodal`：多模态
- `asr`：语音识别
- `t2i`：文生图
- `i2v`：图生视频
- `local`：本地运行

---

## 三、模块设计

### 3.1 目录结构

```
internal/ai/
├── registry.go    # ProviderRegistry + ModelRegistry
└── router.go      # ModelRouter（选择模型 + failover + 冷却）

internal/agent/
└── agent.go       # Provider 缓存逻辑（getProviderForModel + providerCache）
```

### 3.2 `registry.go`

```go
package ai

type ProviderConfig struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	BaseURL  string `yaml:"base_url"`
	APIKey   string `yaml:"api_key"`
}

type ModelConfig struct {
	Name      string   `yaml:"name"`
	Code      string   `yaml:"code"`
	Provider  string   `yaml:"provider"`
	Intellect string   `yaml:"intellect"`
	Speed     string   `yaml:"speed"`
	Cost      string   `yaml:"cost"`
	Skills    []string `yaml:"skills"`
}

func (m *ModelConfig) IntellectText() string
func (m *ModelConfig) SpeedText() string
func (m *ModelConfig) CostText() string
func (m *ModelConfig) SkillsText() string
func (m *ModelConfig) IntellectRank() int

type Registry struct {
	providers map[string]*ProviderConfig
	models    map[string]*ModelConfig
}

func LoadRegistry() (*Registry, error)
func (r *Registry) GetProvider(name string) (*ProviderConfig, bool)
func (r *Registry) GetModel(name string) (*ModelConfig, bool)
func (r *Registry) ListModels() []*ModelConfig
```

### 3.3 `router.go`

```go
package ai

type ModelRouter struct {
	registry      *Registry
	currentModel  *ModelConfig
	failoverStats map[string]*ModelStats
	cooldowns     map[string]time.Time
	cooldownTime  time.Duration
	mu            sync.RWMutex
}

type ModelStats struct {
	successCount int
	failureCount int
	lastSuccess  time.Time
	lastFailure  time.Time
}

func NewModelRouter(registry *Registry, cooldownTime time.Duration) *ModelRouter

func (r *ModelRouter) ListModels() []*ModelConfig
func (r *ModelRouter) GetCurrentModel() *ModelConfig
func (r *ModelRouter) SwitchToModel(name string, force bool) error
func (r *ModelRouter) RecordSuccess(model *ModelConfig)
func (r *ModelRouter) RecordFailure(model *ModelConfig)
func (r *ModelRouter) Failover() (*ModelConfig, error)
func (r *ModelRouter) IsInCooldown(modelName string) bool
func (r *ModelRouter) FormatModelsPrompt() string
```

**Failover 策略**：
1. 优先选择与当前模型**同智力等级**的模型
2. 在同智力等级内，优先选择**同速度**的模型
3. 无可用模型时，**降级**到下一智力等级
4. 跳过冷却中的模型

### 3.4 Provider 缓存（在 `agent.go` 中）

```go
// 在 Agent 结构体中
providerCache map[string]Provider  // key: "provider:modelCode"
providerMu    sync.RWMutex

func (a *Agent) getProviderForModel(model *ai.ModelConfig) (Provider, error)
func (a *Agent) createProvider(cfg *ai.ProviderConfig, modelCode string) (Provider, error)
```

---

## 四、系统提示注入

在 coco 的系统提示中添加以下内容：

```
## 可用模型

以下是你可以使用的 AI 模型及其能力：

{{range .Models}}
- {{.Name}}
  - 智力：{{.IntellectText}}
  - 速度：{{.SpeedText}}
  - 费用：{{.CostText}}
  - 能力：{{.SkillsText}}
{{end}}

## 选择模型原则

1. 普通聊天：优先用"优秀"或"满分"模型，需要多模态时用支持多模态的
2. 简单任务（cron/心跳）：用"快"且"低费用"的模型
3. 复杂思考：用带"思维链"能力的模型
4. 图片/文档理解：用支持"多模态"的模型
5. 你可以通过 `ai.switch_model` 工具切换模型
6. 如果当前模型失败，会自动 failover 到下一个可用模型
```

---

## 五、AI 工具

### 5.1 `ai.list_models`

列出所有可用模型。

```json
{
  "name": "ai.list_models",
  "description": "列出所有可用的 AI 模型及其能力",
  "input_schema": {
    "type": "object",
    "properties": {}
  }
}
```

### 5.2 `ai.switch_model`

切换到指定模型。

```json
{
  "name": "ai.switch_model",
  "description": "切换到指定的 AI 模型",
  "input_schema": {
    "type": "object",
    "properties": {
      "model_name": {
        "type": "string",
        "description": "模型名称，如 gpt-4o、deepseek-chat"
      },
      "force": {
        "type": "boolean",
        "description": "强制切换，忽略冷却状态（默认 false）"
      }
    },
    "required": ["model_name"]
  }
}
```

### 5.3 `ai.get_current_model`

获取当前使用的模型。

```json
{
  "name": "ai.get_current_model",
  "description": "获取当前使用的 AI 模型",
  "input_schema": {
    "type": "object",
    "properties": {}
  }
}
```

---

## 六、修改清单

### 6.1 新增文件

| 文件 | 说明 |
|------|------|
| `internal/ai/registry.go` | ProviderRegistry + ModelRegistry |
| `internal/ai/router.go` | ModelRouter（选择模型 + failover + 冷却） |

### 6.2 修改文件

| 文件 | 修改内容 |
|------|----------|
| `internal/config/config.go` | 删除 `AIConfig`、`ModelConfig`，新增 `ModelCooldown`（默认 5 分钟） |
| `internal/agent/agent.go` | 集成 `ModelRouter`，添加 `providerCache`、`getProviderForModel()`、`chatWithModel()`（含 failover），添加 AI 工具（`ai.list_models`/`ai.switch_model`/`ai.get_current_model`） |
| `cmd/relay.go` | 移除 `--provider/--api-key/--model/--base-url` flags |
| `cmd/onboard.go` | 重写为步骤化向导：生成 `providers.yaml` + `models.yaml` + 模式配置（relay/keeper/both） |
| `cmd/root.go` | 命令面精简为 `relay/keeper/both/onboard`，默认运行 `relay` |
| `cmd/keeper.go` | 增加 `--service` 长期运行入口 |
| `internal/service/manager.go` | 服务化改为按模式管理（relay/keeper/both） |

### 6.3 删除文件

| 文件 | 说明 |
|------|------|
| `cmd/voice.go` | 旧 voice 命令（已整合） |
| `cmd/router.go` | 命令面精简后移除 |
| `cmd/gateway.go` | 命令面精简后移除 |
| `cmd/talk.go` | 命令面精简后移除 |
| `cmd/service.go` | 迁移为各模式 `--service` 入口 |
| `cmd/verify.go` | 已废弃命令移除 |
| `docs/phase1-model-routing.md` | 旧方案文档 |
| `docs/phase1-implementation-blueprint.md` | 旧方案文档 |
| `Plan.md` | 旧计划文档 |

---

## 七、验收标准

1. `providers.yaml` 和 `models.yaml` 成功加载
2. coco 系统提示中包含可用模型列表
3. `ai.list_models` / `ai.switch_model` / `ai.get_current_model` 工具可用
4. 模型切换功能正常
5. failover 机制正常（失败后自动切换）
6. 冷却机制正常（失败的模型短期不使用）

---

## 八、实施顺序

**实际实施顺序**：
1. 创建 `internal/ai/` 目录和 `registry.go`、`router.go`
2. 修改 `internal/config/config.go`
3. 修改 `internal/agent/agent.go`（集成 ModelRouter + Provider 缓存 + AI 工具）
4. 修改 `cmd/*.go`（移除旧 flags）
5. 重写 `cmd/onboard.go`
6. 测试验证

---

## 九、实际配置示例

### `.coco/providers.yaml` 示例

```yaml
providers:
  - name: siliconflow
    type: openai
    base_url: "https://api.siliconflow.cn/v1"
    api_key: "sk-xxx"

  - name: deepseek
    type: deepseek
    base_url: "https://api.deepseek.com"
    api_key: "sk-xxx"

  - name: zhipu
    type: zhipu
    base_url: "https://open.bigmodel.cn/api/paas/v4"
    api_key: "xxx"

  - name: moonshot
    type: kimi
    base_url: "https://api.moonshot.cn"
    api_key: "sk-xxx"

  - name: alibaba
    type: qwen
    base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1"
    api_key: "sk-xxx"
```

### `.coco/models.yaml` 示例

```yaml
models:
  - name: qwen3-8b-free
    code: Qwen/Qwen3-8B
    provider: siliconflow
    intellect: good
    speed: fast
    cost: free
    skills: []

  - name: deepseek-v3-2
    code: Pro/deepseek-ai/DeepSeek-V3.2
    provider: siliconflow
    intellect: excellent
    speed: fast
    cost: medium
    skills: [thinking]

  - name: deepseek-chat
    code: deepseek-chat
    provider: deepseek
    intellect: excellent
    speed: fast
    cost: medium
    skills: [thinking]

  - name: deepseek-reasoner
    code: deepseek-reasoner
    provider: deepseek
    intellect: full
    speed: medium
    cost: medium
    skills: [thinking]
```

---

## 十、2026-02-27 联调与部署变更

### 10.1 线上 Keeper 升级与服务化

1. 服务器：`47.100.66.40`（`deploy` 用户，目录 `~/projects/coco`）
2. 本地编译 Linux 二进制并上传替换 Keeper 运行文件
3. 由 `nohup` 方式切换为 `systemd --user` 托管：
   - 单元文件：`~/.config/systemd/user/coco-keeper.service`
   - 启动命令：`/home/deploy/projects/coco/coco keeper --port 8080 --log info`
4. 验证结果：
   - `systemctl --user is-active coco-keeper.service` => `active`
   - `curl http://127.0.0.1:8080/health` 正常返回
   - 企业微信消息链路可达

### 10.2 本地 Relay 常驻化（Windows）

1. 当前代码原生 `--service` 仅支持 Linux/macOS
2. Windows 采用用户级常驻方案：
   - 守护脚本：`scripts/relay-service.ps1`
   - 登录自启：`HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run` -> `CocoRelayService`
3. 守护策略：
   - 拉起 `coco relay`
   - 进程退出后延迟重启（默认 5 秒）
   - 输出写入 `relay-service.log`

### 10.3 联调结论

1. Keeper <-> Relay WebSocket 连通稳定
2. 企业微信多轮对话成功
3. 模型查询与切换工具可用（示例：切换到低成本模型）
