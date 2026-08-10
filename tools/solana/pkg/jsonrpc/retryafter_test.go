package jsonrpc

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestTools_Solana_JSONRPC_ParseRetryAfter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"absent", "", 0},
		{"delta seconds", "10", 10 * time.Second},
		{"delta seconds, large", "120", 2 * time.Minute},
		// A zero or negative delta means "retry now", which is what our own backoff
		// already does. Reporting 0 keeps the caller on its own schedule.
		{"delta zero", "0", 0},
		{"delta negative", "-5", 0},
		{"http date ahead", "Mon, 10 Aug 2026 12:00:30 GMT", 30 * time.Second},
		// A date already past is stale, not an instruction to wait.
		{"http date behind", "Mon, 10 Aug 2026 11:59:30 GMT", 0},
		{"garbage", "soon", 0},
		{"empty-ish", "   ", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, ParseRetryAfter(tc.value, now))
		})
	}
}

// TestTools_Solana_JSONRPC_NoteRetryAfter_IgnoresForeignContext: NoteRetryAfter is
// called from a transport that cannot know whether the client it serves was built by
// this package. A context without a sink must be a no-op, never a panic.
func TestTools_Solana_JSONRPC_NoteRetryAfter_IgnoresForeignContext(t *testing.T) {
	t.Parallel()

	NoteRetryAfter(context.Background(), 5*time.Second)

	ctx, sink := withRetryAfterSink(context.Background())
	NoteRetryAfter(ctx, 0)
	require.Zero(t, sink.take(), "a non-positive wait is not a wait")

	NoteRetryAfter(ctx, 7*time.Second)
	require.Equal(t, 7*time.Second, sink.take())
	require.Zero(t, sink.take(), "take must clear the slot, or a stale wait paces the next attempt")
}

// TestTools_Solana_JSONRPC_NextWait_PrefersTheEndpointsNumber is the whole point.
// The package backoff totals ~3s across four attempts while the provider fronting our
// mainnet ledger enforces its limits over a rolling 10s window, so every attempt of a
// rate-limited call lands inside the window that refused the first one. Only the
// endpoint knows its window, so its number has to win.
func TestTools_Solana_JSONRPC_NextWait_PrefersTheEndpointsNumber(t *testing.T) {
	t.Parallel()

	opt := RetryOptions{BaseBackoff: 500 * time.Millisecond, MaxBackoff: 5 * time.Second, MaxRetryAfter: 15 * time.Second}

	// No header: our own jittered backoff, which for attempt 2 is [250ms, 500ms].
	wait, honored, ok := nextWait(opt, 2, 0, 0)
	require.True(t, ok)
	require.Zero(t, honored, "backoff we chose ourselves must not count against the allowance")
	require.GreaterOrEqual(t, wait, 250*time.Millisecond)
	require.LessOrEqual(t, wait, 500*time.Millisecond)

	// With a header the wait is the endpoint's number, spread upward only. Arriving
	// before the window rolls is arriving refused, so the spread never subtracts.
	var sawSpread bool
	for range 40 {
		wait, honored, ok = nextWait(opt, 2, 10*time.Second, 0)
		require.True(t, ok)
		require.GreaterOrEqual(t, wait, 10*time.Second,
			"waiting less than the endpoint asked spends the wait and still gets refused")
		require.LessOrEqual(t, wait, 12500*time.Millisecond)
		require.Equal(t, wait, honored, "an honored wait counts fully against the allowance")
		if wait > 10*time.Second {
			sawSpread = true
		}
	}
	require.True(t, sawSpread,
		"every client refused in the same window would resume in lockstep and re-spike the endpoint")

	// A 10s wait beats a 500ms backoff by 20x, which is the entire fix.
	quiet, _, _ := nextWait(opt, 2, 0, 0)
	loud, _, _ := nextWait(opt, 2, 10*time.Second, 0)
	require.Greater(t, loud, 10*quiet)
}

// TestTools_Solana_JSONRPC_NextWait_StopsRatherThanWaitShort: the allowance bounds how
// long one call can be held, and a call that cannot afford what the endpoint asked for
// must end. Waiting a shorter time is the worst of both: it spends the wait and is
// refused anyway, because the window has not rolled.
func TestTools_Solana_JSONRPC_NextWait_StopsRatherThanWaitShort(t *testing.T) {
	t.Parallel()

	opt := RetryOptions{BaseBackoff: 500 * time.Millisecond, MaxBackoff: 5 * time.Second, MaxRetryAfter: 15 * time.Second}

	// Asked for more than the whole allowance.
	_, _, ok := nextWait(opt, 2, 30*time.Second, 0)
	require.False(t, ok, "a wait longer than one call may hold must end the call, not be truncated")

	// Fits on its own, but not on top of what earlier attempts already waited.
	_, _, ok = nextWait(opt, 3, 10*time.Second, 10*time.Second)
	require.False(t, ok, "the allowance is per call, summed across attempts")

	// One 10s wait does fit, from a clean start.
	wait, honored, ok := nextWait(opt, 2, 10*time.Second, 0)
	require.True(t, ok)
	require.Equal(t, wait, honored)

	// A negative allowance turns the header off, leaving the caller's own backoff.
	off := opt
	off.MaxRetryAfter = -1
	wait, honored, ok = nextWait(off, 2, 10*time.Second, 0)
	require.True(t, ok)
	require.Zero(t, honored)
	require.LessOrEqual(t, wait, 500*time.Millisecond)
}

// TestTools_Solana_JSONRPC_DoRetry_HonorsRetryAfterAndStopsWhenTooLong drives the loop
// rather than the helper, so it pins the two things the helper cannot: that the wait is
// actually taken, and that giving up returns the rate-limit error rather than a
// deadline or a nil.
func TestTools_Solana_JSONRPC_DoRetry_HonorsRetryAfterAndStopsWhenTooLong(t *testing.T) {
	t.Parallel()

	rateLimited := &solanaRateLimit{}

	t.Run("waits the endpoints number, then succeeds", func(t *testing.T) {
		t.Parallel()

		opt := RetryOptions{
			MaxAttempts: 4, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond,
			MaxRetryAfter: time.Second, IsRetryableFunc: func(error) bool { return true },
		}
		var attempts int
		start := time.Now()
		err := doRetry(context.Background(), opt, "getTransaction", true, func(ctx context.Context) error {
			attempts++
			if attempts == 1 {
				NoteRetryAfter(ctx, 120*time.Millisecond)
				return rateLimited
			}
			return nil
		})
		elapsed := time.Since(start)

		require.NoError(t, err)
		require.Equal(t, 2, attempts)
		require.GreaterOrEqual(t, elapsed, 120*time.Millisecond,
			"the endpoint's wait must be taken; a 1ms backoff would retry inside the same window")
	})

	t.Run("gives up and returns the rate limit", func(t *testing.T) {
		t.Parallel()

		const method = "getSignaturesForAddress"
		before := testutil.ToFloat64(retryAfterExceededTotal.WithLabelValues(method))

		opt := RetryOptions{
			MaxAttempts: 4, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond,
			MaxRetryAfter: 50 * time.Millisecond, IsRetryableFunc: func(error) bool { return true },
		}
		var attempts int
		err := doRetry(context.Background(), opt, method, true, func(ctx context.Context) error {
			attempts++
			NoteRetryAfter(ctx, 10*time.Second)
			return rateLimited
		})

		require.ErrorIs(t, err, rateLimited,
			"the caller must see the rate limit, not a truncated-wait failure or a nil")
		require.Equal(t, 1, attempts, "no attempt may follow a wait we cannot afford")
		require.Equal(t, before+1, testutil.ToFloat64(retryAfterExceededTotal.WithLabelValues(method)),
			"giving up on the allowance is the series that says the cap is set wrong")
	})

	t.Run("no header leaves the existing backoff alone", func(t *testing.T) {
		t.Parallel()

		opt := RetryOptions{
			MaxAttempts: 3, BaseBackoff: time.Millisecond, MaxBackoff: 2 * time.Millisecond,
			MaxRetryAfter: time.Hour, IsRetryableFunc: func(error) bool { return true },
		}
		var attempts int
		start := time.Now()
		err := doRetry(context.Background(), opt, "getSlot", true, func(ctx context.Context) error {
			attempts++
			return rateLimited
		})

		require.ErrorIs(t, err, rateLimited)
		require.Equal(t, 3, attempts, "all attempts must still be spent when no header is offered")
		require.Less(t, time.Since(start), time.Second,
			"an endpoint that names no number must not be waited on as if it had")
	})
}

// solanaRateLimit stands in for a provider refusal. The shape does not matter here —
// these tests supply their own classifier — only that it is a distinct error value.
type solanaRateLimit struct{}

func (*solanaRateLimit) Error() string {
	return "Too many requests for a specific RPC call"
}

var _ error = (*solanaRateLimit)(nil)
