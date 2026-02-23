package tools

import (
	"context"
	"encoding/json"
	"miri-main/src/internal/tools/cmd"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type CmdToolWrapper struct{}

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
	stdout, stderr, exitCode, err := cmd.Execute(ctx, args.Command)

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
