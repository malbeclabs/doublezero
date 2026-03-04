package serviceability

import (
	"context"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

// RPCClient is the minimal RPC interface needed by the client.
type RPCClient interface {
	GetProgramAccounts(ctx context.Context, publicKey solana.PublicKey) (rpc.GetProgramAccountsResult, error)
}

// ProgramData aggregates all deserialized serviceability accounts.
type ProgramData struct {
	GlobalState        *GlobalState
	GlobalConfig       *GlobalConfig
	Locations          []Location
	Exchanges          []Exchange
	Contributors       []Contributor
	Tenants            []Tenant
	Devices            []Device
	Links              []Link
	Users              []User
	MulticastGroups    []MulticastGroup
	ProgramConfig      *ProgramConfig
	AccessPasses       []AccessPass
	ResourceExtensions []ResourceExtension
}

// ProgramDataProvider is an interface for types that can provide program data.
type ProgramDataProvider interface {
	GetProgramData(ctx context.Context) (*ProgramData, error)
	ProgramID() solana.PublicKey
}

// Client provides read-only access to serviceability program accounts.
type Client struct {
	rpc       RPCClient
	programID solana.PublicKey
}

// New creates a new serviceability client.
func New(rpc RPCClient, programID solana.PublicKey) *Client {
	return &Client{rpc: rpc, programID: programID}
}

// NewForEnv creates a client configured for the given environment.
// Valid environments: "mainnet-beta", "testnet", "devnet", "localnet".
func NewForEnv(env string) *Client {
	return New(NewRPCClient(LedgerRPCURLs[env]), solana.MustPublicKeyFromBase58(ProgramIDs[env]))
}

// NewMainnetBeta creates a client configured for mainnet-beta.
func NewMainnetBeta() *Client {
	return NewForEnv("mainnet-beta")
}

// NewTestnet creates a client configured for testnet.
func NewTestnet() *Client {
	return NewForEnv("testnet")
}

// NewDevnet creates a client configured for devnet.
func NewDevnet() *Client {
	return NewForEnv("devnet")
}

// NewLocalnet creates a client configured for localnet.
func NewLocalnet() *Client {
	return NewForEnv("localnet")
}

// ProgramID returns the program ID this client is configured with.
func (c *Client) ProgramID() solana.PublicKey {
	return c.programID
}

// GetMulticastPublisherBlockResourceExtension fetches the global MulticastPublisherBlock resource extension.
func (c *Client) GetMulticastPublisherBlockResourceExtension(ctx context.Context) (*ResourceExtension, error) {
	pda, _, err := GetMulticastPublisherBlockPDA(c.programID)
	if err != nil {
		return nil, fmt.Errorf("failed to derive MulticastPublisherBlock PDA: %w", err)
	}

	// Fetch the account data
	rpcClient, ok := c.rpc.(interface {
		GetAccountInfo(context.Context, solana.PublicKey) (*rpc.GetAccountInfoResult, error)
	})
	if !ok {
		return nil, fmt.Errorf("RPC client does not support GetAccountInfo")
	}

	accountInfo, err := rpcClient.GetAccountInfo(ctx, pda)
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

// GetProgramData fetches all program accounts and deserializes them by type.
func (c *Client) GetProgramData(ctx context.Context) (*ProgramData, error) {
	out, err := c.rpc.GetProgramAccounts(ctx, c.programID)
	if err != nil {
		return nil, err
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("GetProgramAccounts returned empty result for program %s", c.programID)
	}

	pd := &ProgramData{
		Locations:          []Location{},
		Exchanges:          []Exchange{},
		Contributors:       []Contributor{},
		Tenants:            []Tenant{},
		Devices:            []Device{},
		Links:              []Link{},
		Users:              []User{},
		MulticastGroups:    []MulticastGroup{},
		AccessPasses:       []AccessPass{},
		ResourceExtensions: []ResourceExtension{},
	}

	for _, element := range out {
		data := element.Account.Data.GetBinary()
		if len(data) == 0 {
			continue
		}
		reader := NewByteReader(data)

		switch AccountType(data[0]) {
		case GlobalStateType:
			var gs GlobalState
			DeserializeGlobalState(reader, &gs)
			gs.PubKey = element.Pubkey
			pd.GlobalState = &gs
		case GlobalConfigType:
			var gc GlobalConfig
			DeserializeGlobalConfig(reader, &gc)
			gc.PubKey = element.Pubkey
			pd.GlobalConfig = &gc
		case LocationType:
			var loc Location
			DeserializeLocation(reader, &loc)
			loc.PubKey = element.Pubkey
			pd.Locations = append(pd.Locations, loc)
		case ExchangeType:
			var exch Exchange
			DeserializeExchange(reader, &exch)
			exch.PubKey = element.Pubkey
			pd.Exchanges = append(pd.Exchanges, exch)
		case DeviceType:
			var dev Device
			DeserializeDevice(reader, &dev)
			dev.PubKey = element.Pubkey
			pd.Devices = append(pd.Devices, dev)
		case LinkType:
			var link Link
			DeserializeLink(reader, &link)
			link.PubKey = element.Pubkey
			pd.Links = append(pd.Links, link)
		case UserType:
			var user User
			DeserializeUser(reader, &user)
			user.PubKey = element.Pubkey
			pd.Users = append(pd.Users, user)
		case MulticastGroupType:
			var mg MulticastGroup
			DeserializeMulticastGroup(reader, &mg)
			mg.PubKey = element.Pubkey
			pd.MulticastGroups = append(pd.MulticastGroups, mg)
		case ProgramConfigType:
			var pc ProgramConfig
			DeserializeProgramConfig(reader, &pc)
			pd.ProgramConfig = &pc
		case ContributorType:
			var contrib Contributor
			DeserializeContributor(reader, &contrib)
			contrib.PubKey = element.Pubkey
			pd.Contributors = append(pd.Contributors, contrib)
		case AccessPassType:
			var ap AccessPass
			DeserializeAccessPass(reader, &ap)
			ap.PubKey = element.Pubkey
			pd.AccessPasses = append(pd.AccessPasses, ap)
		case ResourceExtensionType:
			var ext ResourceExtension
			DeserializeResourceExtension(reader, &ext)
			ext.PubKey = element.Pubkey
			pd.ResourceExtensions = append(pd.ResourceExtensions, ext)
		case TenantType:
			var tenant Tenant
			DeserializeTenant(reader, &tenant)
			tenant.PubKey = element.Pubkey
			pd.Tenants = append(pd.Tenants, tenant)
		}
	}

	return pd, nil
}
