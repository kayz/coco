# PromptBuild 内部包

该包用于将系统提示词、任务要求、格式/风格模板、参考文档、对话历史与用户输入拼装为**单一纯文本 Prompt**。  
定位：无状态、只读、可作为 agent 内部模块直接调用。

## 设计目标

- 只负责 prompt 组装，不调用 LLM
- 只读 SQLite 与 Markdown 文件
- 缺失文件自动跳过，不阻断流程
- 输出纯文本，便于上层 agent 发送到任意 LLM

## 目录约定（默认）

由 `prompt_build` 配置控制：

```yaml
prompt_build:
  root_dir: "."
  sqlite_path: ".coco.db"
  templates_dir: "prompts"
  audit_enabled: true
  audit_dir: ".coco/promptbuild-audit"
  audit_retention_days: 7
  audit_file_prefix: "promptbuild"
```

- 模板路径相对 `templates_dir`
- 参考文档路径相对 `root_dir`
- SQLite 路径相对 `root_dir`
- 审计日志按天写入 `audit_dir/{audit_file_prefix}-YYYY-MM-DD.jsonl`
- 每次组装后自动清理超过 `audit_retention_days` 的旧日志

## 主要类型

```go
type HistorySpec struct {
    ConversationID int64
    Platform       string
    ChannelID      string
    UserID         string
    Limit          int
}

type BuildRequest struct {
    System       []string
    Task         []string
    Format       []string
    Style        []string
    Requirements string
    References   []string
    History      HistorySpec
    UserInput    string
    Agent        string
    SpecPath     string
    Inputs       map[string]string
    MaxHistory   int
    IncludeSectionHeaders *bool
}
```

## 组装模式

### 1) 兼容模式（默认）

当 `BuildRequest` 未指定 `agent/spec_path` 时，沿用固定顺序拼装（空内容自动跳过）：

1. System
2. Task
3. Requirements
4. Format
5. Style
6. References
7. Chat History
8. User Input

### 2) Spec 配置模式

当请求指定 `Agent` 或 `SpecPath` 时，加载 YAML 组装配置，按 `order` 渲染 `sections`，并在 `required=true` 且内容为空时返回错误。

- `Agent`: 默认尝试加载 `prompts/specs/<agent>.yaml`
- `SpecPath`: 显式配置文件路径（相对 `root_dir` 或绝对路径）

`source_type` 支持：

- `templates`
- `request_field`
- `references`
- `history`
- `user_input`
- `inline_text`

预算控制（Spec 可选）：
- `defaults.max_prompt_chars`：最终 Prompt 字符上限（按字符数）
- `sections[].max_chars`：单 section 字符上限
- 超限时优先截断末尾的非必填 section，再截断必填 section

完整样例见：`docs/promptbuild-agent-spec.example.yaml`

## 审计日志

每次 `Build()` 完成后会写入一条 JSONL 审计记录（失败仅 warn，不影响主流程返回）。

记录字段：

- `timestamp`
- `request_digest`（请求摘要哈希，避免原始大体积/敏感输入）
- `final_prompt`
- `sections`（最终参与拼装的 section 标题列表）
- `history_meta`

文件名规则：`{audit_file_prefix}-YYYY-MM-DD.jsonl`

## Chat History 读取规则

从 SQLite 读取：

- 表：`conversations`、`messages`
- 使用 `conversation_id` 或 `(platform, channel_id, user_id)` 定位会话
- 取最近 N 条（默认 200），按时间正序输出

## 输出格式（示例）

```
### System
...

### Task
...

### Requirements
...

### Format
...

### Style
...

### References
[REFERENCE:ref.md]
...
[/REFERENCE]

### Chat History
User:
...

### User Input
...
```

## 快速调用示例

```go
cfg, _ := config.Load()
builder := promptbuild.NewBuilder(cfg.PromptBuild)

out, err := builder.Build(promptbuild.BuildRequest{
    Agent:    "daily_research_analyst", // 将尝试加载 prompts/specs/daily_research_analyst.yaml
    Inputs: map[string]string{
        "analysis_instruction": "先给三行结论，再给证据与风险。",
    },
    Requirements: "生成今日策略简报",
    References:   []string{"references/research_digest.md"},
    History: promptbuild.HistorySpec{
        Platform:  "wechat",
        ChannelID: "xxx",
        UserID:    "yyy",
        Limit:     120,
    },
    UserInput: "把今天重点变化写清楚",
})
```
