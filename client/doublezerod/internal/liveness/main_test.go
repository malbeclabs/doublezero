package liveness

import (
	"flag"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"golang.org/x/sys/unix"
)

var (
	debugFlag = flag.Bool("debug", false, "enable debug logging")
	quietFlag = flag.Bool("quiet", false, "disable logging")
)

func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(m.Run())
}

type testWriter struct {
	t  *testing.T
	mu sync.Mutex
}

func (w *testWriter) Write(p []byte) (int, error) {
	w.t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	w.t.Logf("%s", p)
	return len(p), nil
}

func newTestLogger(t *testing.T) *slog.Logger {
	return newTestLoggerWith(t, *quietFlag, *debugFlag)
}

func newTestLoggerWith(t *testing.T, quiet bool, debug bool) *slog.Logger {
	var w io.Writer
	if quiet {
		w = io.Discard
	} else {
		w = &testWriter{t: t}
	}
	logLevel := slog.LevelInfo
	if debug {
		logLevel = slog.LevelDebug
	}
	h := slog.NewTextHandler(w, &slog.HandlerOptions{Level: logLevel})
	return slog.New(h)
}

func wait[T any](t *testing.T, ch <-chan T, d time.Duration, name string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(d):
		t.Fatalf("timeout waiting for %s", name)
		var z T
		return z
	}
}

func newTestRoute(mutate func(*Route)) *Route {
	r := &Route{Route: routing.Route{
		Table:    100,
		Src:      net.IPv4(10, 4, 0, 1),
		Dst:      &net.IPNet{IP: net.IPv4(10, 4, 0, 11), Mask: net.CIDRMask(32, 32)},
		NextHop:  net.IPv4(10, 5, 0, 1),
		Protocol: unix.RTPROT_BGP,
	}}
	if mutate != nil {
		mutate(r)
	}
	return r
}

type MockRouteReaderWriter struct {
	RouteAddFunc        func(*routing.Route) error
	RouteDeleteFunc     func(*routing.Route) error
	RouteByProtocolFunc func(int) ([]*routing.Route, error)

	mu sync.Mutex
}

func (m *MockRouteReaderWriter) RouteAdd(r *routing.Route) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.RouteAddFunc == nil {
		return nil
	}
	return m.RouteAddFunc(r)
}

func (m *MockRouteReaderWriter) RouteDelete(r *routing.Route) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.RouteDeleteFunc == nil {
		return nil
	}
	return m.RouteDeleteFunc(r)
}

func (m *MockRouteReaderWriter) RouteByProtocol(protocol int) ([]*routing.Route, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.RouteByProtocolFunc == nil {
		return nil, nil
	}
	return m.RouteByProtocolFunc(protocol)
}
