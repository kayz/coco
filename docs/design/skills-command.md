# Design: `lingti-bot skills` Subcommand

## Overview

Add a `skills` CLI subcommand to manage the skill/plugin system. Modeled after openclaw's `skills` CLI — auto-discovery from multiple directories, eligibility gating (binary/env/OS requirements), YAML+Markdown skill definitions, and rich terminal output.

## Current State

The project already has `internal/skills/` with:
- `registry.go` — In-memory registry, JSON-based skill loading, trigger/action execution
- `executors.go` — Shell, HTTP, Prompt, Workflow executors

**Problem:** The existing system uses JSON skill files and is not integrated into the CLI. There's no way to list, inspect, or check skill readiness from the command line.

## Proposed CLI Interface

```
lingti-bot skills              # Default: same as "list"
lingti-bot skills list         # List all discovered skills with status
lingti-bot skills info <name>  # Show detailed info for a single skill
lingti-bot skills check        # Summary of ready vs missing skills
lingti-bot skills enable <name>   # Enable a disabled skill
lingti-bot skills disable <name>  # Disable a skill
```

### Global Flags (on all subcommands)

| Flag | Description |
|------|-------------|
| `--json` | Output as JSON instead of table |

### `skills list` Flags

| Flag | Description |
|------|-------------|
| `--eligible` | Show only eligible (ready-to-use) skills |
| `-v, --verbose` | Show missing requirements column |

### Example Output

```
Skills (5/8 ready)

  Status      Skill            Description              Source
  ✓ ready     🐙 github       GitHub CLI integration    bundled
  ✓ ready     👀 peekaboo      macOS UI automation       bundled
  ✓ ready     📦 shell-utils   Shell utility commands    managed
  ✗ missing   🤖 openai        OpenAI API integration    bundled
  ⏸ disabled  📊 grafana       Grafana dashboards        workspace
```

```
# lingti-bot skills info github

🐙 github ✓ Ready

Interact with GitHub using the gh CLI.

Details:
  Source:   bundled
  Path:     ~/.lingti/skills/github/SKILL.md
  Homepage: https://cli.github.com

Requirements:
  Binaries: ✓ gh
```

```
# lingti-bot skills check

Skills Status Check

Total:                 8
✓ Eligible:            5
⏸ Disabled:            1
✗ Missing requirements: 2

Ready to use:
  🐙 github
  👀 peekaboo
  📦 shell-utils
  🔧 system-info
  📁 file-manager

Missing requirements:
  🤖 openai (env: OPENAI_API_KEY)
  📊 grafana (bins: grafana-cli)
```

## Skill Definition Format

Adopt openclaw's SKILL.md format — YAML frontmatter + Markdown body:

```
~/.lingti/skills/github/SKILL.md
```

```yaml
---
name: github
description: "Interact with GitHub using the gh CLI."
homepage: https://cli.github.com
metadata:
  emoji: "🐙"
  os: ["darwin", "linux"]
  requires:
    bins: ["gh"]
    env: []
  install:
    - id: brew
      kind: brew
      formula: gh
      label: "Install GitHub CLI (brew)"
    - id: apt
      kind: apt
      package: gh
      label: "Install GitHub CLI (apt)"
---

# GitHub Skill

Use the `gh` CLI to interact with GitHub...
```

### Frontmatter Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Unique skill identifier |
| `description` | string | yes | Short description (shown in list) |
| `homepage` | string | no | Link to docs/website |
| `metadata.emoji` | string | no | Emoji for display (default: "📦") |
| `metadata.os` | []string | no | Allowed OS: "darwin", "linux", "windows" |
| `metadata.always` | bool | no | Skip all gating, always include |
| `metadata.requires.bins` | []string | no | Required binaries (all must exist in PATH) |
| `metadata.requires.any_bins` | []string | no | At least one must exist |
| `metadata.requires.env` | []string | no | Required env vars (all must be set) |
| `metadata.install` | []InstallSpec | no | How to install missing requirements |

### InstallSpec

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique installer ID |
| `kind` | string | "brew", "apt", "go", "npm", "download" |
| `formula` | string | Homebrew formula name |
| `package` | string | Package name (apt/npm) |
| `module` | string | Go module path |
| `url` | string | Download URL |
| `label` | string | Human-readable install label |
| `bins` | []string | Binaries this installs |

## Skill Discovery

Skills are loaded from multiple directories in precedence order (later overrides earlier):

```
1. Bundled skills    <executable-dir>/../skills/   (shipped with binary)
2. Managed skills    ~/.lingti/skills/              (user-installed)
3. Workspace skills  <cwd>/skills/                  (project-specific)
```

Each directory is scanned for `*/SKILL.md` (one level deep). Duplicate names: later source wins.

### Backward Compatibility

Existing JSON skills in `~/.lingti/skills/*.json` continue to work. The loader checks both:
1. `<dir>/<name>/SKILL.md` (new format, preferred)
2. `<dir>/<name>.json` (legacy format)

## Eligibility Gating

A skill is **eligible** if all gates pass:

```
1. Not explicitly disabled in config
2. OS matches (metadata.os contains runtime.GOOS, or metadata.os is empty)
3. If metadata.always == true → skip remaining gates, INCLUDE
4. All required binaries found (exec.LookPath for each in metadata.requires.bins)
5. At least one "any_bins" found (if metadata.requires.any_bins is set)
6. All required env vars set (os.Getenv for each in metadata.requires.env)
```

Result: `eligible`, `disabled`, or `missing` (with details of what's missing).

## New Package: `internal/skills/discovery.go`

```go
// SkillSource represents where a skill was loaded from
type SkillSource string

const (
    SourceBundled   SkillSource = "bundled"
    SourceManaged   SkillSource = "managed"
    SourceWorkspace SkillSource = "workspace"
)

// SkillEntry is a discovered skill with metadata
type SkillEntry struct {
    Name         string
    Description  string
    Homepage     string
    FilePath     string       // Absolute path to SKILL.md
    Source       SkillSource
    Content      string       // Markdown body (after frontmatter)
    Metadata     SkillMetadata
    Enabled      bool         // From config override
}

type SkillMetadata struct {
    Emoji    string        `yaml:"emoji"`
    OS       []string      `yaml:"os"`
    Always   bool          `yaml:"always"`
    Requires Requirements  `yaml:"requires"`
    Install  []InstallSpec `yaml:"install"`
}

type Requirements struct {
    Bins    []string `yaml:"bins"`
    AnyBins []string `yaml:"any_bins"`
    Env     []string `yaml:"env"`
}

type InstallSpec struct {
    ID      string `yaml:"id"`
    Kind    string `yaml:"kind"`
    Formula string `yaml:"formula,omitempty"`
    Package string `yaml:"package,omitempty"`
    Module  string `yaml:"module,omitempty"`
    URL     string `yaml:"url,omitempty"`
    Label   string `yaml:"label"`
    Bins    []string `yaml:"bins,omitempty"`
}
```

## New Package: `internal/skills/status.go`

```go
// EligibilityStatus represents whether a skill is ready
type EligibilityStatus string

const (
    StatusReady    EligibilityStatus = "ready"
    StatusMissing  EligibilityStatus = "missing"
    StatusDisabled EligibilityStatus = "disabled"
)

// SkillStatus is the full eligibility report for one skill
type SkillStatus struct {
    SkillEntry
    Status       EligibilityStatus
    Missing      MissingRequirements
}

type MissingRequirements struct {
    Bins    []string
    AnyBins []string
    Env     []string
    OS      []string
}

// StatusReport is the full report for all skills
type StatusReport struct {
    Skills         []SkillStatus
    BundledDir     string
    ManagedDir     string
    WorkspaceDir   string
}
```

## Config Integration

Add skills section to `bot.yaml`:

```yaml
skills:
  disabled: ["grafana", "openai"]     # Explicitly disabled skills
  extra_dirs: ["/path/to/more/skills"] # Additional skill directories
```

Add to `internal/config/config.go`:

```go
type Config struct {
    // ... existing fields ...
    Skills SkillsConfig `yaml:"skills,omitempty"`
}

type SkillsConfig struct {
    Disabled  []string `yaml:"disabled,omitempty"`
    ExtraDirs []string `yaml:"extra_dirs,omitempty"`
}
```

## File Changes Summary

| File | Action | Description |
|------|--------|-------------|
| `cmd/skills.go` | **NEW** | Cobra command: `skills`, `skills list`, `skills info`, `skills check`, `skills enable`, `skills disable` |
| `internal/skills/discovery.go` | **NEW** | SKILL.md parsing, multi-dir discovery, frontmatter extraction |
| `internal/skills/status.go` | **NEW** | Eligibility gating logic, status report building |
| `internal/skills/format.go` | **NEW** | Terminal formatting (table, info, check output) |
| `internal/config/config.go` | **EDIT** | Add `SkillsConfig` struct and `Skills` field to `Config` |

## Frontmatter Parsing

Use `gopkg.in/yaml.v3` (already a dependency) to parse YAML frontmatter from SKILL.md files:

```go
func ParseSkillMD(path string) (*SkillEntry, error) {
    data, err := os.ReadFile(path)
    // Split on "---" delimiters
    // Parse YAML frontmatter into struct
    // Remaining content is the markdown body
}
```

No new dependencies needed. The `---` delimiter split is straightforward string processing.

## Directories

```
~/.lingti/
├── bot.yaml              # Main config (existing)
└── skills/               # Managed skills (existing, currently JSON)
    ├── github/
    │   └── SKILL.md      # New format
    ├── peekaboo/
    │   └── SKILL.md
    └── legacy-skill.json  # Old format still works

<project>/
└── skills/               # Workspace skills (optional)
    └── my-custom-skill/
        └── SKILL.md
```

## Non-Goals (for this iteration)

- **No install command** — Users install binaries themselves; we just report what's missing
- **No hot-reload/watcher** — Skills are discovered at command invocation time
- **No remote node support** — Local-only eligibility checking
- **No marketplace/registry** — Skills are local files only
- **No agent prompt injection** — This is CLI-only; agent integration comes later
