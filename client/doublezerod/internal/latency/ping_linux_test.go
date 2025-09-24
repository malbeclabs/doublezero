package latency

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_Latency_UDPPing_Linux(t *testing.T) {
	t.Run("Localhost_ReachesWithinDeadline", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		res := udpPing(ctx, newTestLogger(t), newTestDevice("127.0.0.1"))
		require.True(t, res.Reachable, "localhost should be reachable: %v", res)
		assert.Greater(t, res.Avg, int64(0))
		assert.GreaterOrEqual(t, res.Max, res.Min)
	})
}
