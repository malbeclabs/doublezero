package telemetry

import (
	"context"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

type Client struct {
	rpcClient *rpc.Client
	programID solana.PublicKey
}

func New(rpcClient *rpc.Client, programID solana.PublicKey) *Client {
	return &Client{rpcClient: rpcClient, programID: programID}
}

// NewForEnv creates a client configured for the given environment.
// Valid environments: "mainnet-beta", "testnet", "devnet", "localnet".
func NewForEnv(env string) *Client {
	return New(NewRPCClient(LedgerRPCURLs[env]), ProgramIDs[env])
}

func NewMainnetBeta() *Client {
	return NewForEnv("mainnet-beta")
}

func NewTestnet() *Client {
	return NewForEnv("testnet")
}

func NewDevnet() *Client {
	return NewForEnv("devnet")
}

func NewLocalnet() *Client {
	return NewForEnv("localnet")
}

func (c *Client) GetDeviceLatencySamples(
	ctx context.Context,
	originDevicePK solana.PublicKey,
	targetDevicePK solana.PublicKey,
	linkPK solana.PublicKey,
	epoch uint64,
) (*DeviceLatencySamples, error) {
	addr, _, err := DeriveDeviceLatencySamplesPDA(c.programID, originDevicePK, targetDevicePK, linkPK, epoch)
	if err != nil {
		return nil, err
	}

	info, err := c.rpcClient.GetAccountInfo(ctx, addr)
	if err != nil {
		return nil, err
	}

	return DeserializeDeviceLatencySamples(info.Value.Data.GetBinary())
}

func (c *Client) GetInternetLatencySamples(
	ctx context.Context,
	collectorOraclePK solana.PublicKey,
	dataProviderName string,
	originLocationPK solana.PublicKey,
	targetLocationPK solana.PublicKey,
	epoch uint64,
) (*InternetLatencySamples, error) {
	addr, _, err := DeriveInternetLatencySamplesPDA(c.programID, collectorOraclePK, dataProviderName, originLocationPK, targetLocationPK, epoch)
	if err != nil {
		return nil, err
	}

	info, err := c.rpcClient.GetAccountInfo(ctx, addr)
	if err != nil {
		return nil, err
	}

	return DeserializeInternetLatencySamples(info.Value.Data.GetBinary())
}
