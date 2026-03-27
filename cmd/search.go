package cmd

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/aahoughton/cctx/internal/claude"
	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search <pattern>",
	Short: "Search conversations by content",
	Long: `Search across conversations for matching text.

By default, searches metadata (summary, first prompt) from the session index
to identify candidate conversations, then reads matching conversations to
show the actual message hits in context. Use -f/--full to search all message
content even when metadata doesn't match.

Searches the current project by default. Use -A/--all-projects to search
everywhere, or -p to target a specific project.

Filter by message role with --user or --assistant.

Examples:
  cctx search "auth middleware"          # search current project
  cctx search -f "handleRequest"         # full-text search
  cctx search -A "refactor"              # all projects
  cctx search -E "fix(ed|ing) bug"       # regex
  cctx search -u "please add"            # only user messages
  cctx search -a "created file"          # only assistant messages
  cctx search -l "auth"                  # session IDs only (grep -l style)`,
	Args: cobra.ExactArgs(1),
	RunE: runSearch,
}

var (
	searchFull        bool
	searchAllProjects bool
	searchRegex       bool
	searchUser        bool
	searchAssistant   bool
	searchFilesOnly   bool
)

const maxHitsPerConv = 5

func init() {
	searchCmd.Flags().BoolVarP(&searchFull, "full", "f", false, "search full message content (slower)")
	searchCmd.Flags().BoolVarP(&searchAllProjects, "all-projects", "A", false, "search across all projects")
	searchCmd.Flags().BoolVarP(&searchRegex, "regex", "E", false, "treat pattern as a regular expression")
	searchCmd.Flags().BoolVarP(&searchUser, "user", "u", false, "only match user messages (implies -f)")
	searchCmd.Flags().BoolVarP(&searchAssistant, "assistant", "a", false, "only match assistant messages (implies -f)")
	searchCmd.Flags().BoolVarP(&searchFilesOnly, "files-only", "l", false, "print only session IDs of matching conversations")
	registerCompletions(searchCmd, "")
	rootCmd.AddCommand(searchCmd)
}

// searchHit is a single matching message within a conversation.
type searchHit struct {
	role    string // "user" or "assistant"
	excerpt string
}

// searchResult groups all hits for one conversation.
type searchResult struct {
	project string
	conv    claude.Conversation
	hits    []searchHit
}

func runSearch(cmd *cobra.Command, args []string) error {
	pattern := args[0]

	// Role filters imply full-text search
	if searchUser || searchAssistant {
		searchFull = true
	}

	matcher, err := buildMatcher(pattern)
	if err != nil {
		return err
	}

	var projects []claude.Project
	if searchAllProjects {
		projects, err = store.Projects()
		if err != nil {
			return err
		}
	} else {
		path, err := resolveProject(cmd)
		if err != nil {
			return err
		}
		proj, err := store.FindProjectByPath(path)
		if err != nil {
			return err
		}
		projects = []claude.Project{*proj}
	}

	var results []searchResult
	for _, proj := range projects {
		r, err := searchProject(proj, matcher)
		if err != nil {
			return err
		}
		results = append(results, r...)
	}

	if len(results) == 0 {
		fmt.Println("No matches found.")
		return nil
	}

	if searchFilesOnly {
		printResultsFilesOnly(results)
	} else {
		printResults(results, searchAllProjects)
	}
	return nil
}

func buildMatcher(pattern string) (func(string) bool, error) {
	if searchRegex {
		re, err := regexp.Compile("(?i)" + pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex: %w", err)
		}
		return re.MatchString, nil
	}
	lower := strings.ToLower(pattern)
	return func(s string) bool {
		return strings.Contains(strings.ToLower(s), lower)
	}, nil
}

func searchProject(proj claude.Project, matcher func(string) bool) ([]searchResult, error) {
	convs, err := store.Conversations(proj.DirName)
	if err != nil {
		return nil, err
	}

	var results []searchResult
	for _, conv := range convs {
		// Decide whether to read this conversation's JSONL
		metadataHit := matcher(conv.Summary) || matcher(conv.FirstPrompt)
		if !searchFull && !metadataHit {
			continue
		}

		records, err := store.ReadConversation(conv.FilePath)
		if err != nil {
			continue
		}

		hits := collectHits(records, matcher)
		if len(hits) == 0 && !searchFull && metadataHit {
			// Metadata matched but no message-level hits — still show
			// the conversation with the first user message as context
			for _, rec := range records {
				if rec.Type == "user" {
					text := claude.MessageText(rec)
					if text != "" {
						hits = []searchHit{{
							role:    "user",
							excerpt: excerptFrom(text),
						}}
						break
					}
				}
			}
		}

		if len(hits) > 0 {
			results = append(results, searchResult{
				project: proj.OriginalPath,
				conv:    conv,
				hits:    hits,
			})
		}
	}

	return results, nil
}

func collectHits(records []claude.ConversationRecord, matcher func(string) bool) []searchHit {
	var hits []searchHit
	for _, rec := range records {
		if rec.Type != "user" && rec.Type != "assistant" {
			continue
		}
		if searchUser && rec.Type != "user" {
			continue
		}
		if searchAssistant && rec.Type != "assistant" {
			continue
		}

		text := claude.MessageText(rec)
		if text == "" {
			continue
		}

		if matcher(text) {
			hits = append(hits, searchHit{
				role:    rec.Type,
				excerpt: excerptFrom(text),
			})
			if len(hits) >= maxHitsPerConv {
				break
			}
		}
	}
	return hits
}

func excerptFrom(text string) string {
	s := strings.Join(strings.Fields(text), " ")
	if r := []rune(s); len(r) > 150 {
		s = string(r[:150]) + "..."
	}
	return s
}

func printResultsFilesOnly(results []searchResult) {
	for _, r := range results {
		fmt.Println(r.conv.SessionID)
	}
}

func printResults(results []searchResult, showProject bool) {
	total := 0
	for _, r := range results {
		total += len(r.hits)
	}
	fmt.Printf("%d hit(s) across %d conversation(s):\n\n", total, len(results))

	for _, r := range results {
		id := r.conv.SessionID
		if len(id) > 8 {
			id = id[:8]
		}

		modified := r.conv.Modified.Format(time.RFC3339)

		label := r.conv.Summary
		if label == "" {
			label = r.conv.Slug
		}
		if label == "" {
			label = "(unnamed)"
		}

		if showProject {
			fmt.Printf("  %s  %s  %s  %s\n", id, modified, r.project, label)
		} else {
			fmt.Printf("  %s  %s  %s\n", id, modified, label)
		}

		for _, hit := range r.hits {
			fmt.Printf("    [%s] %s\n", hit.role, hit.excerpt)
		}
		fmt.Println()
	}
}
