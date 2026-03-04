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
	Config             Config
	Locations          []Location
	Exchanges          []Exchange
	Contributors       []Contributor
	Tenants            []Tenant
	Devices            []Device
	Links              []Link
	Users              []User
	MulticastGroups    []MulticastGroup
	ProgramConfig      ProgramConfig
	ResourceExtensions []ResourceExtension
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
	tenants := []Tenant{}
	devices := []Device{}
	links := []Link{}
	users := []User{}
	multicastGroups := []MulticastGroup{}
	programConfig := ProgramConfig{}
	resourceExtensions := []ResourceExtension{}

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
		case byte(TenantType):
			var tenant Tenant
			DeserializeTenant(reader, &tenant)
			tenant.PubKey = element.Pubkey
			tenants = append(tenants, tenant)
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
		case byte(ResourceExtensionType):
			var resourceExtension ResourceExtension
			DeserializeResourceExtension(reader, &resourceExtension)
			resourceExtension.PubKey = element.Pubkey
			resourceExtensions = append(resourceExtensions, resourceExtension)
		}
	}

	return &ProgramData{
		Config:             config,
		Locations:          locations,
		Exchanges:          exchanges,
		Contributors:       contributors,
		Tenants:            tenants,
		Devices:            devices,
		Links:              links,
		Users:              users,
		MulticastGroups:    multicastGroups,
		ProgramConfig:      programConfig,
		ResourceExtensions: resourceExtensions,
	}, errs
}

// GetMulticastPublisherBlockResourceExtension fetches the global MulticastPublisherBlock resource extension.
// Returns nil if the account doesn't exist yet.
func (c *Client) GetMulticastPublisherBlockResourceExtension(ctx context.Context) (*ResourceExtension, error) {
	pda, _, err := GetMulticastPublisherBlockPDA(c.programID)
	if err != nil {
		return nil, fmt.Errorf("failed to derive MulticastPublisherBlock PDA: %w", err)
	}

	// Fetch the account data
	accountInfo, err := c.rpc.GetAccountInfo(ctx, pda)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch MulticastPublisherBlock account: %w", err)
	}

	if accountInfo == nil || accountInfo.Value == nil {
		// Account doesn't exist yet (not initialized)
		return nil, nil
	}

	data := accountInfo.Value.Data.GetBinary()
	if len(data) == 0 {
		return nil, nil
	}

	reader := NewByteReader(data)
	var ext ResourceExtension
	DeserializeResourceExtension(reader, &ext)
	ext.PubKey = pda

	return &ext, nil
}

type ProgramDataProvider interface {
	GetProgramData(ctx context.Context) (*ProgramData, error)
	ProgramID() solana.PublicKey
}
