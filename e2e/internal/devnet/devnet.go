package devnet

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/client"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
)

const (
	defaultContainerNanoCPUs = 1_000_000_000      // 1 core
	defaultContainerMemory   = 1024 * 1024 * 1024 // 1GB

	labelsKeyDomain = "dz.malbeclabs.com"
)

type DevnetSpec struct {
	DeployID   string
	WorkingDir string

	// ExtraLabels are used to identify the resources created by the devnet.
	// These are added to the resources created by the devnet, in addition to default labels based
	// on the deployID.
	ExtraLabels map[string]string

	CYOANetworkSpec CYOANetworkSpec

	Ledger     LedgerSpec
	Manager    ManagerSpec
	Controller ControllerSpec
	Activator  ActivatorSpec
	Devices    []DeviceSpec
	Clients    []ClientSpec
}

type Devnet struct {
	Spec DevnetSpec

	log               *slog.Logger
	subnetAllocator   *docker.SubnetAllocator
	dockerClient      *client.Client
	labels            map[string]string
	mu                sync.RWMutex
	onchainWriteMutex sync.Mutex

	DefaultNetwork *DefaultNetwork
	CYOANetwork    *CYOANetwork

	Ledger     *Ledger
	Manager    *Manager
	Controller *Controller
	Activator  *Activator
	Devices    []*Device
	Clients    []*Client
}

func (s *DevnetSpec) Validate() error {
	if s.DeployID == "" {
		return fmt.Errorf("deployID is required")
	}

	if s.WorkingDir == "" {
		return fmt.Errorf("workingDir is required")
	}

	if err := s.CYOANetworkSpec.Validate(); err != nil {
		return fmt.Errorf("cyoa-network: %w", err)
	}

	if err := s.Ledger.Validate(); err != nil {
		return fmt.Errorf("ledger: %w", err)
	}

	if err := s.Manager.Validate(); err != nil {
		return fmt.Errorf("manager: %w", err)
	}

	if err := s.Controller.Validate(); err != nil {
		return fmt.Errorf("controller: %w", err)
	}

	if err := s.Activator.Validate(); err != nil {
		return fmt.Errorf("activator: %w", err)
	}

	for _, device := range s.Devices {
		if err := device.Validate(s.CYOANetworkSpec); err != nil {
			return fmt.Errorf("device: %w", err)
		}
	}

	for _, client := range s.Clients {
		if err := client.Validate(s.CYOANetworkSpec); err != nil {
			return fmt.Errorf("client: %w", err)
		}
	}

	return nil
}

func New(spec DevnetSpec, logger *slog.Logger, dockerClient *client.Client, subnetAllocator *docker.SubnetAllocator) (*Devnet, error) {
	// Configure the logger.
	log := logger.With("deployID", spec.DeployID)

	// Build the resource labels used to identify resources.
	labels := make(map[string]string)
	maps.Copy(labels, spec.ExtraLabels)
	labels[labelsKeyDomain] = "true"
	labels[labelsKeyDomain+"/type"] = "devnet"
	labels[labelsKeyDomain+"/deploy-id"] = spec.DeployID

	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate devnet spec: %w", err)
	}

	dn := &Devnet{
		log:             log,
		dockerClient:    dockerClient,
		subnetAllocator: subnetAllocator,

		Spec: spec,
	}

	// NOTE: The devnet and log fields need to be set before calling Start on the sub-components,
	// which will then fill in the rest of the fields.
	dn.DefaultNetwork = &DefaultNetwork{
		dn:  dn,
		log: log.With("component", "default-network"),
	}
	dn.CYOANetwork = &CYOANetwork{
		dn:  dn,
		log: log.With("component", "cyoa-network"),
	}
	dn.Ledger = &Ledger{
		dn:  dn,
		log: log.With("component", "ledger"),
	}
	dn.Manager = &Manager{
		dn:  dn,
		log: log.With("component", "manager"),
	}
	dn.Controller = &Controller{
		dn:  dn,
		log: log.With("component", "controller"),
	}
	dn.Activator = &Activator{
		dn:  dn,
		log: log.With("component", "activator"),
	}
	dn.Devices = []*Device{}
	dn.Clients = []*Client{}

	return dn, nil
}

func (d *Devnet) Close() {}

func (d *Devnet) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.log.Info("==> Starting devnet", "deployID", d.Spec.DeployID, "workingDir", d.Spec.WorkingDir)
	start := time.Now()

	// Create the default network.
	if err := d.DefaultNetwork.Create(ctx); err != nil {
		return fmt.Errorf("failed to create default network: %w", err)
	}

	// Start the ledger.
	if err := d.Ledger.Start(ctx); err != nil {
		return fmt.Errorf("failed to start ledger: %w", err)
	}

	// Start the manager.
	if err := d.Manager.Start(ctx); err != nil {
		return fmt.Errorf("failed to start manager: %w", err)
	}

	// Initialize the smart contract.
	if !d.Spec.Manager.NoInitSmartContract {
		if err := d.Manager.InitSmartContract(ctx); err != nil {
			return fmt.Errorf("failed to initialize smart contract: %w", err)
		}
	}

	// Start the controller.
	if err := d.Controller.Start(ctx); err != nil {
		return fmt.Errorf("failed to start controller: %w", err)
	}

	// Start the activator.
	if err := d.Activator.Start(ctx); err != nil {
		return fmt.Errorf("failed to start activator: %w", err)
	}

	// Create the CYOA network.
	if err := d.CYOANetwork.Create(ctx); err != nil {
		return fmt.Errorf("failed to create CYOA network: %w", err)
	}

	// Connect the controller to the device CYOA network.
	if err := d.Controller.ConnectToCYOANetwork(ctx); err != nil {
		return fmt.Errorf("failed to connect controller to device CYOA network: %w", err)
	}

	// We don't support starting with devices yet.
	// The AddDevice method can be used to add devices after the devnet is started.
	if len(d.Spec.Devices) > 0 {
		return fmt.Errorf("starting with devices is not supported yet")
	}

	// We don't support starting with clients yet.
	// The AddClient method can be used to add clients after the devnet is started.
	if len(d.Spec.Clients) > 0 {
		return fmt.Errorf("starting with clients is not supported yet")
	}

	d.log.Info("--> Devnet started", "duration", time.Since(start))
	return nil
}

func (d *Devnet) AddDevice(ctx context.Context, spec DeviceSpec) (int, error) {
	if err := spec.Validate(d.Spec.CYOANetworkSpec); err != nil {
		return 0, fmt.Errorf("failed to validate device spec: %w", err)
	}

	// We want to be able to add/start devices in parallel, so we need to use a closure to
	// to avoid locking the devnet mutex for the entire duration of the AddDevice method.
	device, deviceIndex := func() (*Device, int) {
		d.mu.Lock()
		defer d.mu.Unlock()

		deviceIndex := len(d.Devices)
		d.Spec.Devices = append(d.Spec.Devices, spec)

		// NOTE: The devnet, log, and index fields need to be set before calling Start, which will
		// then fill in the rest of the fields.
		device := &Device{
			dn:    d,
			log:   d.log.With("component", "device", "index", deviceIndex),
			index: deviceIndex,
		}
		d.Devices = append(d.Devices, device)
		return device, deviceIndex
	}()

	err := device.Start(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to start device: %w", err)
	}

	return deviceIndex, nil
}

func (d *Devnet) AddClient(ctx context.Context, spec ClientSpec) (int, error) {
	if err := spec.Validate(d.Spec.CYOANetworkSpec); err != nil {
		return 0, fmt.Errorf("failed to validate client spec: %w", err)
	}

	// We want to be able to add/start clients in parallel, so we need to use a closure to
	// to avoid locking the devnet mutex for the entire duration of the AddClient method.
	client, clientIndex := func() (*Client, int) {
		d.mu.Lock()
		defer d.mu.Unlock()

		clientIndex := len(d.Clients)
		d.Spec.Clients = append(d.Spec.Clients, spec)

		// NOTE: The devnet, log, and index fields need to be set before calling Start, which will
		// then fill in the rest of the fields.
		client := &Client{
			dn:    d,
			log:   d.log.With("component", "client", "index", clientIndex),
			index: clientIndex,
		}
		d.Clients = append(d.Clients, client)

		return client, clientIndex
	}()

	err := client.Start(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to start client: %w", err)
	}

	return clientIndex, nil
}

var (
	ErrDevicePubkeyNotFoundOnchain = errors.New("device pubkey not found onchain")
)

func (d *Devnet) CreateDeviceOnchain(ctx context.Context, deviceCode string, location string, exchange string, publicIP string, prefixes []string) error {
	d.onchainWriteMutex.Lock()
	defer d.onchainWriteMutex.Unlock()

	_, err := d.Manager.Exec(ctx, []string{"doublezero", "device", "create", "--code", deviceCode, "--location", location, "--exchange", exchange, "--public-ip", publicIP, "--dz-prefixes", strings.Join(prefixes, ",")})
	if err != nil {
		return fmt.Errorf("failed to create device onchain: %w", err)
	}
	return nil
}

func (d *Devnet) GetDevicePubkeyOnchain(ctx context.Context, deviceCode string) (string, error) {
	output, err := d.Manager.Exec(ctx, []string{"bash", "-c", "doublezero device get --code " + deviceCode})
	if err != nil {
		return "", fmt.Errorf("failed to get device pubkey onchain: %w", err)
	}

	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "account: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "account: ")), nil
		}
	}

	return "", ErrDevicePubkeyNotFoundOnchain
}

func (d *Devnet) waitContainerHealthy(ctx context.Context, containerID string, timeout time.Duration, delay time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(delay)
	defer ticker.Stop()
	for {
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("timeout waiting for container to be healthy")
		case <-ticker.C:
			inspect, err := d.dockerClient.ContainerInspect(waitCtx, containerID)
			if err != nil {
				return fmt.Errorf("failed to inspect container: %w", err)
			}
			if inspect.State.Health.Status == "healthy" {
				return nil
			}
			d.log.Debug("--> Waiting for container to be healthy", "container", shortContainerID(containerID), "status", inspect.State.Health.Status)
		}
	}
}

func shortContainerID(id string) string {
	return id[:12]
}
