package docker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
)

func Build(ctx context.Context, log *slog.Logger, imageName string, dockerfilePath string, contextDir string, verbose bool, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", "build", "-t", imageName, "-f", dockerfilePath)
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
