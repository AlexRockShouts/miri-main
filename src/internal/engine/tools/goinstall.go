package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"miri-main/src/internal/tools/goinstall"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type GoInstallToolWrapper struct{}

func (g *GoInstallToolWrapper) GetInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "go_install",
		Desc: "Install Go libraries and tools using 'go install'.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"package": {
				Type:     schema.String,
				Desc:     "The Go package path to install (e.g., 'github.com/example/tool@latest')",
				Required: true,
			},
		}),
	}
}

func (g *GoInstallToolWrapper) Info(_ context.Context) (*schema.ToolInfo, error) {
	return g.GetInfo(), nil
}

func (g *GoInstallToolWrapper) StreamableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (*schema.StreamReader[string], error) {
	var args struct {
		Package string `json:"package"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return nil, err
	}
	slog.Debug("installing tool over go cli (streaming), %s", argumentsInJSON)

	rc, err := goinstall.InstallStream(ctx, args.Package)
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
