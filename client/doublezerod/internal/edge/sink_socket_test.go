package edge

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestSocketSink_JSONBroadcast(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "test.sock")

	sink, err := NewSocketSink("json", sockPath)
	if err != nil {
		t.Fatalf("error creating socket sink: %v", err)
	}
	defer sink.Close()

	// Connect two clients.
	conn1, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("error connecting client 1: %v", err)
	}
	defer conn1.Close()

	conn2, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("error connecting client 2: %v", err)
	}
	defer conn2.Close()

	// Give the accept loop time to register clients.
	time.Sleep(50 * time.Millisecond)

	ts := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	records := []Record{
		{
			Type:           "quote",
			Timestamp:      ts,
			ChannelID:      1,
			SequenceNumber: 100,
			InstrumentID:   42,
			Symbol:         "BTC-USDT",
			Fields: map[string]any{
				"bid_price": 67432.5,
			},
		},
	}

	if err := sink.Write(records); err != nil {
		t.Fatalf("error writing to socket sink: %v", err)
	}

	// Both clients should receive the same JSONL record.
	for i, conn := range []net.Conn{conn1, conn2} {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		scanner := bufio.NewScanner(conn)
		if !scanner.Scan() {
			t.Fatalf("client %d: no data received", i+1)
		}
		var r Record
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			t.Fatalf("client %d: error decoding JSON: %v", i+1, err)
		}
		if r.Symbol != "BTC-USDT" {
			t.Errorf("client %d: expected symbol BTC-USDT, got %q", i+1, r.Symbol)
		}
	}
}

func TestSocketSink_CSVBroadcast(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "test.sock")

	sink, err := NewSocketSink("csv", sockPath)
	if err != nil {
		t.Fatalf("error creating socket sink: %v", err)
	}
	defer sink.Close()

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("error connecting: %v", err)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	ts := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	records := []Record{
		{
			Type: "quote", Timestamp: ts, ChannelID: 1, SequenceNumber: 100,
			InstrumentID: 42, Symbol: "BTC-USDT",
			Fields: map[string]any{
				"source_id": uint16(1), "bid_price": 67432.5, "bid_qty": 1.25,
				"ask_price": 67433.0, "ask_qty": 0.8, "bid_source_count": uint16(5),
				"ask_source_count": uint16(3), "update_flags": uint8(3), "snapshot": false,
			},
		},
	}

	if err := sink.Write(records); err != nil {
		t.Fatalf("error writing: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	scanner := bufio.NewScanner(conn)

	// First line should be the CSV header.
	if !scanner.Scan() {
		t.Fatal("no header received")
	}
	header := scanner.Text()
	if header[:4] != "type" {
		t.Errorf("expected CSV header starting with 'type', got %q", header[:4])
	}

	// Second line should be the data row.
	if !scanner.Scan() {
		t.Fatal("no data row received")
	}
	row := scanner.Text()
	if row[:5] != "quote" {
		t.Errorf("expected row starting with 'quote', got %q", row[:5])
	}
}

func TestSocketSink_DropsDisconnectedClient(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "test.sock")

	sink, err := NewSocketSink("json", sockPath)
	if err != nil {
		t.Fatalf("error creating socket sink: %v", err)
	}
	defer sink.Close()

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("error connecting: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Close the client before writing.
	conn.Close()

	ts := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	records := []Record{
		{Type: "heartbeat", Timestamp: ts, ChannelID: 1, SequenceNumber: 1},
	}

	// Write should succeed (disconnected client is dropped, not an error).
	if err := sink.Write(records); err != nil {
		t.Fatalf("expected no error after client disconnect, got: %v", err)
	}

	// Verify client was removed.
	sink.mu.Lock()
	count := len(sink.clients)
	sink.mu.Unlock()
	if count != 0 {
		t.Errorf("expected 0 clients after disconnect, got %d", count)
	}
}
