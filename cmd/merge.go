package cmd

import (
	"fmt"
	"os"

	"github.com/aahoughton/cctx/internal/claude"
	"github.com/spf13/cobra"
)

var mergeCmd = &cobra.Command{
	Use:   "merge <source-path> <target-path>",
	Short: "Merge one project's conversations into another",
	Long: `Merge all conversations from a source project into a target project.

Moves conversation files, merges session indices, rewrites path references,
and handles memory file conflicts. The source project directory is removed
after a successful merge.

Shows a dry-run plan by default. Pass -x/--execute to apply.

Examples:
  cctx merge ~/orphaned/project ~/current/project     # preview
  cctx merge -x ~/orphaned/project ~/current/project  # apply`,
	Args: cobra.ExactArgs(2),
	RunE: runMerge,
}

var mergeExecute bool

func init() {
	mergeCmd.Flags().BoolVarP(&mergeExecute, "execute", "x", false, "apply the changes (default is dry-run)")
	registerCompletions(mergeCmd, "project", "project")
	rootCmd.AddCommand(mergeCmd)
}

func runMerge(cmd *cobra.Command, args []string) error {
	sourcePath := args[0]
	targetPath := args[1]

	plan, err := claude.BuildMergePlan(store, sourcePath, targetPath)
	if err != nil {
		return err
	}

	plan.Render(os.Stdout)

	if !plan.HasChanges() {
		fmt.Println("\nNo changes needed.")
		return nil
	}

	if !mergeExecute {
		fmt.Println("\nDry run. Pass -x/--execute to apply these changes.")
		return nil
	}

	fmt.Println("\nApplying merge...")
	if err := claude.ExecuteMerge(store, sourcePath, targetPath); err != nil {
		return fmt.Errorf("executing merge: %w", err)
	}
	fmt.Println("Done.")
	return nil
}
