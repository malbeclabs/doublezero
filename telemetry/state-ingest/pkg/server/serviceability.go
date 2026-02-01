package server

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jonboulle/clockwork"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/mr-tron/base58"
)

type ServiceabilityView struct {
	log      *slog.Logger
	clock    clockwork.Clock
	interval time.Duration
	rpc      ServiceabilityRPC

	mu      sync.RWMutex
	devices map[string]serviceability.Device

	ready atomic.Bool
}

func NewServiceabilityView(log *slog.Logger, clock clockwork.Clock, interval time.Duration, rpc ServiceabilityRPC) *ServiceabilityView {
	v := &ServiceabilityView{
		log:      log,
		clock:    clock,
		interval: interval,
		rpc:      rpc,
		devices:  make(map[string]serviceability.Device),
	}
	v.ready.Store(false)
	return v
}

func (v *ServiceabilityView) Ready() bool {
	return v.ready.Load()
}

func (v *ServiceabilityView) GetDevice(devicePK string) (serviceability.Device, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	d, ok := v.devices[devicePK]
	return d, ok
}

func (v *ServiceabilityView) Run(ctx context.Context) error {
	if err := v.Refresh(ctx); err != nil {
		v.log.Error("serviceability: initial refresh failed", "error", err)
	}
	ticker := v.clock.NewTicker(v.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.Chan():
			if err := v.Refresh(ctx); err != nil {
				v.log.Error("serviceability: refresh failed", "error", err)
			}
		}
	}
}

func (v *ServiceabilityView) Refresh(ctx context.Context) error {
	pd, err := v.rpc.GetProgramData(ctx)
	if err != nil {
		return err
	}

	m := make(map[string]serviceability.Device, len(pd.Devices))
	for _, d := range pd.Devices {
		m[base58.Encode(d.PubKey[:])] = d
	}

	v.mu.Lock()
	v.devices = m
	v.mu.Unlock()

	v.ready.Store(true)
	return nil
}
