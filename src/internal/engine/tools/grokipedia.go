package tools

import (
	"context"
	"encoding/json"
	"miri-main/src/internal/tools/grokipedia"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type GrokipediaToolWrapper struct{}

func (g *GrokipediaToolWrapper) GetInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "grokipedia",
		Desc: "Lookup facts and summaries on the internet (Wikipedia). Use for general knowledge, biographies, history, and science.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "The topic or fact to look up",
				Required: true,
			},
		}),
	}
}

func (g *GrokipediaToolWrapper) Info(_ context.Context) (*schema.ToolInfo, error) {
	return g.GetInfo(), nil
}

func (g *GrokipediaToolWrapper) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", err
	}
	result, err := grokipedia.Search(ctx, args.Query)
	if err != nil {
		return "", err
	}
	return result, nil
}
