package cmd

import (
	"fmt"
	"os"

	"github.com/aahoughton/cctx/internal/claude"
	"github.com/spf13/cobra"
)

var mvCmd = &cobra.Command{
	Use:   "mv <old-path> <new-path>",
	Short: "Update Claude references after a project moves",
	Long: `Update Claude's internal references when a project directory has been
moved or renamed on disk.

Rewrites path references across sessions-index.json, conversation files,
history.jsonl, and memory files. Refuses to proceed if active Claude
sessions reference the project.

Shows a dry-run plan by default. Pass -x/--execute to apply.

Examples:
  cctx mv ~/old/project ~/new/project        # preview changes
  cctx mv -x ~/old/project ~/new/project     # apply`,
	Args: cobra.ExactArgs(2),
	RunE: runMv,
}

var mvExecute bool

func init() {
	mvCmd.Flags().BoolVarP(&mvExecute, "execute", "x", false, "apply the changes (default is dry-run)")
	registerCompletions(mvCmd, "project")
	rootCmd.AddCommand(mvCmd)
}

func runMv(cmd *cobra.Command, args []string) error {
	oldPath := args[0]
	newPath := args[1]

	plan, err := claude.BuildMvPlan(store, oldPath, newPath)
	if err != nil {
		return err
	}

	plan.Render(os.Stdout)

	if !plan.HasChanges() {
		fmt.Println("\nNo changes needed.")
		return nil
	}

	if !mvExecute {
		fmt.Println("\nDry run. Pass -x/--execute to apply these changes.")
		return nil
	}

	fmt.Println("\nApplying changes...")
	if err := claude.ExecuteMv(store, oldPath, newPath); err != nil {
		return fmt.Errorf("executing rename: %w", err)
	}
	fmt.Println("Done.")
	return nil
}
