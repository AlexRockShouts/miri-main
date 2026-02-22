package tools

import (
	"context"
	"encoding/json"
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
	slog.Debug("running installation script from url, %s", argumentsInJSON)
	stdout, stderr, exitCode, err := curlinstall.Install(ctx, args.URL)
	res := map[string]any{
		"stdout":    stdout,
		"stderr":    stderr,
		"exit_code": exitCode,
	}
	if err != nil {
		slog.Error("error occurred during curl install %s, stderr %s", err, stderr)
		res["error"] = err.Error()
	}

	slog.Debug("curl installation complete, stdout: %s, stderr: %s", stdout, stderr)
	b, _ := json.Marshal(res)
	return string(b), nil
}
