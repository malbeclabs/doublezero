package devnet

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	dockerfilters "github.com/docker/docker/api/types/filters"
	dockernetwork "github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"github.com/malbeclabs/doublezero/e2e/internal/logging"
	"github.com/malbeclabs/doublezero/e2e/internal/netutil"
	"github.com/malbeclabs/doublezero/e2e/internal/solana"
	"github.com/testcontainers/testcontainers-go"
	tcwait "github.com/testcontainers/testcontainers-go/wait"
)

const (
	// ipVerifierInternalPort is the container port the proof endpoint listens on.
	ipVerifierInternalPort = 8080

	// defaultIPVerifierCYOANetworkIPHostID is the host offset the verifier takes on the CYOA
	// network. Kept well clear of the ranges devices (single digits) and clients (100+) use.
	defaultIPVerifierCYOANetworkIPHostID = 250
)

// IPVerifierSpec configures the RFC-27 IP ownership verification service container.
//
// The service must sit on the CYOA network, not only on the default network. It signs the source
// address it observes the request arrive from, and `connect` refuses a proof for any address other
// than the one it is provisioning — which for a local client is its CYOA address. Reached over the
// default network the observed address would be the client's default-network address instead, and
// every connect would fail on the mismatch.
type IPVerifierSpec struct {
	// Disabled leaves the verifier out of the deploy. The zero value runs it, so every devnet
	// exercises the same `connect` path production does: a proof is obtained and attached, and
	// the program validates it. Enforcement is separate — the require-ip-ownership-proof feature
	// flag stays clear, so a create with no proof is still accepted.
	Disabled       bool
	ContainerImage string
	// KeypairPath is the host path to the verifier keypair JSON. Generated into the deploy
	// directory when unset. Its pubkey is written to GlobalState.ip_verifier_authority_pk before
	// the container starts: the service reads the authority from the ledger at startup and exits
	// if it does not name its own key.
	KeypairPath string
	// CYOANetworkIPHostID is the offset into the host portion of the CYOA subnet.
	CYOANetworkIPHostID uint32
	// AuthorityRefreshSecs is how often the service re-reads
	// GlobalState.ip_verifier_authority_pk. Zero leaves the service default. Set it long to hold
	// the service on a stale authority across a rotation, which is how a test produces a proof
	// signed by a key the program no longer trusts.
	AuthorityRefreshSecs int
}

func (s *IPVerifierSpec) Validate(cyoaNetworkSpec CYOANetworkSpec) error {
	if s.Disabled {
		return nil
	}
	if s.ContainerImage == "" {
		s.ContainerImage = os.Getenv("DZ_IP_VERIFIER_IMAGE")
	}
	if s.CYOANetworkIPHostID == 0 {
		s.CYOANetworkIPHostID = defaultIPVerifierCYOANetworkIPHostID
	}
	maxHostID := uint32(1) << (32 - cyoaNetworkSpec.CIDRPrefix)
	if s.CYOANetworkIPHostID >= maxHostID {
		return fmt.Errorf("hostID %d is out of valid range (1 to %d)", s.CYOANetworkIPHostID, maxHostID-1)
	}
	if s.KeypairPath != "" && !filepath.IsAbs(s.KeypairPath) {
		return fmt.Errorf("keypair path must be an absolute path: %s", s.KeypairPath)
	}
	return nil
}

// IPVerifier manages the ip-verifier container.
type IPVerifier struct {
	dn  *Devnet
	log *slog.Logger

	ContainerID string
	// Pubkey is the verifier's signing key, and the value written to
	// GlobalState.ip_verifier_authority_pk.
	Pubkey string
	// CYOANetworkIP is the address clients reach the service on.
	CYOANetworkIP string
	// InternalURL is what a client's DZ_IP_VERIFIER_URL is set to.
	InternalURL string
}

func (v *IPVerifier) dockerContainerHostname() string {
	return "ip-verifier"
}

func (v *IPVerifier) dockerContainerName() string {
	return v.dn.Spec.DeployID + "-" + v.dockerContainerHostname()
}

func (v *IPVerifier) Exists(ctx context.Context) (bool, error) {
	containers, err := v.dn.dockerClient.ContainerList(ctx, dockercontainer.ListOptions{
		All:     true,
		Filters: dockerfilters.NewArgs(dockerfilters.Arg("name", v.dockerContainerName())),
	})
	if err != nil {
		return false, fmt.Errorf("failed to list containers: %w", err)
	}
	for _, container := range containers {
		if container.Names[0] == "/"+v.dockerContainerName() {
			return true, nil
		}
	}
	return false, nil
}

func (v *IPVerifier) StartIfNotRunning(ctx context.Context) (bool, error) {
	exists, err := v.Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if ip-verifier exists: %w", err)
	}
	if !exists {
		return false, v.Start(ctx)
	}

	container, err := v.dn.dockerClient.ContainerInspect(ctx, v.dockerContainerName())
	if err != nil {
		return false, fmt.Errorf("failed to inspect container: %w", err)
	}
	if !container.State.Running {
		if err := v.dn.dockerClient.ContainerStart(ctx, container.ID, dockercontainer.StartOptions{}); err != nil {
			return false, fmt.Errorf("failed to start ip-verifier: %w", err)
		}
	} else {
		v.log.Debug("--> IPVerifier already running", "container", shortContainerID(container.ID))
	}

	if err := v.setState(container.ID); err != nil {
		return false, fmt.Errorf("failed to set ip-verifier state: %w", err)
	}
	return !container.State.Running, nil
}

// Prepare reads the verifier keypair and derives the addresses the container will use, without
// starting it. The pubkey is needed onchain before the container starts, because the service
// treats an authority that is not its own key as a startup error.
func (v *IPVerifier) Prepare() error {
	keypairJSON, err := os.ReadFile(v.dn.Spec.IPVerifier.KeypairPath)
	if err != nil {
		return fmt.Errorf("failed to read ip-verifier keypair: %w", err)
	}
	pubkey, err := solana.PubkeyFromKeypairJSON(keypairJSON)
	if err != nil {
		return fmt.Errorf("failed to parse ip-verifier pubkey: %w", err)
	}
	v.Pubkey = pubkey

	cyoaIP, err := netutil.DeriveIPFromCIDR(v.dn.CYOANetwork.SubnetCIDR, v.dn.Spec.IPVerifier.CYOANetworkIPHostID)
	if err != nil {
		return fmt.Errorf("failed to derive CYOA network IP: %w", err)
	}
	v.CYOANetworkIP = cyoaIP.To4().String()
	v.InternalURL = fmt.Sprintf("http://%s:%d", v.CYOANetworkIP, ipVerifierInternalPort)
	return nil
}

func (v *IPVerifier) Start(ctx context.Context) error {
	v.log.Debug("==> Starting ip-verifier", "image", v.dn.Spec.IPVerifier.ContainerImage)

	if err := v.Prepare(); err != nil {
		return err
	}

	env := map[string]string{
		"DZ_IP_VERIFIER_ENV":         "local",
		"DZ_IP_VERIFIER_LEDGER_RPC":  v.dn.Ledger.InternalRPCURL,
		"DZ_IP_VERIFIER_KEYPAIR":     containerIPVerifierKeypairPath,
		"DZ_IP_VERIFIER_LISTEN_ADDR": fmt.Sprintf("0.0.0.0:%d", ipVerifierInternalPort),
		"DZ_IP_VERIFIER_LOG":         "doublezero_ip_verifier=debug",
		// No trusted proxies: clients reach the service directly, so the connection peer
		// address is signed and forwarded headers are ignored outright.
		//
		// The rate limit is raised well above the production default because a devnet has one
		// source address per client and a test can reconnect in a tight loop; the production
		// value would turn that into `rate_limited` refusals that have nothing to do with what
		// is being tested.
		"DZ_IP_VERIFIER_RATE_LIMIT_BURST":      "1000",
		"DZ_IP_VERIFIER_RATE_LIMIT_PER_MINUTE": "6000",
	}
	// A long refresh holds the service on the authority it read at startup, so a rotation after
	// it is up does not stop it signing. That is what lets a test hand the program a proof signed
	// by a key it no longer trusts.
	if v.dn.Spec.IPVerifier.AuthorityRefreshSecs > 0 {
		env["DZ_IP_VERIFIER_AUTHORITY_REFRESH_SECS"] = strconv.Itoa(v.dn.Spec.IPVerifier.AuthorityRefreshSecs)
	}

	req := testcontainers.ContainerRequest{
		Image: v.dn.Spec.IPVerifier.ContainerImage,
		Name:  v.dockerContainerName(),
		ConfigModifier: func(cfg *dockercontainer.Config) {
			cfg.Hostname = v.dockerContainerHostname()
		},
		ExposedPorts: []string{fmt.Sprintf("%d/tcp", ipVerifierInternalPort)},
		Env:          env,
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      v.dn.Spec.IPVerifier.KeypairPath,
				ContainerFilePath: containerIPVerifierKeypairPath,
			},
		},
		Networks: []string{
			v.dn.DefaultNetwork.Name,
			v.dn.CYOANetwork.Name,
		},
		NetworkAliases: map[string][]string{
			v.dn.DefaultNetwork.Name: {"ip-verifier"},
		},
		EndpointSettingsModifier: func(m map[string]*dockernetwork.EndpointSettings) {
			if m[v.dn.CYOANetwork.Name] == nil {
				m[v.dn.CYOANetwork.Name] = &dockernetwork.EndpointSettings{}
			}
			m[v.dn.CYOANetwork.Name].IPAddress = v.CYOANetworkIP
			m[v.dn.CYOANetwork.Name].IPAMConfig = &dockernetwork.EndpointIPAMConfig{
				IPv4Address: v.CYOANetworkIP,
			}
		},
		// /health is 200 only once the cached ledger epoch is fresh *and* the ledger names this
		// container's key as the verifier authority, so waiting on it proves both.
		WaitingFor: tcwait.ForHTTP("/health").
			WithPort(nat.Port(fmt.Sprintf("%d/tcp", ipVerifierInternalPort))).
			WithStartupTimeout(60 * time.Second).
			WithPollInterval(1 * time.Second),
		Resources: dockercontainer.Resources{
			NanoCPUs: defaultContainerNanoCPUs,
			Memory:   defaultContainerMemory,
		},
		Labels: v.dn.labels,
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           logging.NewTestcontainersAdapter(v.log),
	})
	if err != nil {
		return fmt.Errorf("failed to start ip-verifier: %w", err)
	}

	if err := v.setState(container.GetContainerID()); err != nil {
		return fmt.Errorf("failed to set ip-verifier state: %w", err)
	}

	v.log.Debug("--> IPVerifier started", "container", v.ContainerID, "pubkey", v.Pubkey, "url", v.InternalURL)
	return nil
}

func (v *IPVerifier) setState(containerID string) error {
	v.ContainerID = shortContainerID(containerID)
	return v.Prepare()
}
