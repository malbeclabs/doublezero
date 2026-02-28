package dz

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

type UserType string

const (
	UserTypeIBRL                UserType = "IBRL"
	UserTypeIBRLWithAllocatedIP UserType = "IBRLWithAllocatedIP"
	UserTypeEdgeFiltering       UserType = "EdgeFiltering"
	UserTypeMulticast           UserType = "Multicast"
)

type User struct {
	PubKey      solana.PublicKey
	ValidatorPK solana.PublicKey
	ClientIP    net.IP
	DZIP        net.IP
	Device      *Device
	UserType    UserType
}

type Device struct {
	PubKey   solana.PublicKey
	Code     string
	Name     string
	Exchange *Exchange
}

type Exchange struct {
	PubKey solana.PublicKey
	Code   string
	Name   string
}

type ServiceabilityProgramData struct {
	UsersByPK       map[solana.PublicKey]User
	UsersByDZIP     map[string]User
	UsersByClientIP map[string]User
	DevicesByPK     map[solana.PublicKey]*Device
	DevicesByCode   map[string]*Device
	ExchangesByPK   map[solana.PublicKey]*Exchange
}

type ServiceabilityRPC interface {
	GetProgramData(ctx context.Context) (*serviceability.ProgramData, error)
}

type ServiceabilityView struct {
	log *slog.Logger
	rpc ServiceabilityRPC
}

func mapUserType(ut serviceability.UserUserType) UserType {
	switch ut {
	case serviceability.UserTypeIBRL:
		return UserTypeIBRL
	case serviceability.UserTypeIBRLWithAllocatedIP:
		return UserTypeIBRLWithAllocatedIP
	case serviceability.UserTypeEdgeFiltering:
		return UserTypeEdgeFiltering
	case serviceability.UserTypeMulticast:
		return UserTypeMulticast
	default:
		return UserTypeIBRL
	}
}

func NewServiceabilityView(log *slog.Logger, rpc ServiceabilityRPC) (*ServiceabilityView, error) {
	if log == nil {
		return nil, fmt.Errorf("log is nil")
	}
	if rpc == nil {
		return nil, fmt.Errorf("rpc is nil")
	}
	return &ServiceabilityView{
		log: log,
		rpc: rpc,
	}, nil
}

func (v *ServiceabilityView) GetProgramData(ctx context.Context) (*ServiceabilityProgramData, error) {
	data, err := v.rpc.GetProgramData(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get program data: %w", err)
	}
	rpcDevicesByPK := make(map[solana.PublicKey]*serviceability.Device)
	for _, device := range data.Devices {
		rpcDevicesByPK[device.PubKey] = &device
	}
	rpcExchangesByPK := make(map[solana.PublicKey]*serviceability.Exchange)
	for _, exchange := range data.Exchanges {
		rpcExchangesByPK[exchange.PubKey] = &exchange
	}
	rpcUsersByPK := make(map[solana.PublicKey]serviceability.User)
	for _, user := range data.Users {
		rpcUsersByPK[user.PubKey] = user
	}

	exchangesByPK := make(map[solana.PublicKey]*Exchange)
	for _, exchange := range data.Exchanges {
		exchangesByPK[exchange.PubKey] = &Exchange{
			PubKey: exchange.PubKey,
			Code:   exchange.Code,
			Name:   exchange.Name,
		}
	}

	devicesByPK := make(map[solana.PublicKey]*Device)
	devicesByCode := make(map[string]*Device)
	for _, device := range data.Devices {
		devicesByPK[device.PubKey] = &Device{
			PubKey:   device.PubKey,
			Code:     device.Code,
			Exchange: exchangesByPK[device.ExchangePubKey],
		}
		devicesByCode[device.Code] = &Device{
			PubKey:   device.PubKey,
			Code:     device.Code,
			Exchange: exchangesByPK[device.ExchangePubKey],
		}
	}

	usersByPK := make(map[solana.PublicKey]User)
	usersByDZIP := make(map[string]User)
	usersByClientIP := make(map[string]User)
	for _, userFromRPC := range data.Users {
		user := User{
			PubKey:      userFromRPC.PubKey,
			ValidatorPK: userFromRPC.ValidatorPubKey,
			DZIP:        net.IP(userFromRPC.DzIp[:]),
			ClientIP:    net.IP(userFromRPC.ClientIp[:]),
			UserType:    mapUserType(userFromRPC.UserType),
		}
		device, ok := devicesByPK[userFromRPC.DevicePubKey]
		if ok {
			user.Device = device
		}
		usersByPK[userFromRPC.PubKey] = user
		usersByDZIP[user.DZIP.String()] = user
		// Prefer non-multicast users when multiple users share the same
		// client IP, matching the preference in parseStatus which picks the
		// non-multicast tunnel as the primary status.
		clientIPStr := user.ClientIP.String()
		if existing, ok := usersByClientIP[clientIPStr]; !ok || existing.UserType == UserTypeMulticast {
			usersByClientIP[clientIPStr] = user
		}
	}

	return &ServiceabilityProgramData{
		UsersByPK:       usersByPK,
		UsersByDZIP:     usersByDZIP,
		UsersByClientIP: usersByClientIP,
		DevicesByPK:     devicesByPK,
		DevicesByCode:   devicesByCode,
		ExchangesByPK:   exchangesByPK,
	}, nil
}
