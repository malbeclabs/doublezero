package devnet

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/network"
	"github.com/malbeclabs/doublezero/e2e/internal/netutil"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/malbeclabs/doublezero/e2e/internal/solana"
	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
)

type Client struct {
	ID            string
	PubkeyAddress string
	KeypairPath   string
	Network       *testcontainers.DockerNetwork
	Container     testcontainers.Container
	IP            string

	log *slog.Logger
}

func (d *Devnet) StartClient(ctx context.Context, device *Device) (*Client, error) {
	d.log.Info("==> Starting client")

	// Generate a new client keypair.
	clientID := random.ShortID()
	clientKeypairPath := filepath.Join(d.config.WorkDir, "client-"+clientID+"-keypair.json")
	err := solana.GenerateKeypair(clientKeypairPath)
	if err != nil {
		return nil, fmt.Errorf("failed to generate client keypair: %w", err)
	}

	// Get the client's public address.
	clientPubkeyAddress, err := solana.PublicAddressFromKeypair(clientKeypairPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get client public address: %w", err)
	}

	// Construct an IP address for the client on the CYOA network subnet, the x.y.z.86 address.
	ip4, err := netutil.BuildIPInCIDR(device.CYOASubnetCIDR, 86)
	if err != nil {
		return nil, fmt.Errorf("failed to build client IP in CYOA network subnet: %w", err)
	}
	ip := ip4.String()
	d.log.Info("--> Client IP selected", "ip", ip)

	// Start the client container.
	req := testcontainers.ContainerRequest{
		Image: d.config.ClientImage,
		Name:  d.config.DeployID + "-client-" + clientID,
		Env: map[string]string{
			"DZ_LEDGER_URL": d.InternalLedgerURL,
			"DZ_LEDGER_WS":  d.InternalLedgerWSURL,
			"DZ_PROGRAM_ID": d.ProgramID,
		},
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      clientKeypairPath,
				ContainerFilePath: "/root/.config/doublezero/id.json",
			},
			{
				HostFilePath:      clientKeypairPath,
				ContainerFilePath: "/root/.config/solana/id.json",
			},
		},
		Networks: []string{
			d.defaultNetwork.Name,
			device.CYOANetwork.Name,
		},
		EndpointSettingsModifier: func(m map[string]*network.EndpointSettings) {
			if m[device.CYOANetwork.Name] == nil {
				m[device.CYOANetwork.Name] = &network.EndpointSettings{}
			}
			m[device.CYOANetwork.Name].IPAddress = ip
			m[device.CYOANetwork.Name].IPAMConfig = &network.EndpointIPAMConfig{
				IPv4Address: ip,
			}
		},
		Privileged: true,
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start client: %w", err)
	}

	// Get the client's IP address on the CYOA network.
	ip, err = d.getContainerIPOnNetwork(ctx, container, device.CYOANetwork.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get container IP on CYOA network: %w", err)
	}
	client := Client{
		ID:            clientID,
		PubkeyAddress: clientPubkeyAddress,
		KeypairPath:   clientKeypairPath,
		Container:     container,
		IP:            ip,

		log: d.log.With("clientID", clientID, "pubkeyAddress", clientPubkeyAddress, "container", shortContainerID(container.GetContainerID())),
	}
	d.clients[clientID] = client

	// Fund the client account via airdrop.
	// Retry a couple times to avoid the observed intermittent failures, even on the first airdrop request.
	funded := false
	for range 3 {
		output, err := client.Exec(ctx, []string{"solana", "airdrop", "10", clientPubkeyAddress})
		if err != nil {
			if strings.Contains(string(output), "rate limit") {
				d.log.Info("--> Solana airdrop request failed with rate limit message, retrying", "clientID", clientID, "container", shortContainerID(container.GetContainerID()), "ip", ip, "error", err)
				time.Sleep(1 * time.Second)
				continue
			}
			return nil, fmt.Errorf("failed to fund client account: %w", err)
		}
		funded = true
		break
	}
	if !funded {
		return nil, fmt.Errorf("failed to fund client account after 3 attempts")
	}

	d.log.Info("--> Client started", "clientID", clientID, "container", shortContainerID(container.GetContainerID()), "ip", ip)
	return &client, nil
}

// Exec executes a given command/script on the client container.
func (c *Client) Exec(ctx context.Context, command []string) ([]byte, error) {
	exitCode, execReader, err := c.Container.Exec(ctx, command, tcexec.Multiplexed())
	if err != nil {
		var buf []byte
		if execReader != nil {
			buf, _ = io.ReadAll(execReader)
			if buf != nil {
				fmt.Println(string(buf))
			}
		}
		return buf, fmt.Errorf("failed to execute command: %w", err)
	}
	if exitCode != 0 {
		var buf []byte
		if execReader != nil {
			buf, _ = io.ReadAll(execReader)
			if buf != nil {
				fmt.Println(string(buf))
			}
		}
		return buf, fmt.Errorf("command failed with exit code %d", exitCode)
	}

	buf, err := io.ReadAll(execReader)
	if err != nil {
		return nil, fmt.Errorf("error reading command output: %w", err)
	}
	return buf, nil
}

func (c *Client) ExecReturnJSONList(ctx context.Context, command []string) ([]map[string]any, error) {
	output, err := c.Exec(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("failed to execute command: %w", err)
	}

	links := []map[string]any{}
	err = json.Unmarshal(output, &links)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return links, nil
}
