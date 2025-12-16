package enricher

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

type IfIndexRecord struct {
	Pubkey      string `ch:"pubkey"`
	IfIndex     uint64 `ch:"ifindex"`
	IPv4Address net.IP `ch:"ipv4_address"`
	IfName      string `ch:"ifname"`
	Timestamp   time.Time
}

type IfNameAnnotator struct {
	name   string
	chConn *sql.DB
	cache  map[string]string // key is a composite key of exporterIp:ifindex; val is ifname
	mu     sync.RWMutex
}

func NewIfNameAnnotator() *IfNameAnnotator {
	return &IfNameAnnotator{
		name:  "ifname annotator",
		cache: make(map[string]string),
	}
}

func (i *IfNameAnnotator) populateCache(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	row, err := i.chConn.QueryContext(ctx, "SELECT * from device_ifindex FINAL;")
	if err != nil {
		return fmt.Errorf("error querying clickhouse: %v", err)
	}
	var cache = make(map[string]string)
	for row.Next() {
		record := &IfIndexRecord{}
		if err := row.Scan(&record.Pubkey, &record.IfIndex, &record.IPv4Address, &record.IfName, &record.Timestamp); err != nil {
			log.Printf("An error while reading the data: %s\n", err)
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
func (i *IfNameAnnotator) Init(ctx context.Context, sql *sql.DB) error {
	i.chConn = sql
	if err := i.populateCache(ctx); err != nil {
		return fmt.Errorf("error populating initial ifname cache: %v", err)
	}
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		for {
			select {
			case <-ctx.Done():
				log.Println("ifname annotator closing due to signal")
				return
			case <-ticker.C:
				if err := i.populateCache(ctx); err != nil {
					// TODO: add metric
					log.Printf("error populating ifname cache: %v", err)
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
