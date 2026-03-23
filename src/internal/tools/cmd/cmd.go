package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

func Execute(ctx context.Context, command string, dir string) (stdout, stderr string, exitCode int, err error) {
	const maxRetries = 3
	backoffBase := 200 * time.Millisecond

	for attempt := 0; attempt <= maxRetries; attempt++ {
		timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Second)
		defer timeoutCancel()

		cmd := exec.CommandContext(timeoutCtx, "sh", "-c", command)
		if dir != "" {
			cmd.Dir = dir
		}
		stdoutB := &bytes.Buffer{}
		stderrB := &bytes.Buffer{}
		cmd.Stdout = stdoutB
		cmd.Stderr = stderrB
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		err = cmd.Run()
		exitCode = 0
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}

		if err == nil && exitCode == 0 {
			return stdoutB.String(), stderrB.String(), exitCode, nil
		}

		// Check transient failure
		transient := false
		retryReason := ""
		if exitErr != nil {
			if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				if ws.Signaled() {
					sig := ws.Signal()
					if sig == syscall.SIGKILL || sig == syscall.SIGSEGV {
						transient = true
						retryReason = fmt.Sprintf("killed by signal %d (possible OOM)", sig)
					}
				}
			}
		}

		if !transient || attempt == maxRetries {
			return stdoutB.String(), stderrB.String(), exitCode, err
		}

		// Log retry
		_, _ = fmt.Fprintf(stderrB, "[executor] %s. Retrying (%d/%d)...\n", retryReason, attempt+1, maxRetries)
		time.Sleep(backoffBase * time.Duration(attempt+1))
	}
	return "", "", 0, errors.New("unreachable")
}
