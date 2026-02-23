# Memvra

> Your AI finally remembers your project.

Memvra is a developer CLI tool that gives AI coding assistants a persistent memory of your project. It solves the core problem that AI agents (Claude, GPT, Gemini, etc.) are stateless across sessions — forcing developers to repeatedly re-explain architecture, conventions, constraints, and past decisions every time they start a new conversation.

Runs entirely on your machine. Works with any LLM. No accounts required.

## Features

- **Auto-indexes** your project — tech stack, architecture, file chunks, conventions
- **Remembers** decisions, constraints, and notes across sessions
- **Retrieves** relevant context semantically using vector search
- **Injects** an optimized prompt into every LLM call automatically
- **Extracts** decisions and constraints from AI responses and stores them
- **Exports** to `CLAUDE.md`, `.cursorrules`, markdown, or JSON
- **Incremental updates** — re-indexes only changed files, prunes deleted ones
- **Local-first** — all data stays in `.memvra/` on your machine

## Installation

```bash
# macOS (Homebrew)
brew tap memvra/tap
brew install memvra

# Linux / macOS (curl installer)
curl -fsSL https://get.memvra.dev | sh

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

# 3. Ask a question — full project context is injected automatically
memvra ask "How should I implement the document upload endpoint?"

# 4. Store a decision manually
memvra remember "We use JWT auth, not Devise — API-only mode"

# 5. Check what Memvra knows
memvra status
memvra context
```

## Commands

| Command | Description |
|---------|-------------|
| `memvra setup` | Interactive first-time configuration (API keys, embedding provider) |
| `memvra init` | Scan and index the current project, generate embeddings |
| `memvra ask "<question>"` | Ask a question with full project context injected |
| `memvra remember "<statement>"` | Store a decision, convention, constraint, or note |
| `memvra forget` | Remove specific memories interactively or by ID/type |
| `memvra context` | View the project context Memvra would inject |
| `memvra status` | Show project stats — files, memories, sessions, DB size |
| `memvra update` | Re-index changed files, re-embed modified chunks, prune deleted files |
| `memvra export` | Export context to CLAUDE.md, .cursorrules, markdown, or JSON |
| `memvra version` | Print version, commit, and build date |

### `memvra ask` flags

```
-m, --model string        LLM provider: claude, openai, gemini, ollama
-f, --files strings       Always include these files in context
-e, --extract             Auto-extract decisions/constraints from the response
-v, --verbose             Show which memories and chunks were included
    --no-memory           Skip memory retrieval, use raw question only
    --context-only        Print injected context without calling the LLM
    --max-tokens int      Response token limit (default 4096)
    --temperature float   Sampling temperature (default 0.7)
```

### `memvra export` formats

```bash
memvra export --format claude   > CLAUDE.md          # Claude Code
memvra export --format cursor   > .cursorrules        # Cursor
memvra export --format markdown > PROJECT_CONTEXT.md  # Generic markdown
memvra export --format json     > context.json        # Structured JSON
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

[output]
stream  = true
color   = true

[extraction]
enabled      = false  # Auto-extract memories after every ask
max_extracts = 3
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
| Claude | claude-sonnet-4 | — | `ANTHROPIC_API_KEY` |
| OpenAI | gpt-4o | text-embedding-3-small | `OPENAI_API_KEY` |
| Gemini | gemini-2.0-flash | text-embedding-004 | `GEMINI_API_KEY` |
| Ollama | any local model | nomic-embed-text | Local (no key) |

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

## License

MIT — see [LICENSE](LICENSE)
