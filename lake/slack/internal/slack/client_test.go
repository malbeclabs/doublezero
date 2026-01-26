package slack

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAI_Slack_Client_IsBotMentioned(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		botUserID string
		text      string
		want      bool
	}{
		{
			name:      "mentioned with simple format",
			botUserID: "U12345",
			text:      "Hello <@U12345> how are you?",
			want:      true,
		},
		{
			name:      "mentioned with username format",
			botUserID: "U12345",
			text:      "Hey <@U12345|botname> what's up?",
			want:      true,
		},
		{
			name:      "not mentioned",
			botUserID: "U12345",
			text:      "Hello world",
			want:      false,
		},
		{
			name:      "mentioned different user",
			botUserID: "U12345",
			text:      "Hello <@U99999>",
			want:      false,
		},
		{
			name:      "empty bot user ID",
			botUserID: "",
			text:      "Hello <@U12345>",
			want:      false,
		},
		{
			name:      "empty text",
			botUserID: "U12345",
			text:      "",
			want:      false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := &Client{
				botUserID: tt.botUserID,
				log:       slog.Default(),
			}

			got := client.IsBotMentioned(tt.text)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestAI_Slack_Client_RemoveBotMention(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		botUserID string
		text      string
		want      string
	}{
		{
			name:      "remove simple mention",
			botUserID: "U12345",
			text:      "Hello <@U12345> how are you?",
			want:      "Hello  how are you?", // Note: double space after removal, TrimSpace only trims ends
		},
		{
			name:      "remove username mention",
			botUserID: "U12345",
			text:      "Hey <@U12345|botname> what's up?",
			want:      "Hey  what's up?", // Double space after removal
		},
		{
			name:      "no mention",
			botUserID: "U12345",
			text:      "Hello world",
			want:      "Hello world",
		},
		{
			name:      "multiple mentions",
			botUserID: "U12345",
			text:      "<@U12345> hello <@U12345|bot> world",
			want:      "hello  world", // Double space after removal
		},
		{
			name:      "empty bot user ID",
			botUserID: "",
			text:      "Hello <@U12345>",
			want:      "Hello <@U12345>",
		},
		{
			name:      "empty text",
			botUserID: "U12345",
			text:      "",
			want:      "",
		},
		{
			name:      "only mention",
			botUserID: "U12345",
			text:      "<@U12345>",
			want:      "",
		},
		{
			name:      "mention with extra spaces",
			botUserID: "U12345",
			text:      "  <@U12345>  hello  ",
			want:      "hello",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := &Client{
				botUserID: tt.botUserID,
				log:       slog.Default(),
			}

			got := client.RemoveBotMention(tt.text)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestAI_Slack_Client_NewClient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		botToken string
		appToken string
	}{
		{
			name:     "with app token",
			botToken: "xoxb-test",
			appToken: "xapp-test",
		},
		{
			name:     "without app token",
			botToken: "xoxb-test",
			appToken: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := NewClient(tt.botToken, tt.appToken, slog.Default())
			require.NotNil(t, client)
			require.NotNil(t, client.API())
		})
	}
}

func TestAI_Slack_Client_BotUserID(t *testing.T) {
	t.Parallel()

	client := &Client{
		botUserID: "U12345",
	}

	require.Equal(t, "U12345", client.BotUserID())
}

func TestAI_Slack_Client_API(t *testing.T) {
	t.Parallel()

	client := NewClient("xoxb-test", "", slog.Default())
	api := client.API()
	require.NotNil(t, api)
}
