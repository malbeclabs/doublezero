package devnet

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

	// Get the solana version.
	output, err := docker.Run(ctx, log, os.Getenv("DZ_SOLANA_IMAGE"), verbose, "bash", "-c", "solana --version | awk '{print $2}'")
	if err != nil {
		return fmt.Errorf("failed to get solana version: %w", err)
	}
	solanaVersion := strings.TrimSpace(string(output))
	log.Debug("--> Solana tools version", "version", solanaVersion)

	// Build base image first
	err = docker.Build(ctx, log, os.Getenv("DZ_BASE_IMAGE"), filepath.Join(dockerfilesDir, "base.dockerfile"), workspaceDir, verbose, "--build-arg", "SOLANA_IMAGE="+os.Getenv("DZ_SOLANA_IMAGE"), "--build-arg", "SOLANA_VERSION="+solanaVersion)
	if err != nil {
		return fmt.Errorf("failed to build base image: %w", err)
	}
	baseImageArg := fmt.Sprintf("BASE_IMAGE=%s", os.Getenv("DZ_BASE_IMAGE"))

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
			args:       []string{"--build-arg", baseImageArg, "--build-arg", "DOCKERFILE_DIR=" + filepath.Join(dockerfilesDirRelativeToWorkspace, "activator")},
		},
		{
			name:       "client",
			image:      os.Getenv("DZ_CLIENT_IMAGE"),
			dockerfile: filepath.Join(dockerfilesDir, "client", "Dockerfile"),
			args:       []string{"--build-arg", baseImageArg, "--build-arg", "DOCKERFILE_DIR=" + filepath.Join(dockerfilesDirRelativeToWorkspace, "client")},
		},
		{
			name:       "controller",
			image:      os.Getenv("DZ_CONTROLLER_IMAGE"),
			dockerfile: filepath.Join(dockerfilesDir, "controller", "Dockerfile"),
			args:       []string{"--build-arg", baseImageArg, "--build-arg", "DOCKERFILE_DIR=" + filepath.Join(dockerfilesDirRelativeToWorkspace, "controller")},
		},
		{
			name:       "device",
			image:      os.Getenv("DZ_DEVICE_IMAGE"),
			dockerfile: filepath.Join(dockerfilesDir, "device", "Dockerfile"),
			args:       []string{"--build-arg", baseImageArg, "--build-arg", "DOCKERFILE_DIR=" + filepath.Join(dockerfilesDirRelativeToWorkspace, "device")},
		},
		{
			name:       "ledger",
			image:      os.Getenv("DZ_LEDGER_IMAGE"),
			dockerfile: filepath.Join(dockerfilesDir, "ledger", "Dockerfile"),
			args:       []string{"--build-arg", baseImageArg, "--build-arg", "DOCKERFILE_DIR=" + filepath.Join(dockerfilesDirRelativeToWorkspace, "ledger")},
		},
		{
			name:       "manager",
			image:      os.Getenv("DZ_MANAGER_IMAGE"),
			dockerfile: filepath.Join(dockerfilesDir, "manager", "Dockerfile"),
			args:       []string{"--build-arg", baseImageArg, "--build-arg", "DOCKERFILE_DIR=" + filepath.Join(dockerfilesDirRelativeToWorkspace, "manager")},
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
