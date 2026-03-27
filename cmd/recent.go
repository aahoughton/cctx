package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/aahoughton/cctx/internal/claude"
	"github.com/spf13/cobra"
)

var (
	recentCount    int
	recentAbsolute bool
)

func init() {
	rootCmd.Flags().IntVarP(&recentCount, "number", "n", 10, "number of recent conversations to show")
	rootCmd.Flags().BoolVarP(&recentAbsolute, "absolute", "T", false, "show absolute timestamps instead of relative")
	rootCmd.RunE = runRecent
}

type recentConv struct {
	conv    claude.Conversation
	project string // display path for the project
}

func runRecent(cmd *cobra.Command, args []string) error {
	projects, err := store.Projects()
	if err != nil {
		return err
	}

	var all []recentConv
	for _, p := range projects {
		convs, err := store.Conversations(p.DirName)
		if err != nil {
			continue
		}
		display := compactPath(p.OriginalPath)
		for _, c := range convs {
			all = append(all, recentConv{conv: c, project: display})
		}
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].conv.Modified.After(all[j].conv.Modified)
	})

	if recentCount > 0 && len(all) > recentCount {
		all = all[:recentCount]
	}

	if len(all) == 0 {
		fmt.Println("No conversations found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "SESSION\tPROJECT\tMSGS\tMODIFIED\tSUMMARY")

	for _, rc := range all {
		c := rc.conv
		id := c.SessionID
		if len(id) > 8 {
			id = id[:8]
		}

		label := c.Summary
		if label == "" {
			label = c.Slug
		}
		if label == "" && c.FirstPrompt != "" {
			label = truncateStr(c.FirstPrompt, 50)
		}
		if label == "" {
			label = "(no summary)"
		}
		label = sanitizeLabel(label)

		modified := formatTime(c.Modified, recentAbsolute)

		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n", id, rc.project, c.MessageCount, modified, label)
	}
	return w.Flush()
}

// compactPath shortens an absolute path using ~ for home and trimming to
// the last two path components if the result is still long.
func compactPath(p string) string {
	if u, err := user.Current(); err == nil && strings.HasPrefix(p, u.HomeDir) {
		p = "~" + strings.TrimPrefix(p, u.HomeDir)
	}
	// If still long, show last two components with leading ~/…/
	if len(p) > 30 {
		parts := strings.Split(p, string(filepath.Separator))
		if len(parts) > 2 {
			tail := filepath.Join(parts[len(parts)-2], parts[len(parts)-1])
			if strings.HasPrefix(p, "~") {
				return "~/…/" + tail
			}
			return "…/" + tail
		}
	}
	return p
}
