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
	"time"

	"github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/e2e/internal/poll"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
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
	Interfaces      []InterfaceInfo
}

type InterfaceInfo struct {
	Name         string
	LoopbackType string
}

type DeviceSpec struct {
	Code            string
	ContributorCode string
	LocationCode    string
	ExchangeCode    string
	PublicIP        string
	DzPrefixes      []string
	MgmtVrf         string
	MaxUsers        int
	DeviceType      string
	Interfaces      []InterfaceInfo
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
		if device.Code == code && device.Status != serviceability.DeviceStatusDeleting {
			// Convert DZ prefixes to strings
			var prefixes []string
			for _, prefix := range device.DzPrefixes {
				ip := net.IP(prefix[:4])
				maskLen := prefix[4]
				prefixes = append(prefixes, fmt.Sprintf("%s/%d", ip.String(), maskLen))
			}

			var ifaces []InterfaceInfo
			for _, iface := range device.Interfaces {
				ifaces = append(ifaces, InterfaceInfo{
					Name:         iface.Name,
					LoopbackType: iface.LoopbackType.String(),
				})
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
				Interfaces:      ifaces,
			}, nil
		}
	}

	return nil, fmt.Errorf("device %q not found", code)
}

func (p *ProvisioningTest) GetDeviceSpec(ctx context.Context, device *DeviceInfo) (*DeviceSpec, error) {
	return &DeviceSpec{
		Code:            device.Code,
		ContributorCode: device.ContributorCode,
		LocationCode:    device.LocationCode,
		ExchangeCode:    device.ExchangeCode,
		PublicIP:        device.PublicIP,
		DzPrefixes:      device.DzPrefixes,
		MgmtVrf:         device.MgmtVrf,
		MaxUsers:        device.MaxUsers,
		DeviceType:      device.DeviceType,
		Interfaces:      device.Interfaces,
	}, nil
}

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

func (p *ProvisioningTest) DeleteInterface(ctx context.Context, deviceCode, ifaceName string) error {
	_, err := p.runCLI(ctx, "device", "interface", "delete", deviceCode, ifaceName)
	return err
}

// WaitForRefCountZero polls the device until its reference_count reaches zero.
// This is needed because link deletion is two-phase: the CLI sets status to Deleting,
// and the activator later calls CloseAccount which decrements the reference count.
func (p *ProvisioningTest) WaitForRefCountZero(ctx context.Context, deviceCode string) error {
	for {
		data, err := getProgramDataWithRetry(ctx, p.serviceability)
		if err != nil {
			return fmt.Errorf("failed to get program data: %w", err)
		}

		for _, device := range data.Devices {
			if device.Code == deviceCode {
				if device.ReferenceCount == 0 {
					return nil
				}
				p.log.Info("Waiting for reference count to reach zero",
					"device", deviceCode, "reference_count", device.ReferenceCount)
				break
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func (p *ProvisioningTest) DrainDevice(ctx context.Context, pubkey string) error {
	if _, err := p.runCLI(ctx, "device", "update", "--pubkey", pubkey, "--status", "drained"); err != nil {
		return err
	}
	return poll.Until(ctx, func() (bool, error) {
		data, err := getProgramDataWithRetry(ctx, p.serviceability)
		if err != nil {
			return false, err
		}
		for _, device := range data.Devices {
			if base58.Encode(device.PubKey[:]) == pubkey {
				if device.Status == serviceability.DeviceStatusActivated {
					p.log.Info("Waiting for device to drain", "pubkey", pubkey, "status", device.Status)
					return false, nil
				}
				return true, nil
			}
		}
		return true, nil // device not found, already gone
	}, 2*time.Minute, 5*time.Second)
}

func (p *ProvisioningTest) DeleteDevice(ctx context.Context, pubkey string) error {
	if err := p.DrainDevice(ctx, pubkey); err != nil {
		return fmt.Errorf("failed to drain device: %w", err)
	}
	_, err := p.runCLI(ctx, "device", "delete", "--pubkey", pubkey)
	return err
}

func (p *ProvisioningTest) CreateDevice(ctx context.Context, cfg *DeviceSpec) (string, error) {
	mgmtVrf := cfg.MgmtVrf
	if mgmtVrf == "" {
		mgmtVrf = "default"
	}

	args := []string{
		"device", "create",
		"--code", cfg.Code,
		"--contributor", cfg.ContributorCode,
		"--location", cfg.LocationCode,
		"--exchange", cfg.ExchangeCode,
		"--public-ip", cfg.PublicIP,
		"--dz-prefixes", strings.Join(cfg.DzPrefixes, ","),
		"--mgmt-vrf", mgmtVrf,
		"-w", // wait for confirmation
	}

	if cfg.DeviceType != "" {
		args = append(args, "--device-type", strings.ToLower(cfg.DeviceType))
	}

	output, err := p.runCLI(ctx, args...)
	if err != nil {
		return "", err
	}

	// Get the new device pubkey via CLI (Go SDK may return stale data)
	pubkey, err := p.getDevicePubkeyCLI(ctx, cfg.Code)
	if err != nil {
		return "", fmt.Errorf("device created but failed to retrieve pubkey: %w, output: %s", err, string(output))
	}

	return pubkey, nil
}

// getDevicePubkeyCLI retrieves the device pubkey via the CLI rather than the Go SDK,
// because the Go SDK may return stale data after a delete+recreate cycle.
func (p *ProvisioningTest) getDevicePubkeyCLI(ctx context.Context, code string) (string, error) {
	output, err := p.runCLI(ctx, "device", "get", "--code", code)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "account:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "account:")), nil
		}
	}
	return "", fmt.Errorf("could not parse account pubkey from device get output: %s", string(output))
}

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

func (p *ProvisioningTest) CreateInterface(ctx context.Context, deviceCode, ifaceName, loopbackType string) error {
	args := []string{
		"device", "interface", "create",
		deviceCode, ifaceName,
		"--bandwidth", "10G",
		"-w", // wait for completion
	}

	if loopbackType != "" {
		args = append(args, "--loopback-type", loopbackType)
	}

	_, err := p.runCLI(ctx, args...)
	return err
}

func (p *ProvisioningTest) DrainLink(ctx context.Context, pubkey string) error {
	if _, err := p.runCLI(ctx, "link", "update", "--pubkey", pubkey, "--status", "hard-drained"); err != nil {
		return err
	}
	return poll.Until(ctx, func() (bool, error) {
		data, err := getProgramDataWithRetry(ctx, p.serviceability)
		if err != nil {
			return false, err
		}
		for _, link := range data.Links {
			if base58.Encode(link.PubKey[:]) == pubkey {
				if link.Status == serviceability.LinkStatusActivated {
					p.log.Info("Waiting for link to drain", "pubkey", pubkey, "status", link.Status)
					return false, nil
				}
				return true, nil
			}
		}
		return true, nil // link not found, already gone
	}, 2*time.Minute, 5*time.Second)
}

func (p *ProvisioningTest) DeleteLink(ctx context.Context, pubkey string) error {
	if err := p.DrainLink(ctx, pubkey); err != nil {
		return fmt.Errorf("failed to drain link: %w", err)
	}
	_, err := p.runCLI(ctx, "link", "delete", "--pubkey", pubkey)
	return err
}

// CleanupStaleState cleans up state left over from a failed previous test run.
// If the device is healthy (activated + ready-for-users), no cleanup is needed —
// the health check and the test's own deletion logic will handle it.
// If the device is in any other state, it tears down all links, interfaces, and
// the device itself so the test can reprovision from scratch.
// Links already in Deleting state are skipped since the activator handles them.
// Returns the number of resources cleaned up.
func (p *ProvisioningTest) CleanupStaleState(ctx context.Context, deviceCode string) (int, error) {
	data, err := getProgramDataWithRetry(ctx, p.serviceability)
	if err != nil {
		return 0, fmt.Errorf("failed to get program data: %w", err)
	}

	var device *serviceability.Device
	for i, d := range data.Devices {
		if d.Code == deviceCode && d.Status != serviceability.DeviceStatusDeleting {
			device = &data.Devices[i]
			break
		}
	}
	if device == nil {
		return 0, nil
	}

	if device.Status == serviceability.DeviceStatusActivated &&
		device.DeviceHealth == serviceability.DeviceHealthReadyForUsers {
		return 0, nil
	}

	p.log.Info("Device is not healthy, cleaning up stale state", "code", deviceCode, "status", device.Status, "health", device.DeviceHealth)

	cleaned := 0

	for _, link := range data.Links {
		if link.SideAPubKey != device.PubKey && link.SideZPubKey != device.PubKey {
			continue
		}
		if link.Status == serviceability.LinkStatusDeleting {
			p.log.Info("Skipping link in Deleting state", "code", link.Code)
			continue
		}
		pubkey := base58.Encode(link.PubKey[:])
		p.log.Info("Deleting stale link", "code", link.Code, "pubkey", pubkey, "status", link.Status)
		if err := p.DeleteLink(ctx, pubkey); err != nil {
			p.log.Info("Failed to delete stale link", "code", link.Code, "error", err)
			continue
		}
		cleaned++
	}

	for _, iface := range device.Interfaces {
		p.log.Info("Deleting stale interface", "iface", iface.Name)
		if err := p.DeleteInterface(ctx, deviceCode, iface.Name); err != nil {
			p.log.Info("Failed to delete stale interface", "iface", iface.Name, "error", err)
		}
	}

	if err := p.WaitForRefCountZero(ctx, deviceCode); err != nil {
		return cleaned, fmt.Errorf("timed out waiting for ref count to reach zero during cleanup: %w", err)
	}

	pubkey := base58.Encode(device.PubKey[:])
	if err := p.DeleteDevice(ctx, pubkey); err != nil {
		return cleaned, fmt.Errorf("failed to delete device during cleanup: %w", err)
	}
	p.log.Info("Deleted device during cleanup", "code", deviceCode, "pubkey", pubkey)
	cleaned++

	return cleaned, nil
}

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

	// Resolve SSH key path
	sshKeyFile := os.Getenv("ANSIBLE_SSH_KEY_FILE")
	if sshKeyFile == "" {
		sshKeyFile = "~/.ssh/id_runner"
	}

	// Run agents.yml to restart doublezero-agent
	p.log.Info("Running Ansible to restart doublezero-agent", "device", deviceCode, "pubkey", newPubkey)
	agentArgs := []string{
		"ansible-playbook",
		"-i", inventoryPath,
		"--limit", deviceCode,
		"-e", fmt.Sprintf("bm_host=%s", p.bmHost),
		"-e", fmt.Sprintf("env=%s", p.env),
		"-e", fmt.Sprintf("ansible_ssh_private_key_file=%s", sshKeyFile),
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
		"-e", fmt.Sprintf("ansible_ssh_private_key_file=%s", sshKeyFile),
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
