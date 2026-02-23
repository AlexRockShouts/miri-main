package goinstall

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"
)

// Install executes the 'go install' command for the specified package.
func Install(ctx context.Context, pkg string) (stdout, stderr string, exitCode int, err error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second) // Go install can take some time
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "install", pkg)
	var stdoutB, stderrB io.Writer
	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}
	stdoutB = stdoutBuf
	stderrB = stderrBuf
	cmd.Stdout = stdoutB
	cmd.Stderr = stderrB

	err = cmd.Run()

	exitCode = 0
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		exitCode = exitErr.ExitCode()
	}

	return stdoutBuf.String(), stderrBuf.String(), exitCode, err
}

// InstallStream executes the 'go install' command and streams combined stdout/stderr.
func InstallStream(ctx context.Context, pkg string) (io.ReadCloser, error) {
	// We don't use WithTimeout here because we want the caller to control it via the context,
	// and cmd.Start() returns immediately.
	cmd := exec.CommandContext(ctx, "go", "install", pkg)

	// Combine stdout and stderr
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	go func() {
		err := cmd.Wait()
		if err != nil {
			if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
				pw.CloseWithError(fmt.Errorf("exit code %d: %w", exitErr.ExitCode(), err))
			} else {
				pw.CloseWithError(err)
			}
		} else {
			pw.Close()
		}
	}()

	return pr, nil
}
