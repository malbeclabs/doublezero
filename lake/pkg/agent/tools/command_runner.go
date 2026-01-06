package tools

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"time"
)

// CommandRunner abstracts command execution for testability.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, stdin io.Reader) (stdout, stderr string, err error)
}

// ExecCommandRunner implements CommandRunner using exec.CommandContext.
type ExecCommandRunner struct {
	Timeout time.Duration
}

// Run executes a command with the given arguments and optional stdin.
func (r *ExecCommandRunner) Run(ctx context.Context, name string, args []string, stdin io.Reader) (string, string, error) {
	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, name, args...)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	if stdin != nil {
		cmd.Stdin = stdin
	}

	err := cmd.Run()
	return stdoutBuf.String(), stderrBuf.String(), err
}
