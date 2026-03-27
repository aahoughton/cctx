package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aahoughton/cctx/internal/claude"
	"github.com/spf13/cobra"
)

var summaryCmd = &cobra.Command{
	Use:     "show <session-prefix>",
	Aliases: []string{"summary"},
	Short:   "Show a conversation summary",
	Long: `Display a structured summary of a conversation.

Shows metadata (session ID, timestamps, branch, message count) followed
by either a digest of user messages or the full conversation transcript.

Use --llm to generate an LLM-powered narrative summary (1-3 paragraphs)
describing what was accomplished. Uses the same LLM configuration as rename.

Use -p to specify a project, or defaults to the current working directory.`,
	Args: cobra.ExactArgs(1),
	RunE: runSummary,
}

var (
	summaryFull   bool
	summaryUseLLM bool
)

func init() {
	summaryCmd.Flags().BoolVarP(&summaryFull, "full", "f", false, "show all user/assistant messages, not just a digest")
	summaryCmd.Flags().BoolVar(&summaryUseLLM, "llm", false, "generate a narrative summary using an LLM")
	summaryCmd.Flags().StringVarP(&llmURL, "llm-url", "U", "", "base URL for OpenAI-compatible API")
	summaryCmd.Flags().StringVarP(&llmModel, "llm-model", "M", "", "model name (e.g. llama3, claude-haiku-4-5)")
	summaryCmd.Flags().StringVarP(&llmKey, "llm-key", "K", "", "API key (see also: env vars, config.toml)")
	registerCompletions(summaryCmd, "session")
	rootCmd.AddCommand(summaryCmd)
}

func runSummary(cmd *cobra.Command, args []string) error {
	sessionPrefix := args[0]
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

	// Print metadata header
	fmt.Printf("Session:  %s\n", conv.SessionID)
	if conv.Slug != "" {
		fmt.Printf("Slug:     %s\n", conv.Slug)
	}
	if conv.Summary != "" {
		fmt.Printf("Summary:  %s\n", conv.Summary)
	}
	if !conv.Created.IsZero() {
		fmt.Printf("Created:  %s\n", conv.Created.Format("2006-01-02 15:04:05"))
	}
	if !conv.Modified.IsZero() {
		fmt.Printf("Modified: %s\n", conv.Modified.Format("2006-01-02 15:04:05"))
	}
	if conv.GitBranch != "" {
		fmt.Printf("Branch:   %s\n", conv.GitBranch)
	}
	fmt.Printf("Messages: %d\n", conv.MessageCount)
	fmt.Println()

	// Read and display conversation content
	records, err := store.ReadConversation(conv.FilePath)
	if err != nil {
		return err
	}

	if summaryUseLLM {
		return printLLMSummary(records)
	}
	if summaryFull {
		return printFullConversation(records)
	}
	return printDigest(records)
}

func printFullConversation(records []claude.ConversationRecord) error {
	for _, rec := range records {
		if rec.Type != "user" && rec.Type != "assistant" {
			continue
		}
		text := claude.MessageText(rec)
		if text == "" {
			continue
		}

		role := strings.ToUpper(rec.Type[:1]) + rec.Type[1:]
		fmt.Printf("--- %s ---\n", role)
		fmt.Println(text)
		fmt.Println()
	}
	return nil
}

func printDigest(records []claude.ConversationRecord) error {
	var userMsgs []string
	var toolUses int

	for _, rec := range records {
		switch rec.Type {
		case "user":
			text := claude.MessageText(rec)
			if text != "" {
				userMsgs = append(userMsgs, text)
			}
		case "assistant":
			if rec.Message != nil {
				if blocks, ok := rec.Message.Content.([]interface{}); ok {
					for _, b := range blocks {
						if m, ok := b.(map[string]interface{}); ok {
							if m["type"] == "tool_use" {
								toolUses++
							}
						}
					}
				}
			}
		}
	}

	if len(userMsgs) == 0 {
		fmt.Println("(empty conversation)")
		return nil
	}

	fmt.Println("== First prompt ==")
	fmt.Println(truncateDigest(userMsgs[0], 500))
	fmt.Println()

	if len(userMsgs) > 1 {
		fmt.Printf("== Conversation flow (%d user messages, %d tool uses) ==\n", len(userMsgs), toolUses)
		for i, msg := range userMsgs[1:] {
			fmt.Printf("  [%d] %s\n", i+2, truncateDigest(msg, 120))
		}
	}

	return nil
}

func truncateDigest(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen]) + "..."
}

func printLLMSummary(records []claude.ConversationRecord) error {
	cfg, err := buildLLMConfig()
	if err != nil {
		return err
	}

	detailed := claude.DetailedConversation(records)
	if detailed == "" {
		return fmt.Errorf("conversation has no content to summarize")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result, err := claude.DetailedSummaryWithLLM(ctx, cfg, detailed)
	if err != nil {
		return err
	}

	fmt.Println(result)
	return nil
}
