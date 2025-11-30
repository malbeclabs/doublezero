package dzmon

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

type User struct {
	PubKey      solana.PublicKey
	ValidatorPK solana.PublicKey
	DZIP        net.IP
}

type ServiceabilityRPC interface {
	GetProgramData(ctx context.Context) (*serviceability.ProgramData, error)
}

type ServiceabilityViewConfig struct {
	Logger          *slog.Logger
	RPC             ServiceabilityRPC
	RefreshInterval time.Duration
}

func (c *ServiceabilityViewConfig) Validate() error {
	if c.Logger == nil {
		return errors.New("logger is required")
	}
	if c.RPC == nil {
		return errors.New("rpc is required")
	}
	if c.RefreshInterval <= 0 {
		return errors.New("refresh interval must be greater than 0")
	}
	return nil
}

type ServiceabilityView struct {
	log *slog.Logger
	cfg *ServiceabilityViewConfig

	ready chan struct{}
	once  sync.Once

	users map[solana.PublicKey]User

	mu sync.Mutex
}

func NewServiceabilityView(cfg *ServiceabilityViewConfig) (*ServiceabilityView, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &ServiceabilityView{
		log:   cfg.Logger,
		cfg:   cfg,
		ready: make(chan struct{}),
		once:  sync.Once{},
		users: make(map[solana.PublicKey]User),
	}, nil
}

func (v *ServiceabilityView) Users() map[solana.PublicKey]User {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make(map[solana.PublicKey]User, len(v.users))
	for pk, user := range v.users {
		out[pk] = user
	}
	return out
}

func (v *ServiceabilityView) Ready() <-chan struct{} { return v.ready }

func (v *ServiceabilityView) Start(ctx context.Context, cancel context.CancelFunc) {
	go func() {
		if err := v.Run(ctx); err != nil {
			v.log.Error("serviceability view failed to run", "error", err)
			cancel()
		}
	}()
	<-v.Ready()
}

func (v *ServiceabilityView) Run(ctx context.Context) error {
	v.log.Debug("serviceability view running", "refreshInterval", v.cfg.RefreshInterval)

	ticker := time.NewTicker(v.cfg.RefreshInterval)
	defer ticker.Stop()

	if err := v.refreshOnce(ctx); err != nil {
		v.log.Warn("failed to refresh serviceability view", "error", err)
	} else {
		v.once.Do(func() { close(v.ready) })
	}

	for {
		select {
		case <-ctx.Done():
			v.log.Debug("serviceability view done, stopping", "reason", ctx.Err())
			return nil
		case <-ticker.C:
			if err := v.refreshOnce(ctx); err != nil {
				v.log.Warn("failed to refresh serviceability view", "error", err)
			}
			v.once.Do(func() { close(v.ready) })
		}
	}
}

func (v *ServiceabilityView) refreshOnce(ctx context.Context) error {
	v.log.Debug("refreshing serviceability view", "currentUsers", len(v.users))

	data, err := v.cfg.RPC.GetProgramData(ctx)
	if err != nil {
		return err
	}

	users := make(map[solana.PublicKey]User)
	for _, user := range data.Users {
		users[user.PubKey] = User{
			PubKey:      user.PubKey,
			ValidatorPK: user.ValidatorPubKey,
			DZIP:        net.IP(user.DzIp[:]),
		}
	}

	v.mu.Lock()
	v.users = users
	v.mu.Unlock()

	return nil
}
