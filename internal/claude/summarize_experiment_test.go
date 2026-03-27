package claude

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// This is not a unit test — it's an experiment harness for evaluating
// different summarization prompts. Run with:
//   go test ./internal/claude/ -run TestSummarizeExperiment -v -count=1
//
// Requires LM Studio running at the configured URL.

var prompts = map[string]string{
	"current": namePrompt,

	"structured": `You name coding sessions. Given a condensed log of what the user asked for, respond with ONLY a short name (3-6 lowercase words, no quotes, no punctuation).

Focus on WHAT WAS BUILT or CHANGED, not the first message. If the session spans multiple topics, name the overall project or theme. Examples:
- "build rest api widget service"  ->  "widget rest api"
- "fix auth bug then add tests"   ->  "auth bugfix and tests"
- "initial project setup and cli"  ->  "cli project scaffolding"`,

	"last-message-weight": `You name coding sessions. Given a condensed log of user messages from a coding session, respond with ONLY a short descriptive name (3-6 lowercase words, no quotes, no punctuation).

IMPORTANT: The LAST few messages usually reveal what the session accomplished. The first message is often just a greeting or vague request. Weight later messages more heavily. Name the concrete outcome, not the opening question.`,

	"deliverable-focus": `Name this coding session in 3-6 lowercase words. No quotes, no punctuation.

Rules:
1. Name the DELIVERABLE, not the conversation. What artifact was created or modified?
2. Ignore greetings, meta-discussion, and process talk.
3. If tools like Write, Edit, Bash were used, the session produced code — name what it does.
4. Prefer specific nouns over vague ones: "fish completion generation" not "shell improvements".`,
}

func TestSummarizeExperiment(t *testing.T) {
	if os.Getenv("RUN_EXPERIMENTS") == "" {
		t.Skip("set RUN_EXPERIMENTS=1 to run summarization experiments")
	}

	// Load real conversation data
	store, err := DefaultStore()
	if err != nil {
		t.Fatal(err)
	}

	// Find conversations to test against.
	// Replace these with your own project dirs and session ID prefixes.
	testCases := []struct {
		project string
		prefix  string
		label   string
	}{
		// {"-Users-you-src-myproject", "abcd", "my project session"},
	}

	if len(testCases) == 0 {
		t.Skip("no test cases configured — edit summarize_experiment_test.go to add your own")
	}

	modelFilter := os.Getenv("TEST_MODEL") // "ministral", "gpt-oss", or "" for all
	allModels := []struct {
		name string
		cfg  LLMConfig
	}{
		{"ministral-3-14b", LLMConfig{
			BaseURL: "http://localhost:1234/v1",
			Model:   "mistralai/ministral-3-14b-reasoning",
		}},
		{"gpt-oss-20b", LLMConfig{
			BaseURL: "http://localhost:1234/v1",
			Model:   "openai/gpt-oss-20b",
		}},
		{"qwen3-4", LLMConfig{
			BaseURL: "http://localhost:1234/v1",
			Model:   "qwen3-4",
		}},
	}
	var models []struct {
		name string
		cfg  LLMConfig
	}
	for _, m := range allModels {
		if modelFilter == "" || strings.Contains(m.name, modelFilter) {
			models = append(models, m)
		}
	}

	for _, tc := range testCases {
		convs, err := store.Conversations(tc.project)
		if err != nil || len(convs) == 0 {
			t.Logf("skipping %s: %v", tc.label, err)
			continue
		}

		var conv *Conversation
		for _, c := range convs {
			if strings.HasPrefix(c.SessionID, tc.prefix) {
				conv = &c
				break
			}
		}
		if conv == nil {
			t.Logf("skipping %s: no match for prefix %s", tc.label, tc.prefix)
			continue
		}

		records, err := store.ReadConversation(conv.FilePath)
		if err != nil {
			t.Logf("skipping %s: %v", tc.label, err)
			continue
		}

		compact := CompactConversation(records)
		t.Logf("\n=== %s (compact: %d bytes) ===", tc.label, len(compact))

		inlinePrompt := `Name this coding session in 3-6 lowercase words (no quotes, no punctuation). Name the DELIVERABLE — what was built or changed. Ignore greetings. Respond with ONLY the name, nothing else.`

		for _, model := range models {
			t.Logf("  --- %s ---", model.name)
			for name, prompt := range prompts {
				ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
				result, err := summarizeWithPrompt(ctx, model.cfg, prompt, compact)
				cancel()
				if err != nil {
					t.Logf("    %-25s ERROR: %v", name, err)
					continue
				}
				t.Logf("    %-25s %s", name, result)
			}
			// Also try inline (no system message) for models that may not support it
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			result, err := summarizeInline(ctx, model.cfg, inlinePrompt, compact)
			cancel()
			if err != nil {
				t.Logf("    %-25s ERROR: %v", "inline", err)
			} else {
				t.Logf("    %-25s %s", "inline", result)
			}
		}
	}
}

func summarizeWithPrompt(ctx context.Context, cfg LLMConfig, sysPrompt, compact string) (string, error) {
	return summarizeWithMessages(ctx, cfg, []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: sysPrompt},
		{Role: openai.ChatMessageRoleUser, Content: compact},
	})
}

func summarizeInline(ctx context.Context, cfg LLMConfig, instruction, compact string) (string, error) {
	return summarizeWithMessages(ctx, cfg, []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleUser, Content: instruction + "\n\n" + compact},
	})
}

func summarizeWithMessages(ctx context.Context, cfg LLMConfig, msgs []openai.ChatCompletionMessage) (string, error) {
	config := openai.DefaultConfig(cfg.APIKey)
	if cfg.BaseURL != "" {
		config.BaseURL = cfg.BaseURL
	}
	client := openai.NewClientWithConfig(config)

	resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:     cfg.Model,
		MaxTokens: 30,
		Messages:  msgs,
	})
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices")
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}
