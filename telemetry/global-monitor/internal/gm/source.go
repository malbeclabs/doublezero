package gm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"

	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/dz"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/netutil"
)

type Source struct {
	log *slog.Logger

	DZNetworkEnv string
	PublicIP     net.IP
	PublicIface  string
	DZIface      string
	Metro        string
	MetroName    string
	Host         string
	User         *dz.User
	Status       *dz.Status
}

type SourceConfig struct {
	Serviceability *dz.ServiceabilityView

	DZNetworkEnv string
	PublicIface  string
	PublicIP     net.IP
	DZIface      string
	Metro        string

	MetroNames map[string]string
}

func (cfg *SourceConfig) Validate() error {
	if cfg.Serviceability == nil {
		return errors.New("serviceability view is required")
	}
	if cfg.DZNetworkEnv == "" {
		return errors.New("dz network env is required")
	}
	if cfg.PublicIface == "" {
		return errors.New("public iface is required")
	}
	if cfg.Metro == "" {
		return errors.New("metro is required")
	}
	return nil
}

func NewSource(ctx context.Context, log *slog.Logger, cfg *SourceConfig) (*Source, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	host, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("failed to get hostname: %w", err)
	}
	if cfg.Metro == "" {
		return nil, errors.New("metro is required")
	}
	metroName := cfg.MetroNames[cfg.Metro]
	if metroName == "" {
		return nil, fmt.Errorf("missing metro name mapping for metro: %s", cfg.Metro)
	}
	publicIface := cfg.PublicIface
	if publicIface == "" {
		defaultInterface, err := netutil.DefaultInterface()
		if err != nil {
			return nil, fmt.Errorf("failed to get default interface: %w", err)
		}
		publicIface = defaultInterface.Name
		log.Info("using default interface as public internet interface", "interface", publicIface)
	}
	publicIP := cfg.PublicIP
	if publicIP == nil {
		_, publicIPStr, err := netutil.ResolveInterface(publicIface)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve public interface: %w", err)
		}
		publicIP = net.ParseIP(publicIPStr)
		if publicIP == nil {
			return nil, fmt.Errorf("failed to parse public IP: %s", publicIPStr)
		}
	}
	dzIface := cfg.DZIface
	var user *dz.User
	var status *dz.Status
	if dzIface != "" {
		// Get doublezero status.
		status, err := dz.GetStatus(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get status: %w", err)
		}
		if status.NetworkSlug != cfg.DZNetworkEnv {
			return nil, fmt.Errorf("invalid doublezero network: %s != %s", status.NetworkSlug, cfg.DZNetworkEnv)
		}

		// Get DoubleZero serviceability program data with users and devices.
		svcData, err := cfg.Serviceability.GetProgramData(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get program data: %w", err)
		}

		// Get user by looking for user with client IP matching source public IP.
		dzUser, ok := svcData.UsersByClientIP[publicIP.String()]
		if !ok {
			return nil, fmt.Errorf("user not found: %s", publicIP.String())
		}
		user = &dzUser

		// Check that we are connected to the expected device.
		if user.Device.Code != status.CurrentDeviceCode {
			return nil, fmt.Errorf("user device code does not match status device code: %s != %s", user.Device.Code, status.CurrentDeviceCode)
		}
	}

	return &Source{
		log: log,

		Host:         host,
		PublicIP:     publicIP,
		PublicIface:  publicIface,
		DZIface:      dzIface,
		Metro:        cfg.Metro,
		MetroName:    metroName,
		User:         user,
		Status:       status,
		DZNetworkEnv: cfg.DZNetworkEnv,
	}, nil
}
