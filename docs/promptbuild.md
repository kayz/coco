# Prompt Builder (text-in / text-out)

This module assembles a single prompt string from Markdown templates, reference files, chat history in SQLite, and user input.

## Configuration

Add to `~/.coco.yaml` (or keep defaults):

```yaml
prompt_build:
  root_dir: "."          # base dir for relative paths
  sqlite_path: ".coco.db" # history DB path
  templates_dir: "prompts" # base dir for templates
```

Defaults are:

- `root_dir`: `.`
- `sqlite_path`: `.coco.db`
- `templates_dir`: `prompts`

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

## Output

Single plain text prompt with section headers:

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
