# Memvra — Project Specification

## A Persistent, Model-Agnostic AI Memory Layer for Software Projects

**Version:** 1.0.0-draft
**Last Updated:** February 2026
**Status:** Pre-development MVP Specification

---

## 1. Executive Summary

Memvra is a developer CLI tool that provides persistent, project-aware memory for AI coding assistants. It solves the core problem that AI agents (Claude, GPT, Gemini, etc.) are stateless across sessions — forcing developers to repeatedly re-explain their project architecture, conventions, constraints, and past decisions every time they start a new conversation.

Memvra runs locally, stores everything on the developer's machine, works with any LLM provider, and installs as a single binary via `brew`, `npm`, or direct download.

**One-liner:** "Your AI finally remembers your project."

---

## 2. Problem Statement

Developers using AI coding assistants waste significant time on context re-establishment:

- Re-explaining project architecture and tech stack every session
- Re-uploading relevant files and folder structures
- Re-stating coding conventions, constraints, and team preferences
- Losing track of AI-assisted architectural decisions across sessions
- Getting inconsistent AI suggestions because context varies between sessions
- No portable way to carry project intelligence across different AI tools

Current solutions (`.cursorrules`, `CLAUDE.md`, `.github/copilot-instructions.md`) are:
- Manual and static (developers must write and maintain them by hand)
- Tool-specific (locked to one AI provider or editor)
- Flat text with no semantic understanding
- Missing decision history and evolution tracking

---

## 3. Solution

Memvra is a CLI tool that:

1. **Indexes** a project automatically — tech stack, architecture, folder structure, conventions
2. **Remembers** decisions, constraints, and context across sessions
3. **Retrieves** relevant context semantically when the developer asks a question
4. **Injects** optimized context into any LLM call transparently
5. **Evolves** its understanding as the project changes over time

### Core Principles

- **Local-first**: All data stays on the developer's machine by default
- **Model-agnostic**: Works with Claude, GPT, Gemini, Ollama, or any LLM
- **Zero-config start**: `memvra init` and you're running
- **Non-invasive**: Lives in `.memvra/` directory, adds to `.gitignore`, never modifies source code
- **Composable**: Can be used standalone or piped into other tools

---

## 4. Target Users

### Primary (MVP)
- Individual developers who use terminal-based AI tools (Claude Code, Aider, raw API calls)
- Developers working on medium-to-large codebases where context matters
- Polyglot developers switching between AI providers

### Secondary (Post-MVP)
- Engineering teams wanting shared project intelligence
- Open-source maintainers wanting to help contributors understand their codebase
- AI-native development shops standardizing their AI workflows

---

## 5. MVP Feature Set

### 5.1 `memvra init`

**Purpose:** Initialize Memvra in a project directory.

**Behavior:**
1. Detect project root (look for `.git/`, `package.json`, `Gemfile`, `go.mod`, `Cargo.toml`, etc.)
2. Scan directory tree (respect `.gitignore` patterns)
3. Infer tech stack, framework, language, database, architecture pattern
4. Generate structured project profile
5. Chunk source files and generate embeddings
6. Create `.memvra/` directory with SQLite database and config
7. Append `.memvra/` to `.gitignore` if not already present

**Output directory structure:**
```
.memvra/
├── memvra.db          # SQLite database (memories, embeddings, decisions, sessions)
├── config.toml        # User configuration
└── context.md         # Human-readable/editable project context (auto-generated, user-customizable)
```

**Auto-detected project profile example:**
```json
{
  "project_name": "rfe-ready",
  "language": "Ruby",
  "framework": "Rails 7.2",
  "api_mode": true,
  "frontend": "Vue.js 3",
  "database": "PostgreSQL",
  "architecture_pattern": "API + SPA",
  "test_framework": "RSpec",
  "notable_gems": ["acts_as_tenant", "devise", "sidekiq", "pgvector"],
  "entry_points": ["config/routes.rb", "app/controllers/application_controller.rb"],
  "ci": "GitHub Actions",
  "detected_patterns": ["multi-tenant", "background-jobs", "vector-search"],
  "file_count": 342,
  "indexed_at": "2026-02-23T10:00:00Z"
}
```

**Interactive prompts on first run:**
```
$ memvra init

🔍 Scanning project...
✅ Detected: Rails 7.2 API + Vue.js 3 SPA
✅ Database: PostgreSQL with pgvector
✅ 342 files indexed, 1,204 chunks embedded

📝 Optional: Describe anything else about this project?
   (coding conventions, constraints, team preferences — or press Enter to skip)

> We follow service object pattern. No fat models. All API responses use JSON:API format.

✅ Memvra initialized. Project context saved to .memvra/
💡 Run `memvra ask "your question"` to get started.
```

---

### 5.2 `memvra ask "<question>"`

**Purpose:** Ask a question with full project context injected automatically.

**Behavior:**
1. Embed the question using the configured embedding model
2. Retrieve top-k relevant chunks from vector store (file content, past decisions, session summaries)
3. Load structured project profile
4. Build an optimized prompt with context hierarchy:
   - System: Project profile + conventions + constraints
   - Context: Relevant file chunks + related decisions + recent session summary
   - User: The actual question
5. Send to configured LLM provider
6. Stream response to terminal
7. After response, optionally extract and store any decisions made

**Context injection hierarchy (token budget management):**
```
Total token budget: configurable, default 8000 tokens for context

Priority 1 (always included):  Project profile + conventions     (~500 tokens)
Priority 2 (always included):  Direct file matches               (~2000 tokens)
Priority 3 (if room):          Related decisions                  (~1000 tokens)
Priority 4 (if room):          Semantic search results            (~2000 tokens)
Priority 5 (if room):          Recent session summary             (~500 tokens)
Priority 6 (if room):          Extended file context              (~2000 tokens)
```

**Example usage:**
```bash
# Simple question
$ memvra ask "How should I implement the RFE document upload endpoint?"

# With specific file focus
$ memvra ask "Refactor this controller" --files app/controllers/api/v1/documents_controller.rb

# With a specific model
$ memvra ask "Explain the authentication flow" --model claude

# Pipe output
$ memvra ask "Generate a migration for adding status to documents" | pbcopy
```

**Flags:**
```
--model, -m        Override default LLM provider (claude, openai, gemini, ollama)
--files, -f        Include specific files in context (comma-separated)
--no-memory        Skip memory injection, use raw question only
--context-only     Print the context that would be injected, don't call LLM
--verbose, -v      Show which memories/chunks were included
--max-tokens       Override response max tokens
--temperature      Override temperature
```

---

### 5.3 `memvra remember "<statement>"`

**Purpose:** Manually store a decision, convention, or constraint.

**Behavior:**
1. Parse the statement
2. Classify type: `decision`, `convention`, `constraint`, `note`, `todo`
3. Generate embedding
4. Store in SQLite with metadata
5. Confirm storage

**Example usage:**
```bash
$ memvra remember "We switched from Devise to custom JWT auth because of API-only mode"
✅ Stored as: architecture_decision
   "Switched from Devise to custom JWT authentication for API-only mode"

$ memvra remember "All background jobs must be idempotent"
✅ Stored as: constraint
   "All background jobs must be idempotent"

$ memvra remember "TODO: Add rate limiting to document upload endpoint"
✅ Stored as: todo
   "Add rate limiting to document upload endpoint"
```

**Memory types:**
| Type | Description | Example |
|------|-------------|---------|
| `decision` | Architectural or technical decision | "Chose Sidekiq over DelayedJob for background processing" |
| `convention` | Coding standard or pattern | "All services return Result objects, never raise exceptions" |
| `constraint` | Hard requirement or limitation | "Must support PostgreSQL 14+, no MySQL" |
| `note` | General context | "The upload service was written by the previous team lead" |
| `todo` | Tracked future work | "Need to add pagination to the documents index endpoint" |

---

### 5.4 `memvra context`

**Purpose:** View and manage the current project context.

**Behavior:** Outputs the full context that Memvra would inject into an LLM call.

```bash
# View full context
$ memvra context

# View specific sections
$ memvra context --section profile
$ memvra context --section decisions
$ memvra context --section conventions

# Export context as markdown (useful for pasting into other tools)
$ memvra context --export > project-context.md

# Edit the human-readable context file directly
$ memvra context --edit
```

---

### 5.5 `memvra forget`

**Purpose:** Remove specific memories or reset.

```bash
# Interactive selection
$ memvra forget

# Forget specific memory by ID
$ memvra forget --id mem_abc123

# Forget all decisions of a type
$ memvra forget --type todo

# Full reset (re-initialize required)
$ memvra forget --all
```

---

### 5.6 `memvra status`

**Purpose:** Show current Memvra state for the project.

```bash
$ memvra status

📦 Project: rfe-ready
🔧 Stack:   Rails 7.2 API + Vue.js 3 + PostgreSQL
📊 Indexed:  342 files, 1,204 chunks
🧠 Memories: 23 (8 decisions, 6 conventions, 4 constraints, 3 notes, 2 todos)
📅 Sessions: 12
🔄 Last sync: 2 hours ago
🤖 Model:    claude (default)
📁 DB size:  4.2 MB
```

---

### 5.7 `memvra update`

**Purpose:** Re-scan the project and update the index incrementally.

**Behavior:**
1. Detect changed files since last scan (using git diff or file modification times)
2. Re-chunk and re-embed only changed files
3. Update project profile if tech stack changed
4. Prune embeddings for deleted files

```bash
$ memvra update

🔄 Scanning for changes...
   Modified: 4 files
   Added:    1 file
   Deleted:  0 files
✅ Updated 5 file chunks, 23 new embeddings
📊 Total: 347 files, 1,227 chunks
```

---

### 5.8 `memvra export`

**Purpose:** Export Memvra context into formats compatible with other tools.

```bash
# Export as CLAUDE.md (for Claude Code)
$ memvra export --format claude > CLAUDE.md

# Export as .cursorrules (for Cursor)
$ memvra export --format cursor > .cursorrules

# Export as plain markdown
$ memvra export --format markdown > PROJECT_CONTEXT.md

# Export decisions as JSON
$ memvra export --format json --section decisions > decisions.json
```

This is a key differentiator — Memvra becomes the single source of truth that can output to any tool's format.

---

## 6. Technical Architecture

### 6.1 System Overview

```
┌─────────────────────────────────────────────────┐
│                  memvra CLI                       │
│  (single binary — Go)                            │
├─────────────────────────────────────────────────┤
│                                                   │
│  ┌─────────────┐  ┌──────────────┐  ┌─────────┐ │
│  │   Scanner    │  │   Retriever  │  │ Exporter│ │
│  │ (init/update)│  │ (ask/context)│  │ (export)│ │
│  └──────┬──────┘  └──────┬───────┘  └────┬────┘ │
│         │                │                │       │
│         ▼                ▼                ▼       │
│  ┌────────────────────────────────────────────┐  │
│  │           Memory Orchestrator               │  │
│  │  - Context Builder                          │  │
│  │  - Token Budget Manager                     │  │
│  │  - Memory Ranker                            │  │
│  └─────────────────┬──────────────────────────┘  │
│                    │                              │
│         ┌─────────┴─────────┐                    │
│         ▼                   ▼                    │
│  ┌─────────────┐    ┌──────────────┐             │
│  │  SQLite DB   │    │  Embedding   │             │
│  │ + sqlite-vec │    │  Provider    │             │
│  └─────────────┘    └──────────────┘             │
│                                                   │
│  ┌────────────────────────────────────────────┐  │
│  │           LLM Adapter Layer                 │  │
│  │  Claude | OpenAI | Gemini | Ollama | Custom │  │
│  └────────────────────────────────────────────┘  │
│                                                   │
└─────────────────────────────────────────────────┘
```

### 6.2 Technology Choices

| Component | Choice | Rationale |
|-----------|--------|-----------|
| **Language** | Go 1.22+ | Single binary distribution, fast startup, excellent CLI ecosystem, cross-compilation |
| **CLI Framework** | Cobra + Bubble Tea | Industry standard for Go CLIs, Bubble Tea for interactive TUI elements |
| **Local Database** | SQLite 3 | Zero-config, single file, embedded, battle-tested |
| **Vector Search** | sqlite-vec | SQLite extension for vector similarity search, no external dependencies |
| **Embedding (local)** | Ollama (`nomic-embed-text`) | Free, private, runs locally, good quality for code |
| **Embedding (cloud)** | OpenAI `text-embedding-3-small` | Best quality, fallback when local not available |
| **Config Format** | TOML | Human-readable, standard for CLI tools |
| **Output Format** | Markdown + JSON | Universal compatibility |

### 6.3 Database Schema (SQLite)

```sql
-- Project metadata
CREATE TABLE project (
    id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    name TEXT NOT NULL,
    root_path TEXT NOT NULL,
    tech_stack TEXT NOT NULL,          -- JSON
    architecture TEXT,                  -- JSON: detected patterns, entry points
    conventions TEXT,                   -- JSON: user-defined coding conventions
    file_count INTEGER DEFAULT 0,
    chunk_count INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Source file index
CREATE TABLE files (
    id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    path TEXT NOT NULL UNIQUE,          -- Relative path from project root
    language TEXT,
    last_modified DATETIME,
    content_hash TEXT,                  -- SHA256 for change detection
    indexed_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Content chunks with embeddings
CREATE TABLE chunks (
    id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    file_id TEXT NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    content TEXT NOT NULL,
    start_line INTEGER,
    end_line INTEGER,
    chunk_type TEXT DEFAULT 'code',     -- code, comment, config, test, docs
    embedding BLOB,                     -- Vector stored as blob for sqlite-vec
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Persistent memories (decisions, conventions, constraints)
CREATE TABLE memories (
    id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    content TEXT NOT NULL,
    memory_type TEXT NOT NULL,           -- decision, convention, constraint, note, todo
    importance REAL DEFAULT 0.5,         -- 0.0 to 1.0, affects retrieval priority
    embedding BLOB,
    source TEXT,                         -- 'user' (manual) or 'extracted' (from session)
    related_files TEXT,                  -- JSON array of file paths
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Session history
CREATE TABLE sessions (
    id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    question TEXT NOT NULL,
    context_used TEXT,                   -- JSON: which chunks/memories were injected
    response_summary TEXT,               -- Brief summary of the AI response
    model_used TEXT,
    tokens_used INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Virtual table for vector similarity search (sqlite-vec)
CREATE VIRTUAL TABLE vec_chunks USING vec0(
    id TEXT PRIMARY KEY,
    embedding float[384]                -- Dimension depends on embedding model
);

CREATE VIRTUAL TABLE vec_memories USING vec0(
    id TEXT PRIMARY KEY,
    embedding float[384]
);

-- Indexes
CREATE INDEX idx_memories_type ON memories(memory_type);
CREATE INDEX idx_chunks_file ON chunks(file_id);
CREATE INDEX idx_sessions_created ON sessions(created_at DESC);
CREATE INDEX idx_files_path ON files(path);
```

### 6.4 File Chunking Strategy

Files are split into meaningful chunks for embedding and retrieval:

```
Strategy by file type:

Ruby/Python/JS/Go/Rust:
  - Split by: class, module, method/function boundaries
  - Max chunk size: 150 lines or 2000 tokens
  - Include: file path + class/method name as chunk header
  - Overlap: 10 lines between chunks for context continuity

Config files (routes.rb, database.yml, docker-compose.yml):
  - Treat as single chunk if < 200 lines
  - Split by top-level sections otherwise

Markdown/Docs:
  - Split by heading (## or ###)
  - Max chunk size: 1500 tokens

Test files:
  - Split by describe/context blocks
  - Lower priority in retrieval (importance: 0.3)

Migration files:
  - Single chunk each
  - Medium priority (importance: 0.5)

Skip entirely:
  - Binary files
  - node_modules/, vendor/, .git/
  - Files matching .gitignore patterns
  - Lock files (Gemfile.lock, yarn.lock, package-lock.json)
  - Generated files (schema.rb can be optionally included)
```

### 6.5 Context Building Algorithm

When `memvra ask` is called, the context builder assembles the prompt:

```
function buildContext(question, tokenBudget = 8000):

    context = {}
    remaining = tokenBudget

    // 1. Always include project profile (est. 300-500 tokens)
    context.profile = loadProjectProfile()
    remaining -= tokenCount(context.profile)

    // 2. Always include conventions/constraints (est. 200-500 tokens)
    context.conventions = loadConventions()
    remaining -= tokenCount(context.conventions)

    // 3. If specific files requested via --files flag, include them
    if flagFiles:
        context.requestedFiles = loadFiles(flagFiles)
        remaining -= tokenCount(context.requestedFiles)

    // 4. Embed question, find relevant chunks
    questionEmbedding = embed(question)

    relevantChunks = vectorSearch(
        table: vec_chunks,
        query: questionEmbedding,
        limit: 20,
        minSimilarity: 0.3
    )

    // 5. Find relevant memories
    relevantMemories = vectorSearch(
        table: vec_memories,
        query: questionEmbedding,
        limit: 10,
        minSimilarity: 0.3
    )

    // 6. Rank and fill remaining budget
    ranked = rankByRelevance(relevantChunks + relevantMemories)

    for item in ranked:
        itemTokens = tokenCount(item)
        if itemTokens <= remaining:
            context.add(item)
            remaining -= itemTokens
        else:
            break

    // 7. If room, add recent session summary
    if remaining > 200:
        context.recentSession = loadLastSessionSummary()

    return formatAsPrompt(context)
```

### 6.6 LLM Adapter Interface

Each LLM provider implements a common interface:

```go
type LLMAdapter interface {
    // Send a prompt and stream the response
    Complete(ctx context.Context, request CompletionRequest) (<-chan StreamChunk, error)

    // Generate embeddings for text
    Embed(ctx context.Context, texts []string) ([][]float32, error)

    // Return model capabilities and constraints
    Info() ModelInfo
}

type CompletionRequest struct {
    SystemPrompt string
    Context      string      // Injected by Memvra
    UserMessage  string      // The actual question
    Model        string
    MaxTokens    int
    Temperature  float64
    Stream       bool
}

type ModelInfo struct {
    Name            string
    Provider        string
    MaxContextWindow int
    SupportsStreaming bool
    EmbeddingDimension int  // 0 if not an embedding model
}
```

**Supported providers (MVP):**

| Provider | Completion Model | Embedding Model | Auth |
|----------|-----------------|-----------------|------|
| Claude | claude-sonnet-4-20250514 | N/A (use OpenAI) | ANTHROPIC_API_KEY |
| OpenAI | gpt-4o | text-embedding-3-small | OPENAI_API_KEY |
| Gemini | gemini-2.0-flash | text-embedding-004 | GEMINI_API_KEY |
| Ollama | any local model | nomic-embed-text | Local (no key) |

---

## 7. Configuration

### 7.1 Global Config (`~/.config/memvra/config.toml`)

```toml
# Default LLM provider for completions
default_model = "claude"

# Default embedding provider
default_embedder = "ollama"  # "ollama", "openai", "gemini"

# API Keys (can also use environment variables)
# ANTHROPIC_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY
[keys]
# anthropic = "sk-ant-..."    # Prefer env vars over config file
# openai = "sk-..."

[ollama]
host = "http://localhost:11434"
embed_model = "nomic-embed-text"
completion_model = "llama3.2"

[context]
max_tokens = 8000             # Token budget for context injection
chunk_max_lines = 150         # Max lines per code chunk
similarity_threshold = 0.3    # Min similarity score for retrieval
top_k_chunks = 10             # Max chunks to retrieve
top_k_memories = 5            # Max memories to retrieve

[output]
stream = true                 # Stream responses by default
color = true                  # Colored terminal output
verbose = false               # Show context metadata
```

### 7.2 Project Config (`.memvra/config.toml`)

```toml
# Override global settings per project
default_model = "claude"

[project]
name = "rfe-ready"

# Files/patterns to always include in context
always_include = [
    "config/routes.rb",
    "doc/ARCHITECTURE.md"
]

# Files/patterns to always exclude from indexing
exclude = [
    "spec/fixtures/**",
    "tmp/**",
    "log/**"
]

# Custom conventions (appended to auto-detected ones)
[conventions]
style = "We use service objects in app/services/ for all business logic"
api = "All API responses follow JSON:API specification"
testing = "Every service object must have a corresponding spec"
auth = "Custom JWT authentication, no Devise"
```

---

## 8. Distribution & Installation

### 8.1 Installation Methods

```bash
# macOS (Homebrew)
brew tap memvra/tap
brew install memvra

# Linux (apt)
curl -fsSL https://memvra.com/install.sh | sh

# npm (for Node.js developers — wraps the Go binary)
npm install -g memvra

# Go install
go install github.com/memvra/memvra@latest

# Direct download
curl -L https://github.com/memvra/memvra/releases/latest/download/memvra-$(uname -s)-$(uname -m) -o /usr/local/bin/memvra
chmod +x /usr/local/bin/memvra
```

### 8.2 Binary Distribution

The Go binary is compiled for:

| OS | Architecture | Binary Name |
|----|-------------|-------------|
| macOS | arm64 (Apple Silicon) | memvra-darwin-arm64 |
| macOS | amd64 (Intel) | memvra-darwin-amd64 |
| Linux | amd64 | memvra-linux-amd64 |
| Linux | arm64 | memvra-linux-arm64 |
| Windows | amd64 | memvra-windows-amd64.exe |

SQLite and sqlite-vec are statically compiled into the binary. No external dependencies required.

### 8.3 First-Time Setup

```bash
$ memvra setup

Welcome to Memvra! Let's configure your AI memory layer.

🤖 Which LLM do you primarily use?
   [1] Claude (Anthropic)
   [2] OpenAI (GPT-4)
   [3] Gemini (Google)
   [4] Ollama (Local)

> 1

🔑 Enter your Anthropic API key (or set ANTHROPIC_API_KEY env var):
> sk-ant-...

🧠 For embeddings, do you want:
   [1] Local embeddings via Ollama (private, free, requires Ollama installed)
   [2] OpenAI embeddings (better quality, costs ~$0.001 per indexing)

> 1

✅ Configuration saved to ~/.config/memvra/config.toml
💡 Navigate to any project and run `memvra init` to get started.
```

---

## 9. Project Structure (Go Repository)

```
memvra/
├── cmd/
│   └── memvra/
│       └── main.go                 # Entry point
├── internal/
│   ├── cli/                        # CLI command definitions (Cobra)
│   │   ├── root.go
│   │   ├── init.go
│   │   ├── ask.go
│   │   ├── remember.go
│   │   ├── context.go
│   │   ├── forget.go
│   │   ├── status.go
│   │   ├── update.go
│   │   ├── export.go
│   │   └── setup.go
│   ├── scanner/                    # Project scanning and indexing
│   │   ├── scanner.go              # Main scanner orchestrator
│   │   ├── detector.go             # Tech stack detection
│   │   ├── chunker.go              # File chunking logic
│   │   ├── gitignore.go            # .gitignore pattern matching
│   │   └── languages.go            # Language-specific parsing rules
│   ├── memory/                     # Memory orchestration
│   │   ├── orchestrator.go         # Main memory orchestrator
│   │   ├── store.go                # SQLite read/write operations
│   │   ├── vector.go               # sqlite-vec operations
│   │   ├── ranker.go               # Relevance ranking
│   │   └── types.go                # Memory type definitions
│   ├── context/                    # Context building for prompts
│   │   ├── builder.go              # Context assembly with token budget
│   │   ├── formatter.go            # Format context into prompt sections
│   │   └── tokenizer.go            # Token counting (tiktoken-go)
│   ├── adapter/                    # LLM provider adapters
│   │   ├── adapter.go              # Common interface
│   │   ├── claude.go               # Anthropic Claude adapter
│   │   ├── openai.go               # OpenAI adapter
│   │   ├── gemini.go               # Google Gemini adapter
│   │   ├── ollama.go               # Ollama local adapter
│   │   └── embedder.go             # Embedding provider abstraction
│   ├── export/                     # Export to other tool formats
│   │   ├── exporter.go             # Export interface
│   │   ├── claude_md.go            # CLAUDE.md format
│   │   ├── cursorrules.go          # .cursorrules format
│   │   └── markdown.go             # Generic markdown
│   ├── config/                     # Configuration management
│   │   ├── config.go               # Config loading/saving
│   │   ├── global.go               # Global config (~/.config/memvra/)
│   │   └── project.go              # Project config (.memvra/)
│   └── db/                         # Database management
│       ├── db.go                   # SQLite connection + migrations
│       ├── migrations.go           # Schema migrations
│       └── schema.sql              # Initial schema
├── pkg/                            # Public packages (if any)
├── scripts/
│   ├── build.sh                    # Cross-compilation build script
│   ├── install.sh                  # curl-pipe installer
│   └── release.sh                  # GitHub release automation
├── testdata/                       # Test fixtures
│   ├── rails_project/
│   ├── node_project/
│   └── go_project/
├── .goreleaser.yaml                # GoReleaser config for distribution
├── go.mod
├── go.sum
├── Makefile
├── README.md
├── LICENSE                         # MIT or Apache 2.0
└── CHANGELOG.md
```

---

## 10. Key Go Dependencies

```go
// go.mod dependencies (core)

github.com/spf13/cobra           // CLI framework
github.com/spf13/viper           // Configuration management
github.com/charmbracelet/bubbletea  // Interactive TUI components
github.com/charmbracelet/lipgloss   // Terminal styling
github.com/mattn/go-sqlite3      // SQLite driver (CGo)
github.com/asg017/sqlite-vec-go-bindings  // sqlite-vec for vector search
github.com/pkoukk/tiktoken-go    // Token counting
github.com/sashabaranov/go-openai    // OpenAI API client
github.com/liushuangls/go-anthropic  // Anthropic API client
github.com/go-git/go-git/v5      // Git operations (pure Go)
github.com/BurntSushi/toml       // TOML config parsing
github.com/sabhiram/go-gitignore // .gitignore pattern matching
github.com/schollz/progressbar/v3  // Progress bars for indexing
```

---

## 11. Prompt Templates

### 11.1 System Prompt Template (injected on every `ask`)

```
You are an AI assistant working on the project "{{project_name}}".

## Project Profile
- Language: {{language}}
- Framework: {{framework}} {{framework_version}}
- Database: {{database}}
- Architecture: {{architecture_pattern}}
{{#if api_mode}}- API Mode: Yes (no server-rendered views){{/if}}

## Coding Conventions
{{#each conventions}}
- {{this}}
{{/each}}

## Active Constraints
{{#each constraints}}
- {{this}}
{{/each}}

## Relevant Decisions
{{#each decisions}}
### {{this.title}}
{{this.description}}
{{/each}}

## Relevant Source Code
{{#each code_chunks}}
### {{this.file_path}} (lines {{this.start_line}}-{{this.end_line}})
```{{this.language}}
{{this.content}}
```
{{/each}}

When answering:
1. Respect the established conventions and constraints above
2. Reference specific files and line numbers when relevant
3. Be consistent with existing patterns in the codebase
4. Flag if a suggestion contradicts any stored decisions or constraints
```

---

## 12. Development Phases

### Phase 1: Foundation (Weeks 1-2)
- [ ] Initialize Go project with Cobra CLI skeleton
- [ ] Implement SQLite database layer with migrations
- [ ] Build project scanner (directory tree, tech stack detection)
- [ ] Implement file chunking (basic: by line count)
- [ ] Set up config management (global + project TOML)
- [ ] `memvra init` command (scan + store profile)
- [ ] `memvra status` command

### Phase 2: Memory & Retrieval (Weeks 3-4)
- [ ] Integrate sqlite-vec for vector search
- [ ] Implement embedding providers (Ollama, OpenAI)
- [ ] Build context builder with token budget management
- [ ] Implement memory store (remember/forget)
- [ ] `memvra remember` command
- [ ] `memvra forget` command
- [ ] `memvra context` command

### Phase 3: LLM Integration (Weeks 5-6)
- [ ] Build LLM adapter interface
- [ ] Implement Claude adapter (streaming)
- [ ] Implement OpenAI adapter (streaming)
- [ ] Implement Ollama adapter (streaming)
- [ ] `memvra ask` command with full context injection
- [ ] Session logging

### Phase 4: Polish & Distribution (Weeks 7-8)
- [ ] `memvra update` command (incremental re-indexing)
- [ ] `memvra export` command (CLAUDE.md, .cursorrules, markdown)
- [ ] `memvra setup` command (interactive first-time config)
- [ ] Smarter chunking (AST-aware for Ruby, JS, Go, Python)
- [ ] GoReleaser setup for cross-platform binaries
- [ ] Homebrew tap
- [ ] npm wrapper package
- [ ] README, docs, demo GIF
- [ ] Public launch

### Phase 5: Post-MVP Enhancements
- [ ] Auto-extract decisions from AI responses
- [ ] Git hook integration (auto-update on commit)
- [ ] Session summaries (auto-summarize each conversation)
- [ ] Gemini adapter
- [ ] Watch mode (`memvra watch` — auto-update on file changes)
- [ ] Plugin system for custom adapters
- [ ] `memvra diff` — show how context changed between sessions

---

## 13. Success Metrics (MVP)

| Metric | Target |
|--------|--------|
| Time to install + first query | < 3 minutes |
| Context injection latency | < 500ms |
| Indexing speed (500 files) | < 30 seconds |
| Embedding generation (local) | < 60 seconds for 500 files |
| Binary size | < 30 MB |
| SQLite DB size (avg project) | < 50 MB |
| Retrieval relevance (subjective) | Developer says "it included the right files" 80%+ of the time |

---

## 14. Competitive Differentiation

| Feature | Memvra | CLAUDE.md | .cursorrules | Pieces.app |
|---------|--------|-----------|-------------|------------|
| Model-agnostic | ✅ | ❌ Claude only | ❌ Cursor only | Partial |
| Editor-agnostic | ✅ | ❌ | ❌ | ❌ |
| Auto-indexing | ✅ | ❌ Manual | ❌ Manual | ✅ |
| Semantic search | ✅ | ❌ | ❌ | ✅ |
| Decision tracking | ✅ | ❌ | ❌ | ❌ |
| Exports to other formats | ✅ | N/A | N/A | ❌ |
| Local-first / private | ✅ | ✅ | ✅ | ❌ |
| Open source | ✅ | N/A | N/A | ❌ |
| Zero dependencies | ✅ | ✅ | ✅ | ❌ |

---

## 15. Open Questions for Development

1. **Embedding model choice**: Should we default to local (Ollama) and require it as a dependency, or default to cloud (OpenAI) for easier setup and use local as opt-in?

2. **AST parsing**: Should Phase 1 include AST-aware chunking for better code understanding, or is line-based splitting sufficient for MVP?

3. **Session management**: Should `memvra ask` automatically log every interaction, or should sessions be opt-in? Auto-logging could grow the DB quickly.

4. **Context file editability**: The `.memvra/context.md` file is auto-generated but human-editable. How should conflicts be handled when `memvra update` runs? Should user edits be preserved or overwritten?

5. **Multi-repo support**: Some developers work in monorepos or multi-service architectures. Should Memvra support linking multiple `.memvra/` databases for cross-project context?

6. **Pricing model for future cloud tier**: Per-seat monthly? Per-project? Usage-based? Free for open source projects?

---

## 16. Links & Resources

- **Domain**: memvra.com (to be registered)
- **GitHub**: github.com/memvra/memvra (to be created)
- **License**: MIT (recommended for maximum adoption)
- **Inspiration**: Claude Code memory, Cursor context, Pieces Long-Term Memory, Zed's context system

---

*This document serves as the complete specification for Memvra v1. It can be shared with developers, used as context for AI coding assistants, or referenced during implementation.*
