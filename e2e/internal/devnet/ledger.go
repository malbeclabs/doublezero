package devnet

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/malbeclabs/doublezero/e2e/internal/logging"
	"github.com/malbeclabs/doublezero/e2e/internal/solana"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// Ledger container is more memory intensive than the others.
	ledgerContainerMemory = 2 * 1024 * 1024 * 1024 // 2GB
)

type LedgerSpec struct {
	ContainerImage     string
	ProgramKeypairPath string
}

func (s *LedgerSpec) Validate() error {

	if s.ContainerImage == "" {
		return fmt.Errorf("containerImage is required")
	}

	if s.ProgramKeypairPath == "" {
		return fmt.Errorf("programKeypairPath is required")
	}

	return nil
}

type Ledger struct {
	dn  *Devnet
	log *slog.Logger

	ContainerID   string
	ProgramID     string
	InternalURL   string
	InternalWSURL string
}

// Start starts a ledger container and attaches it to the control plane network.
func (l *Ledger) Start(ctx context.Context) error {
	l.log.Info("==> Starting ledger", "image", l.dn.Spec.Ledger.ContainerImage)

	// Derive the program ID from the program keypair.
	programID, err := solana.PublicAddressFromKeypair(l.dn.Spec.Ledger.ProgramKeypairPath)
	if err != nil {
		return fmt.Errorf("failed to get program pubkey: %v", err)
	}

	containerProgramKeypairPath := "/etc/doublezero/ledger/dz-program-keypair.json"
	req := testcontainers.ContainerRequest{
		Image:        l.dn.Spec.Ledger.ContainerImage,
		Name:         l.dn.Spec.DeployID + "-ledger",
		ExposedPorts: []string{"8899/tcp", "8900/tcp"},
		WaitingFor: wait.ForAll(
			wait.ForExec([]string{"solana", "cluster-version"}).WithExitCodeMatcher(func(code int) bool { return code == 0 }),
			wait.ForHTTP("/").WithPort("8899/tcp").WithMethod("POST").
				WithHeaders(map[string]string{"Content-Type": "application/json"}).
				WithBody(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"getHealth"}`)).
				WithResponseMatcher(func(body io.Reader) bool {
					bodyBytes, _ := io.ReadAll(body)
					return strings.Contains(string(bodyBytes), `"result":"ok"`)
				}),
		).WithDeadline(1000 * time.Second),
		Env: map[string]string{
			"DZ_PROGRAM_KEYPAIR_PATH": containerProgramKeypairPath,
		},
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      l.dn.Spec.Ledger.ProgramKeypairPath,
				ContainerFilePath: containerProgramKeypairPath,
			},
		},
		Networks: []string{l.dn.DefaultNetwork.Name},
		NetworkAliases: map[string][]string{
			l.dn.DefaultNetwork.Name: {"ledger"},
		},
		// NOTE: We intentionally use the deprecated Resources field here instead of the HostConfigModifier
		// because the latter has issues with setting SHM memory and other constraints to 0, which can cause
		// unexpected behavior.
		Resources: container.Resources{
			NanoCPUs: defaultContainerNanoCPUs,
			Memory:   ledgerContainerMemory,
		},
		Labels: l.dn.labels,
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           logging.NewTestcontainersAdapter(l.log),
	})
	if err != nil {
		return fmt.Errorf("failed to start ledger: %w", err)
	}

	l.ContainerID = shortContainerID(container.GetContainerID())
	l.ProgramID = programID
	l.InternalURL = "http://ledger:8899"
	l.InternalWSURL = "ws://ledger:8900"

	l.log.Info("--> Ledger started", "container", l.ContainerID, "programID", l.ProgramID, "internalURL", l.InternalURL, "internalWSURL", l.InternalWSURL)
	return nil
}
