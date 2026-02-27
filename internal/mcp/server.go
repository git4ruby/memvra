// Package mcp implements a Model Context Protocol server that exposes
// Memvra's memory and context capabilities as MCP tools. AI coding
// assistants (Claude Code, Cursor, Windsurf) can call these tools
// automatically to save progress, store decisions, and retrieve context.
package mcp

import (
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/memvra/memvra/internal/config"
	"github.com/memvra/memvra/internal/db"
	"github.com/memvra/memvra/internal/memory"
)

// Server wraps a Memvra project database and exposes it as an MCP tool server.
type Server struct {
	root     string
	database *db.DB
	store    *memory.Store
	vectors  *memory.VectorStore
}

// NewServer opens the Memvra database at the given project root and prepares
// an MCP server. Call Run() to start serving over stdio.
func NewServer(root string) (*Server, error) {
	dbPath := config.ProjectDBPath(root)
	database, err := db.Open(dbPath)
	if err != nil {
		return nil, err
	}

	return &Server{
		root:     root,
		database: database,
		store:    memory.NewStore(database),
		vectors:  memory.NewVectorStore(database),
	}, nil
}

// Run registers all MCP tools and blocks serving over stdio until the
// client disconnects. This is the main entry point for `memvra mcp`.
func (s *Server) Run() error {
	mcpServer := server.NewMCPServer(
		"memvra",
		"1.0.0",
		server.WithToolCapabilities(false),
		server.WithInstructions(serverInstructions),
	)

	s.registerTools(mcpServer)

	return server.ServeStdio(mcpServer)
}

// Close releases the database connection.
func (s *Server) Close() {
	if s.database != nil {
		_ = s.database.Close()
	}
}

const serverInstructions = `Memvra gives you persistent memory across AI sessions. Use these tools to:
- Save your progress before ending a session (memvra_save_progress)
- Store important decisions and constraints (memvra_remember)
- Retrieve relevant project context (memvra_get_context)
- Search code and memories semantically (memvra_search)

IMPORTANT: Always call memvra_save_progress before ending a conversation or when
the user is about to switch to a different AI tool. This ensures continuity.`

// registerTools adds all Memvra tools to the MCP server.
func (s *Server) registerTools(mcpServer *server.MCPServer) {
	mcpServer.AddTool(s.toolSaveProgress())
	mcpServer.AddTool(s.toolRemember())
	mcpServer.AddTool(s.toolGetContext())
	mcpServer.AddTool(s.toolSearch())
	mcpServer.AddTool(s.toolForget())
	mcpServer.AddTool(s.toolProjectStatus())
	mcpServer.AddTool(s.toolListMemories())
	mcpServer.AddTool(s.toolListSessions())
}

// toolSaveProgress returns the tool definition and handler for saving
// the AI's current work progress. This is the key tool that enables
// "continue" across different AI tools.
func (s *Server) toolSaveProgress() (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("memvra_save_progress",
		mcp.WithDescription("Save what you're currently working on so another AI can continue later. Call this before ending a session or when the user might switch tools."),
		mcp.WithString("task",
			mcp.Description("What you were working on (e.g. 'implementing auth middleware')"),
			mcp.Required(),
		),
		mcp.WithString("summary",
			mcp.Description("What was done, key decisions made, and next steps"),
			mcp.Required(),
		),
		mcp.WithString("model",
			mcp.Description("Your model name (e.g. 'claude', 'gemini', 'cursor')"),
			mcp.Required(),
		),
		mcp.WithArray("files_touched",
			mcp.Description("Files modified during this work session"),
			mcp.WithStringItems(),
		),
	)
	return tool, s.handleSaveProgress
}

// toolRemember returns the tool definition and handler for storing
// a decision, convention, constraint, or note.
func (s *Server) toolRemember() (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("memvra_remember",
		mcp.WithDescription("Store a project decision, convention, constraint, or note that should persist across sessions."),
		mcp.WithString("content",
			mcp.Description("The fact to remember (e.g. 'We use JWT auth with RS256')"),
			mcp.Required(),
		),
		mcp.WithString("type",
			mcp.Description("Memory type"),
			mcp.Enum("decision", "convention", "constraint", "note", "todo"),
		),
	)
	return tool, s.handleRemember
}

// toolGetContext returns the tool definition and handler for retrieving
// the full project context.
func (s *Server) toolGetContext() (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("memvra_get_context",
		mcp.WithDescription("Retrieve the project's full context: tech stack, decisions, conventions, constraints, recent activity, and relevant code. Use at the start of a session or when you need project background."),
		mcp.WithString("question",
			mcp.Description("Optional focus query to retrieve the most relevant context"),
		),
	)
	return tool, s.handleGetContext
}

// toolSearch returns the tool definition and handler for semantic search.
func (s *Server) toolSearch() (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("memvra_search",
		mcp.WithDescription("Search across code chunks and stored memories using semantic similarity."),
		mcp.WithString("query",
			mcp.Description("What to search for"),
			mcp.Required(),
		),
		mcp.WithNumber("top_k",
			mcp.Description("Maximum number of results"),
			mcp.DefaultNumber(10),
		),
	)
	return tool, s.handleSearch
}

// toolForget returns the tool definition and handler for deleting a memory.
func (s *Server) toolForget() (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("memvra_forget",
		mcp.WithDescription("Delete a specific memory by its ID."),
		mcp.WithString("id",
			mcp.Description("The memory ID to delete"),
			mcp.Required(),
		),
	)
	return tool, s.handleForget
}

// toolProjectStatus returns the tool definition and handler for getting
// project stats.
func (s *Server) toolProjectStatus() (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("memvra_project_status",
		mcp.WithDescription("Get project statistics: files indexed, memories stored, recent sessions, tech stack."),
	)
	return tool, s.handleProjectStatus
}

// toolListMemories returns the tool definition and handler for listing
// stored memories.
func (s *Server) toolListMemories() (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("memvra_list_memories",
		mcp.WithDescription("List all stored memories, optionally filtered by type."),
		mcp.WithString("type",
			mcp.Description("Filter by memory type"),
			mcp.Enum("decision", "convention", "constraint", "note", "todo"),
		),
	)
	return tool, s.handleListMemories
}

// toolListSessions returns the tool definition and handler for listing
// recent sessions.
func (s *Server) toolListSessions() (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool("memvra_list_sessions",
		mcp.WithDescription("List recent AI sessions (questions asked, summaries, which model was used)."),
		mcp.WithNumber("limit",
			mcp.Description("How many recent sessions to return"),
			mcp.DefaultNumber(10),
		),
	)
	return tool, s.handleListSessions
}
