package cmd

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/aahoughton/cctx/internal/claude"
	"github.com/spf13/cobra"
)

var store *claude.Store

var rootCmd = &cobra.Command{
	Use:   "cctx",
	Short: "Inspect and manage Claude Code conversations and projects",
	Long: `cctx — Claude Code context manager.

Browse projects, list conversations, generate summaries, rename sessions,
and manage project directory references.

Environment variables for LLM-powered features:
  CONTEXT_LLM_URL     Base URL for OpenAI-compatible API
  CONTEXT_LLM_MODEL   Model name (e.g. llama3, gpt-4o-mini)
  LLM_API_KEY         API key (also reads OPENAI_API_KEY, ANTHROPIC_API_KEY)`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := cmd.Flags().GetString("claude-dir")
		if dir != "" {
			store = claude.NewStore(dir)
		} else {
			var err error
			store, err = claude.DefaultStore()
			if err != nil {
				return err
			}
		}
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().String("claude-dir", "", "override path to Claude projects directory")
	rootCmd.PersistentFlags().StringP("project", "p", "", "project path (defaults to current directory)")
	rootCmd.RegisterFlagCompletionFunc("project", completeProjectPath)
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	completionCmd := &cobra.Command{
		Use:   "completion <bash|zsh|fish>",
		Short: "Generate shell completion script",
		Args:  cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish"},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return rootCmd.GenBashCompletionV2(cmd.OutOrStdout(), true)
			case "zsh":
				return rootCmd.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return genFishCompletion()
			default:
				return fmt.Errorf("unsupported shell: %s", args[0])
			}
		},
	}
	rootCmd.AddCommand(completionCmd)
}

// genFishCompletion generates a fish completion script with a fix for
// unbalanced quotes. Cobra's generated script contains apostrophes in
// comments (e.g. "Let's"), and fish parses quotes even inside comments,
// causing syntax errors.
func genFishCompletion() error {
	var buf bytes.Buffer
	if err := rootCmd.GenFishCompletion(&buf, true); err != nil {
		return err
	}
	for _, line := range strings.Split(buf.String(), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			line = strings.ReplaceAll(line, "'", "")
		}
		fmt.Println(line)
	}
	return nil
}

func Execute() error {
	return rootCmd.Execute()
}

// resolveProject resolves the project path from the -p flag, defaulting to cwd.
func resolveProject(cmd *cobra.Command) (string, error) {
	p, _ := cmd.Flags().GetString("project")
	if p != "" {
		return p, nil
	}
	return os.Getwd()
}

// These are used by completion functions which run before PersistentPreRunE.
func newStoreFromDir(dir string) *claude.Store {
	return claude.NewStore(dir)
}

func defaultStoreInit() (*claude.Store, error) {
	return claude.DefaultStore()
}
