package claude

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	openai "github.com/sashabaranov/go-openai"
)

const maxUserMessages = 20
const maxMsgChars = 150

// LLMConfig holds configuration for the LLM summarization endpoint.
// Claude models (detected by "claude-" prefix) use the Anthropic API directly.
// All other models use the OpenAI-compatible chat completions API (Ollama,
// LM Studio, vLLM, llama.cpp, OpenAI, Groq, Together, etc).
type LLMConfig struct {
	BaseURL string `toml:"url"`    // e.g. "http://localhost:11434/v1" for Ollama
	Model   string `toml:"model"`  // e.g. "llama3", "gpt-4o-mini", "claude-haiku-4-5"
	APIKey  string `toml:"api_key"` // optional for local models, required for hosted
}

// CompactConversation builds a condensed representation of a conversation
// suitable for sending to an LLM for summarization. It extracts user messages
// (truncated) and tool names to capture the intent and shape of the work
// without the bulk of code and tool output.
func CompactConversation(records []ConversationRecord) string {
	var (
		userMsgs  []string
		toolNames []string
		toolSeen  = make(map[string]bool)
	)

	for _, rec := range records {
		switch rec.Type {
		case "user":
			text := MessageText(rec)
			if text == "" {
				continue
			}
			// Skip Claude protocol noise
			if strings.HasPrefix(text, "<local-command") || strings.HasPrefix(text, "<command-name>") {
				continue
			}
			text = strings.Join(strings.Fields(text), " ")
			if r := []rune(text); len(r) > maxMsgChars {
				text = string(r[:maxMsgChars]) + "..."
			}
			if len(userMsgs) < maxUserMessages {
				userMsgs = append(userMsgs, text)
			}

		case "assistant":
			if rec.Message == nil {
				continue
			}
			if blocks, ok := rec.Message.Content.([]interface{}); ok {
				for _, b := range blocks {
					if m, ok := b.(map[string]interface{}); ok {
						if m["type"] == "tool_use" {
							if name, ok := m["name"].(string); ok && !toolSeen[name] {
								toolSeen[name] = true
								toolNames = append(toolNames, name)
							}
						}
					}
				}
			}
		}
	}

	var sb strings.Builder
	if len(toolNames) > 0 {
		sb.WriteString("Tools used: ")
		sb.WriteString(strings.Join(toolNames, ", "))
		sb.WriteString("\n\n")
	}
	for i, msg := range userMsgs {
		fmt.Fprintf(&sb, "[%d] %s\n", i+1, msg)
	}
	return sb.String()
}

const namePrompt = "Name this coding session in 3-6 lowercase words. No quotes, no punctuation. " +
	"Name the DELIVERABLE — what was built or changed, not what was discussed. " +
	"Ignore greetings and meta-discussion. " +
	"The LAST few messages usually reveal what the session accomplished — weight them heavily. " +
	"Prefer specific nouns: 'fish completion generation' not 'shell improvements'. " +
	"Respond with ONLY the name, nothing else."

const summaryPrompt = "Summarize this coding session in 1-3 short paragraphs. " +
	"Focus on what was accomplished: what was built, changed, fixed, or decided. " +
	"Mention specific files, functions, or systems when relevant. " +
	"Note any important decisions, trade-offs, or open questions. " +
	"Write in plain prose, no bullet points or headers. Be concrete and specific."

// DetailedConversation builds a richer representation of a conversation for
// LLM summarization. Unlike CompactConversation, it includes assistant text
// and allows longer message excerpts to preserve more context.
func DetailedConversation(records []ConversationRecord) string {
	var sb strings.Builder
	var toolNames []string
	toolSeen := make(map[string]bool)

	for _, rec := range records {
		switch rec.Type {
		case "user":
			text := MessageText(rec)
			if text == "" {
				continue
			}
			if strings.HasPrefix(text, "<local-command") || strings.HasPrefix(text, "<command-name>") {
				continue
			}
			text = strings.Join(strings.Fields(text), " ")
			if r := []rune(text); len(r) > 1000 {
				text = string(r[:1000]) + "..."
			}
			fmt.Fprintf(&sb, "USER: %s\n\n", text)

		case "assistant":
			if rec.Message == nil {
				continue
			}
			text := MessageText(rec)
			if text != "" {
				text = strings.Join(strings.Fields(text), " ")
				if r := []rune(text); len(r) > 500 {
					text = string(r[:500]) + "..."
				}
				fmt.Fprintf(&sb, "ASSISTANT: %s\n\n", text)
			}
			if blocks, ok := rec.Message.Content.([]interface{}); ok {
				for _, b := range blocks {
					if m, ok := b.(map[string]interface{}); ok {
						if m["type"] == "tool_use" {
							if name, ok := m["name"].(string); ok && !toolSeen[name] {
								toolSeen[name] = true
								toolNames = append(toolNames, name)
							}
						}
					}
				}
			}
		}
	}

	var header strings.Builder
	if len(toolNames) > 0 {
		header.WriteString("Tools used: ")
		header.WriteString(strings.Join(toolNames, ", "))
		header.WriteString("\n\n")
	}
	return header.String() + sb.String()
}

// SummarizeWithLLM sends a compacted conversation to an LLM and returns a
// short (~5 word) descriptive name. Claude models use the Anthropic API
// directly; all others use the OpenAI-compatible chat completions API.
func SummarizeWithLLM(ctx context.Context, cfg LLMConfig, compact string) (string, error) {
	return llmComplete(ctx, cfg, namePrompt, 30, compact)
}

// DetailedSummaryWithLLM sends a detailed conversation transcript to an LLM
// and returns a 1-3 paragraph summary of what was accomplished.
func DetailedSummaryWithLLM(ctx context.Context, cfg LLMConfig, detailed string) (string, error) {
	return llmComplete(ctx, cfg, summaryPrompt, 1024, detailed)
}

func llmComplete(ctx context.Context, cfg LLMConfig, system string, maxTokens int, content string) (string, error) {
	if strings.HasPrefix(cfg.Model, "claude-") {
		return completeAnthropic(ctx, cfg, system, maxTokens, content)
	}
	return completeOpenAI(ctx, cfg, system, maxTokens, content)
}

func completeAnthropic(ctx context.Context, cfg LLMConfig, system string, maxTokens int, content string) (string, error) {
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	client := anthropic.NewClient(opts...)

	resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     cfg.Model,
		MaxTokens: int64(maxTokens),
		System: []anthropic.TextBlockParam{
			{Text: system},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(content)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("Anthropic API call failed: %w", err)
	}

	for _, block := range resp.Content {
		if block.Type == "text" {
			result := strings.TrimSpace(block.Text)
			if result != "" {
				return result, nil
			}
		}
	}
	return "", fmt.Errorf("Anthropic returned no text content")
}

func completeOpenAI(ctx context.Context, cfg LLMConfig, system string, maxTokens int, content string) (string, error) {
	config := openai.DefaultConfig(cfg.APIKey)
	if cfg.BaseURL != "" {
		config.BaseURL = cfg.BaseURL
	}
	client := openai.NewClientWithConfig(config)

	resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:     cfg.Model,
		MaxTokens: maxTokens,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: system},
			{Role: openai.ChatMessageRoleUser, Content: content},
		},
	})
	if err != nil {
		return "", fmt.Errorf("LLM API call failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("LLM returned no choices")
	}

	result := strings.TrimSpace(resp.Choices[0].Message.Content)
	if result == "" || strings.HasPrefix(result, "<|") {
		return "", fmt.Errorf("model returned empty or invalid response — it may not support instruction following")
	}
	return result, nil
}
