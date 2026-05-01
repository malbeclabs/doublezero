package enricher

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"
)

// ClickhouseReader is an interface for reading from ClickHouse.
// It's satisfied by *sql.DB and can be easily mocked for testing.
type ClickhouseReader interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type IfIndexRecord struct {
	Pubkey      string `ch:"pubkey"`
	IfIndex     uint64 `ch:"ifindex"`
	IPv4Address net.IP `ch:"ipv4_address"`
	IfName      string `ch:"ifname"`
	Timestamp   time.Time
}

type IfNameAnnotator struct {
	name    string
	querier ClickhouseReader
	logger  *slog.Logger
	cache   map[string]string // key is a composite key of exporterIp:ifindex; val is ifname
	mu      sync.RWMutex
}

// NewIfNameAnnotator creates a new IfNameAnnotator with the given ClickhouseReader.
// The querier is used to query the device_ifindex table for interface name mappings.
// Typically, pass a *sql.DB connected to ClickHouse.
func NewIfNameAnnotator(querier ClickhouseReader, logger *slog.Logger) *IfNameAnnotator {
	return &IfNameAnnotator{
		name:    "ifname annotator",
		querier: querier,
		logger:  logger,
		cache:   make(map[string]string),
	}
}

func (i *IfNameAnnotator) populateCache(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := i.querier.QueryContext(ctx, "SELECT * from device_ifindex FINAL;")
	if err != nil {
		return fmt.Errorf("error querying clickhouse: %v", err)
	}
	defer rows.Close()

	var cache = make(map[string]string)
	for rows.Next() {
		record := &IfIndexRecord{}
		if err := rows.Scan(&record.Pubkey, &record.IfIndex, &record.IPv4Address, &record.IfName, &record.Timestamp); err != nil {
			i.logger.Warn("error scanning ifindex record", "error", err)
			continue
		}
		if record.IPv4Address == nil || record.IfIndex == 0 || record.IfName == "" {
			continue
		}
		key := fmt.Sprintf("%s:%d", record.IPv4Address.String(), record.IfIndex)
		if _, ok := cache[key]; !ok {
			cache[key] = record.IfName
		}
	}
	if len(cache) == 0 {
		return fmt.Errorf("clickhouse returned no data; not updating cache")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.cache = cache
	return nil
}

// Init populates a local cache of per-device ifindexes to interface names.
// It starts a background goroutine that refreshes the cache every minute.
func (i *IfNameAnnotator) Init(ctx context.Context) error {
	if i.querier == nil {
		return fmt.Errorf("querier is required for ifname annotator")
	}
	if err := i.populateCache(ctx); err != nil {
		return fmt.Errorf("error populating initial ifname cache: %v", err)
	}
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		for {
			select {
			case <-ctx.Done():
				i.logger.Info("ifname annotator closing due to signal")
				return
			case <-ticker.C:
				if err := i.populateCache(ctx); err != nil {
					// TODO: add metric
					i.logger.Warn("error populating ifname cache", "error", err)
					continue
				}
			}
		}
	}()
	return nil
}

// Annotate maps a input/output ifindex to the human readable interface name based
// on what's stored in the "device_ifindex" clickhouse table.
func (i *IfNameAnnotator) Annotate(flow *FlowSample) error {
	if flow.SamplerAddress == nil {
		return fmt.Errorf("flow sample has no sampler address: %v", flow)
	}

	annotate := func(samplerAddr string, ifindex int) string {
		key := fmt.Sprintf("%s:%d", samplerAddr, ifindex)
		i.mu.RLock()
		defer i.mu.RUnlock()
		if val, ok := i.cache[key]; ok {
			return val
		}
		return ""
	}

	if flow.InputIfIndex != 0 {
		flow.InputInterface = annotate(flow.SamplerAddress.String(), flow.InputIfIndex)
	}

	if flow.OutputIfIndex != 0 {
		flow.OutputInterface = annotate(flow.SamplerAddress.String(), flow.OutputIfIndex)
	}
	return nil
}

func (i *IfNameAnnotator) String() string {
	return i.name
}
