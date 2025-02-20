package dzsdk

import (
	"context"
	"errors"

	"github.com/davecgh/go-spew/spew"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/near/borsh-go"
)

/************************************************************************************************************/
const PROGRAM_ID = "6i4v8m3i7W2qPGRonFi8mehN76SXUkDcpgk4tPQhE2J4"

/************************************************************************************************************/

type AccountFetcher interface {
	GetProgramAccounts(context.Context, solana.PublicKey) (rpc.GetProgramAccountsResult, error)
}

type Client struct {
	endpoint string
	pubkey   solana.PublicKey

	client AccountFetcher

	Config    Config
	Locations []Location
	Exchanges []Exchange
	Devices   []Device
	Tunnels   []Tunnel
	Users     []User
}

type Option func(*Client)

func New(Endpoint string, options ...Option) *Client {
	c := &Client{
		endpoint: Endpoint,
		pubkey:   solana.MustPublicKeyFromBase58(PROGRAM_ID),
		client:   rpc.New(Endpoint),
	}
	for _, o := range options {
		o(c)
	}
	return c
}

func WithProgramId(programId string) Option {
	return func(c *Client) {
		c.pubkey = solana.MustPublicKeyFromBase58(programId)
	}
}

func (e *Client) Load(ctx context.Context) error {
	out, err := e.client.GetProgramAccounts(ctx, e.pubkey)
	if err != nil {
		return err
	}

	// We need to re-init these fields to prevent appending if this client is reused
	// and Load() is called multiple times.
	e.Locations = []Location{}
	e.Exchanges = []Exchange{}
	e.Devices = []Device{}
	e.Tunnels = []Tunnel{}
	e.Users = []User{}

	var errs error
	for _, element := range out {

		var data []byte = element.Account.Data.GetBinary()
		if len(data) == 0 {
			continue
		}
		data = append(data, element.Pubkey.Bytes()...)

		//fmt.Printf("HEX: %x\n", data)

		switch account_type := data[0]; account_type {
		case byte(ConfigType):
			var config Config
			err := borsh.Deserialize(&config, data)
			if err != nil {
				errs = errors.Join(errs, err)
			}
			e.Config = config
		case byte(LocationType):
			var location Location
			err = borsh.Deserialize(&location, data)
			if err != nil {
				errs = errors.Join(errs, err)
			}
			e.Locations = append(e.Locations, location)
		case byte(ExchangeType):
			var exchange Exchange
			err = borsh.Deserialize(&exchange, data)
			if err != nil {
				errs = errors.Join(errs, err)
			}
			e.Exchanges = append(e.Exchanges, exchange)
		case byte(DeviceType):
			var device Device
			err = borsh.Deserialize(&device, data)
			if err != nil {
				errs = errors.Join(errs, err)
			}
			e.Devices = append(e.Devices, device)
		case byte(TunnelType):
			var tunnel Tunnel
			err = borsh.Deserialize(&tunnel, data)
			if err != nil {
				errs = errors.Join(errs, err)
			}
			e.Tunnels = append(e.Tunnels, tunnel)
		case byte(UserType):
			var user User
			err = borsh.Deserialize(&user, data)
			if err != nil {
				errs = errors.Join(errs, err)
			}
			e.Users = append(e.Users, user)
		}
	}
	return errs
}

func (s *Client) GetDevices() []Device {
	return s.Devices
}

func (s *Client) GetLocations() []Location {
	return s.Locations
}

func (s *Client) GetExchanges() []Exchange {
	return s.Exchanges
}

func (s *Client) GetUsers() []User {
	return s.Users
}

func (s *Client) GetConfig() Config {
	return s.Config
}

func (s *Client) GetTunnels() []Tunnel {
	return s.Tunnels
}

func (s *Client) List() {
	for _, item := range s.Locations {
		spew.Dump(item)
	}
}
