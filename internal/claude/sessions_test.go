package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// deadPid returns the pid of a process that has run to completion and been
// reaped, so it is guaranteed not to exist.
func deadPid(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("running probe process: %v", err)
	}
	return cmd.Process.Pid
}

func TestProcessAlive(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Error("current process should be alive")
	}
	if processAlive(deadPid(t)) {
		t.Error("reaped process should not be alive")
	}
	if processAlive(0) {
		t.Error("pid 0 should not be reported alive")
	}
}

func TestLiveSessions(t *testing.T) {
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "sessions")
	os.MkdirAll(sessDir, 0755)

	write := func(name string, body interface{}) {
		data, _ := json.Marshal(body)
		os.WriteFile(filepath.Join(sessDir, name), data, 0644)
	}

	pid := os.Getpid()
	write(fmt.Sprintf("%d.json", pid), map[string]interface{}{"pid": pid, "cwd": "/a/project"})
	dead := deadPid(t)
	write(fmt.Sprintf("%d.json", dead), map[string]interface{}{"pid": dead, "cwd": "/b/project"})
	write("garbage.json", "not an object")
	// Non-JSON siblings (the .key files Claude writes) must be ignored.
	os.WriteFile(filepath.Join(sessDir, "999.abc.key"), []byte("{}"), 0644)

	live, err := LiveSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 {
		t.Fatalf("expected 1 live session, got %d: %+v", len(live), live)
	}
	if live[0].Pid != pid {
		t.Errorf("pid = %d, want %d", live[0].Pid, pid)
	}
	if live[0].CWD != "/a/project" {
		t.Errorf("cwd = %q, want /a/project", live[0].CWD)
	}
	if live[0].File == "" {
		t.Error("file path should be populated")
	}
}

func TestLiveSessionsMissingDir(t *testing.T) {
	live, err := LiveSessions(t.TempDir())
	if err != nil {
		t.Fatalf("missing sessions dir should not error: %v", err)
	}
	if len(live) != 0 {
		t.Errorf("expected no sessions, got %d", len(live))
	}
}
