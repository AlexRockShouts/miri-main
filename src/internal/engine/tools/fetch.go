package tools

import (
	"context"
	"encoding/json"
	"miri-main/src/internal/tools/webfetch"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type FetchToolWrapper struct{}

func (f *FetchToolWrapper) GetInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "web_fetch",
		Desc: "Fetch the content of a web page.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"url": {
				Type:     schema.String,
				Desc:     "The URL to fetch",
				Required: true,
			},
		}),
	}
}

func (f *FetchToolWrapper) Info(_ context.Context) (*schema.ToolInfo, error) {
	return f.GetInfo(), nil
}

func (f *FetchToolWrapper) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", err
	}
	_, _, body, err := webfetch.Fetch(ctx, args.URL, 0)
	if err != nil {
		return "", err
	}
	return body, nil
}
