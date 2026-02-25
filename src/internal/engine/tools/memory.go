package tools

import (
	"context"
	"encoding/json"
	"miri-main/src/internal/storage"
	"miri-main/src/internal/tools/memory"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type SaveFactToolWrapper struct {
	storage *storage.Storage
}

func NewSaveFactTool(st *storage.Storage) *SaveFactToolWrapper {
	return &SaveFactToolWrapper{storage: st}
}

func (s *SaveFactToolWrapper) GetInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "save_fact",
		Desc: "Save a fact, piece of information, or important note to long-term memory (memory.md). Use this when the user explicitly asks to remember something or when you encounter a fact worth saving for future sessions.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"fact": {
				Type:     schema.String,
				Desc:     "The fact or information to save",
				Required: true,
			},
		}),
	}
}

func (s *SaveFactToolWrapper) Info(_ context.Context) (*schema.ToolInfo, error) {
	return s.GetInfo(), nil
}

func (s *SaveFactToolWrapper) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		Fact string `json:"fact"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", err
	}
	return memory.SaveFact(ctx, s.storage, args.Fact)
}
