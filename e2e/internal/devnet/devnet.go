package devnet

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	dockerfilters "github.com/docker/docker/api/types/filters"
	dockernetwork "github.com/docker/docker/api/types/network"
	dockervolume "github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/joho/godotenv"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/gomod"
	"github.com/malbeclabs/doublezero/e2e/internal/poll"
	"github.com/malbeclabs/doublezero/e2e/internal/solana"
)

const (
	defaultContainerNanoCPUs = 1_000_000_000      // 1 core
	defaultContainerMemory   = 1024 * 1024 * 1024 // 1GB

	LabelsKeyDomain          = "dz.malbeclabs.com"
	LabelsKeyType            = "type"
	LabelsKeyTypeValueDevnet = "devnet"
	LabelsKeyDeployID        = "deploy-id"
	LabelsFilterTypeDevnet   = LabelsKeyDomain + "/" + LabelsKeyType + "=" + LabelsKeyTypeValueDevnet

	containerDoublezeroKeypairPath = "/root/.config/doublezero/id.json"
	containerSolanaKeypairPath     = "/root/.config/solana/id.json"
)

var (
	ErrDevicePubkeyNotFoundOnchain = errors.New("device pubkey not found onchain")
	ErrDeviceNotFoundOnchain       = errors.New("device not found onchain")
)

type DevnetSpec struct {
	DeployID  string
	DeployDir string

	// ExtraLabels are used to identify the resources created by the devnet.
	// These are added to the resources created by the devnet, in addition to default labels based
	// on the deployID.
	ExtraLabels map[string]string

	CYOANetwork CYOANetworkSpec

	// Override the default device tunnel network onchain.
	DeviceTunnelNet    string
	Ledger             LedgerSpec
	Manager            ManagerSpec
	Funder             FunderSpec
	Controller         ControllerSpec
	Activator          ActivatorSpec
	DeviceHealthOracle DeviceHealthOracleSpec
	InfluxDB           InfluxDBSpec
	Prometheus         PrometheusSpec
	Devices            map[string]DeviceSpec
	Clients            map[string]ClientSpec
}

type Devnet struct {
	Spec DevnetSpec

	log               *slog.Logger
	workspaceDir      string
	subnetAllocator   *docker.SubnetAllocator
	dockerClient      *client.Client
	labels            map[string]string
	mu                sync.RWMutex
	onchainWriteMutex sync.Mutex

	ExternalHost string

	DefaultNetwork *DefaultNetwork
	CYOANetwork    *CYOANetwork

	Ledger             *Ledger
	Manager            *Manager
	Funder             *Funder
	Controller         *Controller
	Activator          *Activator
	DeviceHealthOracle *DeviceHealthOracle
	InfluxDB           *InfluxDB
	Prometheus         *Prometheus
	Devices            map[string]*Device
	Clients            map[string]*Client
}

func (s *DevnetSpec) Validate() error {
	if s.DeployID == "" {
		return fmt.Errorf("deployID is required")
	}

	// Validate the deploy directory.
	if s.DeployDir == "" {
		return fmt.Errorf("deployDir is required")
	}
	if !filepath.IsAbs(s.DeployDir) {
		return fmt.Errorf("deploy directory should be an absolute path: %s", s.DeployDir)
	}

	// Validate components.
	if err := s.CYOANetwork.Validate(); err != nil {
		return fmt.Errorf("cyoa-network: %w", err)
	}

	if err := s.Ledger.Validate(); err != nil {
		return fmt.Errorf("ledger: %w", err)
	}

	if err := s.Manager.Validate(); err != nil {
		return fmt.Errorf("manager: %w", err)
	}

	if err := s.Funder.Validate(); err != nil {
		return fmt.Errorf("funder: %w", err)
	}

	if err := s.Controller.Validate(s.CYOANetwork); err != nil {
		return fmt.Errorf("controller: %w", err)
	}

	if err := s.Activator.Validate(); err != nil {
		return fmt.Errorf("activator: %w", err)
	}

	if err := s.DeviceHealthOracle.Validate(); err != nil {
		return fmt.Errorf("device-health-oracle: %w", err)
	}

	if err := s.InfluxDB.Validate(); err != nil {
		return fmt.Errorf("influxdb: %w", err)
	}

	if err := s.Prometheus.Validate(); err != nil {
		return fmt.Errorf("prometheus: %w", err)
	}

	if s.Devices == nil {
		s.Devices = make(map[string]DeviceSpec)
	}
	for _, device := range s.Devices {
		if err := device.Validate(s.CYOANetwork); err != nil {
			return fmt.Errorf("device: %w", err)
		}
	}

	if s.Clients == nil {
		s.Clients = make(map[string]ClientSpec)
	}
	for _, client := range s.Clients {
		if err := client.Validate(s.CYOANetwork); err != nil {
			return fmt.Errorf("client: %w", err)
		}
	}

	if s.DeviceTunnelNet == "" {
		s.DeviceTunnelNet = "172.16.0.0/16"
	}
	return nil
}

func New(spec DevnetSpec, log *slog.Logger, dockerClient *client.Client, subnetAllocator *docker.SubnetAllocator) (*Devnet, error) {
	log = log.With("deployID", spec.DeployID)

	// Find the workspace directory.
	workspaceDir, err := WorkspaceDir()
	if err != nil {
		return nil, fmt.Errorf("failed to find go.mod: %w", err)
	}

	// Check that the deploy directory is configured and exists before we start creating resources.
	if spec.DeployDir == "" {
		return nil, fmt.Errorf("deploy directory is required")
	}
	if !filepath.IsAbs(spec.DeployDir) {
		return nil, fmt.Errorf("deploy directory should be an absolute path: %s", spec.DeployDir)
	}

	// Load environment variables file with docker images repos/names.
	if err := LoadContainerImagesEnvFile(log, workspaceDir); err != nil {
		return nil, fmt.Errorf("failed to load env file: %w", err)
	}

	// If the manager keypair path is not provided, generate a new keypair or use an existing one
	// in the deploy directory if it exists.
	if spec.Manager.ManagerKeypairPath == "" {
		managerKeypairPath := filepath.Join(spec.DeployDir, "manager-keypair.json")
		generated, err := generateKeypairIfNotExists(managerKeypairPath)
		if err != nil {
			return nil, fmt.Errorf("failed to generate manager keypair: %w", err)
		}
		spec.Manager.ManagerKeypairPath = managerKeypairPath
		if generated {
			log.Info("--> Generated manager keypair", "path", managerKeypairPath)
		} else {
			log.Info("--> Using existing manager keypair", "path", managerKeypairPath)
		}
	}

	// If the funder keypair path is not provided, use the manager keypair path.
	if spec.Funder.KeypairPath == "" {
		spec.Funder.KeypairPath = spec.Manager.ManagerKeypairPath
	}

	// If the serviceability program keypair path is not provided, generate a new keypair or use an
	// existing one in the deploy directory if it exists.
	if spec.Manager.ServiceabilityProgramKeypairPath == "" {
		serviceabilityProgramKeypairPath := filepath.Join(spec.DeployDir, "serviceability-program-keypair.json")
		generated, err := generateKeypairIfNotExists(serviceabilityProgramKeypairPath)
		if err != nil {
			return nil, fmt.Errorf("failed to generate serviceability program keypair: %w", err)
		}
		spec.Manager.ServiceabilityProgramKeypairPath = serviceabilityProgramKeypairPath
		if generated {
			log.Info("--> Generated serviceability program keypair", "path", serviceabilityProgramKeypairPath)
		} else {
			log.Info("--> Using existing serviceability program keypair", "path", serviceabilityProgramKeypairPath)
		}
	}

	// If the telemetry program keypair path is not provided, generate a new keypair or use an
	// existing one in the deploy directory if it exists.
	if spec.Manager.TelemetryProgramKeypairPath == "" {
		telemetryProgramKeypairPath := filepath.Join(spec.DeployDir, "telemetry-program-keypair.json")
		generated, err := generateKeypairIfNotExists(telemetryProgramKeypairPath)
		if err != nil {
			return nil, fmt.Errorf("failed to generate telemetry program keypair: %w", err)
		}
		spec.Manager.TelemetryProgramKeypairPath = telemetryProgramKeypairPath
		if generated {
			log.Info("--> Generated telemetry program keypair", "path", telemetryProgramKeypairPath)
		} else {
			log.Info("--> Using existing telemetry program keypair", "path", telemetryProgramKeypairPath)
		}

	}

	// Validate the spec.
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate spec: %w", err)
	}

	// Build the resource labels used to identify resources.
	labels := make(map[string]string)
	maps.Copy(labels, spec.ExtraLabels)
	labels[LabelsKeyDomain] = "true"
	labels[LabelsKeyDomain+"/"+LabelsKeyType] = LabelsKeyTypeValueDevnet
	labels[LabelsKeyDomain+"/"+LabelsKeyDeployID] = spec.DeployID

	// Create deploy directory if it doesn't exist.
	if err := os.MkdirAll(spec.DeployDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create deploy directory: %w", err)
	}

	dn := &Devnet{
		log:             log,
		workspaceDir:    workspaceDir,
		dockerClient:    dockerClient,
		subnetAllocator: subnetAllocator,
		labels:          labels,

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
	dn.Funder = &Funder{
		dn:  dn,
		log: log.With("component", "funder"),
	}
	dn.Controller = &Controller{
		dn:  dn,
		log: log.With("component", "controller"),
	}
	dn.Activator = &Activator{
		dn:  dn,
		log: log.With("component", "activator"),
	}
	dn.DeviceHealthOracle = &DeviceHealthOracle{
		dn:  dn,
		log: log.With("component", "device-health-oracle"),
	}
	dn.InfluxDB = &InfluxDB{
		dn:  dn,
		log: log.With("component", "influxdb"),
	}
	dn.Prometheus = &Prometheus{
		dn:  dn,
		log: log.With("component", "prometheus"),
	}
	dn.Devices = make(map[string]*Device)
	dn.Clients = make(map[string]*Client)

	// Set the external host.
	localhost := os.Getenv("DIND_LOCALHOST")
	if localhost == "" {
		localhost = "localhost"
	}
	dn.ExternalHost = localhost

	return dn, nil
}

func WorkspaceDir() (string, error) {
	return gomod.FindGoModDir(".", "github.com/malbeclabs/doublezero")
}

func LoadContainerImagesEnvFile(log *slog.Logger, workspaceDir string) error {
	envFilePath := filepath.Join(workspaceDir, dockerfilesEnvFilePathRelativeToWorkspace)
	if _, err := os.Stat(envFilePath); os.IsNotExist(err) {
		return fmt.Errorf("env file not found: %w", err)
	}
	if err := godotenv.Load(envFilePath); err != nil {
		return fmt.Errorf("failed to load env file: %w", err)
	}
	return nil
}

func (d *Devnet) Close() {}

type BuildConfig struct {
	Verbose bool
}

func (d *Devnet) Start(ctx context.Context, buildConfig *BuildConfig) error {
	if buildConfig != nil {
		if err := BuildContainerImages(ctx, d.log, d.workspaceDir, buildConfig.Verbose); err != nil {
			return fmt.Errorf("failed to build docker images: %w", err)
		}
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	d.log.Info("==> Starting devnet")
	start := time.Now()

	// Create the default network if it doesn't exist.
	if _, err := d.DefaultNetwork.CreateIfNotExists(ctx); err != nil {
		return fmt.Errorf("failed to create default network: %w", err)
	}

	// Start the ledger.
	if _, err := d.Ledger.StartIfNotRunning(ctx); err != nil {
		return fmt.Errorf("failed to start ledger: %w", err)
	}

	// Start the manager.
	if _, err := d.Manager.StartIfNotRunning(ctx); err != nil {
		return fmt.Errorf("failed to start manager: %w", err)
	}

	// Start the funder if it's not already running.
	if _, err := d.Funder.StartIfNotRunning(ctx); err != nil {
		return fmt.Errorf("failed to start funder: %w", err)
	}

	var wg sync.WaitGroup
	errChan := make(chan error, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()

		// Deploy the serviceability program if it's not already deployed.
		if _, err := d.DeployServiceabilityProgramIfNotDeployed(ctx); err != nil {
			errChan <- fmt.Errorf("failed to deploy serviceability program: %w", err)
		}

		// Initialize the smart contract.
		if _, err := d.InitSmartContractIfNotInitialized(ctx); err != nil {
			errChan <- fmt.Errorf("failed to initialize smart contract: %w", err)
		}
	}()

	go func() {
		defer wg.Done()

		// Deploy the telemetry program if it's not already deployed.
		if _, err := d.DeployTelemetryProgramIfNotDeployed(ctx); err != nil {
			errChan <- fmt.Errorf("failed to deploy telemetry program: %w", err)
		}
	}()

	wg.Wait()
	close(errChan)
	for err := range errChan {
		if err != nil {
			return fmt.Errorf("failed to deploy programs: %w", err)
		}
	}

	// Start the influxdb if it's not already running.
	if _, err := d.InfluxDB.StartIfNotRunning(ctx); err != nil {
		return fmt.Errorf("failed to start influxdb: %w", err)
	}

	// Start the prometheus if it's not already running.
	if _, err := d.Prometheus.StartIfNotRunning(ctx); err != nil {
		return fmt.Errorf("failed to start prometheus: %w", err)
	}

	// Start the controller if it's not already running.
	if _, err := d.Controller.StartIfNotRunning(ctx); err != nil {
		return fmt.Errorf("failed to start controller: %w", err)
	}

	// Start the activator if it's not already running.
	if _, err := d.Activator.StartIfNotRunning(ctx); err != nil {
		return fmt.Errorf("failed to start activator: %w", err)
	}

	// Start the device-health-oracle if it's not already running.
	if _, err := d.DeviceHealthOracle.StartIfNotRunning(ctx); err != nil {
		return fmt.Errorf("failed to start device-health-oracle: %w", err)
	}

	// Create the CYOA network if it doesn't exist.
	if _, err := d.CYOANetwork.CreateIfNotExists(ctx); err != nil {
		return fmt.Errorf("failed to create CYOA network: %w", err)
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

	d.log.Info("--> Devnet running", "duration", time.Since(start))
	return nil
}

func (d *Devnet) AddDevice(ctx context.Context, spec DeviceSpec) (*Device, error) {
	// If the telemetry keypair path is not provided, generate a new keypair or use an existing one
	// in the deploy directory if it exists.
	if spec.Telemetry.KeypairPath == "" {
		telemetryKeypairPath := filepath.Join(d.Spec.DeployDir, "device-"+spec.Code+"-telemetry-keypair.json")
		generated, err := generateKeypairIfNotExists(telemetryKeypairPath)
		if err != nil {
			return nil, fmt.Errorf("failed to generate telemetry keypair: %w", err)
		}
		spec.Telemetry.KeypairPath = telemetryKeypairPath
		if generated {
			d.log.Info("--> Generated telemetry keypair", "path", telemetryKeypairPath)
		} else {
			d.log.Info("--> Using existing telemetry keypair", "path", telemetryKeypairPath)
		}
	}

	if err := spec.Validate(d.Spec.CYOANetwork); err != nil {
		return nil, fmt.Errorf("failed to validate device spec: %w", err)
	}

	// We want to be able to add/start devices in parallel, so we need to use a closure to
	// to avoid locking the devnet mutex for the entire duration of the AddDevice method.
	device := func() *Device {
		d.mu.Lock()
		defer d.mu.Unlock()

		d.Spec.Devices[spec.Code] = spec

		// NOTE: The devnet, log, and index fields need to be set before calling Start, which will
		// then fill in the rest of the fields.
		device := &Device{
			dn:   d,
			log:  d.log.With("component", "device", "code", spec.Code),
			Spec: &spec,
		}
		d.Devices[spec.Code] = device
		return device
	}()

	_, err := device.StartIfNotRunning(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start device: %w", err)
	}

	return device, nil
}

func (d *Devnet) AddClient(ctx context.Context, spec ClientSpec) (*Client, error) {
	// If the client keypair path is not provided, generate a new keypair and write it to the deploy
	// directory.
	if spec.KeypairPath == "" {
		privKeyJSON, err := solana.GenerateKeypairJSON()
		if err != nil {
			return nil, fmt.Errorf("failed to generate client keypair: %w", err)
		}

		pubkey, err := solana.PubkeyFromKeypairJSON(privKeyJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to get client pubkey: %w", err)
		}

		keypairPath := filepath.Join(d.Spec.DeployDir, "client-"+pubkey, "keypair.json")
		err = os.MkdirAll(filepath.Dir(keypairPath), 0755)
		if err != nil {
			return nil, fmt.Errorf("failed to create client keypair directory: %w", err)
		}
		if err := os.WriteFile(keypairPath, privKeyJSON, 0644); err != nil {
			return nil, fmt.Errorf("failed to write client keypair: %w", err)
		}
		spec.KeypairPath = keypairPath
	}

	// Validate the spec.
	if err := spec.Validate(d.Spec.CYOANetwork); err != nil {
		return nil, fmt.Errorf("failed to validate client spec: %w", err)
	}

	// Read the client keypair.
	keypairJSON, err := os.ReadFile(spec.KeypairPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read client keypair: %w", err)
	}

	// Get the client's keypair pubkey.
	pubkey, err := solana.PubkeyFromKeypairJSON(keypairJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to get client pubkey: %w", err)
	}

	// We want to be able to add/start clients in parallel, so we need to use a closure to
	// to avoid locking the devnet mutex for the entire duration of the AddClient method.
	client := func() *Client {
		d.mu.Lock()
		defer d.mu.Unlock()

		d.Spec.Clients[pubkey] = spec

		// NOTE: The devnet, log, and index fields need to be set before calling Start, which will
		// then fill in the rest of the fields.
		client := &Client{
			dn:   d,
			log:  d.log.With("component", "client", "pubkey", pubkey),
			Spec: &spec,
		}
		d.Clients[pubkey] = client

		return client
	}()

	_, err = client.StartIfNotRunning(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start client: %w", err)
	}

	return client, nil
}

func (d *Devnet) Stop(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.log.Info("==> Stopping devnet", "deployID", d.Spec.DeployID)

	start := time.Now()

	// Find all containers with the deployID label.
	containers, err := d.dockerClient.ContainerList(ctx, dockercontainer.ListOptions{
		Filters: dockerfilters.NewArgs(dockerfilters.Arg("label", LabelsKeyDomain+"/"+LabelsKeyDeployID+"="+d.Spec.DeployID)),
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	// Stop containers in parallel.
	wg := sync.WaitGroup{}
	errChan := make(chan error, len(containers))
	for _, container := range containers {
		wg.Add(1)
		go func(container *dockercontainer.Summary) {
			defer wg.Done()
			d.log.Debug("==> Stopping container", "id", container.ID, "name", container.Names)
			err := d.dockerClient.ContainerStop(ctx, container.ID, dockercontainer.StopOptions{})
			if err != nil {
				d.log.Error("failed to stop container", "id", container.ID, "name", container.Names, "error", err)
				errChan <- err
				return
			}
			d.log.Debug("--> Container stopped", "id", container.ID, "name", container.Names)
		}(&container)
	}
	wg.Wait()
	close(errChan)
	for err := range errChan {
		if err != nil {
			return fmt.Errorf("failed to stop container: %w", err)
		}
	}
	d.log.Info("--> Devnet stopped", "duration", time.Since(start))

	return nil
}

func (d *Devnet) Destroy(ctx context.Context, all bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.log.Info("==> Destroying devnet", "deployID", d.Spec.DeployID, "all", all)

	start := time.Now()

	var filters dockerfilters.Args
	if all {
		filters = dockerfilters.NewArgs(dockerfilters.Arg("label", LabelsFilterTypeDevnet))
	} else {
		filters = dockerfilters.NewArgs(dockerfilters.Arg("label", LabelsKeyDomain+"/"+LabelsKeyDeployID+"="+d.Spec.DeployID))
	}

	// Remove all containers matching the labels.
	containers, err := d.dockerClient.ContainerList(ctx, dockercontainer.ListOptions{
		All:     true, // Include non-running containers.
		Filters: filters,
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}
	wg := sync.WaitGroup{}
	errChan := make(chan error, len(containers))
	for _, container := range containers {
		wg.Add(1)
		go func(container *dockercontainer.Summary) {
			defer wg.Done()
			d.log.Debug("==> Destroying container", "id", container.ID, "name", container.Names)
			err := d.dockerClient.ContainerRemove(ctx, container.ID, dockercontainer.RemoveOptions{
				Force: true,
			})
			if err != nil {
				d.log.Error("failed to destroy container", "id", container.ID, "name", container.Names, "error", err)
				errChan <- err
				return
			}
			d.log.Debug("--> Container destroyed", "id", container.ID, "name", container.Names)
		}(&container)
	}
	wg.Wait()
	close(errChan)
	for err := range errChan {
		if err != nil {
			return fmt.Errorf("failed to destroy container: %w", err)
		}
	}

	// Remove all networks matching the labels.
	networks, err := d.dockerClient.NetworkList(ctx, dockernetwork.ListOptions{
		Filters: filters,
	})
	if err != nil {
		return fmt.Errorf("failed to list networks: %w", err)
	}
	wg = sync.WaitGroup{}
	errChan = make(chan error, len(networks))
	for _, network := range networks {
		wg.Add(1)
		go func(network *dockernetwork.Summary) {
			defer wg.Done()
			d.log.Debug("==> Destroying network", "id", network.ID, "name", network.Name)
			err := d.dockerClient.NetworkRemove(ctx, network.ID)
			if err != nil {
				d.log.Error("failed to destroy network", "id", network.ID, "name", network.Name, "error", err)
				errChan <- err
				return
			}
			d.log.Debug("--> Network destroyed", "id", network.ID, "name", network.Name)
		}(&network)
	}
	wg.Wait()
	close(errChan)
	for err := range errChan {
		if err != nil {
			return fmt.Errorf("failed to destroy network: %w", err)
		}
	}

	// Remove all volumes matching the labels.
	volumes, err := d.dockerClient.VolumeList(ctx, dockervolume.ListOptions{
		Filters: dockerfilters.NewArgs(dockerfilters.Arg("label", LabelsKeyDomain+"/"+LabelsKeyDeployID+"="+d.Spec.DeployID)),
	})
	if err != nil {
		return fmt.Errorf("failed to list volumes: %w", err)
	}
	wg = sync.WaitGroup{}
	errChan = make(chan error, len(volumes.Volumes))
	for _, volume := range volumes.Volumes {
		wg.Add(1)
		go func(volume *dockervolume.Volume) {
			defer wg.Done()
			d.log.Debug("==> Destroying volume", "name", volume.Name)
			err := d.dockerClient.VolumeRemove(ctx, volume.Name, true)
			if err != nil {
				d.log.Error("failed to destroy volume", "name", volume.Name, "error", err)
				errChan <- err
				return
			}
			d.log.Debug("--> Volume destroyed", "name", volume.Name)
		}(volume)
	}
	wg.Wait()
	close(errChan)
	for err := range errChan {
		if err != nil {
			return fmt.Errorf("failed to destroy volume: %w", err)
		}
	}

	d.log.Info("--> Devnet destroyed", "duration", time.Since(start))

	return nil
}

func (d *Devnet) GetOrCreateDeviceOnchain(ctx context.Context, deviceCode string, location string, exchange string, metricsPublisherPK string, publicIP string, prefixes []string, mgmtVrf string) (string, error) {
	d.onchainWriteMutex.Lock()
	defer d.onchainWriteMutex.Unlock()

	deviceAddress, err := d.GetDevicePubkeyOnchain(ctx, deviceCode)
	if err != nil {
		if errors.Is(err, ErrDeviceNotFoundOnchain) {
			args := []string{"doublezero", "device", "create", "--contributor", "co01", "--code", deviceCode, "--location", location, "--exchange", exchange, "--public-ip", publicIP, "--dz-prefixes", strings.Join(prefixes, ","), "--mgmt-vrf", mgmtVrf}
			if metricsPublisherPK != "" {
				args = append(args, "--metrics-publisher", metricsPublisherPK)
			}
			_, err := d.Manager.Exec(ctx, args)
			if err != nil {
				return "", fmt.Errorf("failed to create device onchain: %w", err)
			}

			deviceAddress, err = d.GetDevicePubkeyOnchain(ctx, deviceCode)
			if err != nil {
				return "", fmt.Errorf("failed to get device agent pubkey onchain for device %s: %w", deviceCode, err)
			}

			_, err = d.Manager.Exec(ctx, []string{"doublezero", "device", "update", "--pubkey", deviceAddress, "--max-users", "128", "--desired-status", "activated"})
			if err != nil {
				return "", fmt.Errorf("failed to update device onchain: %w", err)
			}

			return deviceAddress, nil
		}

		return "", fmt.Errorf("failed to get device agent pubkey onchain for device %s: %w", deviceCode, err)
	}

	return deviceAddress, nil
}

func (d *Devnet) CreateDeviceOnchain(ctx context.Context, deviceCode string, location string, exchange string, publicIP string, prefixes []string, mgmtVrf string) error {
	d.onchainWriteMutex.Lock()
	defer d.onchainWriteMutex.Unlock()

	_, err := d.Manager.Exec(ctx, []string{"doublezero", "device", "create", "--code", deviceCode, "--contributor", "co01", "--location", location, "--exchange", exchange, "--public-ip", publicIP, "--dz-prefixes", strings.Join(prefixes, ","), "--mgmt-vrf", mgmtVrf}, docker.NoPrintOnError())
	if err != nil {
		return fmt.Errorf("failed to create device onchain: %w", err)
	}

	_, err = d.Manager.Exec(ctx, []string{"doublezero", "device", "update", "--pubkey", deviceCode, "--max-users", "128", "--desired-status", "activated"})
	if err != nil {
		return fmt.Errorf("failed to update device onchain: %w", err)
	}

	return nil
}

func (d *Devnet) GetDevicePubkeyOnchain(ctx context.Context, deviceCode string) (string, error) {
	output, err := d.Manager.Exec(ctx, []string{"bash", "-c", "doublezero device get --code " + deviceCode}, docker.NoPrintOnError())
	if err != nil {
		if strings.Contains(string(output), "not found") {
			return "", ErrDeviceNotFoundOnchain
		}
		fmt.Println(string(output))
		return "", fmt.Errorf("failed to get device pubkey onchain: %w", err)
	}

	for _, line := range strings.SplitAfter(string(output), "\n") {
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
	var lastInspectErr error
	for {
		select {
		case <-waitCtx.Done():
			if lastInspectErr != nil {
				return fmt.Errorf("timeout waiting for container to be healthy (last inspect error: %w)", lastInspectErr)
			}
			return fmt.Errorf("timeout waiting for container to be healthy")
		case <-ticker.C:
			inspect, err := d.dockerClient.ContainerInspect(waitCtx, containerID)
			if err != nil {
				lastInspectErr = err
				d.log.Debug("--> Retrying container inspect after error", "container", shortContainerID(containerID), "error", err)
				continue
			}
			lastInspectErr = nil
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

// generateKeypairIfNotExists generates and writes a new keypair to the keypair path if it does not exist.
func generateKeypairIfNotExists(keypairPath string) (bool, error) {
	if _, err := os.Stat(keypairPath); err != nil {
		if os.IsNotExist(err) {
			keypair, err := solana.GenerateKeypairJSON()
			if err != nil {
				return false, fmt.Errorf("failed to generate manager keypair: %w", err)
			}
			if err := os.WriteFile(keypairPath, keypair, 0644); err != nil {
				return false, fmt.Errorf("failed to write manager keypair: %w", err)
			}
			return true, nil
		} else {
			return false, fmt.Errorf("failed to stat manager keypair: %w", err)
		}
	}

	return false, nil
}

func (d *Devnet) CreateDeviceLoopbackInterface(ctx context.Context, deviceCode string, interfaceName string, loopbackType string) error {
	d.log.Info("==> Creating loopback interface for device", "code", deviceCode)
	d.onchainWriteMutex.Lock()
	defer d.onchainWriteMutex.Unlock()

	_, err := d.Manager.Exec(ctx, []string{"doublezero", "device", "interface", "create", deviceCode, interfaceName, "--loopback-type", loopbackType})
	if err != nil {
		return fmt.Errorf("failed to create loopback interface %s of type %s for device %s: %w", interfaceName, loopbackType, deviceCode, err)
	}

	return nil
}

func (d *Devnet) DeleteDeviceLoopbackInterface(ctx context.Context, deviceCode string, interfaceName string) error {
	d.log.Info("==> Deleting loopback interface for device", "code", deviceCode)
	d.onchainWriteMutex.Lock()
	defer d.onchainWriteMutex.Unlock()

	_, err := d.Manager.Exec(ctx, []string{"doublezero", "device", "interface", "delete", deviceCode, interfaceName})
	if err != nil {
		return fmt.Errorf("failed to delete loopback interface %s for device %s: %w", interfaceName, deviceCode, err)
	}

	return nil
}

func (d *Devnet) waitForContainerPortExposed(ctx context.Context, containerID string, port int, timeout time.Duration) (int, error) {
	loggedWait := false
	attempts := 0
	var container dockercontainer.InspectResponse
	var exposedPort int
	err := poll.Until(ctx, func() (bool, error) {
		attempts++
		var err error
		container, err = d.dockerClient.ContainerInspect(ctx, containerID)
		if err != nil {
			return false, fmt.Errorf("failed to inspect container: %w", err)
		}
		ports, ok := container.NetworkSettings.Ports[nat.Port(fmt.Sprintf("%d/tcp", port))]
		if !ok || len(ports) == 0 {
			if !loggedWait && attempts > 1 {
				d.log.Debug("--> Waiting for port to be exposed", "container", shortContainerID(container.ID), "timeout", timeout)
				loggedWait = true
			}
			return false, nil
		}
		exposedPort, err = strconv.Atoi(ports[0].HostPort)
		if err != nil {
			return false, fmt.Errorf("failed to get port: %w", err)
		}
		return true, nil
	}, timeout, 500*time.Millisecond)
	if err != nil {
		return 0, fmt.Errorf("failed to wait for port to be exposed: %w", err)
	}
	return exposedPort, nil
}
