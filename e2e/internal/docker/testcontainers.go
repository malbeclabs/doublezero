package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// The following code is adapted from the Exec function in testcontainers-go:
// https://github.com/testcontainers/testcontainers-go/blob/main/docker.go
//
// The MIT license applies **only to this function and the related types or logic
// directly derived from the above source**. All other code in this file/project is
// licensed under the Apache License 2.0.
//
// The MIT License (MIT)
//
// # Copyright (c) 2017–2019 Gianluca Arbezzano
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
func ExecReturnReader(ctx context.Context, cli *client.Client, containerID string, cmd []string, options ...ProcessOption) (int, io.Reader, error) {
	processOptions := NewProcessOptions(cmd)

	// Process all the options in a first loop because for the multiplexed option we first need
	// to have a containerExecCreateResponse
	for _, o := range options {
		o.Apply(processOptions)
	}

	response, err := cli.ContainerExecCreate(ctx, containerID, processOptions.ExecConfig)
	if err != nil {
		return 0, nil, fmt.Errorf("container exec create: %w", err)
	}

	hijack, err := cli.ContainerExecAttach(ctx, response.ID, container.ExecAttachOptions{})
	if err != nil {
		return 0, nil, fmt.Errorf("container exec attach: %w", err)
	}

	processOptions.Reader = hijack.Reader

	// Second loop to process the multiplexed option, as now we have a reader from the created
	// exec response.
	for _, o := range options {
		o.Apply(processOptions)
	}

	var exitCode int
	for {
		execResp, err := cli.ContainerExecInspect(ctx, response.ID)
		if err != nil {
			return 0, nil, fmt.Errorf("container exec inspect: %w", err)
		}

		if !execResp.Running {
			exitCode = execResp.ExitCode
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	return exitCode, processOptions.Reader, nil
}

// ProcessOptions defines options applicable to the reader processor
type ProcessOptions struct {
	ExecConfig container.ExecOptions
	Reader     io.Reader
}

// NewProcessOptions returns a new ProcessOptions instance
// with the given command and default options:
// - detach: false
// - attach stdout: true
// - attach stderr: true
func NewProcessOptions(cmd []string) *ProcessOptions {
	return &ProcessOptions{
		ExecConfig: container.ExecOptions{
			Cmd:          cmd,
			Detach:       false,
			AttachStdout: true,
			AttachStderr: true,
		},
	}
}

// ProcessOption defines a common interface to modify the reader processor
// These options can be passed to the Exec function in a variadic way to customize the returned Reader instance
type ProcessOption interface {
	Apply(opts *ProcessOptions)
}

type ProcessOptionFunc func(opts *ProcessOptions)

func (fn ProcessOptionFunc) Apply(opts *ProcessOptions) {
	fn(opts)
}

func WithUser(user string) ProcessOption {
	return ProcessOptionFunc(func(opts *ProcessOptions) {
		opts.ExecConfig.User = user
	})
}

func WithWorkingDir(workingDir string) ProcessOption {
	return ProcessOptionFunc(func(opts *ProcessOptions) {
		opts.ExecConfig.WorkingDir = workingDir
	})
}

func WithEnv(env []string) ProcessOption {
	return ProcessOptionFunc(func(opts *ProcessOptions) {
		opts.ExecConfig.Env = env
	})
}

// safeBuffer is a goroutine safe buffer.
type safeBuffer struct {
	mtx sync.Mutex
	buf bytes.Buffer
	err error
}

// Error sets an error for the next read.
func (sb *safeBuffer) Error(err error) {
	sb.mtx.Lock()
	defer sb.mtx.Unlock()

	sb.err = err
}

// Write writes p to the buffer.
// It is safe for concurrent use by multiple goroutines.
func (sb *safeBuffer) Write(p []byte) (n int, err error) {
	sb.mtx.Lock()
	defer sb.mtx.Unlock()

	return sb.buf.Write(p)
}

// Read reads up to len(p) bytes into p from the buffer.
// It is safe for concurrent use by multiple goroutines.
func (sb *safeBuffer) Read(p []byte) (n int, err error) {
	sb.mtx.Lock()
	defer sb.mtx.Unlock()

	if sb.err != nil {
		return 0, sb.err
	}

	return sb.buf.Read(p)
}

// Multiplexed returns a [ProcessOption] that configures the command execution
// to combine stdout and stderr into a single stream without Docker's multiplexing headers.
func Multiplexed() ProcessOption {
	return ProcessOptionFunc(func(opts *ProcessOptions) {
		// returning fast to bypass those options with a nil reader,
		// which could be the case when other options are used
		// to configure the exec creation.
		if opts.Reader == nil {
			return
		}

		done := make(chan struct{})

		var outBuff safeBuffer
		var errBuff safeBuffer
		go func() {
			defer close(done)
			if _, err := stdcopy.StdCopy(&outBuff, &errBuff, opts.Reader); err != nil {
				outBuff.Error(fmt.Errorf("copying output: %w", err))
				return
			}
		}()

		<-done

		opts.Reader = io.MultiReader(&outBuff, &errBuff)
	})
}
