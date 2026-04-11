package edge

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestParserRegistry(t *testing.T) {
	// topofbook should be registered via init().
	p, ok := NewParser("topofbook")
	if !ok {
		t.Fatal("topofbook parser not found in registry")
	}
	if p.Name() != "topofbook" {
		t.Errorf("expected parser name topofbook, got %s", p.Name())
	}

	_, ok = NewParser("nonexistent")
	if ok {
		t.Error("expected nonexistent parser to not be found")
	}

	names := RegisteredParsers()
	found := false
	for _, n := range names {
		if n == "topofbook" {
			found = true
		}
	}
	if !found {
		t.Error("expected topofbook in registered parsers")
	}
}

func TestManager_ServeStatus_Empty(t *testing.T) {
	mgr := NewManager(nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/edge/status", nil)
	w := httptest.NewRecorder()
	mgr.ServeStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var statuses []FeedStatus
	if err := json.NewDecoder(w.Body).Decode(&statuses); err != nil {
		t.Fatalf("error decoding response: %v", err)
	}
	if len(statuses) != 0 {
		t.Errorf("expected 0 statuses, got %d", len(statuses))
	}
}

func TestManager_ServeEnable_MissingFields(t *testing.T) {
	mgr := NewManager(nil, nil)

	body := bytes.NewBufferString(`{"code": "mg01"}`)
	req := httptest.NewRequest(http.MethodPost, "/edge/enable", body)
	w := httptest.NewRecorder()
	mgr.ServeEnable(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestManager_ServeDisable_NotEnabled(t *testing.T) {
	mgr := NewManager(nil, nil)

	body := bytes.NewBufferString(`{"code": "mg01"}`)
	req := httptest.NewRequest(http.MethodPost, "/edge/disable", body)
	w := httptest.NewRecorder()
	mgr.ServeDisable(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestManager_Enable_PortValidation(t *testing.T) {
	groupIP := net.IPv4(239, 0, 0, 1)
	mgr := NewManager(
		func(code string) (net.IP, error) { return groupIP, nil },
		func(ip net.IP) bool { return true },
	)

	tests := []struct {
		name    string
		cfg     FeedConfig
		wantErr string
	}{
		{
			name: "missing marketdata port",
			cfg: FeedConfig{
				Code: "mg01", ParserName: "topofbook", Format: "json",
				OutputPath:  filepath.Join(t.TempDir(), "out.jsonl"),
				RefdataPort: 7001,
			},
			wantErr: "marketdata_port",
		},
		{
			name: "missing refdata port",
			cfg: FeedConfig{
				Code: "mg01", ParserName: "topofbook", Format: "json",
				OutputPath:     filepath.Join(t.TempDir(), "out.jsonl"),
				MarketdataPort: 7000,
			},
			wantErr: "refdata_port",
		},
		{
			name: "same port for both",
			cfg: FeedConfig{
				Code: "mg01", ParserName: "topofbook", Format: "json",
				OutputPath:     filepath.Join(t.TempDir(), "out.jsonl"),
				MarketdataPort: 7000,
				RefdataPort:    7000,
			},
			wantErr: "must differ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mgr.Enable(tt.cfg)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestManager_Enable_NotSubscribed(t *testing.T) {
	groupIP := net.IPv4(239, 0, 0, 1)

	mgr := NewManager(
		func(code string) (net.IP, error) { return groupIP, nil },
		func(ip net.IP) bool { return false }, // not subscribed
	)

	err := mgr.Enable(FeedConfig{
		Code:           "mg01",
		ParserName:     "topofbook",
		Format:         "json",
		OutputPath:     filepath.Join(t.TempDir(), "out.jsonl"),
		MarketdataPort: 7000,
		RefdataPort:    7001,
	})
	if err == nil {
		t.Fatal("expected error when not subscribed, got nil")
	}
	if !strings.Contains(err.Error(), "not subscribed") {
		t.Errorf("expected 'not subscribed' error, got: %v", err)
	}
}

func TestManager_Enable_Subscribed(t *testing.T) {
	groupIP := net.IPv4(239, 0, 0, 1)

	mgr := NewManager(
		func(code string) (net.IP, error) { return groupIP, nil },
		func(ip net.IP) bool { return ip.Equal(groupIP) }, // subscribed
	)

	err := mgr.Enable(FeedConfig{
		Code:           "mg01",
		ParserName:     "topofbook",
		Format:         "json",
		OutputPath:     filepath.Join(t.TempDir(), "out.jsonl"),
		MarketdataPort: 7000,
		RefdataPort:    7001,
	})
	// Enable will fail at ListenMulticastUDP (no real network), but it
	// should get past the subscription check. Verify it didn't fail with
	// the subscription guard error.
	if err != nil && strings.Contains(err.Error(), "not subscribed") {
		t.Errorf("should have passed subscription check, got: %v", err)
	}

	// Clean up any runner that may have started.
	mgr.Close()
}
