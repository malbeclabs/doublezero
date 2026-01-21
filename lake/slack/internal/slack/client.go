package slack

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/malbeclabs/doublezero/lake/utils/pkg/retry"
	"github.com/slack-go/slack"
)

// Client wraps the Slack API client with additional functionality
type Client struct {
	api       *slack.Client
	botUserID string
	log       *slog.Logger
}

// NewClient creates a new Slack client
func NewClient(botToken, appToken string, log *slog.Logger) *Client {
	var api *slack.Client
	if appToken != "" {
		api = slack.New(botToken, slack.OptionAppLevelToken(appToken))
	} else {
		api = slack.New(botToken)
	}

	return &Client{
		api: api,
		log: log,
	}
}

// API returns the underlying Slack API client
func (c *Client) API() *slack.Client {
	return c.api
}

// Initialize performs initial setup like auth test and returns the bot user ID
func (c *Client) Initialize(ctx context.Context) (string, error) {
	authTest, err := c.api.AuthTestContext(ctx)
	if err != nil {
		c.log.Warn("slack auth test failed", "error", err)
		return "", err
	}

	c.botUserID = authTest.UserID
	c.log.Info("slack auth test successful", "user_id", authTest.UserID, "team", authTest.Team, "bot_id", authTest.BotID)
	return c.botUserID, nil
}

// BotUserID returns the bot's user ID
func (c *Client) BotUserID() string {
	return c.botUserID
}

// AddProcessingReaction adds a processing reaction to a message
func (c *Client) AddProcessingReaction(ctx context.Context, channelID, timestamp string) error {
	itemRef := slack.NewRefToMessage(channelID, timestamp)

	var err error
	retryCfg := retry.DefaultConfig()
	err = retry.Do(ctx, retryCfg, func() error {
		err = c.api.AddReactionContext(ctx, "speech_balloon", itemRef)
		if err != nil {
			// Don't retry on missing_scope errors - these are configuration issues
			if strings.Contains(err.Error(), "missing_scope") {
				c.log.Error("SCOPE ISSUE: reactions:write scope is missing from your bot token",
					"action_required", "1. Go to api.slack.com/apps -> Your App -> OAuth & Permissions",
					"action_required_2", "2. Verify 'reactions:write' is in Bot Token Scopes",
					"action_required_3", "3. If not there, add it and click 'Reinstall to Workspace'",
					"action_required_4", "4. Copy the NEW Bot User OAuth Token (even if it looks the same)",
					"action_required_5", "5. Update SLACK_BOT_TOKEN environment variable",
					"action_required_6", "6. Restart the bot")
				// Return a non-retryable error
				return fmt.Errorf("missing_scope: %w", err)
			}
			return err
		}
		return nil
	})

	if err != nil {
		if !strings.Contains(err.Error(), "missing_scope") {
			c.log.Warn("failed to add reaction after retries", "emoji", "speech_balloon", "error", err, "channel", channelID)
		}
		return err
	}

	c.log.Info("successfully added reaction", "emoji", "speech_balloon", "channel", channelID, "timestamp", timestamp)
	return nil
}

// RemoveProcessingReaction removes a processing reaction from a message
func (c *Client) RemoveProcessingReaction(ctx context.Context, channelID, timestamp string) error {
	itemRef := slack.NewRefToMessage(channelID, timestamp)

	var err error
	retryCfg := retry.DefaultConfig()
	err = retry.Do(ctx, retryCfg, func() error {
		err = c.api.RemoveReactionContext(ctx, "speech_balloon", itemRef)
		return err
	})

	if err != nil {
		c.log.Debug("failed to remove reaction after retries (may not have been added)", "emoji", "speech_balloon", "error", err)
		return err
	}

	c.log.Debug("removed reaction", "channel", channelID, "emoji", "speech_balloon")
	return nil
}

// PostMessage posts a message to a channel, optionally in a thread
func (c *Client) PostMessage(ctx context.Context, channelID string, text string, blocks []slack.Block, threadTS string) (string, error) {
	var msgOpts []slack.MsgOption

	// Always thread responses: use existing thread if available, otherwise create new thread
	if threadTS != "" {
		msgOpts = append(msgOpts, slack.MsgOptionTS(threadTS))
	}

	// Use blocks if provided, otherwise use plain text
	if blocks != nil {
		msgOpts = append(msgOpts, slack.MsgOptionBlocks(blocks...))
	} else {
		msgOpts = append(msgOpts, slack.MsgOptionText(text, false))
	}

	var respTS string
	var err error
	retryCfg := retry.DefaultConfig()
	err = retry.Do(ctx, retryCfg, func() error {
		_, respTS, err = c.api.PostMessageContext(ctx, channelID, msgOpts...)
		if err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return "", fmt.Errorf("failed to post message after retries: %w", err)
	}

	return respTS, nil
}

// CheckRootMessageMentioned checks if the root message of a thread mentioned the bot
func (c *Client) CheckRootMessageMentioned(ctx context.Context, channelID, threadTS, botUserID string) (bool, error) {
	c.log.Info("checkRootMessageMentioned: fetching thread replies", "channel", channelID, "thread_ts", threadTS)
	// Fetch the thread replies - the first message is the root message
	params := &slack.GetConversationRepliesParameters{
		ChannelID: channelID,
		Timestamp: threadTS,
		Limit:     1, // Only need the root message
	}

	var msgs []slack.Message
	var err error
	retryCfg := retry.DefaultConfig()
	err = retry.Do(ctx, retryCfg, func() error {
		var hasMore bool
		var nextCursor string
		msgs, hasMore, nextCursor, err = c.api.GetConversationRepliesContext(ctx, params)
		_ = hasMore
		_ = nextCursor
		if err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		c.log.Error("checkRootMessageMentioned: failed to get thread replies after retries", "error", err, "channel", channelID, "thread_ts", threadTS)
		return false, fmt.Errorf("failed to get thread replies: %w", err)
	}

	if len(msgs) == 0 {
		c.log.Warn("checkRootMessageMentioned: no messages found in thread", "thread_ts", threadTS)
		return false, nil
	}

	// First message is the root message
	rootMsg := msgs[0]
	c.log.Info("checkRootMessageMentioned: got root message", "root_ts", rootMsg.Timestamp, "text_preview", TruncateString(rootMsg.Text, 100))

	// Check if bot is mentioned in the root message
	mentionPattern1 := fmt.Sprintf("<@%s>", botUserID)
	mentionPattern2 := fmt.Sprintf("<@%s|", botUserID)
	mentioned := strings.Contains(rootMsg.Text, mentionPattern1) || strings.Contains(rootMsg.Text, mentionPattern2)

	c.log.Info("checkRootMessageMentioned: result", "thread_ts", threadTS, "root_ts", rootMsg.Timestamp, "mentioned", mentioned, "pattern1", mentionPattern1, "pattern2", mentionPattern2)
	return mentioned, nil
}

// IsBotMentioned checks if the bot is mentioned in the given text
func (c *Client) IsBotMentioned(text string) bool {
	if c.botUserID == "" {
		return false
	}
	mentionPattern1 := fmt.Sprintf("<@%s>", c.botUserID)
	mentionPattern2 := fmt.Sprintf("<@%s|", c.botUserID)
	return strings.Contains(text, mentionPattern1) || strings.Contains(text, mentionPattern2)
}

// UpdateMessage updates an existing message
func (c *Client) UpdateMessage(ctx context.Context, channelID, timestamp string, text string, blocks []slack.Block) error {
	var msgOpts []slack.MsgOption

	// Use blocks if provided, otherwise use plain text
	if blocks != nil {
		msgOpts = append(msgOpts, slack.MsgOptionBlocks(blocks...))
	} else {
		msgOpts = append(msgOpts, slack.MsgOptionText(text, false))
	}

	var err error
	retryCfg := retry.DefaultConfig()
	err = retry.Do(ctx, retryCfg, func() error {
		_, _, _, err = c.api.UpdateMessageContext(ctx, channelID, timestamp, msgOpts...)
		if err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to update message after retries: %w", err)
	}

	return nil
}

// DeleteMessage deletes an existing message
func (c *Client) DeleteMessage(ctx context.Context, channelID, timestamp string) error {
	var err error
	retryCfg := retry.DefaultConfig()
	err = retry.Do(ctx, retryCfg, func() error {
		_, _, err = c.api.DeleteMessageContext(ctx, channelID, timestamp)
		if err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to delete message after retries: %w", err)
	}

	return nil
}

// RemoveBotMention removes bot mention from text for cleaner processing
func (c *Client) RemoveBotMention(text string) string {
	if c.botUserID == "" {
		return text
	}
	// Remove <@USERID> or <@USERID|username> mentions
	mentionPattern1 := fmt.Sprintf("<@%s>", c.botUserID)
	mentionPattern2 := fmt.Sprintf("<@%s|", c.botUserID)
	text = strings.ReplaceAll(text, mentionPattern1, "")
	// Handle <@USERID|username> format - need to remove up to the closing >
	if strings.Contains(text, mentionPattern2) {
		// Use regex to remove <@USERID|username> pattern
		re := regexp.MustCompile(fmt.Sprintf(`<@%s\|[^>]+>`, regexp.QuoteMeta(c.botUserID)))
		text = re.ReplaceAllString(text, "")
	}
	return strings.TrimSpace(text)
}
