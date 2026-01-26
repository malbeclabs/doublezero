package v3

import (
	"testing"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/workflow"
)

func TestBuildMessages_SkipsEmptyContent(t *testing.T) {
	// Create a workflow with minimal config for testing
	p := &Workflow{}

	tests := []struct {
		name         string
		userQuestion string
		history      []workflow.ConversationMessage
		wantMsgCount int // expected number of messages
	}{
		{
			name:         "no history",
			userQuestion: "What is DZ?",
			history:      nil,
			wantMsgCount: 1, // just the user question
		},
		{
			name:         "normal history",
			userQuestion: "Follow-up question",
			history: []workflow.ConversationMessage{
				{Role: "user", Content: "First question"},
				{Role: "assistant", Content: "First answer"},
			},
			wantMsgCount: 3, // 2 history + 1 user question
		},
		{
			name:         "history with empty assistant content (streaming placeholder)",
			userQuestion: "Follow-up question",
			history: []workflow.ConversationMessage{
				{Role: "user", Content: "First question"},
				{Role: "assistant", Content: ""}, // streaming placeholder with empty content
			},
			wantMsgCount: 2, // 1 user history + 1 user question (empty assistant skipped)
		},
		{
			name:         "history with empty user content",
			userQuestion: "Follow-up question",
			history: []workflow.ConversationMessage{
				{Role: "user", Content: ""}, // shouldn't happen but defensive
				{Role: "assistant", Content: "Answer"},
			},
			wantMsgCount: 2, // 1 assistant history + 1 user question (empty user skipped)
		},
		{
			name:         "all empty history",
			userQuestion: "Question",
			history: []workflow.ConversationMessage{
				{Role: "user", Content: ""},
				{Role: "assistant", Content: ""},
			},
			wantMsgCount: 1, // just the user question
		},
		{
			name:         "mixed empty and non-empty",
			userQuestion: "Final question",
			history: []workflow.ConversationMessage{
				{Role: "user", Content: "Q1"},
				{Role: "assistant", Content: "A1"},
				{Role: "user", Content: "Q2"},
				{Role: "assistant", Content: ""}, // incomplete streaming
			},
			wantMsgCount: 4, // 3 non-empty history + 1 user question
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messages := p.buildMessages(tt.userQuestion, tt.history)

			if len(messages) != tt.wantMsgCount {
				t.Errorf("buildMessages() returned %d messages, want %d", len(messages), tt.wantMsgCount)
				for i, msg := range messages {
					t.Logf("  message[%d]: role=%s, content=%q", i, msg.Role, msg.Content[0].Text)
				}
				return
			}

			// Verify no message has empty text content
			for i, msg := range messages {
				for j, block := range msg.Content {
					if block.Type == "text" && block.Text == "" {
						t.Errorf("buildMessages() message[%d].content[%d] has empty text", i, j)
					}
				}
			}

			// Verify the last message is the user question
			lastMsg := messages[len(messages)-1]
			if lastMsg.Role != "user" {
				t.Errorf("buildMessages() last message role = %q, want 'user'", lastMsg.Role)
			}
			if len(lastMsg.Content) != 1 || lastMsg.Content[0].Text != tt.userQuestion {
				t.Errorf("buildMessages() last message content = %q, want %q", lastMsg.Content[0].Text, tt.userQuestion)
			}
		})
	}
}
