package slack

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAI_Slack_EventHandler_NewEventHandler(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	handler := NewEventHandler(nil, nil, nil, slog.Default(), "U123", ctx)

	require.NotNil(t, handler)
	require.Equal(t, "U123", handler.botUserID)
	require.Equal(t, ctx, handler.shutdownCtx)
}

func TestAI_Slack_EventHandler_StartCleanup(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := NewEventHandler(nil, nil, nil, slog.Default(), "U123", ctx)

	// Start cleanup - should not panic
	handler.StartCleanup(ctx)

	// Give it a moment to start
	time.Sleep(10 * time.Millisecond)

	// Cancel context to stop cleanup
	cancel()

	// Wait a bit for cleanup to stop
	time.Sleep(10 * time.Millisecond)
}
