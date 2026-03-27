package claude

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Plan collects proposed changes for a project operation (mv or merge)
// and can render them as a human-readable summary before execution.
type Plan struct {
	Steps []PlanStep
}

// StepKind identifies the type of operation in a plan step.
type StepKind int

const (
	StepRenameDir StepKind = iota
	StepRewriteIndex
	StepRewriteJSONL
	StepRewriteHistory
	StepRewriteMemory
	StepMoveFile
	StepMergeIndex
	StepDeleteDir
	StepInfo
	StepWarning
)

// PlanStep is a single proposed change.
type PlanStep struct {
	Kind        StepKind
	Description string
	Detail      string // optional multi-line detail (e.g. diff preview)
	FilePath    string
	Count       int // e.g. number of lines affected
}

func (p *Plan) Add(step PlanStep) {
	p.Steps = append(p.Steps, step)
}

func (p *Plan) AddInfo(msg string) {
	p.Steps = append(p.Steps, PlanStep{Kind: StepInfo, Description: msg})
}

func (p *Plan) AddWarning(msg string) {
	p.Steps = append(p.Steps, PlanStep{Kind: StepWarning, Description: msg})
}

// Render writes a human-readable summary of the plan.
func (p *Plan) Render(w io.Writer) {
	for _, s := range p.Steps {
		prefix := kindPrefix(s.Kind)
		fmt.Fprintf(w, "%s %s", prefix, s.Description)
		if s.Count > 0 {
			fmt.Fprintf(w, " (%d lines)", s.Count)
		}
		fmt.Fprintln(w)
		if s.Detail != "" {
			for _, line := range strings.Split(s.Detail, "\n") {
				fmt.Fprintf(w, "    %s\n", line)
			}
		}
	}
}

// HasChanges returns true if the plan contains any non-info steps.
func (p *Plan) HasChanges() bool {
	for _, s := range p.Steps {
		if s.Kind != StepInfo && s.Kind != StepWarning {
			return true
		}
	}
	return false
}

func kindPrefix(k StepKind) string {
	switch k {
	case StepRenameDir:
		return "[rename]"
	case StepRewriteIndex:
		return "[index] "
	case StepRewriteJSONL:
		return "[jsonl] "
	case StepRewriteHistory:
		return "[history]"
	case StepRewriteMemory:
		return "[memory]"
	case StepMoveFile:
		return "[move]  "
	case StepMergeIndex:
		return "[merge] "
	case StepDeleteDir:
		return "[delete]"
	case StepWarning:
		return "[WARN]  "
	default:
		return "[info]  "
	}
}

// BuildMvPlan creates a plan for renaming a project from oldPath to newPath.
func BuildMvPlan(store *Store, oldPath, newPath string) (*Plan, error) {
	plan := &Plan{}
	claudeDir := filepath.Dir(store.BaseDir) // ~/.claude

	// Find the project
	project, err := store.FindProjectByPath(oldPath)
	if err != nil {
		return nil, fmt.Errorf("finding project: %w", err)
	}

	// Check for active sessions
	activeSessions, err := ActiveSessionsForPath(claudeDir, oldPath)
	if err != nil {
		plan.AddWarning(fmt.Sprintf("could not check active sessions: %v", err))
	} else if len(activeSessions) > 0 {
		return nil, fmt.Errorf(
			"project has %d active session(s) — close them before renaming:\n  %s",
			len(activeSessions), strings.Join(activeSessions, "\n  "),
		)
	}

	oldProjDir := filepath.Join(store.BaseDir, project.DirName)
	newDirName := EncodeDirName(newPath)
	newProjDir := filepath.Join(store.BaseDir, newDirName)

	replacer := &PathReplacer{OldPath: oldPath, NewPath: newPath}

	// 1. Rename project directory
	plan.Add(PlanStep{
		Kind:        StepRenameDir,
		Description: fmt.Sprintf("%s -> %s", project.DirName, newDirName),
	})

	// 2. Rewrite sessions-index.json
	indexPath := filepath.Join(oldProjDir, "sessions-index.json")
	if fileExists(indexPath) {
		plan.Add(PlanStep{
			Kind:        StepRewriteIndex,
			Description: fmt.Sprintf("update paths in sessions-index.json"),
			FilePath:    indexPath,
		})
	}

	// 3. Rewrite conversation JSONL files
	convFiles, _ := filepath.Glob(filepath.Join(oldProjDir, "*.jsonl"))
	for _, f := range convFiles {
		count, err := replacer.PreviewJSONLFile(f, []string{"cwd"})
		if err != nil {
			plan.AddWarning(fmt.Sprintf("could not preview %s: %v", filepath.Base(f), err))
			continue
		}
		if count > 0 {
			plan.Add(PlanStep{
				Kind:        StepRewriteJSONL,
				Description: fmt.Sprintf("rewrite cwd in %s", filepath.Base(f)),
				FilePath:    f,
				Count:       count,
			})
		}
	}

	// 4. Rewrite subagent JSONL files
	subagentFiles, _ := filepath.Glob(filepath.Join(oldProjDir, "*/subagents/*.jsonl"))
	for _, f := range subagentFiles {
		count, err := replacer.PreviewJSONLFile(f, []string{"cwd"})
		if err != nil {
			continue
		}
		if count > 0 {
			relPath, _ := filepath.Rel(oldProjDir, f)
			plan.Add(PlanStep{
				Kind:        StepRewriteJSONL,
				Description: fmt.Sprintf("rewrite cwd in %s", relPath),
				FilePath:    f,
				Count:       count,
			})
		}
	}

	// 5. Rewrite history.jsonl
	historyPath := filepath.Join(claudeDir, "history.jsonl")
	if fileExists(historyPath) {
		count, err := replacer.PreviewJSONLFile(historyPath, []string{"project"})
		if err != nil {
			plan.AddWarning(fmt.Sprintf("could not preview history.jsonl: %v", err))
		} else if count > 0 {
			plan.Add(PlanStep{
				Kind:        StepRewriteHistory,
				Description: "rewrite project field in history.jsonl",
				FilePath:    historyPath,
				Count:       count,
			})
		}
	}

	// 6. Scan memory files
	memRefs, err := replacer.ScanMemoryFiles(oldProjDir)
	if err != nil {
		plan.AddWarning(fmt.Sprintf("could not scan memory files: %v", err))
	}
	for fpath, refs := range memRefs {
		var detail strings.Builder
		for _, ref := range refs {
			fmt.Fprintf(&detail, "L%d: %s\n  -> %s\n", ref.LineNum, ref.Line, ref.After)
		}
		relPath, _ := filepath.Rel(oldProjDir, fpath)
		plan.Add(PlanStep{
			Kind:        StepRewriteMemory,
			Description: fmt.Sprintf("update path references in %s", relPath),
			FilePath:    fpath,
			Detail:      strings.TrimRight(detail.String(), "\n"),
			Count:       len(refs),
		})
	}

	_ = newProjDir // used during execution
	return plan, nil
}

// ExecuteMv executes a project rename based on a previously built plan.
func ExecuteMv(store *Store, oldPath, newPath string) error {
	project, err := store.FindProjectByPath(oldPath)
	if err != nil {
		return err
	}

	claudeDir := filepath.Dir(store.BaseDir)
	oldProjDir := filepath.Join(store.BaseDir, project.DirName)
	newDirName := EncodeDirName(newPath)
	newProjDir := filepath.Join(store.BaseDir, newDirName)

	replacer := &PathReplacer{OldPath: oldPath, NewPath: newPath}

	// 1. Rewrite conversation JSONL files (before moving, so paths are still valid)
	convFiles, _ := filepath.Glob(filepath.Join(oldProjDir, "*.jsonl"))
	for _, f := range convFiles {
		if _, err := replacer.RewriteJSONLFile(f, []string{"cwd"}); err != nil {
			return fmt.Errorf("rewriting %s: %w", filepath.Base(f), err)
		}
	}

	// 2. Rewrite subagent files
	subagentFiles, _ := filepath.Glob(filepath.Join(oldProjDir, "*/subagents/*.jsonl"))
	for _, f := range subagentFiles {
		replacer.RewriteJSONLFile(f, []string{"cwd"})
	}

	// 3. Rewrite sessions-index.json
	indexPath := filepath.Join(oldProjDir, "sessions-index.json")
	if fileExists(indexPath) {
		if err := replacer.RewriteSessionsIndex(indexPath, newProjDir); err != nil {
			return fmt.Errorf("rewriting sessions-index.json: %w", err)
		}
	}

	// 4. Rewrite memory files
	memRefs, _ := replacer.ScanMemoryFiles(oldProjDir)
	for fpath := range memRefs {
		replacer.RewriteMemoryFile(fpath)
	}

	// 5. Rewrite history.jsonl
	historyPath := filepath.Join(claudeDir, "history.jsonl")
	if fileExists(historyPath) {
		if _, err := replacer.RewriteJSONLFile(historyPath, []string{"project"}); err != nil {
			return fmt.Errorf("rewriting history.jsonl: %w", err)
		}
	}

	// 6. Rename the directory (last, so if anything above fails we haven't moved yet)
	if err := os.Rename(oldProjDir, newProjDir); err != nil {
		return fmt.Errorf("renaming project directory: %w", err)
	}

	return nil
}

// BuildMergePlan creates a plan for merging a source project into a target.
func BuildMergePlan(store *Store, sourcePath, targetPath string) (*Plan, error) {
	plan := &Plan{}
	claudeDir := filepath.Dir(store.BaseDir)

	sourceProj, err := store.FindProjectByPath(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("finding source project: %w", err)
	}
	targetProj, err := store.FindProjectByPath(targetPath)
	if err != nil {
		return nil, fmt.Errorf("finding target project: %w", err)
	}

	// Check for active sessions on source
	activeSessions, err := ActiveSessionsForPath(claudeDir, sourcePath)
	if err != nil {
		plan.AddWarning(fmt.Sprintf("could not check active sessions: %v", err))
	} else if len(activeSessions) > 0 {
		return nil, fmt.Errorf(
			"source project has %d active session(s) — close them before merging:\n  %s",
			len(activeSessions), strings.Join(activeSessions, "\n  "),
		)
	}

	sourceProjDir := filepath.Join(store.BaseDir, sourceProj.DirName)
	targetProjDir := filepath.Join(store.BaseDir, targetProj.DirName)

	replacer := &PathReplacer{OldPath: sourcePath, NewPath: targetPath}

	// 1. List conversations to move
	convFiles, _ := filepath.Glob(filepath.Join(sourceProjDir, "*.jsonl"))
	for _, f := range convFiles {
		base := filepath.Base(f)
		destPath := filepath.Join(targetProjDir, base)
		if fileExists(destPath) {
			plan.AddWarning(fmt.Sprintf("conversation %s exists in both source and target — will skip", base))
			continue
		}
		count, _ := replacer.PreviewJSONLFile(f, []string{"cwd"})
		plan.Add(PlanStep{
			Kind:        StepMoveFile,
			Description: fmt.Sprintf("move and rewrite %s", base),
			FilePath:    f,
			Count:       count,
		})
	}

	// 2. Move session subdirectories (subagents, tool-results)
	sessionDirs, _ := filepath.Glob(filepath.Join(sourceProjDir, "*/"))
	for _, d := range sessionDirs {
		base := filepath.Base(d)
		if base == "memory" {
			continue // handled separately
		}
		destPath := filepath.Join(targetProjDir, base)
		if fileExists(destPath) {
			plan.AddWarning(fmt.Sprintf("session dir %s exists in both — will skip", base))
			continue
		}
		plan.Add(PlanStep{
			Kind:        StepMoveFile,
			Description: fmt.Sprintf("move session dir %s/", base),
		})
	}

	// 3. Merge sessions-index.json
	plan.Add(PlanStep{
		Kind:        StepMergeIndex,
		Description: "merge session entries into target sessions-index.json",
	})

	// 4. Rewrite history.jsonl
	historyPath := filepath.Join(claudeDir, "history.jsonl")
	if fileExists(historyPath) {
		count, _ := replacer.PreviewJSONLFile(historyPath, []string{"project"})
		if count > 0 {
			plan.Add(PlanStep{
				Kind:        StepRewriteHistory,
				Description: "rewrite project field in history.jsonl",
				FilePath:    historyPath,
				Count:       count,
			})
		}
	}

	// 5. Scan source memory files
	memRefs, _ := replacer.ScanMemoryFiles(sourceProjDir)
	for fpath, refs := range memRefs {
		var detail strings.Builder
		for _, ref := range refs {
			fmt.Fprintf(&detail, "L%d: %s\n  -> %s\n", ref.LineNum, ref.Line, ref.After)
		}
		relPath, _ := filepath.Rel(sourceProjDir, fpath)
		plan.Add(PlanStep{
			Kind:        StepRewriteMemory,
			Description: fmt.Sprintf("update path references in source %s", relPath),
			FilePath:    fpath,
			Detail:      strings.TrimRight(detail.String(), "\n"),
			Count:       len(refs),
		})
	}

	// 6. Check for memory conflicts
	sourceMemDir := filepath.Join(sourceProjDir, "memory")
	targetMemDir := filepath.Join(targetProjDir, "memory")
	if dirExists(sourceMemDir) && dirExists(targetMemDir) {
		sourceEntries, _ := os.ReadDir(sourceMemDir)
		targetEntries, _ := os.ReadDir(targetMemDir)
		targetNames := make(map[string]bool)
		for _, e := range targetEntries {
			targetNames[e.Name()] = true
		}
		for _, e := range sourceEntries {
			if targetNames[e.Name()] {
				plan.AddWarning(fmt.Sprintf("memory file %s exists in both projects — will keep target version", e.Name()))
			} else {
				plan.Add(PlanStep{
					Kind:        StepMoveFile,
					Description: fmt.Sprintf("move memory/%s to target", e.Name()),
				})
			}
		}
	} else if dirExists(sourceMemDir) {
		plan.Add(PlanStep{
			Kind:        StepMoveFile,
			Description: "move entire memory/ directory to target",
		})
	}

	// 7. Delete source directory
	plan.Add(PlanStep{
		Kind:        StepDeleteDir,
		Description: fmt.Sprintf("remove %s", sourceProj.DirName),
	})

	return plan, nil
}

// ExecuteMerge executes a project merge based on a previously built plan.
func ExecuteMerge(store *Store, sourcePath, targetPath string) error {
	sourceProj, err := store.FindProjectByPath(sourcePath)
	if err != nil {
		return err
	}
	targetProj, err := store.FindProjectByPath(targetPath)
	if err != nil {
		return err
	}

	claudeDir := filepath.Dir(store.BaseDir)
	sourceProjDir := filepath.Join(store.BaseDir, sourceProj.DirName)
	targetProjDir := filepath.Join(store.BaseDir, targetProj.DirName)

	replacer := &PathReplacer{OldPath: sourcePath, NewPath: targetPath}

	// 1. Move and rewrite conversation files
	convFiles, _ := filepath.Glob(filepath.Join(sourceProjDir, "*.jsonl"))
	for _, f := range convFiles {
		base := filepath.Base(f)
		destPath := filepath.Join(targetProjDir, base)
		if fileExists(destPath) {
			continue // skip conflicts
		}
		if _, err := replacer.RewriteJSONLFile(f, []string{"cwd"}); err != nil {
			return fmt.Errorf("rewriting %s: %w", base, err)
		}
		if err := os.Rename(f, destPath); err != nil {
			return fmt.Errorf("moving %s: %w", base, err)
		}
	}

	// 2. Move session subdirectories
	entries, _ := os.ReadDir(sourceProjDir)
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "memory" {
			continue
		}
		src := filepath.Join(sourceProjDir, entry.Name())
		dest := filepath.Join(targetProjDir, entry.Name())
		if fileExists(dest) {
			continue
		}
		// Rewrite subagent files inside
		subagentFiles, _ := filepath.Glob(filepath.Join(src, "subagents/*.jsonl"))
		for _, sf := range subagentFiles {
			if _, err := replacer.RewriteJSONLFile(sf, []string{"cwd"}); err != nil {
				return fmt.Errorf("rewriting subagent %s: %w", filepath.Base(sf), err)
			}
		}
		if err := os.Rename(src, dest); err != nil {
			return fmt.Errorf("moving session dir %s: %w", entry.Name(), err)
		}
	}

	// 3. Merge sessions-index.json
	if err := mergeSessionsIndices(sourceProjDir, targetProjDir, targetPath); err != nil {
		return fmt.Errorf("merging session indices: %w", err)
	}

	// 4. Move memory files
	sourceMemDir := filepath.Join(sourceProjDir, "memory")
	targetMemDir := filepath.Join(targetProjDir, "memory")
	if dirExists(sourceMemDir) {
		if err := os.MkdirAll(targetMemDir, 0700); err != nil {
			return fmt.Errorf("creating memory dir: %w", err)
		}
		memEntries, _ := os.ReadDir(sourceMemDir)
		for _, e := range memEntries {
			src := filepath.Join(sourceMemDir, e.Name())
			dest := filepath.Join(targetMemDir, e.Name())
			if fileExists(dest) {
				continue // keep target version
			}
			if err := replacer.RewriteMemoryFile(src); err != nil {
				return fmt.Errorf("rewriting memory file %s: %w", e.Name(), err)
			}
			if err := os.Rename(src, dest); err != nil {
				return fmt.Errorf("moving memory file %s: %w", e.Name(), err)
			}
		}
	}

	// 5. Rewrite history.jsonl
	historyPath := filepath.Join(claudeDir, "history.jsonl")
	if fileExists(historyPath) {
		if _, err := replacer.RewriteJSONLFile(historyPath, []string{"project"}); err != nil {
			return fmt.Errorf("rewriting history.jsonl: %w", err)
		}
	}

	// 6. Remove source directory
	return os.RemoveAll(sourceProjDir)
}

func mergeSessionsIndices(sourceProjDir, targetProjDir, targetPath string) error {
	// Read source index (or bootstrap from files)
	var sourceIdx SessionsIndex
	sourceIndexPath := filepath.Join(sourceProjDir, "sessions-index.json")
	if data, err := os.ReadFile(sourceIndexPath); err == nil {
		if err := json.Unmarshal(data, &sourceIdx); err != nil {
			return fmt.Errorf("parsing source index: %w", err)
		}
	}

	// Read or bootstrap target index
	targetIndexPath := filepath.Join(targetProjDir, "sessions-index.json")
	var targetIdx SessionsIndex
	if data, err := os.ReadFile(targetIndexPath); err == nil {
		if err := json.Unmarshal(data, &targetIdx); err != nil {
			return fmt.Errorf("parsing target index: %w", err)
		}
	} else {
		targetIdx.Version = SchemaVersion
		targetIdx.OriginalPath = targetPath
	}

	// Add source entries to target, updating paths
	existingIDs := make(map[string]bool)
	for _, e := range targetIdx.Entries {
		existingIDs[e.SessionID] = true
	}

	for _, e := range sourceIdx.Entries {
		if existingIDs[e.SessionID] {
			continue
		}
		e.ProjectPath = targetPath
		e.FullPath = filepath.Join(targetProjDir, filepath.Base(e.FullPath))
		targetIdx.Entries = append(targetIdx.Entries, e)
	}

	out, err := json.MarshalIndent(targetIdx, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling merged index: %w", err)
	}
	return atomicWrite(targetIndexPath, out, 0600)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
