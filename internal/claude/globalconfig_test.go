package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig writes a config fixture and returns its path.
func writeConfig(t *testing.T, body string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".claude.json")
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

// readConfig parses a config file back into a generic map.
func readConfig(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	return out
}

func projectsOf(t *testing.T, cfg map[string]interface{}) map[string]interface{} {
	t.Helper()
	p, ok := cfg["projects"].(map[string]interface{})
	if !ok {
		t.Fatalf("projects missing or wrong type: %T", cfg["projects"])
	}
	return p
}

const sampleConfig = `{
  "numStartups": 371,
  "projects": {
    "/old/path": {
      "hasTrustDialogAccepted": true,
      "allowedTools": ["Bash(git *)"],
      "lastCost": 47.87
    },
    "/unrelated": {
      "hasTrustDialogAccepted": false
    }
  },
  "githubRepoPaths": {
    "me/proj": ["/old/path"],
    "me/multi": ["/old/path", "/somewhere/else"],
    "me/other": ["/unrelated"]
  }
}`

func TestGlobalConfigPath(t *testing.T) {
	got := GlobalConfigPath("/home/u/.claude")
	want := "/home/u/.claude.json"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestApplyGlobalConfigMv(t *testing.T) {
	path := writeConfig(t, sampleConfig, 0644)

	if err := ApplyGlobalConfigMv(path, "/old/path", "/new/path", false); err != nil {
		t.Fatal(err)
	}

	cfg := readConfig(t, path)
	projects := projectsOf(t, cfg)

	if _, stale := projects["/old/path"]; stale {
		t.Error("old project key should be gone")
	}
	moved, ok := projects["/new/path"].(map[string]interface{})
	if !ok {
		t.Fatal("new project key missing")
	}
	if moved["hasTrustDialogAccepted"] != true {
		t.Error("trust flag did not survive the move")
	}
	if moved["lastCost"] != 47.87 {
		t.Errorf("lastCost = %v, want 47.87", moved["lastCost"])
	}
	tools, _ := moved["allowedTools"].([]interface{})
	if len(tools) != 1 || tools[0] != "Bash(git *)" {
		t.Errorf("allowedTools = %v", moved["allowedTools"])
	}

	if _, ok := projects["/unrelated"]; !ok {
		t.Error("unrelated project entry should be untouched")
	}
	if cfg["numStartups"] != float64(371) {
		t.Errorf("unknown top-level key lost: %v", cfg["numStartups"])
	}
}

func TestApplyGlobalConfigMvRewritesRepoPaths(t *testing.T) {
	path := writeConfig(t, sampleConfig, 0644)

	if err := ApplyGlobalConfigMv(path, "/old/path", "/new/path", false); err != nil {
		t.Fatal(err)
	}

	repos := readConfig(t, path)["githubRepoPaths"].(map[string]interface{})

	single := repos["me/proj"].([]interface{})
	if len(single) != 1 || single[0] != "/new/path" {
		t.Errorf("me/proj = %v", single)
	}

	multi := repos["me/multi"].([]interface{})
	if len(multi) != 2 || multi[0] != "/new/path" || multi[1] != "/somewhere/else" {
		t.Errorf("me/multi = %v, other paths should be preserved in order", multi)
	}

	other := repos["me/other"].([]interface{})
	if len(other) != 1 || other[0] != "/unrelated" {
		t.Errorf("me/other = %v, should be untouched", other)
	}
}

func TestApplyGlobalConfigMvCollapsesDuplicateRepoPath(t *testing.T) {
	path := writeConfig(t, `{
  "projects": {"/old/path": {}},
  "githubRepoPaths": {"me/proj": ["/old/path", "/new/path"]}
}`, 0644)

	if err := ApplyGlobalConfigMv(path, "/old/path", "/new/path", true); err != nil {
		t.Fatal(err)
	}

	repos := readConfig(t, path)["githubRepoPaths"].(map[string]interface{})
	got := repos["me/proj"].([]interface{})
	if len(got) != 1 || got[0] != "/new/path" {
		t.Errorf("duplicate path not collapsed: %v", got)
	}
}

func TestApplyGlobalConfigMvRefusesExistingDest(t *testing.T) {
	path := writeConfig(t, `{
  "projects": {
    "/old/path": {"lastCost": 1},
    "/new/path": {"lastCost": 2}
  }
}`, 0644)

	err := ApplyGlobalConfigMv(path, "/old/path", "/new/path", false)
	if err == nil {
		t.Fatal("expected refusal when destination entry exists")
	}
	if !strings.Contains(err.Error(), "--replace-config-entry") {
		t.Errorf("error should name the flag: %v", err)
	}

	projects := projectsOf(t, readConfig(t, path))
	if projects["/old/path"] == nil {
		t.Error("refused call must not modify the file")
	}

	if err := ApplyGlobalConfigMv(path, "/old/path", "/new/path", true); err != nil {
		t.Fatal(err)
	}
	projects = projectsOf(t, readConfig(t, path))
	dest := projects["/new/path"].(map[string]interface{})
	if dest["lastCost"] != float64(1) {
		t.Errorf("source entry should have replaced the destination, got %v", dest)
	}
}

func TestApplyGlobalConfigMvPreservesMode(t *testing.T) {
	path := writeConfig(t, sampleConfig, 0644)

	if err := ApplyGlobalConfigMv(path, "/old/path", "/new/path", false); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0644 {
		t.Errorf("mode = %v, want 0644", info.Mode().Perm())
	}
}

func TestApplyGlobalConfigMvMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")

	edit, err := PreviewGlobalConfigMv(path, "/old", "/new")
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if !edit.Empty() {
		t.Error("missing file should yield an empty edit")
	}
	if err := ApplyGlobalConfigMv(path, "/old", "/new", false); err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("missing file should not be created")
	}
}

func TestLoadGlobalConfigMalformed(t *testing.T) {
	body := `{"projects": {broken`
	path := writeConfig(t, body, 0644)

	if _, err := PreviewGlobalConfigMv(path, "/old", "/new"); err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
	if err := ApplyGlobalConfigMv(path, "/old", "/new", false); err == nil {
		t.Fatal("expected an error for malformed JSON")
	}

	data, _ := os.ReadFile(path)
	if string(data) != body {
		t.Error("malformed file must be left exactly as found")
	}
}

func TestPreviewGlobalConfigMv(t *testing.T) {
	path := writeConfig(t, sampleConfig, 0644)

	edit, err := PreviewGlobalConfigMv(path, "/old/path", "/new/path")
	if err != nil {
		t.Fatal(err)
	}
	if !edit.HasSourceEntry {
		t.Error("source entry should be reported")
	}
	if edit.HasDestEntry {
		t.Error("destination entry should not be reported")
	}
	if len(edit.RepoPathKeys) != 2 {
		t.Errorf("RepoPathKeys = %v, want 2 entries", edit.RepoPathKeys)
	}
	if edit.Empty() {
		t.Error("edit with changes should not be empty")
	}

	none, _ := PreviewGlobalConfigMv(path, "/absent", "/new")
	if !none.Empty() {
		t.Errorf("unknown project should yield an empty edit: %+v", none)
	}
}

func TestApplyGlobalConfigDelete(t *testing.T) {
	path := writeConfig(t, sampleConfig, 0644)

	if err := ApplyGlobalConfigDelete(path, "/old/path"); err != nil {
		t.Fatal(err)
	}

	cfg := readConfig(t, path)
	projects := projectsOf(t, cfg)
	if _, ok := projects["/old/path"]; ok {
		t.Error("project entry should be gone")
	}
	if _, ok := projects["/unrelated"]; !ok {
		t.Error("unrelated entry should survive")
	}

	repos := cfg["githubRepoPaths"].(map[string]interface{})
	if _, ok := repos["me/proj"]; ok {
		t.Error("repo key with no remaining paths should be dropped")
	}
	multi := repos["me/multi"].([]interface{})
	if len(multi) != 1 || multi[0] != "/somewhere/else" {
		t.Errorf("me/multi = %v, want only the surviving path", multi)
	}
}

func TestApplyGlobalConfigDeleteUnknownProject(t *testing.T) {
	path := writeConfig(t, sampleConfig, 0644)
	before, _ := os.ReadFile(path)

	if err := ApplyGlobalConfigDelete(path, "/never/seen"); err != nil {
		t.Fatal(err)
	}

	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("deleting an unknown project should not rewrite the file")
	}
}
