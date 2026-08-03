// Package rpclog records which RPC endpoint the agent is actually talking to.
//
// A ledger RPC URL is usually a hostname in front of several load balancer addresses, and
// nothing in the RPC stack reports which of them a given connection landed on. When one
// address misbehaves and the others are fine, that is the fact triage needs first, and it
// was missing during the 2026-07-29 outage.
package rpclog

import (
	"log/slog"
	"sync"
)

// EndpointLogger logs the resolved remote address of new RPC connections.
//
// It logs transitions, not dials: the first connection to a dial target, and any later
// connection that resolves somewhere new. Repeats are silent, which matters because the
// namespaced transport does not pool connections and dials once per request.
type EndpointLogger struct {
	log *slog.Logger

	mu   sync.Mutex
	seen map[string]string // dial target -> last logged remote address
}

func NewEndpointLogger(log *slog.Logger) *EndpointLogger {
	return &EndpointLogger{
		log:  log,
		seen: make(map[string]string),
	}
}

// OnDial reports a newly established connection to addr, resolved to remote. It is safe for
// concurrent use and does no I/O beyond the log line it may emit.
func (l *EndpointLogger) OnDial(addr, remote string) {
	l.mu.Lock()
	previous, known := l.seen[addr]
	if known && previous == remote {
		l.mu.Unlock()
		return
	}
	l.seen[addr] = remote
	l.mu.Unlock()

	if known {
		l.log.Info("Ledger RPC endpoint changed", "host", addr, "remote", remote, "previousRemote", previous)
		return
	}
	l.log.Info("Ledger RPC connection established", "host", addr, "remote", remote)
}
