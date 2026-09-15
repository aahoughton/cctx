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
history.jsonl, and memory files, and moves the project's entry in
~/.claude.json, which carries folder trust, tool permissions and MCP
approvals.

Refuses to proceed while any Claude session is running. Sessions rewrite
~/.claude.json in full when they exit, which would undo the change.

Shows a dry-run plan by default. Pass -x/--execute to apply.

Examples:
  cctx mv ~/old/project ~/new/project        # preview changes
  cctx mv -x ~/old/project ~/new/project     # apply
  cctx mv --config-only -x ~/old ~/new       # update ~/.claude.json only`,
	Args: cobra.ExactArgs(2),
	RunE: runMv,
}

var (
	mvExecute      bool
	mvConfigOnly   bool
	mvReplaceEntry bool
)

func init() {
	mvCmd.Flags().BoolVarP(&mvExecute, "execute", "x", false, "apply the changes (default is dry-run)")
	mvCmd.Flags().BoolVar(&mvConfigOnly, "config-only", false, "update only the ~/.claude.json entry, for a move whose files already landed")
	mvCmd.Flags().BoolVar(&mvReplaceEntry, "replace-config-entry", false, "overwrite an existing ~/.claude.json entry at the new path")
	registerCompletions(mvCmd, "project")
	rootCmd.AddCommand(mvCmd)
}

func runMv(cmd *cobra.Command, args []string) error {
	oldPath := args[0]
	newPath := args[1]

	opts := claude.MvOptions{ConfigOnly: mvConfigOnly, ReplaceEntry: mvReplaceEntry}

	plan, err := claude.BuildMvPlan(store, oldPath, newPath, opts)
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
	if err := claude.ExecuteMv(store, oldPath, newPath, opts); err != nil {
		return fmt.Errorf("executing rename: %w", err)
	}
	fmt.Println("Done.")
	return nil
}
