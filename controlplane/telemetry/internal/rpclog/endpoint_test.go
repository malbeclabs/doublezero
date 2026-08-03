package rpclog_test

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/rpclog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentTelemetry_RPCLog_EndpointLogger(t *testing.T) {
	t.Parallel()

	newLogger := func() (*rpclog.EndpointLogger, *bytes.Buffer) {
		var logs bytes.Buffer
		return rpclog.NewEndpointLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))), &logs
	}

	t.Run("logs the first connection to a target", func(t *testing.T) {
		t.Parallel()

		l, logs := newLogger()
		l.OnDial("ledger.example.com:443", "10.0.0.1:443")

		out := logs.String()
		assert.Contains(t, out, "Ledger RPC connection established")
		assert.Contains(t, out, "host=ledger.example.com:443")
		assert.Contains(t, out, "remote=10.0.0.1:443")
	})

	t.Run("stays quiet while the resolved address is unchanged", func(t *testing.T) {
		t.Parallel()

		l, logs := newLogger()
		for range 100 {
			l.OnDial("ledger.example.com:443", "10.0.0.1:443")
		}

		// The namespaced transport dials once per request, so repeats must not each log.
		assert.Equal(t, 1, strings.Count(logs.String(), "Ledger RPC"), "repeat dials to the same address should log once")
	})

	t.Run("logs a move to a different address behind the same host", func(t *testing.T) {
		t.Parallel()

		l, logs := newLogger()
		l.OnDial("ledger.example.com:443", "10.0.0.1:443")
		l.OnDial("ledger.example.com:443", "10.0.0.2:443")
		l.OnDial("ledger.example.com:443", "10.0.0.2:443")

		out := logs.String()
		assert.Contains(t, out, "Ledger RPC endpoint changed")
		assert.Contains(t, out, "remote=10.0.0.2:443")
		assert.Contains(t, out, "previousRemote=10.0.0.1:443")
		assert.Equal(t, 2, strings.Count(out, "Ledger RPC"), "one line for the first connection, one for the change")
	})

	t.Run("tracks targets independently", func(t *testing.T) {
		t.Parallel()

		l, logs := newLogger()
		l.OnDial("ledger.example.com:443", "10.0.0.1:443")
		l.OnDial("other.example.com:443", "10.0.0.1:443")

		assert.Equal(t, 2, strings.Count(logs.String(), "Ledger RPC connection established"))
	})

	t.Run("is safe for concurrent dials", func(t *testing.T) {
		t.Parallel()

		l, logs := newLogger()

		var wg sync.WaitGroup
		for range 50 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				l.OnDial("ledger.example.com:443", "10.0.0.1:443")
			}()
		}
		wg.Wait()

		require.Equal(t, 1, strings.Count(logs.String(), "Ledger RPC"))
	})
}
