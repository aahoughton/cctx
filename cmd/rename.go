package cmd

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/aahoughton/cctx/internal/claude"
	"github.com/spf13/cobra"
)

var renameCmd = &cobra.Command{
	Use:   "rename [session-prefix] [new-name]",
	Short: "Rename a conversation",
	Long: `Rename a conversation's summary in sessions-index.json.

If new-name is omitted, an LLM generates the name automatically (requires
LLM configuration). Use -a/--all to batch-rename all unnamed conversations.

LLM settings: flags (-U, -M, -K), environment variables, or config file.
See "cctx --help" for details.

Examples:
  cctx rename abc123 "my new name"    # manual
  cctx rename abc123                  # auto-name via LLM
  cctx rename -a                      # batch-rename all unnamed
  cctx rename -an                     # dry-run batch rename
  cctx rename -M mistral abc123       # override model`,
	Args: cobra.RangeArgs(0, 2),
	RunE: runRename,
}

var (
	llmURL    string
	llmModel  string
	llmKey    string
	dryRun    bool
	renameAll bool
)

func init() {
	renameCmd.Flags().BoolVarP(&renameAll, "all", "a", false, "rename all unnamed conversations in the project")
	renameCmd.Flags().StringVarP(&llmURL, "llm-url", "U", "", "base URL for OpenAI-compatible API")
	renameCmd.Flags().StringVarP(&llmModel, "llm-model", "M", "", "model name (e.g. llama3, gpt-4o-mini)")
	renameCmd.Flags().StringVarP(&llmKey, "llm-key", "K", "", "API key (see also: env vars, config.toml)")
	renameCmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "print the proposed name without writing it")
	registerCompletions(renameCmd, "session")
	rootCmd.AddCommand(renameCmd)
}

func runRename(cmd *cobra.Command, args []string) error {
	if renameAll {
		return runRenameAll(cmd)
	}

	if len(args) == 0 {
		return fmt.Errorf("session-prefix is required (or use -a to rename all)")
	}

	sessionPrefix := args[0]
	var newNameArg string
	if len(args) == 2 {
		newNameArg = args[1]
	}

	path, err := resolveProject(cmd)
	if err != nil {
		return err
	}

	project, err := store.FindProjectByPath(path)
	if err != nil {
		return err
	}

	conv, err := store.FindConversation(project.DirName, sessionPrefix)
	if err != nil {
		return err
	}

	var newName string
	if newNameArg != "" {
		newName = newNameArg
	} else {
		cfg, cfgErr := buildLLMConfig()
		if cfgErr == nil {
			newName, err = llmSummary(store, conv, cfg)
			if err != nil {
				return fmt.Errorf("generating LLM summary: %w", err)
			}
			fmt.Printf("LLM-generated name: %s\n", newName)
		} else {
			return fmt.Errorf("no name provided and no LLM configured\n\n" +
				"Either provide a name:  cctx rename %s \"my name\"\n" +
				"Or configure an LLM:    ~/.config/cctx/config.toml\n\n" +
				"  [llm]\n" +
				"  url = \"http://localhost:11434/v1\"\n" +
				"  model = \"qwen3-4\"", sessionPrefix)
		}
	}

	if dryRun {
		return nil
	}

	return store.UpdateSessionsIndex(project.DirName, func(idx *claude.SessionsIndex) error {
		for i := range idx.Entries {
			if idx.Entries[i].SessionID == conv.SessionID {
				old := idx.Entries[i].Summary
				idx.Entries[i].Summary = newName
				fmt.Printf("Renamed: %q -> %q\n", old, newName)
				return nil
			}
		}
		return fmt.Errorf("session %s not found in index", conv.SessionID)
	})
}

// slugPattern matches Claude's auto-generated slugs: word-word-word
var slugPattern = regexp.MustCompile(`^[a-z]+-[a-z]+-[a-z]+$`)

// isUnnamed returns true if a conversation has no real summary — just an
// auto-generated slug, empty string, or no useful name.
func isUnnamed(conv claude.Conversation) bool {
	s := conv.Summary
	if s == "" {
		return true
	}
	// Claude's auto-generated slugs are adjective-adjective-noun, all lowercase
	if slugPattern.MatchString(s) {
		return true
	}
	return false
}

func runRenameAll(cmd *cobra.Command) error {
	cfg, err := buildLLMConfig()
	if err != nil {
		return fmt.Errorf("-a/--all requires an LLM: %w", err)
	}

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

	var unnamed []claude.Conversation
	for _, c := range convs {
		if isUnnamed(c) {
			unnamed = append(unnamed, c)
		}
	}

	if len(unnamed) == 0 {
		fmt.Println("All conversations already have names.")
		return nil
	}

	fmt.Printf("Found %d unnamed conversation(s) in %s\n\n", len(unnamed), project.OriginalPath)

	type result struct {
		conv    claude.Conversation
		newName string
		err     error
	}
	var results []result

	for _, conv := range unnamed {
		id := conv.SessionID
		if len(id) > 8 {
			id = id[:8]
		}

		old := conv.Summary
		if old == "" {
			old = conv.Slug
		}

		newName, err := llmSummary(store, &conv, cfg)
		if err != nil {
			fmt.Printf("  %s  %-30s  ERROR: %v\n", id, old, err)
			results = append(results, result{conv: conv, err: err})
			continue
		}

		fmt.Printf("  %s  %-30s -> %s\n", id, old, newName)
		results = append(results, result{conv: conv, newName: newName})
	}

	if dryRun {
		fmt.Printf("\nDry run. Pass without -n to apply.\n")
		return nil
	}

	// Apply all renames in a single index update
	applied := 0
	err = store.UpdateSessionsIndex(project.DirName, func(idx *claude.SessionsIndex) error {
		for _, r := range results {
			if r.err != nil || r.newName == "" {
				continue
			}
			for i := range idx.Entries {
				if idx.Entries[i].SessionID == r.conv.SessionID {
					idx.Entries[i].Summary = r.newName
					applied++
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	fmt.Printf("\nRenamed %d conversation(s).\n", applied)
	return nil
}

func buildLLMConfig() (claude.LLMConfig, error) {
	fileCfg := claude.LoadConfig()
	cfg := fileCfg.LLM

	if v := os.Getenv("CONTEXT_LLM_URL"); v != "" {
		cfg.BaseURL = v
	}
	if v := os.Getenv("CONTEXT_LLM_MODEL"); v != "" {
		cfg.Model = v
	}
	if v := os.Getenv("LLM_API_KEY"); v != "" {
		cfg.APIKey = v
	} else if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		cfg.APIKey = v
	} else if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		cfg.APIKey = v
	}

	if llmURL != "" {
		cfg.BaseURL = llmURL
	}
	if llmModel != "" {
		cfg.Model = llmModel
	}
	if llmKey != "" {
		cfg.APIKey = llmKey
	}

	if cfg.Model == "" {
		return cfg, fmt.Errorf("no model specified — set via -M, CONTEXT_LLM_MODEL, or [llm].model in config.toml")
	}

	return cfg, nil
}

const llmTimeout = 60 * time.Second

func llmSummary(s *claude.Store, conv *claude.Conversation, cfg claude.LLMConfig) (string, error) {
	records, err := s.ReadConversation(conv.FilePath)
	if err != nil {
		return "", err
	}

	compact := claude.CompactConversation(records)
	if compact == "" {
		return "", fmt.Errorf("conversation has no content to summarize")
	}
	ctx, cancel := context.WithTimeout(context.Background(), llmTimeout)
	defer cancel()
	return claude.SummarizeWithLLM(ctx, cfg, compact)
}
