package devnet

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/docker"
)

const (
	dockerfilesDirRelativeToWorkspace         = "e2e/docker"
	dockerfilesEnvFilePathRelativeToWorkspace = "e2e/.env.local"
)

func BuildContainerImages(ctx context.Context, log *slog.Logger, workspaceDir string, verbose bool) error {
	log.Info("==> Building docker images (this may take a while)", "verbose", verbose, "parallel", !verbose)

	start := time.Now()

	dockerfilesDir := filepath.Join(workspaceDir, dockerfilesDirRelativeToWorkspace)

	// Build base image first.
	// CACHE_BUSTER forces Docker to re-run build commands so that cargo/go can do
	// proper incremental compilation. Without this, switching branches can result
	// in stale cached artifacts being used.
	cacheBusterBuildArg := fmt.Sprintf("CACHE_BUSTER=%d", time.Now().Unix())
	extraArgs := []string{"--build-arg", cacheBusterBuildArg, "--platform", "linux/amd64"}
	err := docker.Build(ctx, log, os.Getenv("DZ_BASE_IMAGE"), filepath.Join(dockerfilesDir, "base.dockerfile"), workspaceDir, verbose, extraArgs...)
	if err != nil {
		return fmt.Errorf("failed to build base image: %w", err)
	}
	baseImageArg := fmt.Sprintf("BASE_IMAGE=%s", os.Getenv("DZ_BASE_IMAGE"))

	// Add the ARISTA_CEOS_IMAGE device image build args if set
	var ceosImageArg string
	if os.Getenv("ARISTA_CEOS_IMAGE") != "" {
		ceosImageArg = fmt.Sprintf("ARISTA_CEOS_IMAGE=%s", os.Getenv("ARISTA_CEOS_IMAGE"))
	}
	deviceExtraArgs := append([]string{}, extraArgs...)
	if ceosImageArg != "" {
		deviceExtraArgs = append(deviceExtraArgs, "--build-arg", ceosImageArg)
	}

	// Define build tasks
	buildTasks := []struct {
		name       string
		image      string
		dockerfile string
		args       []string
	}{
		{
			name:       "activator",
			image:      os.Getenv("DZ_ACTIVATOR_IMAGE"),
			dockerfile: filepath.Join(dockerfilesDir, "activator", "Dockerfile"),
			args:       append([]string{"--build-arg", baseImageArg, "--build-arg", "DOCKERFILE_DIR=" + filepath.Join(dockerfilesDirRelativeToWorkspace, "activator")}, extraArgs...),
		},
		{
			name:       "client",
			image:      os.Getenv("DZ_CLIENT_IMAGE"),
			dockerfile: filepath.Join(dockerfilesDir, "client", "Dockerfile"),
			args:       append([]string{"--build-arg", baseImageArg, "--build-arg", "DOCKERFILE_DIR=" + filepath.Join(dockerfilesDirRelativeToWorkspace, "client")}, extraArgs...),
		},
		{
			name:       "controller",
			image:      os.Getenv("DZ_CONTROLLER_IMAGE"),
			dockerfile: filepath.Join(dockerfilesDir, "controller", "Dockerfile"),
			args:       append([]string{"--build-arg", baseImageArg, "--build-arg", "DOCKERFILE_DIR=" + filepath.Join(dockerfilesDirRelativeToWorkspace, "controller")}, extraArgs...),
		},
		{
			name:       "device",
			image:      os.Getenv("DZ_DEVICE_IMAGE"),
			dockerfile: filepath.Join(dockerfilesDir, "device", "Dockerfile"),
			args:       append([]string{"--build-arg", baseImageArg, "--build-arg", "DOCKERFILE_DIR=" + filepath.Join(dockerfilesDirRelativeToWorkspace, "device")}, deviceExtraArgs...),
		},
		{
			name:       "ledger",
			image:      os.Getenv("DZ_LEDGER_IMAGE"),
			dockerfile: filepath.Join(dockerfilesDir, "ledger", "Dockerfile"),
			args:       append([]string{"--build-arg", baseImageArg, "--build-arg", "DOCKERFILE_DIR=" + filepath.Join(dockerfilesDirRelativeToWorkspace, "ledger")}, extraArgs...),
		},
		{
			name:       "manager",
			image:      os.Getenv("DZ_MANAGER_IMAGE"),
			dockerfile: filepath.Join(dockerfilesDir, "manager", "Dockerfile"),
			args:       append([]string{"--build-arg", baseImageArg, "--build-arg", "DOCKERFILE_DIR=" + filepath.Join(dockerfilesDirRelativeToWorkspace, "manager")}, extraArgs...),
		},
		{
			name:       "funder",
			image:      os.Getenv("DZ_FUNDER_IMAGE"),
			dockerfile: filepath.Join(dockerfilesDir, "funder", "Dockerfile"),
			args:       append([]string{"--build-arg", baseImageArg, "--build-arg", "DOCKERFILE_DIR=" + filepath.Join(dockerfilesDirRelativeToWorkspace, "funder")}, extraArgs...),
		},
		{
			name:       "device-health-oracle",
			image:      os.Getenv("DZ_DEVICE_HEALTH_ORACLE_IMAGE"),
			dockerfile: filepath.Join(dockerfilesDir, "device-health-oracle", "Dockerfile"),
			args:       append([]string{"--build-arg", baseImageArg, "--build-arg", "DOCKERFILE_DIR=" + filepath.Join(dockerfilesDirRelativeToWorkspace, "device-health-oracle")}, extraArgs...),
		},
	}

	if verbose {
		// Build sequentially when verbose is true
		for _, task := range buildTasks {
			select {
			case <-ctx.Done():
				return fmt.Errorf("build cancelled: %w", ctx.Err())
			default:
			}

			err := docker.Build(ctx, log, task.image, task.dockerfile, workspaceDir, verbose, task.args...)
			if err != nil {
				return fmt.Errorf("failed to build %s image: %w", task.name, err)
			}
		}
	} else {
		// Build in parallel when verbose is false
		var wg sync.WaitGroup
		errChan := make(chan error, len(buildTasks))

		for _, task := range buildTasks {
			wg.Add(1)
			go func(task struct {
				name       string
				image      string
				dockerfile string
				args       []string
			}) {
				defer wg.Done()

				select {
				case <-ctx.Done():
					errChan <- fmt.Errorf("build cancelled: %w", ctx.Err())
					return
				default:
				}

				err := docker.Build(ctx, log, task.image, task.dockerfile, workspaceDir, verbose, task.args...)
				if err != nil {
					errChan <- fmt.Errorf("failed to build %s image: %w", task.name, err)
					return
				}
			}(task)
		}

		wg.Wait()
		close(errChan)

		// Check for any errors
		for err := range errChan {
			if err != nil {
				return err
			}
		}
	}

	log.Info("--> Docker images built", "duration", time.Since(start))
	return nil
}
