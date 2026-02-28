package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"miri-main/src/internal/tools/cmd"
	"os"
	"path/filepath"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type CmdToolWrapper struct {
	StorageDir string
}

func NewCmdTool(storageDir string) *CmdToolWrapper {
	return &CmdToolWrapper{StorageDir: storageDir}
}

func (c *CmdToolWrapper) GetInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "execute_command",
		Desc: "Run a shell command on the local OS. Useful for system information, file operations, or software installation.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"command": {
				Type:     schema.String,
				Desc:     "The shell command to execute",
				Required: true,
			},
		}),
	}
}

func (c *CmdToolWrapper) Info(_ context.Context) (*schema.ToolInfo, error) {
	return c.GetInfo(), nil
}

func (c *CmdToolWrapper) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", err
	}

	// Ensure the generated directory exists
	if err := os.MkdirAll(c.StorageDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create storage directory: %w", err)
	}

	stdout, stderr, exitCode, err := cmd.Execute(ctx, args.Command, c.StorageDir)

	// Post-execution: if files were created in the base storage dir (one level up), move them to .generated
	// This helps with "if a file is created somewhere else move it there"
	// and ensures all file generation is sandboxed.
	parentDir := filepath.Dir(c.StorageDir)
	if entries, err := os.ReadDir(parentDir); err == nil {
		whitelist := map[string]bool{
			"soul.txt":   true,
			"skills":     true,
			"vector_db":  true,
			"checkpoint": true, // check if it's checkpoint or checkpoints
			".generated": true,
		}

		for _, entry := range entries {
			name := entry.Name()
			if !whitelist[name] && !entry.IsDir() {
				src := filepath.Join(parentDir, name)
				dst := filepath.Join(c.StorageDir, name)
				// Only move if dst doesn't exist to avoid overwriting (or should we overwrite?)
				// Let's overwrite to ensure it's in the sandbox.
				os.Rename(src, dst)
			}
		}
	}

	const maxOutput = 4096
	if len(stdout) > maxOutput {
		stdout = stdout[:maxOutput] + "\n... (stdout truncated)"
	}
	if len(stderr) > maxOutput {
		stderr = stderr[:maxOutput] + "\n... (stderr truncated)"
	}

	res := struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exit_code"`
		Error    string `json:"error,omitempty"`
	}{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: exitCode,
	}
	if err != nil {
		res.Error = err.Error()
	}
	b, _ := json.Marshal(res)
	return string(b), nil
}
