# Memvra

> Your AI finally remembers your project.

Memvra is a developer CLI tool that gives AI coding assistants a persistent memory of your project. It solves the core problem that AI agents (Claude, GPT, Gemini, etc.) are stateless across sessions — forcing developers to repeatedly re-explain architecture, conventions, constraints, and past decisions every time they start a new conversation.

**Switch between AI tools seamlessly.** Working with Claude and hit your token limit? Open a new terminal, start Gemini or Cursor, type "continue" — and it knows exactly where you left off. Memvra auto-exports your project context to every format so any AI tool can pick it up immediately.

Runs entirely on your machine. Works with any LLM. No accounts required.

## Features

- **Auto-indexes** your project — tech stack, architecture, file chunks, conventions
- **Remembers** decisions, constraints, and notes across sessions
- **Retrieves** relevant context semantically using vector search
- **Injects** an optimized prompt into every LLM call automatically
- **Extracts** decisions and constraints from AI responses and stores them
- **Auto-exports** to `CLAUDE.md`, `.cursorrules`, `PROJECT_CONTEXT.md`, and `memvra-context.json` — including session history and git working state
- **MCP server** — Claude Code, Cursor, and Windsurf call Memvra tools automatically via the Model Context Protocol
- **Wrap any CLI tool** — `memvra wrap gemini` launches Gemini (or Aider, Ollama, etc.) with project context auto-injected and the session captured on exit
- **Seamless model switching** — switch between Claude, Gemini, Cursor, or any AI tool mid-session without losing context
- **Incremental updates** — re-indexes only changed files, prunes deleted ones
- **Watch mode** — auto-reindexes on file changes in the background
- **Git hook integration** — auto-updates the index after every commit
- **Local-first** — all data stays in `.memvra/` on your machine

## Installation

```bash
# macOS (Homebrew)
brew tap memvra/tap
brew install memvra

# Linux / macOS (curl installer)
curl -fsSL https://memvra.com/install.sh | sh

# Go install (requires Go 1.22+ with CGO)
go install github.com/memvra/memvra@latest
```

## Quick Start

```bash
# 1. Configure API keys and embedding provider (one-time)
memvra setup

# 2. Initialize in your project
cd /path/to/your/project
memvra init
# → Scans your project, generates CLAUDE.md, .cursorrules, PROJECT_CONTEXT.md, memvra-context.json

# 3. Ask a question — full project context is injected automatically
memvra ask "How should I implement the document upload endpoint?"

# 4. Store a decision — all export files update automatically
memvra remember "We use JWT auth, not Devise — API-only mode"
# → auto-exported: CLAUDE.md, .cursorrules, PROJECT_CONTEXT.md, memvra-context.json

# 5. Check what Memvra knows
memvra status
memvra context
```

After `memvra init`, your project root will contain context files for every major AI tool:

| File | Read by |
|------|---------|
| `CLAUDE.md` | Claude Code, Claude CLI |
| `.cursorrules` | Cursor |
| `PROJECT_CONTEXT.md` | Any markdown-aware tool, Windsurf, Copilot |
| `memvra-context.json` | Custom integrations, scripts, APIs |

These files are auto-generated and added to `.gitignore` automatically.

## Seamless Model Switching

Memvra's core promise: **no matter which AI tool you switch to next, the context is already there.**

### The problem

You're deep in a coding session with Claude Code. You hit your token limit, or your API quota runs out, or you just want a second opinion from Gemini. You open a new terminal — and the new AI has zero context about what you were working on.

### How Memvra solves it

Memvra provides three integration tracks so every AI tool gets context automatically:

**Track 1: MCP Server** — For MCP-compatible tools (Claude Code, Cursor, Windsurf)

```bash
memvra mcp install   # One-time setup — registers Memvra as an MCP server
```

The AI tool calls Memvra tools automatically (`memvra_save_progress`, `memvra_remember`, `memvra_get_context`, etc.) — no manual steps needed. Sessions are saved and context files are regenerated on every interaction.

**Track 2: Wrap** — For non-MCP CLI tools (Gemini CLI, Aider, Ollama, etc.)

```bash
memvra wrap gemini   # Launches Gemini with project context auto-injected
```

Memvra injects session history + decisions as Gemini's first message, proxies all I/O transparently, captures the full conversation on exit, and auto-exports updated context.

**Track 3: Ask** — Direct Q&A with any LLM

```bash
memvra ask "Continue implementing the auth middleware"
```

Full project context is injected into the LLM call automatically.

All three tracks auto-export context files to every format:

```
your-project/
├── CLAUDE.md              ← Claude Code reads this automatically
├── .cursorrules           ← Cursor reads this automatically
├── PROJECT_CONTEXT.md     ← Generic markdown for any tool
├── memvra-context.json    ← Structured JSON for custom integrations
└── .memvra/               ← Memvra's internal database
```

### Example workflow

```bash
# Morning: Working with Claude Code (MCP — automatic)
# Claude calls memvra_save_progress before ending the session
# → All context files updated with session history

# Afternoon: Claude token limit reached — switch to Gemini
memvra wrap gemini
# → Gemini starts with your full project context pre-loaded:
#   "Here is project context from previous AI sessions..."
#   Sessions, decisions, TODOs — all injected automatically
# Type "continue" and Gemini picks up right where Claude left off

# Evening: Want to use Cursor IDE for the frontend
# Just open the project in Cursor — it reads .cursorrules automatically
# Cursor sees recent sessions, decisions, and conventions
```

### What each AI tool sees

Every exported context file includes:

- **Recent activity** — session history with timestamps, model used, and summaries
- **Work in progress** — current git branch, staged/unstaged/untracked files
- **Project profile** — name, tech stack, language, framework
- **Decisions** — "Use PostgreSQL for JSONB support", "JWT with RS256"
- **Conventions** — "camelCase for API fields", "Service objects in app/services/"
- **Constraints** — "Never expose API keys in client code"
- **TODOs** — "Refactor auth module before v2 launch"
- **Notes** — "API uses REST, rate limited to 100 req/min"

The AI doesn't need to be told what happened in previous sessions — it already knows.

## Commands

| Command | Description |
|---------|-------------|
| `memvra setup` | Interactive first-time configuration (API keys, embedding provider) |
| `memvra init` | Scan and index the current project, generate embeddings |
| `memvra ask "<question>"` | Ask a question with full project context injected |
| `memvra remember "<statement>"` | Store a decision, convention, constraint, or note |
| `memvra forget` | Remove specific memories interactively or by ID/type |
| `memvra context` | View the project context Memvra would inject |
| `memvra diff` | Show file index, memory, and session changes since last update |
| `memvra status` | Show project stats — files, memories, sessions, DB size |
| `memvra update` | Re-index changed files, re-embed modified chunks, prune deleted files |
| `memvra watch` | Watch for file changes and auto-reindex in the background |
| `memvra export` | Export context to CLAUDE.md, .cursorrules, markdown, or JSON |
| `memvra wrap <tool>` | Wrap a CLI tool — inject context, proxy I/O, capture session |
| `memvra mcp` | Start the MCP server (called by AI tools, not manually) |
| `memvra mcp install` | Register Memvra as an MCP server in Claude Code and Cursor |
| `memvra hook install` | Install a post-commit git hook for automatic re-indexing |
| `memvra hook uninstall` | Remove the post-commit hook (preserves other hooks) |
| `memvra hook status` | Check if the post-commit hook is installed |
| `memvra prune` | Remove old sessions to reduce database size |
| `memvra version` | Print version, commit, and build date |

### `memvra ask` flags

```
-m, --model string        LLM provider: claude, openai, gemini, ollama
-f, --files strings       Always include these files in context
-e, --extract             Auto-extract decisions/constraints from the response
-s, --summarize           Auto-summarize session with an LLM call
-v, --verbose             Show which memories and chunks were included
    --no-memory           Skip memory retrieval, use raw question only
    --context-only        Print injected context without calling the LLM
    --max-tokens int      Response token limit (default 4096)
    --temperature float   Sampling temperature (default 0.7)
```

### `memvra init` flags

```
-r, --root string     Project root directory (default: auto-detect from cwd)
    --no-prompt       Skip the interactive notes prompt
```

### `memvra remember` flags

```
-t, --type string     Memory type: decision, convention, constraint, note, todo
                      (auto-detected from content if not set)
```

### `memvra forget` flags

```
    --id string       Delete a specific memory by ID
-t, --type string     Delete all memories of this type
    --all             Delete all memories (requires confirmation)
```

### `memvra context` flags

```
-s, --section string   Show only a specific section: profile, decisions, conventions,
                       constraints, notes, todos
    --export           Also write context to .memvra/context.md
    --edit             Open .memvra/context.md in $EDITOR
```

### `memvra diff` flags

```
    --files-only       Show only file index changes
    --memories-only    Show only memory changes
    --sessions-only    Show only session changes
    --since string     Override time anchor (e.g. "24h", "7d", "2h30m")
    --no-scan          Skip filesystem scan (show only memory/session changes)
```

### `memvra update` flags

```
    --force       Re-index all files, ignoring content hashes
    --quiet       Suppress output (used by git hooks)
```

### `memvra watch` flags

```
    --debounce int   Debounce interval in milliseconds (default 500)
```

### `memvra prune` flags

```
    --older-than int   Remove sessions older than N days
    --keep int         Keep only the latest N sessions (default 100)
    --dry-run          Preview what would be deleted
```

### `memvra wrap` flags

```
-m, --model string   LLM provider for session summarization (claude, openai, gemini, ollama)
-s, --summarize      Force session summarization on exit
-e, --extract        Force memory extraction from session transcript
    --no-inject      Skip injecting project context into the wrapped tool
```

```bash
memvra wrap gemini                         # Launch Gemini with context injection
memvra wrap aider --no-auto-commits        # Wrap Aider (unknown flags pass through)
memvra wrap ollama run llama3.2            # Wrap Ollama with a specific model
memvra wrap gemini --no-inject             # Launch without context injection
memvra wrap gemini -s -e                   # Summarize + extract on exit
```

### `memvra mcp install`

Registers Memvra as an MCP server. Writes config to:
- Claude Code: `~/.claude/mcp.json`
- Cursor: `.cursor/mcp.json` (project-level)

After installation, the AI tool automatically discovers and calls Memvra's 8 tools:

| MCP Tool | Description |
|----------|-------------|
| `memvra_save_progress` | Save session summary (called before ending a session) |
| `memvra_remember` | Store a decision, convention, or note |
| `memvra_get_context` | Retrieve relevant context for a question |
| `memvra_search` | Semantic search across code and memories |
| `memvra_forget` | Remove a memory by ID |
| `memvra_project_status` | Get project stats |
| `memvra_list_memories` | List stored memories |
| `memvra_list_sessions` | List recent sessions |

### `memvra export` flags

> **Note:** With auto-export enabled (default), you rarely need to run `memvra export` manually. Context files are regenerated automatically on every memory change. Use this command when you want to export to a custom path or filter by memory type.

```
    --format string    Output format: claude, cursor, markdown, json (default "markdown")
-s, --section string   Export only memories of this type: decision, convention,
                       constraint, note, todo
```

```bash
memvra export --format claude   > CLAUDE.md          # Claude Code
memvra export --format cursor   > .cursorrules        # Cursor
memvra export --format markdown > PROJECT_CONTEXT.md  # Generic markdown
memvra export --format json     > context.json        # Structured JSON
memvra export --format json --section decision        # Decisions only
```

## Configuration

### Global config — `~/.config/memvra/config.toml`

```toml
default_model    = "claude"   # claude | openai | gemini | ollama
default_embedder = "ollama"   # ollama | openai

[keys]
# Prefer environment variables: ANTHROPIC_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY

[ollama]
host             = "http://localhost:11434"
embed_model      = "nomic-embed-text"
completion_model = "llama3.2"

[context]
max_tokens           = 8000   # Token budget for context injection
similarity_threshold = 0.3    # Minimum similarity score for retrieval
top_k_chunks         = 10     # Max code chunks to retrieve
top_k_memories       = 5      # Max memories to retrieve
top_k_sessions       = 3      # Recent session summaries to inject (0 = skip)
session_token_budget = 500    # Max tokens for session history block

[output]
stream  = true
color   = true

[extraction]
enabled      = false  # Auto-extract memories after every ask
max_extracts = 3

[summarization]
enabled    = true   # Auto-summarize sessions after every ask
max_tokens = 256    # Max tokens for the summary LLM call

[auto_export]
enabled = true                                       # Auto-regenerate context files on memory changes
formats = ["claude", "cursor", "markdown", "json"]   # All formats by default
```

Auto-export triggers on: `memvra init`, `memvra remember`, `memvra ask --extract`, `memvra update`, `memvra watch` (via update), git hooks (via update), MCP tool calls (`save_progress`, `remember`, `forget`), and `memvra wrap` (on session exit).

To disable auto-export or limit formats:

```toml
[auto_export]
enabled = false                   # Disable completely

# Or export only specific formats:
[auto_export]
enabled = true
formats = ["claude", "cursor"]    # Only CLAUDE.md and .cursorrules
```

### Project config — `.memvra/config.toml`

```toml
default_model = "claude"

[project]
name = "my-project"

# Files always injected into every ask (no --files flag needed)
always_include = [
    "config/routes.rb",
    "doc/ARCHITECTURE.md",
]

# Patterns excluded from indexing
exclude = [
    "spec/fixtures/**",
    "tmp/**",
]

[conventions]
style = "Service objects in app/services/ for all business logic"
api   = "All API responses follow JSON:API specification"
```

## Supported LLM Providers

| Provider | Completion | Embedding | Auth |
|----------|-----------|-----------|------|
| Claude | claude-sonnet-4 | — (use OpenAI or Ollama) | `ANTHROPIC_API_KEY` |
| OpenAI | gpt-4o | text-embedding-3-small | `OPENAI_API_KEY` |
| Gemini | gemini-2.0-flash | text-embedding-004 | `GEMINI_API_KEY` |
| Ollama | any local model | nomic-embed-text | Local (no key) |

All four providers support streaming completions.

## How It Works

```
┌─────────────────────────────────────────────────────────┐
│                      Your Project                       │
│                                                         │
│  Source files ──► Scanner ──► Chunker ──► Embedder      │
│                      │           │           │          │
│                      ▼           ▼           ▼          │
│                ┌──────────────────────────────────┐     │
│                │  .memvra/memvra.db (SQLite)      │     │
│                │  ├── projects    (tech stack)     │     │
│                │  ├── file_index  (content hashes) │     │
│                │  ├── chunks     (code segments)   │     │
│                │  ├── memories   (decisions, etc.) │     │
│                │  ├── sessions   (conversation log)│     │
│                │  └── vec_*      (vector embeddings│)    │
│                └──────────────────────────────────┘     │
│                      │                                  │
│                      ▼                                  │
│  ┌─────────────────────────────────────────┐           │
│  │ Context Builder                          │           │
│  │  1. Semantic search (vector similarity)  │           │
│  │  2. Token-budget-aware assembly          │           │
│  │  3. Priority: decisions > conventions >  │           │
│  │     constraints > retrieved chunks       │           │
│  └─────────────────────────────────────────┘           │
│          │                        │                     │
│          ▼                        ▼                     │
│  LLM Provider               Auto-Export                 │
│  (Claude/OpenAI/             ├── CLAUDE.md              │
│   Gemini/Ollama)             ├── .cursorrules           │
│                              ├── PROJECT_CONTEXT.md     │
│                              └── memvra-context.json    │
└─────────────────────────────────────────────────────────┘
```

1. **Scan** — `memvra init` walks your project, detects the tech stack (language, framework, build tools), and chunks source files into segments.
2. **Embed** — Each chunk and memory is embedded into a 768-dimensional vector using your configured embedder (Ollama/OpenAI/Gemini).
3. **Store** — Everything lives in a single SQLite database at `.memvra/memvra.db`, with vector search powered by `sqlite-vec`.
4. **Retrieve** — When you ask a question, the context builder performs semantic similarity search to find the most relevant code chunks and memories, assembles them into an optimized prompt within your token budget, and sends it to the LLM.
5. **Export** — After every memory change, Memvra regenerates context files in all formats so that any AI tool can read the project context natively.

## Development

Requires Go 1.22+ with CGO enabled (SQLite dependency).

```bash
make build     # Compile for current OS/arch → dist/memvra
make install   # Install to $GOPATH/bin
make test      # Run tests
make lint      # Run golangci-lint
make snapshot  # Build all platforms locally via GoReleaser (no publish)
make release   # Full release via GoReleaser (requires GITHUB_TOKEN)
```

## Author

Created by [Mohit Chandana](https://github.com/git4ruby)

## License

MIT — see [LICENSE](LICENSE)
