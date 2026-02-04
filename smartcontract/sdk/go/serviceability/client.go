package serviceability

import (
	"context"
	"fmt"

	"github.com/gagliardetto/solana-go"
)

type Client struct {
	rpc       RPCClient
	programID solana.PublicKey
}

type ProgramData struct {
	Config          Config
	Locations       []Location
	Exchanges       []Exchange
	Contributors    []Contributor
	Devices         []Device
	Links           []Link
	Users           []User
	MulticastGroups []MulticastGroup
	ProgramConfig   ProgramConfig
}

func New(rpc RPCClient, programID solana.PublicKey) *Client {
	return &Client{rpc: rpc, programID: programID}
}

func (c *Client) ProgramID() solana.PublicKey {
	return c.programID
}

func (c *Client) GetProgramData(ctx context.Context) (*ProgramData, error) {
	out, err := c.rpc.GetProgramAccounts(ctx, c.programID)
	if err != nil {
		return nil, err
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("GetProgramAccounts returned empty result for program %s", c.programID)
	}

	// We need to re-init these fields to prevent appending if this client is reused
	// and Load() is called multiple times.
	config := Config{}
	locations := []Location{}
	exchanges := []Exchange{}
	contributors := []Contributor{}
	devices := []Device{}
	links := []Link{}
	users := []User{}
	multicastGroups := []MulticastGroup{}
	programConfig := ProgramConfig{}

	var errs error
	for _, element := range out {

		var data []byte = element.Account.Data.GetBinary()
		if len(data) == 0 {
			continue
		}
		reader := NewByteReader(data)

		switch account_type := data[0]; account_type {
		case byte(ConfigType):
			DeserializeConfig(reader, &config)
			config.PubKey = element.Pubkey
		case byte(LocationType):
			var location Location
			DeserializeLocation(reader, &location)
			location.PubKey = element.Pubkey
			locations = append(locations, location)
		case byte(ExchangeType):
			var exchange Exchange
			DeserializeExchange(reader, &exchange)
			exchange.PubKey = element.Pubkey
			exchanges = append(exchanges, exchange)
		case byte(ContributorType):
			var contributor Contributor
			DeserializeContributor(reader, &contributor)
			contributor.PubKey = element.Pubkey
			contributors = append(contributors, contributor)
		case byte(DeviceType):
			var device Device
			DeserializeDevice(reader, &device)
			device.PubKey = element.Pubkey
			devices = append(devices, device)
		case byte(LinkType):
			var link Link
			DeserializeLink(reader, &link)
			link.PubKey = element.Pubkey
			links = append(links, link)
		case byte(UserType):
			var user User
			DeserializeUser(reader, &user)
			user.PubKey = element.Pubkey
			users = append(users, user)
		case byte(MulticastGroupType):
			var multicastgroup MulticastGroup
			DeserializeMulticastGroup(reader, &multicastgroup)
			multicastgroup.PubKey = element.Pubkey
			multicastGroups = append(multicastGroups, multicastgroup)
		case byte(ProgramConfigType):
			DeserializeProgramConfig(reader, &programConfig)
		}
	}

	return &ProgramData{
		Config:          config,
		Locations:       locations,
		Exchanges:       exchanges,
		Contributors:    contributors,
		Devices:         devices,
		Links:           links,
		Users:           users,
		MulticastGroups: multicastGroups,
		ProgramConfig:   programConfig,
	}, errs
}

type ProgramDataProvider interface {
	GetProgramData(ctx context.Context) (*ProgramData, error)
}
