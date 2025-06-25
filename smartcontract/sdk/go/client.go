package dzsdk

import (
	"context"
	"errors"

	"github.com/davecgh/go-spew/spew"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

/************************************************************************************************************/
const URL_DOUBLEZERO = "https://doublezerolocalnet.rpcpool.com/f50e62d0-06e7-410e-867e-6873e358ed30"
const PROGRAM_ID_TESTNET = "DZtnuQ839pSaDMFG5q1ad2V95G82S5EC4RrB3Ndw2Heb"
const PROGRAM_ID_DEVNET = "DZdnB7bhR9azxLAUEH7ZVtW168wRdreiDKhi4McDfKZt"

/************************************************************************************************************/

// ErrNoPrivateKey is returned when a transaction signing operation is attempted without a configured private key
var ErrNoPrivateKey = errors.New("no private key configured")

type AccountFetcher interface {
	GetProgramAccounts(context.Context, solana.PublicKey) (rpc.GetProgramAccountsResult, error)
}

// RpcClient combines read and write capabilities
type RpcClient interface {
	AccountFetcher
	TransactionSender
}

type Client struct {
	endpoint string
	pubkey   solana.PublicKey

	client             AccountFetcher
	rpcClient          *rpc.Client
	telemetryProgramID solana.PublicKey
	signer             *solana.PrivateKey

	Config          Config
	Locations       []Location
	Exchanges       []Exchange
	Devices         []Device
	Links           []Link
	Users           []User
	MulticastGroups []MulticastGroup
}

type Option func(*Client)

func New(Endpoint string, options ...Option) *Client {
	rpcClient := rpc.New(Endpoint)
	c := &Client{
		endpoint:           Endpoint,
		pubkey:             solana.MustPublicKeyFromBase58(PROGRAM_ID_TESTNET),
		client:             rpcClient,
		rpcClient:          rpcClient,
		telemetryProgramID: solana.MustPublicKeyFromBase58(TELEMETRY_PROGRAM_ID_TESTNET),
	}
	for _, o := range options {
		o(c)
	}
	return c
}

// Configure the program ID to use for the client
// This is useful if you want to use a different program ID
// than the default one.
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
	e.Links = []Link{}
	e.Users = []User{}
	e.MulticastGroups = []MulticastGroup{}

	var errs error
	for _, element := range out {

		var data []byte = element.Account.Data.GetBinary()
		if len(data) == 0 {
			continue
		}
		reader := NewByteReader(data)

		switch account_type := data[0]; account_type {
		case byte(ConfigType):
			DeserializeConfig(reader, &e.Config)
			e.Config.PubKey = element.Pubkey
		case byte(LocationType):
			var location Location
			DeserializeLocation(reader, &location)
			location.PubKey = element.Pubkey
			e.Locations = append(e.Locations, location)
		case byte(ExchangeType):
			var exchange Exchange
			DeserializeExchange(reader, &exchange)
			exchange.PubKey = element.Pubkey
			e.Exchanges = append(e.Exchanges, exchange)
		case byte(DeviceType):
			var device Device
			DeserializeDevice(reader, &device)
			device.PubKey = element.Pubkey
			e.Devices = append(e.Devices, device)
		case byte(LinkType):
			var link Link
			DeserializeLink(reader, &link)
			link.PubKey = element.Pubkey
			e.Links = append(e.Links, link)
		case byte(UserType):
			var user User
			DeserializeUser(reader, &user)
			user.PubKey = element.Pubkey
			e.Users = append(e.Users, user)
		case byte(MulticastGroupType):
			var multicastgroup MulticastGroup
			DeserializeMulticastGroup(reader, &multicastgroup)
			multicastgroup.PubKey = element.Pubkey
			e.MulticastGroups = append(e.MulticastGroups, multicastgroup)
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

func (s *Client) GetLinks() []Link {
	return s.Links
}

func (s *Client) GetMulticastGroups() []MulticastGroup {
	return s.MulticastGroups
}

func (s *Client) List() {
	for _, item := range s.Locations {
		spew.Dump(item)
	}
}

// Configures the client with a private key for signing transactions
func WithSigner(privateKey solana.PrivateKey) Option {
	return func(c *Client) {
		c.signer = &privateKey
	}
}

// Configure the telemetry program ID
func WithTelemetryProgramID(programID string) Option {
	return func(c *Client) {
		c.telemetryProgramID = solana.MustPublicKeyFromBase58(programID)
	}
}

// Initializes a new DZ latency samples account
func (c *Client) InitializeDzLatencySamples(
	ctx context.Context,
	deviceAPk solana.PublicKey,
	deviceZPk solana.PublicKey,
	linkPk solana.PublicKey,
	epoch uint64,
	samplingIntervalMicroseconds uint64,
) (solana.Signature, error) {
	if c.signer == nil {
		return solana.Signature{}, ErrNoPrivateKey
	}

	args := &InitializeDzLatencySamplesArgs{
		DeviceAPk:                    deviceAPk,
		DeviceZPk:                    deviceZPk,
		LinkPk:                       linkPk,
		Epoch:                        epoch,
		SamplingIntervalMicroseconds: samplingIntervalMicroseconds,
	}

	// Build the instruction
	instruction, err := BuildInitializeDzLatencySamplesInstruction(
		c.pubkey, // serviceability program ID
		c.telemetryProgramID,
		c.signer.PublicKey(),
		args,
	)
	if err != nil {
		return solana.Signature{}, err
	}

	// Get latest blockhash
	blockhashResult, err := c.rpcClient.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return solana.Signature{}, err
	}

	// Build transaction
	tx, err := solana.NewTransaction(
		[]solana.Instruction{instruction},
		blockhashResult.Value.Blockhash,
		solana.TransactionPayer(c.signer.PublicKey()),
	)
	if err != nil {
		return solana.Signature{}, err
	}

	// Sign transaction
	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(c.signer.PublicKey()) {
			return c.signer
		}
		return nil
	})
	if err != nil {
		return solana.Signature{}, err
	}

	// Send transaction
	sig, err := c.rpcClient.SendTransaction(ctx, tx)
	if err != nil {
		return solana.Signature{}, err
	}

	return sig, nil
}

// Writes latency samples to an existing DZ latency samples account
func (c *Client) WriteDzLatencySamples(
	ctx context.Context,
	latencySamplesAccount solana.PublicKey,
	startTimestampMicroseconds uint64,
	samples []uint32,
) (solana.Signature, error) {
	if c.signer == nil {
		return solana.Signature{}, ErrNoPrivateKey
	}

	args := &WriteDzLatencySamplesArgs{
		StartTimestampMicroseconds: startTimestampMicroseconds,
		Samples:                    samples,
	}

	// Build the instruction
	instruction, err := BuildWriteDzLatencySamplesInstruction(
		c.telemetryProgramID,
		latencySamplesAccount,
		c.signer.PublicKey(),
		args,
	)
	if err != nil {
		return solana.Signature{}, err
	}

	// Get latest blockhash
	blockhashResult, err := c.rpcClient.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return solana.Signature{}, err
	}

	// Build transaction
	tx, err := solana.NewTransaction(
		[]solana.Instruction{instruction},
		blockhashResult.Value.Blockhash,
		solana.TransactionPayer(c.signer.PublicKey()),
	)
	if err != nil {
		return solana.Signature{}, err
	}

	// Sign transaction
	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(c.signer.PublicKey()) {
			return c.signer
		}
		return nil
	})
	if err != nil {
		return solana.Signature{}, err
	}

	// Send transaction
	sig, err := c.rpcClient.SendTransaction(ctx, tx)
	if err != nil {
		return solana.Signature{}, err
	}

	return sig, nil
}

// Returns the PDA for a DZ latency samples account
func (c *Client) GetDzLatencySamplesPDA(
	deviceAPk solana.PublicKey,
	deviceZPk solana.PublicKey,
	linkPk solana.PublicKey,
	epoch uint64,
) (solana.PublicKey, error) {
	pda, _, err := DeriveDzLatencySamplesPDA(
		c.telemetryProgramID,
		deviceAPk,
		deviceZPk,
		linkPk,
		epoch,
	)
	return pda, err
}
