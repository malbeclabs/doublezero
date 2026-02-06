package docker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

func Build(ctx context.Context, log *slog.Logger, imageName string, dockerfilePath string, contextDir string, verbose bool, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", "build", "-t", imageName, "-f", dockerfilePath)

	// Add registry cache if DOCKER_CACHE_REGISTRY is set (e.g., "ghcr.io/malbeclabs/dz-cache")
	if cacheRegistry := os.Getenv("DOCKER_CACHE_REGISTRY"); cacheRegistry != "" {
		// Extract image name without registry/tag (e.g., "dz-local/base:dev" -> "base")
		baseName := imageName
		if idx := strings.LastIndex(baseName, "/"); idx != -1 {
			baseName = baseName[idx+1:]
		}
		if idx := strings.Index(baseName, ":"); idx != -1 {
			baseName = baseName[:idx]
		}
		cacheRef := cacheRegistry + "/" + baseName + ":cache"
		cmd.Args = append(cmd.Args, "--cache-from", "type=registry,ref="+cacheRef)
		cmd.Args = append(cmd.Args, "--cache-to", "type=registry,ref="+cacheRef+",mode=max")
	}

	cmd.Args = append(cmd.Args, args...)
	cmd.Args = append(cmd.Args, ".")
	cmd.Dir = contextDir
	cmd.Env = append(os.Environ(), "DOCKER_BUILDKIT=1")

	if verbose {
		log.Debug("--> Executing command", "cmd", cmd.Args)
	}

	if verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to build docker images: %w", err)
		}
	} else {
		output, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Println(string(output))
			return fmt.Errorf("failed to build docker images: %w", err)
		}
	}
	return nil
}
