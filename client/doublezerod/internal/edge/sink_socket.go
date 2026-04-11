package edge

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
)

// SocketSink listens on a Unix domain socket and writes records to all
// connected clients. Each client gets its own encoder instance. Slow or
// disconnected clients are dropped silently.
type SocketSink struct {
	format   string
	sockPath string

	mu       sync.Mutex
	listener net.Listener
	clients  map[net.Conn]recordWriter
	closed   bool
}

// recordWriter writes records to a single connected client.
type recordWriter interface {
	writeRecords(records []Record) error
}

// NewSocketSink creates a Unix domain socket at sockPath and begins
// accepting connections. format must be "json" or "csv".
func NewSocketSink(format, sockPath string) (*SocketSink, error) {
	// Remove any stale socket file.
	os.Remove(sockPath) //nolint:errcheck

	lis, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("listening on unix socket %s: %w", sockPath, err)
	}

	if err := os.Chmod(sockPath, 0666); err != nil {
		lis.Close()
		return nil, fmt.Errorf("setting socket permissions: %w", err)
	}

	s := &SocketSink{
		format:   format,
		sockPath: sockPath,
		listener: lis,
		clients:  make(map[net.Conn]recordWriter),
	}

	go s.acceptLoop()
	return s, nil
}

func (s *SocketSink) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return
			}
			slog.Warn("edge: socket accept error", "path", s.sockPath, "error", err)
			continue
		}

		s.mu.Lock()
		var w recordWriter
		switch s.format {
		case "json":
			w = newJSONConnWriter(conn)
		case "csv":
			w = newCSVConnWriter(conn)
		}
		s.clients[conn] = w
		s.mu.Unlock()

		slog.Info("edge: socket client connected", "path", s.sockPath, "remote", conn.RemoteAddr())
	}
}

func (s *SocketSink) Write(records []Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for conn, w := range s.clients {
		if err := w.writeRecords(records); err != nil {
			slog.Warn("edge: dropping socket client", "path", s.sockPath, "error", err)
			conn.Close()
			delete(s.clients, conn)
		}
	}
	return nil
}

func (s *SocketSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true
	for conn := range s.clients {
		conn.Close()
	}
	s.clients = nil

	err := s.listener.Close()
	os.Remove(s.sockPath) //nolint:errcheck
	return err
}

// jsonConnWriter writes JSONL to a single connection.
type jsonConnWriter struct {
	enc *json.Encoder
}

func newJSONConnWriter(w io.Writer) *jsonConnWriter {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &jsonConnWriter{enc: enc}
}

func (j *jsonConnWriter) writeRecords(records []Record) error {
	for i := range records {
		if err := j.enc.Encode(&records[i]); err != nil {
			return err
		}
	}
	return nil
}

// csvConnWriter writes CSV quote/trade rows to a single connection.
type csvConnWriter struct {
	w             *csv.Writer
	wroteQuoteHdr bool
	wroteTradeHdr bool
}

func newCSVConnWriter(w io.Writer) *csvConnWriter {
	return &csvConnWriter{w: csv.NewWriter(w)}
}

func (c *csvConnWriter) writeRecords(records []Record) error {
	for i := range records {
		r := &records[i]
		switch r.Type {
		case "quote":
			if !c.wroteQuoteHdr {
				if err := c.w.Write(quoteCSVHeader); err != nil {
					return err
				}
				c.wroteQuoteHdr = true
			}
			if err := c.w.Write(quoteToCSVRow(r)); err != nil {
				return err
			}
		case "trade":
			if !c.wroteTradeHdr {
				if err := c.w.Write(tradeCSVHeader); err != nil {
					return err
				}
				c.wroteTradeHdr = true
			}
			if err := c.w.Write(tradeToCSVRow(r)); err != nil {
				return err
			}
		default:
			continue
		}
	}
	c.w.Flush()
	return c.w.Error()
}
