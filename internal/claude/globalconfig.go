package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// GlobalConfigPath returns the path to Claude Code's global config file, given
// the Claude data directory (~/.claude). The config lives beside that
// directory rather than inside it.
func GlobalConfigPath(claudeDir string) string {
	return filepath.Join(filepath.Dir(claudeDir), ".claude.json")
}

// GlobalConfigEdit reports what a pending change to the global config would
// touch. A nil edit means the file does not exist.
type GlobalConfigEdit struct {
	Path           string
	HasSourceEntry bool
	HasDestEntry   bool
	RepoPathKeys   []string
}

// Empty reports whether the edit would change nothing.
func (e *GlobalConfigEdit) Empty() bool {
	return e == nil || (!e.HasSourceEntry && len(e.RepoPathKeys) == 0)
}

// globalConfig is the parsed form of ~/.claude.json. Every key cctx does not
// understand is held as raw JSON so it survives a round trip untouched.
type globalConfig struct {
	top      map[string]json.RawMessage
	projects map[string]json.RawMessage
	repos    map[string][]string
	mode     os.FileMode
}

// loadGlobalConfig reads and parses the config. A missing file returns
// (nil, nil); callers treat that as nothing to do. Malformed JSON is an error,
// never something to repair by guessing.
func loadGlobalConfig(path string) (*globalConfig, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := &globalConfig{mode: info.Mode().Perm()}
	if err := json.Unmarshal(data, &cfg.top); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	if raw, ok := cfg.top["projects"]; ok {
		if err := json.Unmarshal(raw, &cfg.projects); err != nil {
			return nil, fmt.Errorf("parsing projects in %s: %w", path, err)
		}
	}
	if raw, ok := cfg.top["githubRepoPaths"]; ok {
		if err := json.Unmarshal(raw, &cfg.repos); err != nil {
			return nil, fmt.Errorf("parsing githubRepoPaths in %s: %w", path, err)
		}
	}
	return cfg, nil
}

func (c *globalConfig) save(path string) error {
	if c.projects != nil {
		raw, err := json.Marshal(c.projects)
		if err != nil {
			return err
		}
		c.top["projects"] = raw
	}
	if c.repos != nil {
		raw, err := json.Marshal(c.repos)
		if err != nil {
			return err
		}
		c.top["githubRepoPaths"] = raw
	}

	out, err := json.MarshalIndent(c.top, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, out, c.mode)
}

// repoKeysFor returns the githubRepoPaths keys whose value list mentions path.
func (c *globalConfig) repoKeysFor(path string) []string {
	var keys []string
	for repo, paths := range c.repos {
		for _, p := range paths {
			if p == path {
				keys = append(keys, repo)
				break
			}
		}
	}
	return keys
}

// PreviewGlobalConfigMv reports what moving a project entry would change.
func PreviewGlobalConfigMv(path, oldPath, newPath string) (*GlobalConfigEdit, error) {
	cfg, err := loadGlobalConfig(path)
	if err != nil || cfg == nil {
		return nil, err
	}

	_, hasSource := cfg.projects[oldPath]
	_, hasDest := cfg.projects[newPath]
	return &GlobalConfigEdit{
		Path:           path,
		HasSourceEntry: hasSource,
		HasDestEntry:   hasDest,
		RepoPathKeys:   cfg.repoKeysFor(oldPath),
	}, nil
}

// ApplyGlobalConfigMv moves the project entry from oldPath to newPath and
// repoints any githubRepoPaths references. It refuses to clobber an existing
// destination entry unless replaceDest is set.
func ApplyGlobalConfigMv(path, oldPath, newPath string, replaceDest bool) error {
	cfg, err := loadGlobalConfig(path)
	if err != nil || cfg == nil {
		return err
	}

	entry, hasSource := cfg.projects[oldPath]
	if _, hasDest := cfg.projects[newPath]; hasDest && hasSource && !replaceDest {
		return fmt.Errorf(
			"%s already has a project entry for %s; pass --replace-config-entry to overwrite it",
			path, newPath,
		)
	}

	changed := false
	if hasSource {
		cfg.projects[newPath] = entry
		delete(cfg.projects, oldPath)
		changed = true
	}

	for _, repo := range cfg.repoKeysFor(oldPath) {
		cfg.repos[repo] = replaceInPaths(cfg.repos[repo], oldPath, newPath)
		changed = true
	}

	if !changed {
		return nil
	}
	return cfg.save(path)
}

// PreviewGlobalConfigDelete reports what removing a project entry would change.
func PreviewGlobalConfigDelete(path, projectPath string) (*GlobalConfigEdit, error) {
	cfg, err := loadGlobalConfig(path)
	if err != nil || cfg == nil {
		return nil, err
	}

	_, has := cfg.projects[projectPath]
	return &GlobalConfigEdit{
		Path:           path,
		HasSourceEntry: has,
		RepoPathKeys:   cfg.repoKeysFor(projectPath),
	}, nil
}

// ApplyGlobalConfigDelete removes the project entry and drops the path from any
// githubRepoPaths references. A repo key whose last path is removed is deleted.
func ApplyGlobalConfigDelete(path, projectPath string) error {
	cfg, err := loadGlobalConfig(path)
	if err != nil || cfg == nil {
		return err
	}

	changed := false
	if _, has := cfg.projects[projectPath]; has {
		delete(cfg.projects, projectPath)
		changed = true
	}

	for _, repo := range cfg.repoKeysFor(projectPath) {
		remaining := removeFromPaths(cfg.repos[repo], projectPath)
		if len(remaining) == 0 {
			delete(cfg.repos, repo)
		} else {
			cfg.repos[repo] = remaining
		}
		changed = true
	}

	if !changed {
		return nil
	}
	return cfg.save(path)
}

// replaceInPaths swaps oldPath for newPath, collapsing the result if newPath
// was already present in the list.
func replaceInPaths(paths []string, oldPath, newPath string) []string {
	out := make([]string, 0, len(paths))
	seen := make(map[string]bool, len(paths))
	for _, p := range paths {
		if p == oldPath {
			p = newPath
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func removeFromPaths(paths []string, target string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p != target {
			out = append(out, p)
		}
	}
	return out
}
