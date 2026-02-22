package tools

import (
	"context"
	"encoding/json"
	"miri-main/src/internal/tools/websearch"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type SearchToolWrapper struct{}

func (s *SearchToolWrapper) GetInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "web_search",
		Desc: "Search the web for current information.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "The search query",
				Required: true,
			},
		}),
	}
}

func (s *SearchToolWrapper) Info(_ context.Context) (*schema.ToolInfo, error) {
	return s.GetInfo(), nil
}

func (s *SearchToolWrapper) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", err
	}
	results, err := websearch.Search(ctx, args.Query)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(results)
	return string(b), nil
}
