package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathReplacer_RewriteJSONLine(t *testing.T) {
	r := &PathReplacer{OldPath: "/old/path", NewPath: "/new/path"}

	line := `{"cwd":"/old/path","type":"user","other":"keep"}`
	result, changed := r.rewriteJSONLine([]byte(line), []string{"cwd"})
	if !changed {
		t.Fatal("expected change")
	}

	var obj map[string]interface{}
	json.Unmarshal(result, &obj)
	if obj["cwd"] != "/new/path" {
		t.Errorf("cwd = %q, want /new/path", obj["cwd"])
	}
	if obj["other"] != "keep" {
		t.Errorf("other field was modified")
	}
}

func TestPathReplacer_SubpathReplacement(t *testing.T) {
	r := &PathReplacer{OldPath: "/old/path", NewPath: "/new/path"}

	line := `{"cwd":"/old/path/subdir/file.go"}`
	result, changed := r.rewriteJSONLine([]byte(line), []string{"cwd"})
	if !changed {
		t.Fatal("expected change")
	}

	var obj map[string]interface{}
	json.Unmarshal(result, &obj)
	if obj["cwd"] != "/new/path/subdir/file.go" {
		t.Errorf("cwd = %q, want /new/path/subdir/file.go", obj["cwd"])
	}
}

func TestPathReplacer_NoMatchNoChange(t *testing.T) {
	r := &PathReplacer{OldPath: "/old/path", NewPath: "/new/path"}

	line := `{"cwd":"/other/path","type":"user"}`
	_, changed := r.rewriteJSONLine([]byte(line), []string{"cwd"})
	if changed {
		t.Error("expected no change for non-matching path")
	}
}

func TestPathReplacer_RewriteJSONLFile(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "test.jsonl")

	lines := []string{
		`{"cwd":"/old/path","type":"user"}`,
		`{"cwd":"/other","type":"user"}`,
		`{"cwd":"/old/path/sub","type":"assistant"}`,
	}
	os.WriteFile(fpath, []byte(strings.Join(lines, "\n")+"\n"), 0644)

	r := &PathReplacer{OldPath: "/old/path", NewPath: "/new/path"}
	modified, err := r.RewriteJSONLFile(fpath, []string{"cwd"})
	if err != nil {
		t.Fatal(err)
	}
	if modified != 2 {
		t.Errorf("modified = %d, want 2", modified)
	}

	// Verify file contents
	data, _ := os.ReadFile(fpath)
	if !strings.Contains(string(data), "/new/path") {
		t.Error("file should contain /new/path")
	}
	if strings.Contains(string(data), "/old/path") {
		t.Error("file should not contain /old/path")
	}
	if !strings.Contains(string(data), "/other") {
		t.Error("non-matching lines should be preserved")
	}
}

func TestPathReplacer_PreviewJSONLFile(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "test.jsonl")
	os.WriteFile(fpath, []byte(`{"cwd":"/old/path"}`+"\n"), 0644)

	r := &PathReplacer{OldPath: "/old/path", NewPath: "/new/path"}
	count, err := r.PreviewJSONLFile(fpath, []string{"cwd"})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}

	// File should be unchanged
	data, _ := os.ReadFile(fpath)
	if strings.Contains(string(data), "/new/path") {
		t.Error("preview should not modify file")
	}
}

func TestPathReplacer_ScanMemoryFiles(t *testing.T) {
	dir := t.TempDir()
	memDir := filepath.Join(dir, "memory")
	os.MkdirAll(memDir, 0755)

	// Write a memory file with path references
	os.WriteFile(filepath.Join(memDir, "test.md"), []byte(
		"---\nname: test\n---\nThe project at /old/path has auth code\n"+
			"See /old/path/services/auth for details\n"+
			"Unrelated line\n",
	), 0644)

	// Write MEMORY.md
	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte(
		"- [test](memory/test.md) - auth at /old/path\n",
	), 0644)

	r := &PathReplacer{OldPath: "/old/path", NewPath: "/new/path"}
	refs, err := r.ScanMemoryFiles(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(refs) != 2 {
		t.Fatalf("expected 2 files with refs, got %d", len(refs))
	}

	memFile := filepath.Join(memDir, "test.md")
	if len(refs[memFile]) != 2 {
		t.Errorf("expected 2 refs in test.md, got %d", len(refs[memFile]))
	}

	// Verify the After field
	for _, ref := range refs[memFile] {
		if strings.Contains(ref.After, "/old/path") {
			t.Errorf("After should not contain old path: %s", ref.After)
		}
		if !strings.Contains(ref.After, "/new/path") {
			t.Errorf("After should contain new path: %s", ref.After)
		}
	}
}

func TestPathReplacer_RewriteMemoryFile(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "test.md")
	os.WriteFile(fpath, []byte("project at /old/path is great\n"), 0644)

	r := &PathReplacer{OldPath: "/old/path", NewPath: "/new/path"}
	err := r.RewriteMemoryFile(fpath)
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(fpath)
	if string(data) != "project at /new/path is great\n" {
		t.Errorf("got %q", string(data))
	}
}

func TestActiveSessionsForPath(t *testing.T) {
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "sessions")
	os.MkdirAll(sessDir, 0755)

	// Active session matching our path
	sess1 := map[string]interface{}{"cwd": "/test/project", "pid": 12345}
	data1, _ := json.Marshal(sess1)
	os.WriteFile(filepath.Join(sessDir, "12345.json"), data1, 0644)

	// Active session for different path
	sess2 := map[string]interface{}{"cwd": "/other/project", "pid": 67890}
	data2, _ := json.Marshal(sess2)
	os.WriteFile(filepath.Join(sessDir, "67890.json"), data2, 0644)

	active, err := ActiveSessionsForPath(dir, "/test/project")
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Errorf("expected 1 active session, got %d", len(active))
	}

	active2, _ := ActiveSessionsForPath(dir, "/nonexistent")
	if len(active2) != 0 {
		t.Errorf("expected 0 active sessions for nonexistent, got %d", len(active2))
	}
}

func TestPathReplacer_RewriteSessionsIndex(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "sessions-index.json")

	idx := SessionsIndex{
		Version:      1,
		OriginalPath: "/old/path",
		Entries: []IndexEntry{
			{
				SessionID:   "abc",
				FullPath:    "/old/dir/abc.jsonl",
				ProjectPath: "/old/path",
			},
		},
	}
	data, _ := json.Marshal(idx)
	os.WriteFile(fpath, data, 0644)

	r := &PathReplacer{OldPath: "/old/path", NewPath: "/new/path"}
	err := r.RewriteSessionsIndex(fpath, "/new/dir")
	if err != nil {
		t.Fatal(err)
	}

	readData, _ := os.ReadFile(fpath)
	var result SessionsIndex
	json.Unmarshal(readData, &result)

	if result.OriginalPath != "/new/path" {
		t.Errorf("originalPath = %q", result.OriginalPath)
	}
	if result.Entries[0].ProjectPath != "/new/path" {
		t.Errorf("projectPath = %q", result.Entries[0].ProjectPath)
	}
	if result.Entries[0].FullPath != "/new/dir/abc.jsonl" {
		t.Errorf("fullPath = %q", result.Entries[0].FullPath)
	}
}
