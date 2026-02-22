package curlinstall

import (
	"bytes"
	"context"
	"errors"
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
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		exitCode = exitErr.ExitCode()
	}

	return stdoutB.String(), stderrB.String(), exitCode, err
}
