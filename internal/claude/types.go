// Package claude provides types and parsers for Claude Code's local storage format.
//
// Claude Code stores conversation data under ~/.claude/projects/ in a structure of
// project directories (named by path encoding) containing JSONL conversation files
// and optional sessions-index.json metadata.
//
// This package is designed to detect upstream format changes via schema validation,
// so that breaking changes in Claude's storage format surface as clear errors rather
// than silent data corruption.
package claude

import "time"

// SchemaVersion tracks the expected format version of sessions-index.json.
// Bump this when the upstream format changes and this code is updated to match.
const SchemaVersion = 1

// SessionsIndex represents the sessions-index.json file found in each project directory.
type SessionsIndex struct {
	Version      int          `json:"version"`
	Entries      []IndexEntry `json:"entries"`
	OriginalPath string       `json:"originalPath"`
}

// IndexEntry is one conversation's metadata in sessions-index.json.
type IndexEntry struct {
	SessionID   string `json:"sessionId"`
	FullPath    string `json:"fullPath"`
	FileMtime   int64  `json:"fileMtime"`
	FirstPrompt string `json:"firstPrompt"`
	Summary     string `json:"summary"`
	MessageCount int   `json:"messageCount"`
	Created     string `json:"created"`
	Modified    string `json:"modified"`
	GitBranch   string `json:"gitBranch"`
	ProjectPath string `json:"projectPath"`
	IsSidechain bool   `json:"isSidechain"`
}

// ConversationRecord represents a single line in a .jsonl conversation file.
// Not all fields are present on every record type.
type ConversationRecord struct {
	Type       string          `json:"type"`
	SessionID  string          `json:"sessionId,omitempty"`
	UUID       string          `json:"uuid,omitempty"`
	ParentUUID *string         `json:"parentUuid,omitempty"`
	Timestamp  string          `json:"timestamp,omitempty"`
	Slug       string          `json:"slug,omitempty"`
	CWD        string          `json:"cwd,omitempty"`
	GitBranch  string          `json:"gitBranch,omitempty"`
	Version    string          `json:"version,omitempty"`
	UserType   string          `json:"userType,omitempty"`
	Message    *MessageContent `json:"message,omitempty"`
}

// MessageContent holds the role and content of a user or assistant message.
type MessageContent struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []ContentBlock
	Model   string      `json:"model,omitempty"`
}

// ContentBlock represents a structured content block in assistant messages.
type ContentBlock struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	Name  string `json:"name,omitempty"`
	Input interface{} `json:"input,omitempty"`
}

// Project represents a discovered Claude project directory.
type Project struct {
	DirName      string // encoded directory name, e.g. "-Users-jane-src-foo"
	OriginalPath string // decoded filesystem path, e.g. "/Users/jane/src/foo"
	Orphaned     bool   // true if OriginalPath no longer exists on disk
}

// Conversation is a resolved conversation with metadata from both the index and filesystem.
type Conversation struct {
	SessionID    string
	Slug         string
	Summary      string
	FirstPrompt  string
	MessageCount int
	Created      time.Time
	Modified     time.Time
	GitBranch    string
	FilePath     string
}
