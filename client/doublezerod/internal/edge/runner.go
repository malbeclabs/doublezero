package edge

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync/atomic"
	"time"

	"golang.org/x/net/ipv4"
)

const (
	// maxDatagramSize is the maximum UDP datagram size we read.
	maxDatagramSize = 65535

	// summaryInterval is how often the runner logs a periodic
	// summary of records written, buffered messages, and known instruments.
	summaryInterval = 30 * time.Second
)

// RunnerConfig configures a single feed runner.
type RunnerConfig struct {
	Code           string
	GroupIP        net.IP
	MarketdataPort int
	RefdataPort    int
	Format         string
	OutputPath     string
	Parser         Parser
	Sink           OutputSink
}

// Runner listens for multicast packets on a group and decodes them.
type Runner struct {
	cfg              RunnerConfig
	recordsWritten   atomic.Uint64
	running          atomic.Bool
	firstFrameLogged atomic.Bool
	cancel           context.CancelFunc
}

// NewRunner creates a new feed runner. Call Run to start it.
func NewRunner(cfg RunnerConfig) *Runner {
	return &Runner{cfg: cfg}
}

// Run starts listening for multicast packets on both the marketdata and
// refdata ports. It blocks until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) error {
	r.running.Store(true)
	defer r.running.Store(false)

	errCh := make(chan error, 2)

	go func() {
		errCh <- r.listenPort(ctx, r.cfg.RefdataPort, "refdata")
	}()

	go func() {
		errCh <- r.listenPort(ctx, r.cfg.MarketdataPort, "marketdata")
	}()

	go r.logPeriodicSummary(ctx)

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

// logPeriodicSummary emits a periodic INFO log describing the runner's
// current state so operators can see liveness without polling edge status.
func (r *Runner) logPeriodicSummary(ctx context.Context) {
	ticker := time.NewTicker(summaryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			slog.Info("edge: runner summary",
				"code", r.cfg.Code,
				"records_written", r.recordsWritten.Load(),
				"buffered", r.cfg.Parser.Buffered(),
				"instruments_known", r.cfg.Parser.InstrumentCount())
		}
	}
}

// listenPort joins the multicast group on the given port and reads packets.
func (r *Runner) listenPort(ctx context.Context, port int, label string) (retErr error) {
	defer func() {
		if rv := recover(); rv != nil {
			retErr = fmt.Errorf("panic in %s listener for %s: %v", label, r.cfg.Code, rv)
			slog.Error("edge: feed runner panic recovered", "code", r.cfg.Code, "port", label, "panic", rv)
		}
	}()

	addr := &net.UDPAddr{
		IP:   r.cfg.GroupIP,
		Port: port,
	}

	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		return fmt.Errorf("joining multicast group %s port %d: %w", r.cfg.GroupIP, port, err)
	}
	defer conn.Close()

	// Allow multiple listeners on the same group (hot + ref may share an IP).
	pc := ipv4.NewPacketConn(conn)
	if err := pc.SetControlMessage(ipv4.FlagDst, true); err != nil {
		slog.Warn("edge: could not set control message flag", "code", r.cfg.Code, "error", err)
	}

	slog.Info("edge: listening for multicast", "code", r.cfg.Code, "group", r.cfg.GroupIP, "port", port, "label", label)

	buf := make([]byte, maxDatagramSize)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			slog.Warn("edge: read error", "code", r.cfg.Code, "port", label, "error", err)
			continue
		}

		records, err := r.cfg.Parser.Parse(buf[:n])
		if err != nil {
			slog.Warn("edge: parse error", "code", r.cfg.Code, "port", label, "error", err)
			continue
		}

		if len(records) > 0 {
			if r.firstFrameLogged.CompareAndSwap(false, true) {
				slog.Info("edge: parser producing records",
					"code", r.cfg.Code,
					"port", label,
					"first_batch_size", len(records))
			}
			if err := r.cfg.Sink.Write(records); err != nil {
				slog.Error("edge: sink write error", "code", r.cfg.Code, "error", err)
				continue
			}
			r.recordsWritten.Add(uint64(len(records)))
		}
	}
}

// Stop cancels the runner's context and closes the sink.
func (r *Runner) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
	if r.cfg.Sink != nil {
		r.cfg.Sink.Close() //nolint:errcheck
	}
}

// Status returns the current status of the runner.
func (r *Runner) Status() FeedStatus {
	return FeedStatus{
		Code:           r.cfg.Code,
		ParserName:     r.cfg.Parser.Name(),
		Format:         r.cfg.Format,
		OutputPath:     r.cfg.OutputPath,
		RecordsWritten: r.recordsWritten.Load(),
		Buffered:       r.cfg.Parser.Buffered(),
		Running:        r.running.Load(),
	}
}
