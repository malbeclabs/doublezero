package serviceability

import (
	"context"

	"github.com/davecgh/go-spew/spew"
	"github.com/gagliardetto/solana-go"
)

type Client struct {
	rpc       RPCClient
	programID solana.PublicKey

	Config          Config
	Locations       []Location
	Exchanges       []Exchange
	Devices         []Device
	Links           []Link
	Users           []User
	MulticastGroups []MulticastGroup
}

func New(rpc RPCClient, programID solana.PublicKey) *Client {
	return &Client{rpc: rpc, programID: programID}
}

func (c *Client) ProgramID() solana.PublicKey {
	return c.programID
}

func (c *Client) Load(ctx context.Context) error {
	out, err := c.rpc.GetProgramAccounts(ctx, c.programID)
	if err != nil {
		return err
	}

	// We need to re-init these fields to prevent appending if this client is reused
	// and Load() is called multiple times.
	c.Locations = []Location{}
	c.Exchanges = []Exchange{}
	c.Devices = []Device{}
	c.Links = []Link{}
	c.Users = []User{}
	c.MulticastGroups = []MulticastGroup{}

	var errs error
	for _, element := range out {

		var data []byte = element.Account.Data.GetBinary()
		if len(data) == 0 {
			continue
		}
		reader := NewByteReader(data)

		switch account_type := data[0]; account_type {
		case byte(ConfigType):
			DeserializeConfig(reader, &c.Config)
			c.Config.PubKey = element.Pubkey
		case byte(LocationType):
			var location Location
			DeserializeLocation(reader, &location)
			location.PubKey = element.Pubkey
			c.Locations = append(c.Locations, location)
		case byte(ExchangeType):
			var exchange Exchange
			DeserializeExchange(reader, &exchange)
			exchange.PubKey = element.Pubkey
			c.Exchanges = append(c.Exchanges, exchange)
		case byte(DeviceType):
			var device Device
			DeserializeDevice(reader, &device)
			device.PubKey = element.Pubkey
			c.Devices = append(c.Devices, device)
		case byte(LinkType):
			var link Link
			DeserializeLink(reader, &link)
			link.PubKey = element.Pubkey
			c.Links = append(c.Links, link)
		case byte(UserType):
			var user User
			DeserializeUser(reader, &user)
			user.PubKey = element.Pubkey
			c.Users = append(c.Users, user)
		case byte(MulticastGroupType):
			var multicastgroup MulticastGroup
			DeserializeMulticastGroup(reader, &multicastgroup)
			multicastgroup.PubKey = element.Pubkey
			c.MulticastGroups = append(c.MulticastGroups, multicastgroup)
		}
	}
	return errs
}

func (c *Client) GetDevices() []Device {
	return c.Devices
}

func (c *Client) GetLocations() []Location {
	return c.Locations
}

func (c *Client) GetExchanges() []Exchange {
	return c.Exchanges
}

func (c *Client) GetUsers() []User {
	return c.Users
}

func (c *Client) GetConfig() Config {
	return c.Config
}

func (c *Client) GetLinks() []Link {
	return c.Links
}

func (c *Client) GetMulticastGroups() []MulticastGroup {
	return c.MulticastGroups
}

func (c *Client) List() {
	for _, item := range c.Locations {
		spew.Dump(item)
	}
}
