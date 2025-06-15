package devnet

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/network"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/solana"
	"github.com/testcontainers/testcontainers-go"
)

type ClientSpec struct {
	ContainerImage string
	KeypairPath    string
	CYOANetworkIP  string
}

func (s *ClientSpec) Validate() error {
	if s.ContainerImage == "" {
		return fmt.Errorf("containerImage is required")
	}

	if s.KeypairPath == "" {
		return fmt.Errorf("keypairPath is required")
	}

	if s.CYOANetworkIP == "" {
		return fmt.Errorf("cyoaNetworkIP is required")
	}

	return nil
}

type Client struct {
	dn  *Devnet
	log *slog.Logger

	index int

	ContainerID string
	Pubkey      string
}

func (c *Client) Spec() *ClientSpec {
	return &c.dn.Spec.Clients[c.index]
}

func (c *Client) Start(ctx context.Context) error {
	spec := c.Spec()
	c.log.Info("==> Starting client", "image", spec.ContainerImage, "cyoaNetworkIP", spec.CYOANetworkIP)

	// Get the client's public address.
	clientPubkey, err := solana.PublicAddressFromKeypair(spec.KeypairPath)
	if err != nil {
		return fmt.Errorf("failed to get client public address: %w", err)
	}

	// Start the client container.
	req := testcontainers.ContainerRequest{
		Image: spec.ContainerImage,
		Name:  c.dn.Spec.DeployID + "-client-" + strconv.Itoa(c.index),
		Env: map[string]string{
			"DZ_LEDGER_URL": c.dn.Ledger.InternalURL,
			"DZ_LEDGER_WS":  c.dn.Ledger.InternalWSURL,
			"DZ_PROGRAM_ID": c.dn.Ledger.ProgramID,
		},
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      spec.KeypairPath,
				ContainerFilePath: "/root/.config/doublezero/id.json",
			},
			{
				HostFilePath:      spec.KeypairPath,
				ContainerFilePath: "/root/.config/solana/id.json",
			},
		},
		Networks: []string{
			c.dn.DefaultNetwork.Name,
			c.dn.CYOANetwork.Name,
		},
		EndpointSettingsModifier: func(m map[string]*network.EndpointSettings) {
			if m[c.dn.CYOANetwork.Name] == nil {
				m[c.dn.CYOANetwork.Name] = &network.EndpointSettings{}
			}
			m[c.dn.CYOANetwork.Name].IPAddress = spec.CYOANetworkIP
			m[c.dn.CYOANetwork.Name].IPAMConfig = &network.EndpointIPAMConfig{
				IPv4Address: spec.CYOANetworkIP,
			}
		},
		Privileged: true,
		Labels:     c.dn.labels,
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return fmt.Errorf("failed to start client: %w", err)
	}

	c.ContainerID = shortContainerID(container.GetContainerID())
	c.Pubkey = clientPubkey

	// Fund the client account via airdrop.
	// Retry a couple times to avoid the observed intermittent failures, even on the first airdrop request.
	funded := false
	var output []byte
	for range 3 {
		c.log.Info("--> Funding client account", "clientPubkey", clientPubkey)
		output, err = c.Exec(ctx, []string{"solana", "airdrop", "10", clientPubkey}, docker.NoPrintOnError())
		if err != nil {
			if strings.Contains(string(output), "rate limit") {
				c.log.Info("--> Solana airdrop request failed with rate limit message, retrying")
				time.Sleep(1 * time.Second)
				continue
			}
			fmt.Println(string(output))
			return fmt.Errorf("failed to fund client account: %w", err)
		}
		funded = true
		break
	}
	if !funded {
		fmt.Println(string(output))
		return fmt.Errorf("failed to fund client account after 3 attempts")
	}

	c.log.Info("--> Client started", "container", c.ContainerID, "pubkey", c.Pubkey)
	return nil
}

func (c *Client) Exec(ctx context.Context, command []string, options ...docker.ExecOption) ([]byte, error) {
	c.log.Debug("--> Executing command", "command", command)
	output, err := docker.Exec(ctx, c.dn.dockerClient, c.ContainerID, command, options...)
	if err != nil {
		// NOTE: We return the output here because it can contain useful information on error.
		return output, fmt.Errorf("failed to execute command from client: %w", err)
	}
	return output, nil
}

func (c *Client) ExecReturnJSONList(ctx context.Context, command []string, options ...docker.ExecOption) ([]map[string]any, error) {
	list, err := docker.ExecReturnJSONList(ctx, c.dn.dockerClient, c.ContainerID, command, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute command from client: %w", err)
	}

	return list, nil
}
