//go:build qa

package qa

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/mr-tron/base58"
)

type ProvisioningTest struct {
	log            *slog.Logger
	networkConfig  *config.NetworkConfig
	env            string
	bmHost         string
	infraPath      string
	serviceability *serviceability.Client
}

type DeviceInfo struct {
	Pubkey          string
	Code            string
	ContributorCode string
	LocationCode    string
	ExchangeCode    string
	PublicIP        string
	DzPrefixes      []string
	MgmtVrf         string
	MaxUsers        int
	UsersCount      int
	Status          string
	Health          string
	DeviceType      string
	DesiredStatus   string
}

type DeviceConfig struct {
	Code            string
	ContributorCode string
	LocationCode    string
	ExchangeCode    string
	PublicIP        string
	DzPrefixes      []string
	MgmtVrf         string
	MaxUsers        int
	DeviceType      string
}

type LinkInfo struct {
	Pubkey          string
	Code            string
	ContributorCode string
	SideACode       string
	SideAIfaceName  string
	SideZCode       string
	SideZIfaceName  string
	LinkType        string
	Bandwidth       uint64
	Mtu             uint32
	DelayMs         uint64
	JitterMs        uint64
	DesiredStatus   string
}

func NewProvisioningTest(ctx context.Context, log *slog.Logger, networkConfig *config.NetworkConfig, env, bmHost string) (*ProvisioningTest, error) {
	serviceabilityClient := serviceability.New(rpc.New(networkConfig.LedgerPublicRPCURL), networkConfig.ServiceabilityProgramID)

	infraPath := os.Getenv("INFRA_REPO_PATH")
	if infraPath == "" {
		infraPath = "../infra"
	}

	return &ProvisioningTest{
		log:            log,
		networkConfig:  networkConfig,
		env:            env,
		bmHost:         bmHost,
		infraPath:      infraPath,
		serviceability: serviceabilityClient,
	}, nil
}

func (p *ProvisioningTest) runCLI(ctx context.Context, args ...string) ([]byte, error) {
	// Build SSH command: ssh <bm-host> doublezero <args...>
	sshArgs := []string{p.bmHost, "doublezero"}
	sshArgs = append(sshArgs, args...)

	p.log.Debug("Running CLI command via SSH", "host", p.bmHost, "args", args)
	cmd := exec.CommandContext(ctx, "ssh", sshArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("CLI command failed: %w, output: %s", err, string(output))
	}
	return output, nil
}

func (p *ProvisioningTest) GetDeviceByCode(ctx context.Context, code string) (*DeviceInfo, error) {
	data, err := getProgramDataWithRetry(ctx, p.serviceability)
	if err != nil {
		return nil, fmt.Errorf("failed to get program data: %w", err)
	}

	// Build lookup maps for codes
	contributors := make(map[[32]uint8]string)
	for _, c := range data.Contributors {
		contributors[c.PubKey] = c.Code
	}
	locations := make(map[[32]uint8]string)
	for _, l := range data.Locations {
		locations[l.PubKey] = l.Code
	}
	exchanges := make(map[[32]uint8]string)
	for _, e := range data.Exchanges {
		exchanges[e.PubKey] = e.Code
	}

	for _, device := range data.Devices {
		if device.Code == code {
			// Convert DZ prefixes to strings
			var prefixes []string
			for _, prefix := range device.DzPrefixes {
				ip := net.IP(prefix[:4])
				maskLen := prefix[4]
				prefixes = append(prefixes, fmt.Sprintf("%s/%d", ip.String(), maskLen))
			}

			return &DeviceInfo{
				Pubkey:          base58.Encode(device.PubKey[:]),
				Code:            device.Code,
				ContributorCode: contributors[device.ContributorPubKey],
				LocationCode:    locations[device.LocationPubKey],
				ExchangeCode:    exchanges[device.ExchangePubKey],
				PublicIP:        net.IP(device.PublicIp[:]).String(),
				DzPrefixes:      prefixes,
				MgmtVrf:         device.MgmtVrf,
				MaxUsers:        int(device.MaxUsers),
				UsersCount:      int(device.UsersCount),
				Status:          device.Status.String(),
				Health:          device.DeviceHealth.String(),
				DeviceType:      device.DeviceType.String(),
				DesiredStatus:   device.DeviceDesiredStatus.String(),
			}, nil
		}
	}

	return nil, fmt.Errorf("device %q not found", code)
}

// CaptureDeviceConfig captures the device configuration for recreation.
func (p *ProvisioningTest) CaptureDeviceConfig(ctx context.Context, device *DeviceInfo) (*DeviceConfig, error) {
	return &DeviceConfig{
		Code:            device.Code,
		ContributorCode: device.ContributorCode,
		LocationCode:    device.LocationCode,
		ExchangeCode:    device.ExchangeCode,
		PublicIP:        device.PublicIP,
		DzPrefixes:      device.DzPrefixes,
		MgmtVrf:         device.MgmtVrf,
		MaxUsers:        device.MaxUsers,
		DeviceType:      device.DeviceType,
	}, nil
}

// GetLinksForDevice retrieves all links connected to a device.
func (p *ProvisioningTest) GetLinksForDevice(ctx context.Context, deviceCode string) ([]*LinkInfo, error) {
	data, err := getProgramDataWithRetry(ctx, p.serviceability)
	if err != nil {
		return nil, fmt.Errorf("failed to get program data: %w", err)
	}

	// Build device pubkey lookup
	devicePubkeys := make(map[string][32]uint8)
	deviceCodes := make(map[[32]uint8]string)
	for _, d := range data.Devices {
		devicePubkeys[d.Code] = d.PubKey
		deviceCodes[d.PubKey] = d.Code
	}

	// Build contributor lookup
	contributors := make(map[[32]uint8]string)
	for _, c := range data.Contributors {
		contributors[c.PubKey] = c.Code
	}

	targetPubkey, ok := devicePubkeys[deviceCode]
	if !ok {
		return nil, fmt.Errorf("device %q not found", deviceCode)
	}

	var links []*LinkInfo
	for _, link := range data.Links {
		// Check if this link connects to our device
		if link.SideAPubKey == targetPubkey || link.SideZPubKey == targetPubkey {
			links = append(links, &LinkInfo{
				Pubkey:          base58.Encode(link.PubKey[:]),
				Code:            link.Code,
				ContributorCode: contributors[link.ContributorPubKey],
				SideACode:       deviceCodes[link.SideAPubKey],
				SideAIfaceName:  link.SideAIfaceName,
				SideZCode:       deviceCodes[link.SideZPubKey],
				SideZIfaceName:  link.SideZIfaceName,
				LinkType:        link.LinkType.String(),
				Bandwidth:       link.Bandwidth,
				Mtu:             link.Mtu,
				DelayMs:         link.DelayNs / 1_000_000,
				JitterMs:        link.JitterNs / 1_000_000,
				DesiredStatus:   link.LinkDesiredStatus.String(),
			})
		}
	}

	return links, nil
}

// DeleteDevice deletes a device from the ledger.
func (p *ProvisioningTest) DeleteDevice(ctx context.Context, pubkey string) error {
	_, err := p.runCLI(ctx, "device", "delete", "--pubkey", pubkey)
	return err
}

// CreateDevice creates a device on the ledger and returns the new pubkey.
func (p *ProvisioningTest) CreateDevice(ctx context.Context, cfg *DeviceConfig) (string, error) {
	args := []string{
		"device", "create",
		"--code", cfg.Code,
		"--contributor", cfg.ContributorCode,
		"--location", cfg.LocationCode,
		"--exchange", cfg.ExchangeCode,
		"--public-ip", cfg.PublicIP,
		"--dz-prefixes", strings.Join(cfg.DzPrefixes, ","),
		"--mgmt-vrf", cfg.MgmtVrf,
	}

	if cfg.DeviceType != "" {
		args = append(args, "--device-type", strings.ToLower(cfg.DeviceType))
	}

	output, err := p.runCLI(ctx, args...)
	if err != nil {
		return "", err
	}

	// Get the new device to retrieve its pubkey
	device, err := p.GetDeviceByCode(ctx, cfg.Code)
	if err != nil {
		return "", fmt.Errorf("device created but failed to retrieve pubkey: %w, output: %s", err, string(output))
	}

	return device.Pubkey, nil
}

// UpdateDevice updates a device's max-users and desired-status.
func (p *ProvisioningTest) UpdateDevice(ctx context.Context, pubkey string, maxUsers int, desiredStatus string) error {
	args := []string{
		"device", "update",
		"--pubkey", pubkey,
		"--max-users", fmt.Sprintf("%d", maxUsers),
		"--desired-status", desiredStatus,
	}

	_, err := p.runCLI(ctx, args...)
	return err
}

// CreateInterface creates a device interface.
func (p *ProvisioningTest) CreateInterface(ctx context.Context, deviceCode, ifaceName, loopbackType string) error {
	args := []string{
		"device", "interface", "create",
		deviceCode, ifaceName,
		"-w", // wait for completion
	}

	if loopbackType != "" {
		args = append(args, "--loopback-type", loopbackType)
	}

	_, err := p.runCLI(ctx, args...)
	return err
}

// DeleteLink deletes a link from the ledger.
func (p *ProvisioningTest) DeleteLink(ctx context.Context, pubkey string) error {
	_, err := p.runCLI(ctx, "link", "delete", "--pubkey", pubkey)
	return err
}

// CreateLink creates a link on the ledger.
func (p *ProvisioningTest) CreateLink(ctx context.Context, link *LinkInfo) error {
	linkType := strings.ToLower(link.LinkType)
	if linkType == "" {
		linkType = "wan"
	}

	args := []string{
		"link", "create", linkType,
		"--code", link.Code,
		"--contributor", link.ContributorCode,
		"--side-a", link.SideACode,
		"--side-a-interface", link.SideAIfaceName,
		"--side-z", link.SideZCode,
		"--side-z-interface", link.SideZIfaceName,
		"--bandwidth", formatBandwidth(link.Bandwidth),
		"--mtu", fmt.Sprintf("%d", link.Mtu),
		"--delay-ms", fmt.Sprintf("%d", link.DelayMs),
		"--jitter-ms", fmt.Sprintf("%d", link.JitterMs),
		"-w", // wait for completion
	}

	if link.DesiredStatus != "" {
		args = append(args, "--desired-status", strings.ToLower(link.DesiredStatus))
	}

	_, err := p.runCLI(ctx, args...)
	return err
}

// RunAnsibleAgentRestart runs Ansible playbooks to restart both doublezero-agent
// and doublezero-telemetry daemons with the new pubkey.
func (p *ProvisioningTest) RunAnsibleAgentRestart(ctx context.Context, deviceCode, newPubkey string) error {
	inventoryPath := filepath.Join(p.infraPath, "ansible/inventory", p.env, "hosts.yml")

	// Prepare vault password file if available
	var vaultArgs []string
	vaultPass := os.Getenv("ANSIBLE_VAULT_PASSWORD")
	if vaultPass != "" {
		vaultFile, err := os.CreateTemp("", "vault-pass")
		if err != nil {
			return fmt.Errorf("failed to create vault pass file: %w", err)
		}
		defer os.Remove(vaultFile.Name())
		if _, err := vaultFile.WriteString(vaultPass); err != nil {
			return fmt.Errorf("failed to write vault pass: %w", err)
		}
		vaultFile.Close()
		vaultArgs = []string{"--vault-password-file", vaultFile.Name()}
	}

	// Run agents.yml to restart doublezero-agent
	p.log.Info("Running Ansible to restart doublezero-agent", "device", deviceCode, "pubkey", newPubkey)
	agentArgs := []string{
		"ansible-playbook",
		"-i", inventoryPath,
		"--limit", deviceCode,
		"-e", fmt.Sprintf("bm_host=%s", p.bmHost),
		"-e", fmt.Sprintf("env=%s", p.env),
		filepath.Join(p.infraPath, "ansible/playbooks/agents.yml"),
	}
	agentArgs = append(agentArgs, vaultArgs...)

	cmd := exec.CommandContext(ctx, agentArgs[0], agentArgs[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ansible agents.yml failed: %w, output: %s", err, string(output))
	}
	p.log.Debug("Ansible agents.yml output", "output", string(output))

	// Run device_telemetry_agent.yml to restart doublezero-telemetry
	p.log.Info("Running Ansible to restart doublezero-telemetry", "device", deviceCode, "pubkey", newPubkey)
	telemetryArgs := []string{
		"ansible-playbook",
		"-i", inventoryPath,
		"--limit", deviceCode,
		"-e", fmt.Sprintf("bm_host=%s", p.bmHost),
		"-e", fmt.Sprintf("env=%s", p.env),
		filepath.Join(p.infraPath, "ansible/playbooks/device_telemetry_agent.yml"),
	}
	telemetryArgs = append(telemetryArgs, vaultArgs...)

	cmd = exec.CommandContext(ctx, telemetryArgs[0], telemetryArgs[1:]...)
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ansible device_telemetry_agent.yml failed: %w, output: %s", err, string(output))
	}
	p.log.Debug("Ansible device_telemetry_agent.yml output", "output", string(output))

	return nil
}

func formatBandwidth(bps uint64) string {
	if bps >= 1_000_000_000 {
		return fmt.Sprintf("%d Gbps", bps/1_000_000_000)
	}
	if bps >= 1_000_000 {
		return fmt.Sprintf("%d Mbps", bps/1_000_000)
	}
	if bps >= 1_000 {
		return fmt.Sprintf("%d Kbps", bps/1_000)
	}
	return fmt.Sprintf("%d bps", bps)
}

type CLIDeviceOutput struct {
	Account         string   `json:"account"`
	Code            string   `json:"code"`
	ContributorCode string   `json:"contributor_code"`
	LocationCode    string   `json:"location_code"`
	ExchangeCode    string   `json:"exchange_code"`
	DeviceType      string   `json:"device_type"`
	PublicIP        string   `json:"public_ip"`
	DzPrefixes      []string `json:"dz_prefixes"`
	Users           int      `json:"users"`
	MaxUsers        int      `json:"max_users"`
	Status          string   `json:"status"`
	Health          string   `json:"health"`
	MgmtVrf         string   `json:"mgmt_vrf"`
	Owner           string   `json:"owner"`
}

func parseDeviceListJSON(output []byte) ([]CLIDeviceOutput, error) {
	var devices []CLIDeviceOutput
	if err := json.Unmarshal(output, &devices); err != nil {
		return nil, fmt.Errorf("failed to parse device list JSON: %w", err)
	}
	return devices, nil
}
