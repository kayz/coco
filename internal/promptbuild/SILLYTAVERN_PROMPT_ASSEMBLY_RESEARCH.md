# SillyTavern Prompt 组装能力研究（面向 COCO 的可迁移设计）

> 目标：总结 SillyTavern 的 prompt 组装机制，并提炼适用于 COCO（多场景 Agent、低 UI 个性化、数据源复杂）的设计原则。

## 1. 核心结论

SillyTavern 的核心不是“拼字符串”，而是：

1. **分层 Prompt 源管理**（系统/角色/世界书/扩展/历史）
2. **可控注入顺序**（位置、深度、角色、优先级）
3. **Token 预算驱动**（先保留刚性块，再装入历史）
4. **事件 Hook 扩展点**（组装前后可被订阅改写）

这套机制非常适合迁移为 COCO 的“场景契约驱动 Prompt Assembler”。

---

## 2. SillyTavern 的关键实现路径

### 2.1 入口与主分叉

- 生成总入口在 `Generate()`：`public/script.js:5118`
- 关键分叉：
  - `main_api === 'openai'` → 走 **chat-completion 结构化 messages 路径**
  - 其他 API → 走 **text-completion 字符串拼接路径**

### 2.2 OpenAI/chat-completion 路径（推荐重点借鉴）

- 主入口：`prepareOpenAIMessages` `public/scripts/openai.js:1435`
- Prompt 合并：`preparePromptsForChatCompletion` `public/scripts/openai.js:1260`
- 上下文装配：`populateChatCompletion` `public/scripts/openai.js:1078`
- 历史注入（预算内）：`populateChatHistory` `public/scripts/openai.js:836`
- 示例注入：`populateDialogueExamples` `public/scripts/openai.js:994`
- 请求参数构建：`createGenerationParameters` `public/scripts/openai.js:2449`
- 请求发送：`sendOpenAIRequest` `public/scripts/openai.js:2820`

### 2.3 Text-completion 路径（字符串组装）

- 世界书、扩展注入、story string 组装：`public/script.js:4466`, `public/script.js:4553`
- 深度注入：`doChatInject` `public/script.js:5461`
- 最终 combine：`public/script.js:5015`

---

## 3. SillyTavern Prompt 组装模型（抽象）

可以抽象为四层：

1. **Source Layer**：角色卡、世界书、扩展、历史、用户输入
2. **PromptCollection Layer**：统一表示 Prompt 块（id/role/content/position/depth/order）
3. **Assembly Layer**：按顺序和预算组装为 messages 或字符串
4. **Transport Layer**：按模型供应商能力裁剪参数并发送

其中第二层由 PromptManager 抽象：

- Prompt 数据结构：`public/scripts/PromptManager.js:79`
- 注入位类型：`public/scripts/PromptManager.js:36`
- 集合能力（get/index/override）：`public/scripts/PromptManager.js:200`

---

## 4. 对 COCO 的适配判断

你们的场景和 ST 不同：

- ST：用户 UI 个性化强
- COCO：用户交互简化（对话窗），但 **Agent 场景更广、数据源更复杂**

因此应从“用户可配置 prompt”转为“**场景契约 prompt**”：

- 每个 Agent 对应一个固定组装协议（结构稳定）
- 数据源多样但统一到标准上下文
- 每次任务只变数据，不变组装骨架

---

## 5. 建议的 COCO Prompt Assembler 设计

## 5.1 设计原则

1. **Contract First**：每个 Agent 都有 prompt 合约（固定块、顺序、约束）
2. **Canonical Context**：所有输入先归一化，再装配
3. **Budget First**：刚性块先保留，证据块按价值裁剪
4. **Traceable**：每块标注来源（source id、时间、版本）
5. **Deterministic**：同输入应得到可重现输出（除随机抽样）

## 5.2 关键模块（建议）

1. `Source Adapters`
   - OCR/网页采集、本地文件、数据库、工具结果
2. `Normalizer`
   - 统一为 CanonicalContext（字段稳定）
3. `Assembler`
   - 根据 Agent 配置生成 PromptBlocks
4. `Budget Planner`
   - token 预算分配、截断、摘要
5. `Renderer`
   - 渲染为 chat messages 或 provider-specific payload
6. `Auditor`
   - 记录每次组装产物用于回放/排错

---

## 6. 研报 Agent 场景（你给的例子）

输入源：

1. 研报收集器（OCR + 网页）
2. 交易风格（本地文件）
3. 持仓（数据库）
4. 输出格式（模板文件）
5. 固定提示词

建议组装顺序（固定）：

1. `system_role`（角色与安全边界）
2. `task_objective`（本次任务目标）
3. `portfolio_constraints`（风格与持仓约束）
4. `evidence_digest`（结构化研报证据）
5. `decision_framework`（分析框架/打分法）
6. `output_contract`（输出格式与必填字段）
7. `self_check`（冲突检查、置信度、缺失项）

---

## 7. Agent 配置示意（可落地）

```yaml
agent: daily_research_analyst
model_policy:
  primary: deep_think_model
  fallback: fast_model

sources:
  - id: research_digest
    required: true
    adapter: report_collector
  - id: trading_style
    required: true
    adapter: local_file
  - id: current_positions
    required: true
    adapter: db_query
  - id: output_format_spec
    required: true
    adapter: template_file
  - id: analysis_instruction
    required: true
    adapter: static_text

assembly:
  - block: system_role
    priority: 100
    required: true
  - block: task_objective
    priority: 90
    required: true
  - block: portfolio_constraints
    from: [trading_style, current_positions]
    priority: 80
  - block: evidence_digest
    from: [research_digest]
    priority: 70
    truncation: summarize_then_rank
  - block: output_contract
    from: [output_format_spec]
    priority: 60
  - block: analysis_instruction
    from: [analysis_instruction]
    priority: 50
```

---

## 8. 与 ST 的对照：保留与舍弃

应保留：

- PromptCollection 思路（结构化块）
- 注入顺序控制（position/depth/order）
- token 预算管理
- 组装前后 Hook

可舍弃/弱化：

- 大量用户 UI Prompt 编辑能力
- 对对话小说场景的特化字段
- 依赖“角色卡实时覆盖”的交互模式

---

## 9. 对 COCO promptbuild 的实施建议（阶段化）

### 阶段 1：骨架

- 定义 PromptBlock / SourcePayload / AssemblyPlan
- 支持固定顺序组装
- 支持 required/optional 块

### 阶段 2：预算与裁剪

- 每块估算 token
- mandatory 优先
- evidence 块降级策略（摘要→关键点→裁剪）

### 阶段 3：多 Agent 配置化

- 一个 Agent 一份 YAML/JSON 规范
- 提供配置校验
- 支持版本化

### 阶段 4：可观测性

- 每次组装保存：输入摘要、块顺序、token 消耗、最终 payload
- 支持 debug 回放

---

## 10. 最终建议

对 COCO 来说，最佳路径不是复刻 ST 的 UI，而是复刻其“**结构化 + 可控 + 有预算**”的组装内核，并升级为“**场景契约驱动**”。

这会让你们在多专家 Agent、多源数据、低交互输入的条件下，保持 prompt 质量的一致性和可维护性。
