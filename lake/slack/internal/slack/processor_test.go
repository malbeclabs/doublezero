package slack

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAI_Slack_Processor_HasResponded(t *testing.T) {
	t.Parallel()

	p := NewProcessor(nil, nil, nil, "", slog.Default())
	messageKey := "C123:1234567890.123456"

	require.False(t, p.HasResponded(messageKey))

	p.MarkResponded(messageKey)
	require.True(t, p.HasResponded(messageKey))
}

func TestAI_Slack_Processor_MarkResponded(t *testing.T) {
	t.Parallel()

	p := NewProcessor(nil, nil, nil, "", slog.Default())
	messageKey := "C123:1234567890.123456"

	p.MarkResponded(messageKey)
	require.True(t, p.HasResponded(messageKey))
}

func TestAI_Slack_Processor_StartCleanup(t *testing.T) {
	t.Parallel()

	p := NewProcessor(nil, nil, nil, "", slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start cleanup - should not panic
	p.StartCleanup(ctx)

	// Give it a moment to start
	time.Sleep(10 * time.Millisecond)

	// Cancel context to stop cleanup
	cancel()

	// Wait a bit for cleanup to stop
	time.Sleep(10 * time.Millisecond)
}

func TestAI_Slack_Processor_NewProcessor(t *testing.T) {
	t.Parallel()

	p := NewProcessor(nil, nil, nil, "", slog.Default())
	require.NotNil(t, p)
	require.IsType(t, &Processor{}, p)
}
