package claude

import (
	"strings"
	"testing"
)

func TestCompactConversation(t *testing.T) {
	records := []ConversationRecord{
		{Type: "user", Message: &MessageContent{Content: "build me a REST API for managing widgets"}},
		{Type: "assistant", Message: &MessageContent{Content: []interface{}{
			map[string]interface{}{"type": "tool_use", "name": "Write"},
			map[string]interface{}{"type": "text", "text": "I'll create the API..."},
		}}},
		{Type: "user", Message: &MessageContent{Content: "add pagination to the list endpoint"}},
		{Type: "assistant", Message: &MessageContent{Content: []interface{}{
			map[string]interface{}{"type": "tool_use", "name": "Edit"},
			map[string]interface{}{"type": "tool_use", "name": "Bash"},
		}}},
		{Type: "user", Message: &MessageContent{Content: "looks good, now add tests"}},
	}

	compact := CompactConversation(records)

	if !strings.Contains(compact, "Tools used: Write, Edit, Bash") {
		t.Errorf("expected tool list in compact output, got:\n%s", compact)
	}
	if !strings.Contains(compact, "[1] build me a REST API") {
		t.Errorf("expected first user message, got:\n%s", compact)
	}
	if !strings.Contains(compact, "[3] looks good, now add tests") {
		t.Errorf("expected third user message, got:\n%s", compact)
	}
}

func TestCompactConversation_FiltersNoise(t *testing.T) {
	records := []ConversationRecord{
		{Type: "user", Message: &MessageContent{Content: "real message"}},
		{Type: "user", Message: &MessageContent{Content: "<local-command-caveat>noise</local-command-caveat>"}},
		{Type: "user", Message: &MessageContent{Content: "<command-name>/exit</command-name>"}},
		{Type: "user", Message: &MessageContent{Content: "another real message"}},
	}

	compact := CompactConversation(records)

	if strings.Contains(compact, "local-command") || strings.Contains(compact, "command-name") {
		t.Errorf("protocol noise not filtered:\n%s", compact)
	}
	if !strings.Contains(compact, "[1] real message") || !strings.Contains(compact, "[2] another real message") {
		t.Errorf("real messages missing or mis-numbered:\n%s", compact)
	}
}

func TestCompactConversation_TruncatesLongMessages(t *testing.T) {
	longMsg := strings.Repeat("word ", 100) // 500 chars
	records := []ConversationRecord{
		{Type: "user", Message: &MessageContent{Content: longMsg}},
	}

	compact := CompactConversation(records)

	// Should be truncated to maxMsgChars + "..."
	lines := strings.Split(strings.TrimSpace(compact), "\n")
	last := lines[len(lines)-1]
	if !strings.HasSuffix(last, "...") {
		t.Errorf("long message not truncated: len=%d", len(last))
	}
	// The "[1] " prefix is 4 chars, so content should be ~maxMsgChars + 3 for "..."
	content := strings.TrimPrefix(last, "[1] ")
	if len(content) > maxMsgChars+10 {
		t.Errorf("truncated content too long: %d chars", len(content))
	}
}

func TestCompactConversation_CapsMessages(t *testing.T) {
	var records []ConversationRecord
	for i := 0; i < 30; i++ {
		records = append(records, ConversationRecord{
			Type:    "user",
			Message: &MessageContent{Content: "message"},
		})
	}

	compact := CompactConversation(records)

	count := strings.Count(compact, "[")
	if count != maxUserMessages {
		t.Errorf("expected %d messages, got %d", maxUserMessages, count)
	}
}

func TestCompactConversation_DeduplicatesTools(t *testing.T) {
	records := []ConversationRecord{
		{Type: "assistant", Message: &MessageContent{Content: []interface{}{
			map[string]interface{}{"type": "tool_use", "name": "Read"},
		}}},
		{Type: "assistant", Message: &MessageContent{Content: []interface{}{
			map[string]interface{}{"type": "tool_use", "name": "Read"},
			map[string]interface{}{"type": "tool_use", "name": "Edit"},
		}}},
	}

	compact := CompactConversation(records)

	if strings.Count(compact, "Read") != 1 {
		t.Errorf("Read should appear once, got:\n%s", compact)
	}
}
