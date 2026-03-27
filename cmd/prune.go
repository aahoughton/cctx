package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aahoughton/cctx/internal/claude"
	"github.com/spf13/cobra"
)

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Remove empty or trivial conversations",
	Long: `Remove conversations that contain no real user content.

A conversation is considered empty if it has no user messages, or if all
user messages are protocol noise (exit commands, local-command wrappers).

Use -p to specify a project, or defaults to the current working directory.
Use -A/--all-projects to prune everywhere.
Dry-run by default — pass -x/--execute to delete files.

Examples:
  cctx prune                  # dry-run current project
  cctx prune -x               # apply
  cctx prune -p /path/to/proj # specific project
  cctx prune -A               # dry-run all projects
  cctx prune -Ax              # apply to all projects`,
	Args: cobra.NoArgs,
	RunE: runPrune,
}

var (
	pruneExecute     bool
	pruneAllProjects bool
)

func init() {
	pruneCmd.Flags().BoolVarP(&pruneExecute, "execute", "x", false, "delete the files (default is dry-run)")
	pruneCmd.Flags().BoolVarP(&pruneAllProjects, "all-projects", "A", false, "prune across all projects")
	rootCmd.AddCommand(pruneCmd)
}

func runPrune(cmd *cobra.Command, args []string) error {
	if pruneAllProjects {
		return pruneAll()
	}

	path, err := resolveProject(cmd)
	if err != nil {
		return err
	}
	project, err := store.FindProjectByPath(path)
	if err != nil {
		return err
	}

	return pruneProject(project)
}

func pruneAll() error {
	projects, err := store.Projects()
	if err != nil {
		return err
	}

	totalPruned := 0
	for _, p := range projects {
		n, err := pruneProjectCount(&p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", p.OriginalPath, err)
			continue
		}
		totalPruned += n
	}

	if totalPruned == 0 {
		fmt.Println("No empty conversations found.")
	} else if !pruneExecute {
		fmt.Printf("\nTotal: %d empty conversation(s). Pass -x to delete.\n", totalPruned)
	} else {
		fmt.Printf("\nDeleted %d conversation(s).\n", totalPruned)
	}
	return nil
}

func pruneProject(project *claude.Project) error {
	n, err := pruneProjectCount(project)
	if err != nil {
		return err
	}
	if n == 0 {
		fmt.Println("No empty conversations found.")
	} else if !pruneExecute {
		fmt.Printf("\n%d empty conversation(s). Pass -x to delete.\n", n)
	} else {
		fmt.Printf("\nDeleted %d conversation(s).\n", n)
	}
	return nil
}

func pruneProjectCount(project *claude.Project) (int, error) {
	convs, err := store.Conversations(project.DirName)
	if err != nil {
		return 0, err
	}

	pruned := 0
	for _, conv := range convs {
		if !isEmptyConversation(store, &conv) {
			continue
		}

		id := conv.SessionID
		if len(id) > 8 {
			id = id[:8]
		}
		fmt.Printf("  %-45s %s (%d msgs)\n", project.OriginalPath, id, conv.MessageCount)

		if pruneExecute {
			// Remove the JSONL file
			if err := os.Remove(conv.FilePath); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "    error removing %s: %v\n", conv.FilePath, err)
				continue
			}

			// Remove session subdirectory if it exists (subagents, tool-results)
			sessionDir := strings.TrimSuffix(conv.FilePath, ".jsonl")
			if info, err := os.Stat(sessionDir); err == nil && info.IsDir() {
				os.RemoveAll(sessionDir)
			}

			// Remove from sessions-index.json
			store.UpdateSessionsIndex(project.DirName, func(idx *claude.SessionsIndex) error {
				for i := range idx.Entries {
					if idx.Entries[i].SessionID == conv.SessionID {
						idx.Entries = append(idx.Entries[:i], idx.Entries[i+1:]...)
						return nil
					}
				}
				return nil
			})
		}
		pruned++
	}

	// If we pruned everything and the project dir is now empty, offer to remove it
	if pruneExecute && pruned > 0 {
		projDir := filepath.Join(store.BaseDir, project.DirName)
		remaining, _ := filepath.Glob(filepath.Join(projDir, "*.jsonl"))
		if len(remaining) == 0 {
			// Check if there's anything else worth keeping (memory, etc.)
			entries, _ := os.ReadDir(projDir)
			hasContent := false
			for _, e := range entries {
				if e.Name() != "sessions-index.json" {
					hasContent = true
					break
				}
			}
			if !hasContent {
				os.RemoveAll(projDir)
				fmt.Printf("  removed empty project dir %s\n", project.DirName)
			}
		}
	}

	return pruned, nil
}

// isEmptyConversation returns true if a conversation has no real user content.
func isEmptyConversation(s *claude.Store, conv *claude.Conversation) bool {
	records, err := s.ReadConversation(conv.FilePath)
	if err != nil {
		return false // can't read, don't prune
	}

	for _, rec := range records {
		if rec.Type != "user" {
			continue
		}
		text := claude.MessageText(rec)
		if text == "" {
			continue
		}
		// Skip protocol noise
		if strings.HasPrefix(text, "<local-command") ||
			strings.HasPrefix(text, "<command-name>") ||
			strings.HasPrefix(text, "<local-command-stdout>") ||
			strings.HasPrefix(text, "<local-command-caveat>") {
			continue
		}
		// Found a real user message
		return false
	}

	return true
}
