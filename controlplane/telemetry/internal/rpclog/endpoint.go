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
// It logs each distinct (target, remote) pair once: a dial target is usually a hostname in
// front of several load balancer addresses served off a single rotating A record, so "same
// target" does not imply "same remote" from one dial to the next. Deduping on target alone
// would make a routine DNS rotation look like a constant stream of endpoint changes; deduping
// on the pair means output is bounded by the backend pool size, and a genuinely new backend
// still logs once.
type EndpointLogger struct {
	log *slog.Logger

	mu   sync.Mutex
	seen map[string]map[string]struct{} // dial target -> set of remotes already logged
}

func NewEndpointLogger(log *slog.Logger) *EndpointLogger {
	return &EndpointLogger{
		log:  log,
		seen: make(map[string]map[string]struct{}),
	}
}

// OnDial reports a newly established connection to addr, resolved to remote. It is safe for
// concurrent use and does no I/O beyond the log line it may emit.
func (l *EndpointLogger) OnDial(addr, remote string) {
	l.mu.Lock()
	remotes, known := l.seen[addr]
	if !known {
		remotes = make(map[string]struct{})
		l.seen[addr] = remotes
	}
	if _, seen := remotes[remote]; seen {
		l.mu.Unlock()
		return
	}
	remotes[remote] = struct{}{}
	l.mu.Unlock()

	l.log.Info("Ledger RPC connection established", "host", addr, "remote", remote)
}
