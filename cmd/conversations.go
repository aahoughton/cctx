package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var convsAbsolute bool

var conversationsCmd = &cobra.Command{
	Use:     "convs",
	Aliases: []string{"conversations"},
	Short:   "List conversations for a project",
	Long: `List all conversations in a project directory.

Shows session ID prefix, message count, last modification time, and
summary or first prompt. Use -p to specify a project, or defaults to
the current working directory.`,
	Args: cobra.NoArgs,
	RunE: runConversations,
}

func init() {
	conversationsCmd.Flags().BoolVarP(&convsAbsolute, "absolute", "T", false, "show absolute timestamps instead of relative")
	rootCmd.AddCommand(conversationsCmd)
}

func runConversations(cmd *cobra.Command, args []string) error {
	path, err := resolveProject(cmd)
	if err != nil {
		return err
	}

	project, err := store.FindProjectByPath(path)
	if err != nil {
		return err
	}

	convs, err := store.Conversations(project.DirName)
	if err != nil {
		return err
	}

	if len(convs) == 0 {
		fmt.Println("No conversations found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "SESSION\tMSGS\tMODIFIED\tSUMMARY")

	for _, c := range convs {
		id := c.SessionID
		if len(id) > 8 {
			id = id[:8]
		}

		label := c.Summary
		if label == "" {
			label = c.Slug
		}
		if label == "" && c.FirstPrompt != "" {
			label = truncateStr(c.FirstPrompt, 60)
		}
		if label == "" {
			label = "(no summary)"
		}
		label = sanitizeLabel(label)

		modified := formatTime(c.Modified, convsAbsolute)

		fmt.Fprintf(w, "%s\t%d\t%s\t%s\n", id, c.MessageCount, modified, label)
	}
	return w.Flush()
}

func truncateStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

// sanitizeLabel collapses newlines and surrounding whitespace into a single
// space and truncates the result so it fits in a table row.
func sanitizeLabel(s string) string {
	// Replace any run of \r, \n, or surrounding whitespace with a single space.
	var b strings.Builder
	inSpace := false
	for _, r := range s {
		if r == '\n' || r == '\r' {
			if !inSpace {
				b.WriteByte(' ')
				inSpace = true
			}
			continue
		}
		inSpace = false
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	return truncateStr(out, 60)
}
