package cmd

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"
)

func Execute(ctx context.Context, command string) (stdout, stderr string, exitCode int, err error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
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
