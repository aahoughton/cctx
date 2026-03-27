package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Store provides read access to Claude Code's local project and conversation data.
type Store struct {
	// BaseDir is the path to the Claude projects directory (typically ~/.claude/projects).
	BaseDir string
}

// NewStore creates a Store rooted at the given base directory.
func NewStore(baseDir string) *Store {
	return &Store{BaseDir: baseDir}
}

// DefaultStore returns a Store using the default ~/.claude/projects location.
func DefaultStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("finding home directory: %w", err)
	}
	return NewStore(filepath.Join(home, ".claude", "projects")), nil
}

// DecodeDirName converts an encoded project directory name back to a filesystem path
// using a naive replacement of every "-" with "/". This is ambiguous because literal
// hyphens in path components are also encoded as "-". Use ResolveProjectPath for
// accurate results.
func DecodeDirName(name string) string {
	if name == "" {
		return ""
	}
	return "/" + strings.ReplaceAll(name[1:], "-", "/")
}

// EncodeDirName converts a filesystem path to the encoded directory name format.
func EncodeDirName(path string) string {
	if path == "" {
		return ""
	}
	return "-" + strings.ReplaceAll(strings.TrimPrefix(path, "/"), "/", "-")
}

// ResolveProjectPath determines the real filesystem path for an encoded project
// directory name. The encoding is lossy (both "/" and literal "-" become "-"),
// so we resolve the ambiguity using these sources, in priority order:
//
//  1. originalPath from sessions-index.json (authoritative when present)
//  2. cwd from the first conversation record that has one
//  3. Filesystem walk trying both "/" and "-" at each encoded "-"
//  4. Naive decode (all "-" become "/") as last resort
func ResolveProjectPath(projectDir string) string {
	// Strategy 1: sessions-index.json
	if path := resolveFromIndex(projectDir); path != "" {
		return path
	}

	// Strategy 2: cwd from conversation JSONL files
	if path := resolveFromConversationCWD(projectDir); path != "" {
		return path
	}

	// Strategy 3: filesystem walk
	if path := resolveByFilesystemWalk(projectDir); path != "" {
		return path
	}

	// Strategy 4: naive decode
	return DecodeDirName(filepath.Base(projectDir))
}

func resolveFromIndex(projectDir string) string {
	data, err := os.ReadFile(filepath.Join(projectDir, "sessions-index.json"))
	if err != nil {
		return ""
	}
	var idx struct {
		OriginalPath string `json:"originalPath"`
	}
	if json.Unmarshal(data, &idx) == nil && idx.OriginalPath != "" {
		return idx.OriginalPath
	}
	return ""
}

func resolveFromConversationCWD(projectDir string) string {
	matches, err := filepath.Glob(filepath.Join(projectDir, "*.jsonl"))
	if err != nil || len(matches) == 0 {
		return ""
	}

	// Check the first conversation file — read just enough lines to find a cwd
	f, err := os.Open(matches[0])
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	for i := 0; scanner.Scan() && i < 20; i++ {
		var rec struct {
			CWD string `json:"cwd"`
		}
		if json.Unmarshal(scanner.Bytes(), &rec) == nil && rec.CWD != "" {
			return rec.CWD
		}
	}
	return ""
}

func resolveByFilesystemWalk(projectDir string) string {
	name := filepath.Base(projectDir)
	if name == "" || !strings.HasPrefix(name, "-") {
		return ""
	}

	// segments between "-" delimiters (skip leading "-")
	parts := strings.Split(name[1:], "-")
	if len(parts) == 0 {
		return ""
	}

	// DFS: at each part boundary, try extending current path component with "-"
	// or starting a new "/" separated component.
	type candidate struct {
		path string // path built so far
		idx  int    // next index into parts to consume
	}

	queue := []candidate{{path: "/" + parts[0], idx: 1}}

	for len(queue) > 0 {
		c := queue[len(queue)-1]
		queue = queue[:len(queue)-1]

		if c.idx == len(parts) {
			// Full path consumed — check if it exists
			if _, err := os.Stat(c.path); err == nil {
				return c.path
			}
			continue
		}

		next := parts[c.idx]

		// Option A: this "-" is a "/" (directory separator)
		queue = append(queue, candidate{
			path: c.path + "/" + next,
			idx:  c.idx + 1,
		})

		// Option B: this "-" is a literal hyphen in the same path component
		queue = append(queue, candidate{
			path: c.path + "-" + next,
			idx:  c.idx + 1,
		})
	}

	return ""
}

// Projects returns all discovered project directories.
func (s *Store) Projects() ([]Project, error) {
	entries, err := os.ReadDir(s.BaseDir)
	if err != nil {
		return nil, fmt.Errorf("reading projects directory %s: %w", s.BaseDir, err)
	}

	var projects []Project
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "-") {
			continue
		}
		fullProjDir := filepath.Join(s.BaseDir, name)
		origPath := ResolveProjectPath(fullProjDir)
		_, statErr := os.Stat(origPath)
		projects = append(projects, Project{
			DirName:      name,
			OriginalPath: origPath,
			Orphaned:     os.IsNotExist(statErr),
		})
	}

	sort.Slice(projects, func(i, j int) bool {
		return projects[i].OriginalPath < projects[j].OriginalPath
	})
	return projects, nil
}

// Conversations returns all conversations for a given project directory name.
// It first tries to read sessions-index.json; if that's missing or has an
// unexpected version, it falls back to scanning .jsonl files directly.
func (s *Store) Conversations(projectDir string) ([]Conversation, error) {
	projPath := filepath.Join(s.BaseDir, projectDir)

	convs, err := s.conversationsFromIndex(projPath)
	if err == nil {
		return convs, nil
	}

	return s.conversationsFromFiles(projPath)
}

func (s *Store) conversationsFromIndex(projPath string) ([]Conversation, error) {
	indexPath := filepath.Join(projPath, "sessions-index.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, err
	}

	var idx SessionsIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parsing sessions-index.json: %w", err)
	}

	if idx.Version != SchemaVersion {
		return nil, fmt.Errorf(
			"sessions-index.json version %d does not match expected version %d — "+
				"Claude's storage format may have changed",
			idx.Version, SchemaVersion,
		)
	}

	var convs []Conversation
	for _, e := range idx.Entries {
		if e.IsSidechain {
			continue
		}
		created, _ := time.Parse(time.RFC3339Nano, e.Created)
		modified, _ := time.Parse(time.RFC3339Nano, e.Modified)
		convs = append(convs, Conversation{
			SessionID:    e.SessionID,
			Summary:      e.Summary,
			FirstPrompt:  e.FirstPrompt,
			MessageCount: e.MessageCount,
			Created:      created,
			Modified:     modified,
			GitBranch:    e.GitBranch,
			FilePath:     e.FullPath,
		})
	}

	sort.Slice(convs, func(i, j int) bool {
		return convs[i].Modified.After(convs[j].Modified)
	})
	return convs, nil
}

func (s *Store) conversationsFromFiles(projPath string) ([]Conversation, error) {
	matches, err := filepath.Glob(filepath.Join(projPath, "*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("globbing conversation files: %w", err)
	}

	var convs []Conversation
	for _, fpath := range matches {
		conv, err := s.parseConversationFile(fpath)
		if err != nil {
			continue // skip unparseable files
		}
		convs = append(convs, conv)
	}

	sort.Slice(convs, func(i, j int) bool {
		return convs[i].Modified.After(convs[j].Modified)
	})
	return convs, nil
}

func (s *Store) parseConversationFile(fpath string) (Conversation, error) {
	f, err := os.Open(fpath)
	if err != nil {
		return Conversation{}, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return Conversation{}, err
	}

	var (
		conv       Conversation
		firstTime  time.Time
		lastTime   time.Time
		msgCount   int
		slug       string
		sessionID  string
		firstPrompt string
	)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max line
	for scanner.Scan() {
		var rec ConversationRecord
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			continue
		}

		if rec.SessionID != "" && sessionID == "" {
			sessionID = rec.SessionID
		}
		if rec.Slug != "" {
			slug = rec.Slug
		}

		if rec.Timestamp != "" {
			t, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
			if err == nil {
				if firstTime.IsZero() || t.Before(firstTime) {
					firstTime = t
				}
				if t.After(lastTime) {
					lastTime = t
				}
			}
		}

		if rec.Type == "user" || rec.Type == "assistant" {
			msgCount++
		}

		if rec.Type == "user" && firstPrompt == "" && rec.Message != nil {
			if s, ok := rec.Message.Content.(string); ok {
				firstPrompt = truncate(s, 200)
			}
		}
	}

	conv.SessionID = sessionID
	conv.Slug = slug
	conv.FirstPrompt = firstPrompt
	conv.MessageCount = msgCount
	conv.Created = firstTime
	conv.Modified = lastTime
	conv.FilePath = fpath

	if conv.Modified.IsZero() {
		conv.Modified = info.ModTime()
	}

	return conv, nil
}

// ReadConversation reads all records from a conversation JSONL file.
func (s *Store) ReadConversation(fpath string) ([]ConversationRecord, error) {
	f, err := os.Open(fpath)
	if err != nil {
		return nil, fmt.Errorf("opening conversation file: %w", err)
	}
	defer f.Close()

	var records []ConversationRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		var rec ConversationRecord
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			continue
		}
		records = append(records, rec)
	}
	return records, scanner.Err()
}

// MessageText extracts the plain text from a ConversationRecord's message.
func MessageText(rec ConversationRecord) string {
	if rec.Message == nil {
		return ""
	}
	switch v := rec.Message.Content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, block := range v {
			if m, ok := block.(map[string]interface{}); ok {
				if t, ok := m["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// FindConversation looks up a conversation by session ID prefix within a project.
func (s *Store) FindConversation(projectDir, sessionPrefix string) (*Conversation, error) {
	convs, err := s.Conversations(projectDir)
	if err != nil {
		return nil, err
	}

	var matches []Conversation
	for _, c := range convs {
		if strings.HasPrefix(c.SessionID, sessionPrefix) {
			matches = append(matches, c)
		}
	}

	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no conversation matching prefix %q", sessionPrefix)
	case 1:
		return &matches[0], nil
	default:
		return nil, fmt.Errorf("ambiguous prefix %q matches %d conversations", sessionPrefix, len(matches))
	}
}

// FindProjectByPath finds a project whose original path matches or contains the given path.
func (s *Store) FindProjectByPath(path string) (*Project, error) {
	projects, err := s.Projects()
	if err != nil {
		return nil, err
	}

	// Exact match first
	for _, p := range projects {
		if p.OriginalPath == path {
			return &p, nil
		}
	}

	// Substring match on path boundary
	for _, p := range projects {
		if strings.HasSuffix(p.OriginalPath, "/"+path) {
			return &p, nil
		}
	}

	return nil, fmt.Errorf("no project matching path %q", path)
}

// UpdateSessionsIndex reads, modifies, and writes back the sessions-index.json
// for a project. If the index file doesn't exist, it bootstraps one from the
// JSONL conversation files in the project directory.
func (s *Store) UpdateSessionsIndex(projectDir string, updateFn func(*SessionsIndex) error) error {
	indexPath := filepath.Join(s.BaseDir, projectDir, "sessions-index.json")
	projPath := filepath.Join(s.BaseDir, projectDir)

	var idx SessionsIndex

	data, err := os.ReadFile(indexPath)
	if os.IsNotExist(err) {
		idx, err = s.bootstrapIndex(projPath)
		if err != nil {
			return fmt.Errorf("bootstrapping sessions-index.json: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("reading sessions-index.json: %w", err)
	} else {
		if err := json.Unmarshal(data, &idx); err != nil {
			return fmt.Errorf("parsing sessions-index.json: %w", err)
		}
		if idx.Version != SchemaVersion {
			return fmt.Errorf(
				"sessions-index.json version %d does not match expected version %d",
				idx.Version, SchemaVersion,
			)
		}
	}

	if err := updateFn(&idx); err != nil {
		return err
	}

	out, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling sessions-index.json: %w", err)
	}

	return atomicWrite(indexPath, out, 0600)
}

// bootstrapIndex creates a SessionsIndex by scanning JSONL files in a project directory.
func (s *Store) bootstrapIndex(projPath string) (SessionsIndex, error) {
	origPath := ResolveProjectPath(projPath)
	idx := SessionsIndex{
		Version:      SchemaVersion,
		OriginalPath: origPath,
	}

	convs, err := s.conversationsFromFiles(projPath)
	if err != nil {
		return idx, err
	}

	for _, c := range convs {
		// Match Claude's timestamp format: ISO 8601 with milliseconds and Z suffix
		const msFormat = "2006-01-02T15:04:05.000Z"
		var fileMtime int64
		if !c.Modified.IsZero() {
			fileMtime = c.Modified.UnixMilli()
		}
		idx.Entries = append(idx.Entries, IndexEntry{
			SessionID:    c.SessionID,
			FullPath:     c.FilePath,
			FileMtime:    fileMtime,
			FirstPrompt:  c.FirstPrompt,
			Summary:      c.Summary,
			MessageCount: c.MessageCount,
			Created:      c.Created.UTC().Format(msFormat),
			Modified:     c.Modified.UTC().Format(msFormat),
			GitBranch:    c.GitBranch,
			ProjectPath:  origPath,
		})
	}

	return idx, nil
}

func truncate(s string, maxLen int) string {
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen]) + "..."
}
