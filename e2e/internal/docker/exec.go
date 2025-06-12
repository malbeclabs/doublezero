package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/docker/docker/client"
)

type ExecOption func(*ExecOptions)

type ExecOptions struct {
	PrintOnError bool
}

func NoPrintOnError() ExecOption {
	return func(opts *ExecOptions) {
		opts.PrintOnError = false
	}
}

func Exec(ctx context.Context, cli *client.Client, containerID string, cmd []string, options ...ExecOption) ([]byte, error) {
	execOptions := &ExecOptions{
		PrintOnError: true,
	}
	for _, option := range options {
		option(execOptions)
	}
	exitCode, execReader, err := ExecReturnReader(ctx, cli, containerID, cmd, Multiplexed())
	if err != nil {
		var buf []byte
		if execReader != nil {
			buf, _ = io.ReadAll(execReader)
			if buf != nil {
				if execOptions.PrintOnError {
					fmt.Println(string(buf))
				}
			}
		}
		return buf, fmt.Errorf("failed to execute command: %w", err)
	}
	if exitCode != 0 {
		var buf []byte
		if execReader != nil {
			buf, _ = io.ReadAll(execReader)
			if buf != nil {
				if execOptions.PrintOnError {
					fmt.Println(string(buf))
				}
			}
		}
		return buf, fmt.Errorf("command failed with exit code %d", exitCode)
	}

	buf, err := io.ReadAll(execReader)
	if err != nil {
		return buf, fmt.Errorf("error reading command output: %w", err)
	}
	return buf, nil
}

func ExecReturnJSONList(ctx context.Context, cli *client.Client, containerID string, cmd []string, options ...ExecOption) ([]map[string]any, error) {
	buf, err := Exec(ctx, cli, containerID, cmd, options...)
	if err != nil {
		return nil, err
	}

	list := []map[string]any{}
	err = json.Unmarshal(buf, &list)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return list, nil
}
