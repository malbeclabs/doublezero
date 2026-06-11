package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/tools/stress/device-orchestrator/pkg/agent"
	"github.com/stretchr/testify/require"
)

func TestNoopRunner_ClosesEventsWhenContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	r := agent.NewNoop(nil)
	require.NoError(t, r.Start(ctx))

	select {
	case <-r.Events():
		t.Fatal("noop runner emitted an event")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	select {
	case _, ok := <-r.Events():
		require.False(t, ok, "events channel should close on cancel")
	case <-time.After(time.Second):
		t.Fatal("events channel did not close after context cancel")
	}
}
