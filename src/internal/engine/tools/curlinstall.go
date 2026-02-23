package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"miri-main/src/internal/tools/curlinstall"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type CurlInstallToolWrapper struct{}

func (c *CurlInstallToolWrapper) GetInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "curl_install",
		Desc: "Install tools or execute scripts using 'curl -fsSL <url> | sh'.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"url": {
				Type:     schema.String,
				Desc:     "The script/installer URL to fetch and pipe to 'sh'",
				Required: true,
			},
		}),
	}
}

func (c *CurlInstallToolWrapper) Info(_ context.Context) (*schema.ToolInfo, error) {
	return c.GetInfo(), nil
}

func (c *CurlInstallToolWrapper) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", err
	}
	slog.Debug("running installation script from url", "args", argumentsInJSON)
	stdout, stderr, exitCode, err := curlinstall.Install(ctx, args.URL)

	const maxOutput = 4096
	if len(stdout) > maxOutput {
		stdout = stdout[:maxOutput] + "\n... (stdout truncated)"
	}
	if len(stderr) > maxOutput {
		stderr = stderr[:maxOutput] + "\n... (stderr truncated)"
	}

	res := map[string]any{
		"stdout":    stdout,
		"stderr":    stderr,
		"exit_code": exitCode,
	}
	if err != nil {
		slog.Error("error occurred during curl install", "error", err, "stderr", stderr)
		res["error"] = err.Error()
	}

	slog.Debug("curl installation complete", "stdout", stdout, "stderr", stderr)
	b, _ := json.Marshal(res)
	return string(b), nil
}

func (c *CurlInstallToolWrapper) StreamableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (*schema.StreamReader[string], error) {
	var args struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return nil, err
	}
	slog.Debug("running installation script from url (streaming)", "args", argumentsInJSON)

	rc, err := curlinstall.InstallStream(ctx, args.URL)
	if err != nil {
		return nil, err
	}

	sr, sw := schema.Pipe[string](1)

	go func() {
		defer rc.Close()
		defer sw.Close()

		reader := bufio.NewReader(rc)
		for {
			line, err := reader.ReadString('\n')
			if line != "" {
				sw.Send(line, nil)
			}
			if err != nil {
				if err != io.EOF {
					sw.Send("", err)
				}
				break
			}
		}
	}()

	return sr, nil
}
