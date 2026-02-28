# Prompt Builder (text-in / text-out)

This module assembles a single prompt string from Markdown templates, reference files, chat history in SQLite, and user input.

## Configuration

Add to `~/.coco.yaml` (or keep defaults):

```yaml
prompt_build:
  root_dir: "."             # base dir for relative paths
  sqlite_path: ".coco.db"   # history DB path
  templates_dir: "prompts"   # base dir for templates
  audit_enabled: true
  audit_dir: ".coco/promptbuild-audit"
  audit_retention_days: 7
  audit_file_prefix: "promptbuild"
```

Defaults are:

- `root_dir`: `.`
- `sqlite_path`: `.coco.db`
- `templates_dir`: `prompts`
- `audit_enabled`: `true`
- `audit_dir`: `.coco/promptbuild-audit`
- `audit_retention_days`: `7`
- `audit_file_prefix`: `promptbuild`

## Template Layout (example)

```
obsidian/
  prompts/
    system/
      writer_role.md
    task/
      summarize_doc.md
    format/
      doc_template.md
    style/
      formal.md
  references/
    ref_doc_001.md
```

## Build Request (concept)

- system/task/format/style: list of template paths (relative to `templates_dir`)
- references: list of file paths (relative to `root_dir`)
- history: uses SQLite `conversations` + `messages` tables
- user_input: the current instruction
- include_section_headers: optional (default true)
- agent/spec_path: optional, enable spec-driven assembly
- inputs: optional key/value map for spec `request_field` / `inline_text` sources
  - built-in runtime input example: `memory_recall` (markdown/RAG recall text)

## Assembly Modes

### Legacy compatibility mode (default)

If `agent` and `spec_path` are both empty, the builder keeps existing fixed-order behavior and output format.

### Spec-driven mode

If `agent` or `spec_path` is provided, the builder loads a YAML assembly spec and renders sections by `order`.

- `agent`: defaults to `prompts/specs/<agent>.yaml`
- `spec_path`: explicit spec file path

Supported `source_type` values:

- `templates`
- `request_field`
- `references`
- `history`
- `user_input`
- `inline_text`

Token budget controls in spec:
- `defaults.max_prompt_chars`: hard cap for final prompt length (character-based)
- `sections[].max_chars`: per-section cap before final assembly
- When over budget, non-required trailing sections are truncated first, then required sections if still needed

Common `inputs` keys from agent runtime:
- `thinking_prompt`
- `report_notification`
- `memory_recall`
- `planner_instruction`
- `workspace_contract`
- `bootstrap_instruction`

Runtime default:
- `internal/agent` now sends `agent: coco`, which resolves to `prompts/specs/coco.yaml` when PromptBuild is enabled.

If a section is marked `required: true` and resolves to empty content, `Build()` returns an error.

Example spec file: `docs/promptbuild-agent-spec.example.yaml`

## Build Request (spec mode example)

```json
{
  "agent": "daily_research_analyst",
  "requirements": "Generate today's research brief",
  "references": ["references/research_digest.md"],
  "inputs": {
    "analysis_instruction": "Start with 3-line conclusion, then evidence and risks."
  },
  "history": {
    "platform": "wechat",
    "channel_id": "channel-1",
    "user_id": "user-1",
    "limit": 120
  },
  "user_input": "Focus on unusual changes and confidence"
}
```

## Output

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
[REFERENCE:...]
...
[/REFERENCE]

### Chat History
User:
...

### User Input
...
```

## CLI

Build from a JSON request:

```bash
coco promptbuild --request request.json
```

Write output to a file:

```bash
coco promptbuild --request request.json --output out.txt
```

Record request/output for debugging:

```bash
coco promptbuild --request request.json --record
```

Record to a custom directory:

```bash
coco promptbuild --request request.json --record --record-dir logs/promptbuild
```
