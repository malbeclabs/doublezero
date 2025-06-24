package docker

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
)

func Pull(ctx context.Context, log *slog.Logger, imageName string, verbose bool) error {
	cmd := exec.CommandContext(ctx, "docker", "pull", imageName)

	if verbose {
		log.Debug("--> Executing command", "cmd", cmd.Args)
	}

	_, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to pull docker image: %w", err)
	}
	return nil
}
