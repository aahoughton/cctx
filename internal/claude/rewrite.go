package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PathReplacer rewrites path references in Claude's storage files.
// It operates on specific JSON fields without disturbing the rest of the structure.
type PathReplacer struct {
	OldPath string
	NewPath string
}

// RewriteJSONLFile rewrites path references in a JSONL file, modifying only
// the specified fields. Returns the number of lines modified.
func (r *PathReplacer) RewriteJSONLFile(fpath string, fields []string) (int, error) {
	content, modified, err := r.rewriteJSONLContent(fpath, fields)
	if err != nil {
		return 0, err
	}
	if modified == 0 {
		return 0, nil
	}
	return modified, atomicWrite(fpath, content, 0600)
}

// PreviewJSONLFile returns the count of lines that would be modified without writing.
func (r *PathReplacer) PreviewJSONLFile(fpath string, fields []string) (int, error) {
	_, modified, err := r.rewriteJSONLContent(fpath, fields)
	return modified, err
}

func (r *PathReplacer) rewriteJSONLContent(fpath string, fields []string) ([]byte, int, error) {
	f, err := os.Open(fpath)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	var (
		output   []byte
		modified int
	)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		newLine, changed := r.rewriteJSONLine(line, fields)
		if changed {
			modified++
		}
		output = append(output, newLine...)
		output = append(output, '\n')
	}

	if err := scanner.Err(); err != nil {
		return nil, 0, fmt.Errorf("scanning %s: %w", fpath, err)
	}

	return output, modified, nil
}

// rewriteJSONLine replaces path values in specific fields of a JSON line.
// It uses json.Unmarshal/Marshal to preserve the JSON structure while only
// touching the targeted fields.
func (r *PathReplacer) rewriteJSONLine(line []byte, fields []string) ([]byte, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(line, &obj); err != nil {
		return line, false
	}

	changed := false
	for _, field := range fields {
		raw, ok := obj[field]
		if !ok {
			continue
		}
		var val string
		if err := json.Unmarshal(raw, &val); err != nil {
			continue
		}
		newVal := r.replacePath(val)
		if newVal != val {
			newRaw, _ := json.Marshal(newVal)
			obj[field] = newRaw
			changed = true
		}
	}

	if !changed {
		return line, false
	}

	out, err := json.Marshal(obj)
	if err != nil {
		return line, false
	}
	return out, true
}

// replacePath replaces old path with new path, handling both exact matches
// and subpath references (e.g. /old/path/src/main.go -> /new/path/src/main.go).
func (r *PathReplacer) replacePath(val string) string {
	if val == r.OldPath {
		return r.NewPath
	}
	if strings.HasPrefix(val, r.OldPath+"/") {
		return r.NewPath + val[len(r.OldPath):]
	}
	return val
}

// RewriteSessionsIndex updates path references in a sessions-index.json file.
func (r *PathReplacer) RewriteSessionsIndex(fpath string, newFullPathDir string) error {
	data, err := os.ReadFile(fpath)
	if err != nil {
		return err
	}

	var idx SessionsIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return err
	}

	idx.OriginalPath = r.replacePath(idx.OriginalPath)
	for i := range idx.Entries {
		idx.Entries[i].ProjectPath = r.replacePath(idx.Entries[i].ProjectPath)
		if newFullPathDir != "" {
			// Update fullPath to point to the new directory
			base := filepath.Base(idx.Entries[i].FullPath)
			idx.Entries[i].FullPath = filepath.Join(newFullPathDir, base)
		}
	}

	out, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(fpath, out, 0600)
}

// ScanMemoryFiles scans memory/*.md and MEMORY.md for references to the old path.
// Returns a map of filepath -> list of lines containing references.
func (r *PathReplacer) ScanMemoryFiles(projectDir string) (map[string][]MemoryReference, error) {
	refs := make(map[string][]MemoryReference)

	memoryDir := filepath.Join(projectDir, "memory")
	entries, err := os.ReadDir(memoryDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	// Scan individual memory files
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		fpath := filepath.Join(memoryDir, entry.Name())
		lineRefs, err := r.scanFileForPaths(fpath)
		if err != nil {
			continue
		}
		if len(lineRefs) > 0 {
			refs[fpath] = lineRefs
		}
	}

	// Also scan top-level MEMORY.md
	memIndex := filepath.Join(projectDir, "MEMORY.md")
	if lineRefs, err := r.scanFileForPaths(memIndex); err == nil && len(lineRefs) > 0 {
		refs[memIndex] = lineRefs
	}

	return refs, nil
}

// MemoryReference records a path reference found in a memory file.
type MemoryReference struct {
	LineNum int
	Line    string
	After   string // what the line would look like after replacement
}

func (r *PathReplacer) scanFileForPaths(fpath string) ([]MemoryReference, error) {
	f, err := os.Open(fpath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var refs []MemoryReference
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if strings.Contains(line, r.OldPath) {
			refs = append(refs, MemoryReference{
				LineNum: lineNum,
				Line:    line,
				After:   strings.ReplaceAll(line, r.OldPath, r.NewPath),
			})
		}
	}
	return refs, scanner.Err()
}

// RewriteMemoryFile replaces all occurrences of the old path with the new path
// in a memory file.
func (r *PathReplacer) RewriteMemoryFile(fpath string) error {
	data, err := os.ReadFile(fpath)
	if err != nil {
		return err
	}
	newData := strings.ReplaceAll(string(data), r.OldPath, r.NewPath)
	if newData == string(data) {
		return nil
	}
	return atomicWrite(fpath, []byte(newData), 0600)
}

// ActiveSessionsForPath checks ~/.claude/sessions/ for active sessions
// referencing the given path. Returns session file paths that match.
func ActiveSessionsForPath(claudeDir, projectPath string) ([]string, error) {
	sessDir := filepath.Join(claudeDir, "sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var active []string
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
			CWD string `json:"cwd"`
		}
		if json.Unmarshal(data, &sess) == nil {
			if sess.CWD == projectPath || strings.HasPrefix(sess.CWD, projectPath+"/") {
				active = append(active, fpath)
			}
		}
	}
	return active, nil
}
