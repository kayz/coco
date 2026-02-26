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
```

- 模板路径相对 `templates_dir`
- 参考文档路径相对 `root_dir`
- SQLite 路径相对 `root_dir`

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
    MaxHistory   int
    IncludeSectionHeaders *bool
}
```

## 组装顺序

固定顺序拼装（空内容会跳过）：

1. System
2. Task
3. Requirements
4. Format
5. Style
6. References
7. Chat History
8. User Input

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
    System:    []string{"system/writer_role.md"},
    Task:      []string{"task/summarize_doc.md"},
    Format:    []string{"format/doc_template.md"},
    Style:     []string{"style/formal.md"},
    References: []string{"references/ref_doc_001.md"},
    History: promptbuild.HistorySpec{
        Platform:  "wechat",
        ChannelID: "xxx",
        UserID:    "yyy",
        Limit:     200,
    },
    UserInput: "把刚刚的聊天整理成一篇文档。",
})
```
