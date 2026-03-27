package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var rmCmd = &cobra.Command{
	Use:   "rm [project-path]",
	Short: "Remove a project and all its conversations",
	Long: `Remove a Claude project directory and everything inside it —
conversations, session data, memory files, and the sessions index.

Resolves the target project from:
  1. A positional argument (project path or substring)
  2. The -p/--project flag
  3. The current working directory

Dry-run by default — pass -x/--execute to delete files.

Examples:
  cctx rm                        # dry-run current project
  cctx rm -x                     # apply
  cctx rm /path/to/proj          # dry-run specific project
  cctx rm -x -p /path/to/proj   # apply via flag`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRm,
}

var rmExecute bool

func init() {
	rmCmd.Flags().BoolVarP(&rmExecute, "execute", "x", false, "delete the files (default is dry-run)")
	registerCompletions(rmCmd, "project")
	rootCmd.AddCommand(rmCmd)
}

func runRm(cmd *cobra.Command, args []string) error {
	var path string
	if len(args) == 1 {
		path = args[0]
	} else {
		var err error
		path, err = resolveProject(cmd)
		if err != nil {
			return err
		}
	}

	project, err := store.FindProjectByPath(path)
	if err != nil {
		return err
	}

	projDir := filepath.Join(store.BaseDir, project.DirName)

	// Gather contents
	convs, _ := store.Conversations(project.DirName)

	// Count files in the project directory
	entries, err := os.ReadDir(projDir)
	if err != nil {
		return fmt.Errorf("reading project dir: %w", err)
	}

	var totalFiles int
	var totalDirs int
	for _, e := range entries {
		if e.IsDir() {
			totalDirs++
		} else {
			totalFiles++
		}
	}

	orphanLabel := ""
	if project.Orphaned {
		orphanLabel = " (orphaned)"
	}

	fmt.Printf("Project: %s%s\n", project.OriginalPath, orphanLabel)
	fmt.Printf("Dir:     %s\n", projDir)
	fmt.Printf("         %d conversation(s), %d file(s), %d subdir(s)\n",
		len(convs), totalFiles, totalDirs)

	if len(convs) > 0 {
		fmt.Println()
		for _, c := range convs {
			id := c.SessionID
			if len(id) > 8 {
				id = id[:8]
			}
			label := c.Summary
			if label == "" {
				label = truncateStr(c.FirstPrompt, 60)
			}
			if label == "" {
				label = "(no summary)"
			}
			fmt.Printf("  %s  %s\n", id, label)
		}
	}

	if !rmExecute {
		fmt.Printf("\nDry run. Pass -x/--execute to delete this project.\n")
		return nil
	}

	if err := os.RemoveAll(projDir); err != nil {
		return fmt.Errorf("removing project directory: %w", err)
	}

	fmt.Printf("\nDeleted %s\n", projDir)
	return nil
}
