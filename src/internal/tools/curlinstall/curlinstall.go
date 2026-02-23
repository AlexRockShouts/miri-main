package curlinstall

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"
)

// Install executes a curl-based installation command, typically "curl -fsSL <url> | sh".
func Install(ctx context.Context, url string) (stdout, stderr string, exitCode int, err error) {
	ctx, cancel := context.WithTimeout(ctx, 300*time.Second) // 5 minutes for complex installs
	defer cancel()

	// Using sh -c to allow piping, which is common for curl-based installers.
	// We use -fsSL as it's the standard for many installers (like homebrew, rustup, etc).
	cmd := exec.CommandContext(ctx, "sh", "-c", "curl -fsSL "+url+" | sh")
	stdoutB := &bytes.Buffer{}
	stderrB := &bytes.Buffer{}
	cmd.Stdout = stdoutB
	cmd.Stderr = stderrB

	err = cmd.Run()

	exitCode = 0
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		exitCode = exitErr.ExitCode()
	}

	return stdoutB.String(), stderrB.String(), exitCode, err
}

// InstallStream executes a curl-based installation command and streams combined stdout/stderr.
func InstallStream(ctx context.Context, url string) (io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", "curl -fsSL "+url+" | sh")

	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		pw.Close()
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
