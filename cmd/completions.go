package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// completeProjectPath provides dynamic completions for project path arguments.
func completeProjectPath(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	initStoreForCompletion(cmd)
	if store == nil {
		return nil, cobra.ShellCompDirectiveError
	}

	projects, err := store.Projects()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var comps []string
	for _, p := range projects {
		if strings.Contains(p.OriginalPath, toComplete) {
			status := ""
			if p.Orphaned {
				status = " (orphaned)"
			}
			comps = append(comps, fmt.Sprintf("%s\t%s%s", p.OriginalPath, p.DirName, status))
		}
	}
	return comps, cobra.ShellCompDirectiveNoFileComp
}

// completeSession provides session completions. It resolves the project from
// the -p flag, falling back to CWD. If neither matches a known project, it
// falls back to completing project paths instead.
func completeSession(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	initStoreForCompletion(cmd)
	if store == nil {
		return nil, cobra.ShellCompDirectiveError
	}

	projectPath, _ := cmd.Flags().GetString("project")
	if projectPath == "" {
		projectPath, _ = os.Getwd()
	}

	if projectPath != "" {
		if project, err := store.FindProjectByPath(projectPath); err == nil {
			return sessionCompletionsForProject(project.DirName, toComplete)
		}
	}

	// CWD isn't a project and -p wasn't given; show projects instead.
	return completeProjectPath(cmd, args, toComplete)
}

func sessionCompletionsForProject(dirName, toComplete string) ([]string, cobra.ShellCompDirective) {
	convs, err := store.Conversations(dirName)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var comps []string
	for _, c := range convs {
		id := c.SessionID
		if len(id) > 8 {
			id = id[:8]
		}
		if strings.HasPrefix(id, toComplete) {
			label := c.Summary
			if label == "" {
				label = c.Slug
			}
			if label == "" {
				label = truncateStr(c.FirstPrompt, 40)
			}
			label = sanitizeLabel(label)
			comps = append(comps, fmt.Sprintf("%s\t%s", id, label))
		}
	}
	return comps, cobra.ShellCompDirectiveNoFileComp
}

func initStoreForCompletion(cmd *cobra.Command) {
	if store != nil {
		return
	}
	dir, _ := cmd.Flags().GetString("claude-dir")
	if dir != "" {
		store = newStoreFromDir(dir)
	} else {
		s, err := defaultStoreInit()
		if err != nil {
			return
		}
		store = s
	}
}

// registerCompletions wires up dynamic completions for commands that accept
// project paths and session IDs.
//
// Supported position types:
//   - "project": completes project paths
//   - "session": completes session IDs (uses -p flag, then CWD, then falls back to projects)
func registerCompletions(cmd *cobra.Command, argPositions ...string) {
	cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) < len(argPositions) {
			switch argPositions[len(args)] {
			case "project":
				return completeProjectPath(cmd, nil, toComplete)
			case "session":
				return completeSession(cmd, args, toComplete)
			}
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}
