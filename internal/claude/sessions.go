package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// LiveSession describes a Claude Code process that is currently running.
type LiveSession struct {
	Pid  int
	CWD  string
	File string
}

// LiveSessions reads <claudeDir>/sessions/*.json and returns the entries whose
// process is still running. Session files are named after the pid that created
// them and are normally removed on exit, but a crashed session leaves one
// behind, so the pid is verified rather than trusted.
//
// Pid reuse is not guarded against: a recycled pid reports as live, which fails
// toward refusing to act.
func LiveSessions(claudeDir string) ([]LiveSession, error) {
	sessDir := filepath.Join(claudeDir, "sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var live []LiveSession
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		fpath := filepath.Join(sessDir, entry.Name())
		data, err := os.ReadFile(fpath)
		if err != nil {
			continue
		}
		var sess struct {
			Pid int    `json:"pid"`
			CWD string `json:"cwd"`
		}
		if json.Unmarshal(data, &sess) != nil {
			continue
		}
		if sess.Pid <= 0 || !processAlive(sess.Pid) {
			continue
		}
		live = append(live, LiveSession{Pid: sess.Pid, CWD: sess.CWD, File: fpath})
	}
	return live, nil
}

// processAlive reports whether a process with the given pid exists. Signal 0
// performs the permission and existence checks without delivering anything:
// ESRCH means the process is gone, EPERM means it exists under another uid.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	return err == syscall.EPERM
}
