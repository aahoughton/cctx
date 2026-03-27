package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDecodeDirName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"-Users-jane-src-foo", "/Users/jane/src/foo"},
		{"-Users-jane-work-myproject", "/Users/jane/work/myproject"},
		{"", ""},
		{"-", "/"},
	}
	for _, tt := range tests {
		got := DecodeDirName(tt.input)
		if got != tt.want {
			t.Errorf("DecodeDirName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestEncodeDirName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/Users/jane/src/foo", "-Users-jane-src-foo"},
		{"/Users/jane/work/myproject", "-Users-jane-work-myproject"},
		{"", ""},
	}
	for _, tt := range tests {
		got := EncodeDirName(tt.input)
		if got != tt.want {
			t.Errorf("EncodeDirName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	// Round-trip only works for paths without literal hyphens
	paths := []string{
		"/Users/jane/src/foo",
		"/home/user/projects/bar",
		"/tmp/test",
	}
	for _, path := range paths {
		encoded := EncodeDirName(path)
		decoded := DecodeDirName(encoded)
		if decoded != path {
			t.Errorf("round-trip failed for %q: encoded=%q, decoded=%q", path, encoded, decoded)
		}
	}
}

func TestResolveProjectPath_FromIndex(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "-fake-project-dir")
	os.MkdirAll(projDir, 0755)

	// Write sessions-index.json with originalPath
	idx := map[string]interface{}{
		"version":      1,
		"originalPath": "/real/hyphen-path/here",
		"entries":      []interface{}{},
	}
	data, _ := json.Marshal(idx)
	os.WriteFile(filepath.Join(projDir, "sessions-index.json"), data, 0644)

	got := ResolveProjectPath(projDir)
	if got != "/real/hyphen-path/here" {
		t.Errorf("ResolveProjectPath from index = %q, want /real/hyphen-path/here", got)
	}
}

func TestResolveProjectPath_FromCWD(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "-fake-project")
	os.MkdirAll(projDir, 0755)

	// Write a JSONL file with cwd
	f, _ := os.Create(filepath.Join(projDir, "test-session.jsonl"))
	rec := map[string]interface{}{
		"type":      "user",
		"cwd":       "/actual/transform-engine",
		"sessionId": "abc",
	}
	data, _ := json.Marshal(rec)
	f.Write(data)
	f.Write([]byte("\n"))
	f.Close()

	got := ResolveProjectPath(projDir)
	if got != "/actual/transform-engine" {
		t.Errorf("ResolveProjectPath from cwd = %q, want /actual/transform-engine", got)
	}
}

func TestResolveProjectPath_FilesystemWalk(t *testing.T) {
	// Create a directory structure with a hyphenated name
	dir := t.TempDir()
	hyphenDir := filepath.Join(dir, "my-project")
	os.MkdirAll(hyphenDir, 0755)

	// The encoded name for dir + "/my-project" would look like the dir's
	// encoded form + "-my-project". We'll simulate by creating the project
	// dir inside a mock base.
	base := t.TempDir()
	// Encode: /dir/my-project -> the encoded form has ambiguous "-"
	// We'll test the walk directly by creating the right structure.
	encodedName := EncodeDirName(dir) + "-my-project"
	projDir := filepath.Join(base, encodedName)
	os.MkdirAll(projDir, 0755)

	got := ResolveProjectPath(projDir)
	// The walk should find dir + "/my-project" (with literal hyphen) since it exists
	if got != hyphenDir {
		// The walk might also find /dir/my/project if that path existed.
		// Since only hyphenDir exists, the walk should return it.
		t.Errorf("ResolveProjectPath filesystem walk = %q, want %q", got, hyphenDir)
	}
}

func TestResolveProjectPath_NaiveFallback(t *testing.T) {
	// When no strategies work, falls back to naive decode
	got := ResolveProjectPath("/nonexistent/base/-foo-bar-baz")
	if got != "/foo/bar/baz" {
		t.Errorf("ResolveProjectPath fallback = %q, want /foo/bar/baz", got)
	}
}

// setupTestStore creates a temporary directory structure mimicking Claude's storage.
func setupTestStore(t *testing.T) (store *Store, baseDir string) {
	t.Helper()
	baseDir = t.TempDir()

	// Create a project directory that maps to our temp dir (so it won't be "orphaned")
	projName := EncodeDirName(baseDir)
	projDir := filepath.Join(baseDir, projName)
	if err := os.MkdirAll(projDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create an orphaned project directory
	orphanDir := filepath.Join(baseDir, "-nonexistent-path-project")
	if err := os.MkdirAll(orphanDir, 0755); err != nil {
		t.Fatal(err)
	}

	return NewStore(baseDir), baseDir
}

func TestProjects(t *testing.T) {
	store, baseDir := setupTestStore(t)

	projects, err := store.Projects()
	if err != nil {
		t.Fatal(err)
	}

	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}

	// Find the orphaned one
	var orphaned, active int
	for _, p := range projects {
		if p.Orphaned {
			orphaned++
			if p.OriginalPath != "/nonexistent/path/project" {
				t.Errorf("orphaned project path = %q, want /nonexistent/path/project", p.OriginalPath)
			}
		} else {
			active++
			if p.OriginalPath != baseDir {
				t.Errorf("active project path = %q, want %q", p.OriginalPath, baseDir)
			}
		}
	}
	if orphaned != 1 || active != 1 {
		t.Errorf("expected 1 orphaned and 1 active, got %d orphaned and %d active", orphaned, active)
	}
}

func writeConversationJSONL(t *testing.T, dir, sessionID string, records []ConversationRecord) string {
	t.Helper()
	fpath := filepath.Join(dir, sessionID+".jsonl")
	f, err := os.Create(fpath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, rec := range records {
		if err := enc.Encode(rec); err != nil {
			t.Fatal(err)
		}
	}
	return fpath
}

func writeSessionsIndex(t *testing.T, dir string, idx SessionsIndex) {
	t.Helper()
	data, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sessions-index.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestConversationsFromFiles(t *testing.T) {
	store, baseDir := setupTestStore(t)
	projName := EncodeDirName(baseDir)
	projDir := filepath.Join(baseDir, projName)

	now := time.Now().UTC()
	sid := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	parentPtr := func(s string) *string { return &s }

	records := []ConversationRecord{
		{
			Type:      "user",
			SessionID: sid,
			UUID:      "msg-1",
			Timestamp: now.Add(-10 * time.Minute).Format(time.RFC3339Nano),
			Slug:      "test-conversation",
			Message:   &MessageContent{Role: "user", Content: "hello world"},
		},
		{
			Type:       "assistant",
			SessionID:  sid,
			UUID:       "msg-2",
			ParentUUID: parentPtr("msg-1"),
			Timestamp:  now.Add(-9 * time.Minute).Format(time.RFC3339Nano),
			Message:    &MessageContent{Role: "assistant", Content: "Hi! How can I help?"},
		},
		{
			Type:       "user",
			SessionID:  sid,
			UUID:       "msg-3",
			ParentUUID: parentPtr("msg-2"),
			Timestamp:  now.Format(time.RFC3339Nano),
			Message:    &MessageContent{Role: "user", Content: "explain goroutines"},
		},
	}
	writeConversationJSONL(t, projDir, sid, records)

	convs, err := store.Conversations(projName)
	if err != nil {
		t.Fatal(err)
	}

	if len(convs) != 1 {
		t.Fatalf("expected 1 conversation, got %d", len(convs))
	}

	c := convs[0]
	if c.SessionID != sid {
		t.Errorf("sessionID = %q, want %q", c.SessionID, sid)
	}
	if c.Slug != "test-conversation" {
		t.Errorf("slug = %q, want test-conversation", c.Slug)
	}
	if c.MessageCount != 3 {
		t.Errorf("messageCount = %d, want 3", c.MessageCount)
	}
	if c.FirstPrompt != "hello world" {
		t.Errorf("firstPrompt = %q, want %q", c.FirstPrompt, "hello world")
	}
}

func TestConversationsFromIndex(t *testing.T) {
	store, baseDir := setupTestStore(t)
	projName := EncodeDirName(baseDir)
	projDir := filepath.Join(baseDir, projName)

	now := time.Now().UTC()
	sid := "11111111-2222-3333-4444-555555555555"

	idx := SessionsIndex{
		Version: SchemaVersion,
		Entries: []IndexEntry{
			{
				SessionID:    sid,
				FullPath:     filepath.Join(projDir, sid+".jsonl"),
				Summary:      "goroutine discussion",
				FirstPrompt:  "hello world",
				MessageCount: 5,
				Created:      now.Add(-1 * time.Hour).Format(time.RFC3339),
				Modified:     now.Format(time.RFC3339),
				GitBranch:    "main",
			},
			{
				SessionID:   "sidechain-id",
				IsSidechain: true,
			},
		},
		OriginalPath: baseDir,
	}
	writeSessionsIndex(t, projDir, idx)

	convs, err := store.Conversations(projName)
	if err != nil {
		t.Fatal(err)
	}

	if len(convs) != 1 {
		t.Fatalf("expected 1 conversation (sidechain filtered), got %d", len(convs))
	}

	c := convs[0]
	if c.Summary != "goroutine discussion" {
		t.Errorf("summary = %q, want %q", c.Summary, "goroutine discussion")
	}
	if c.GitBranch != "main" {
		t.Errorf("gitBranch = %q, want main", c.GitBranch)
	}
}

func TestSchemaVersionMismatch(t *testing.T) {
	store, baseDir := setupTestStore(t)
	projName := EncodeDirName(baseDir)
	projDir := filepath.Join(baseDir, projName)

	idx := SessionsIndex{
		Version: 999, // future version
		Entries: []IndexEntry{},
	}
	writeSessionsIndex(t, projDir, idx)

	// With no .jsonl files as fallback, this should still succeed with empty results
	// because the version mismatch causes index parsing to fail, and fallback finds nothing
	convs, err := store.Conversations(projName)
	if err != nil {
		t.Fatal(err)
	}
	if len(convs) != 0 {
		t.Errorf("expected 0 conversations from fallback, got %d", len(convs))
	}
}

func TestFindConversation(t *testing.T) {
	store, baseDir := setupTestStore(t)
	projName := EncodeDirName(baseDir)
	projDir := filepath.Join(baseDir, projName)

	sid := "abcdef12-3456-7890-abcd-ef1234567890"
	records := []ConversationRecord{
		{Type: "user", SessionID: sid, UUID: "m1", Timestamp: "2026-01-01T00:00:00Z",
			Message: &MessageContent{Role: "user", Content: "test"}},
	}
	writeConversationJSONL(t, projDir, sid, records)

	// Prefix match
	conv, err := store.FindConversation(projName, "abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if conv.SessionID != sid {
		t.Errorf("found session %q, want %q", conv.SessionID, sid)
	}

	// No match
	_, err = store.FindConversation(projName, "zzzzz")
	if err == nil {
		t.Error("expected error for non-matching prefix")
	}
}

func TestMessageText(t *testing.T) {
	// String content
	rec := ConversationRecord{
		Message: &MessageContent{Content: "simple text"},
	}
	if got := MessageText(rec); got != "simple text" {
		t.Errorf("MessageText string = %q", got)
	}

	// Array content (like assistant messages with tool_use blocks)
	rec2 := ConversationRecord{
		Message: &MessageContent{
			Content: []interface{}{
				map[string]interface{}{"type": "text", "text": "first part"},
				map[string]interface{}{"type": "text", "text": "second part"},
				map[string]interface{}{"type": "tool_use", "name": "Read"},
			},
		},
	}
	got := MessageText(rec2)
	if got != "first part\nsecond part" {
		t.Errorf("MessageText array = %q", got)
	}

	// Nil message
	rec3 := ConversationRecord{}
	if got := MessageText(rec3); got != "" {
		t.Errorf("MessageText nil = %q", got)
	}
}

func TestUpdateSessionsIndex(t *testing.T) {
	store, baseDir := setupTestStore(t)
	projName := EncodeDirName(baseDir)
	projDir := filepath.Join(baseDir, projName)

	sid := "update-test-session-id"
	idx := SessionsIndex{
		Version: SchemaVersion,
		Entries: []IndexEntry{
			{SessionID: sid, Summary: "old name"},
		},
	}
	writeSessionsIndex(t, projDir, idx)

	err := store.UpdateSessionsIndex(projName, func(idx *SessionsIndex) error {
		for i := range idx.Entries {
			if idx.Entries[i].SessionID == sid {
				idx.Entries[i].Summary = "new name"
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify the change persisted
	data, _ := os.ReadFile(filepath.Join(projDir, "sessions-index.json"))
	var updated SessionsIndex
	json.Unmarshal(data, &updated)

	if updated.Entries[0].Summary != "new name" {
		t.Errorf("summary after update = %q, want %q", updated.Entries[0].Summary, "new name")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 100); got != "short" {
		t.Errorf("truncate short = %q", got)
	}
	if got := truncate("this is a longer string", 10); got != "this is a ..." {
		t.Errorf("truncate long = %q", got)
	}
}
