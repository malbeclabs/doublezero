package telemetry

import (
	"context"
	"errors"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

var ErrAccountNotFound = errors.New("account not found")

type Client struct {
	rpcClient *rpc.Client
	programID solana.PublicKey
}

func New(rpcClient *rpc.Client, programID solana.PublicKey) *Client {
	return &Client{rpcClient: rpcClient, programID: programID}
}

func NewMainnetBeta() *Client {
	return New(NewRPCClient(LedgerRPCURLs["mainnet-beta"]), ProgramIDs["mainnet-beta"])
}

func NewTestnet() *Client {
	return New(NewRPCClient(LedgerRPCURLs["testnet"]), ProgramIDs["testnet"])
}

func NewDevnet() *Client {
	return New(NewRPCClient(LedgerRPCURLs["devnet"]), ProgramIDs["devnet"])
}

func NewLocalnet() *Client {
	return New(NewRPCClient(LedgerRPCURLs["localnet"]), ProgramIDs["localnet"])
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
	if info == nil || info.Value == nil {
		return nil, ErrAccountNotFound
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
	if info == nil || info.Value == nil {
		return nil, ErrAccountNotFound
	}

	return DeserializeInternetLatencySamples(info.Value.Data.GetBinary())
}
