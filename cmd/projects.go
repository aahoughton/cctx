package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var projectsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"projects", "ps"},
	Short:   "List Claude project directories",
	Long: `List all project directories found in Claude's local storage.

Shows the original filesystem path, status (ok or orphaned), and the
encoded directory name. Orphaned projects are those whose original path
no longer exists on disk.`,
	RunE: runProjects,
}

var orphanedOnly bool

func init() {
	projectsCmd.Flags().BoolVarP(&orphanedOnly, "orphaned", "o", false, "show only orphaned projects")
	rootCmd.AddCommand(projectsCmd)
}

func runProjects(cmd *cobra.Command, args []string) error {
	projects, err := store.Projects()
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PATH\tSTATUS\tDIR NAME")

	for _, p := range projects {
		if orphanedOnly && !p.Orphaned {
			continue
		}
		status := "ok"
		if p.Orphaned {
			status = "orphaned"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", p.OriginalPath, status, p.DirName)
	}
	return w.Flush()
}
