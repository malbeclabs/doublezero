package docker

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
)

func Run(ctx context.Context, log *slog.Logger, imageName string, verbose bool, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", imageName)
	cmd.Args = append(cmd.Args, args...)

	if verbose {
		log.Debug("--> Executing command", "cmd", cmd.Args)
	}

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run docker container: %w", err)
	}
	return output, nil
}
