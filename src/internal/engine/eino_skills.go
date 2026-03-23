package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cloudwego/eino/schema"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/tool"

	"miri-main/src/internal/config"
	"miri-main/src/internal/cotgraph"
	"miri-main/src/internal/engine/skills"
	"miri-main/src/internal/engine/tools"
	"miri-main/src/internal/session"
	"miri-main/src/internal/tools/skillmanager"
	"miri-main/src/internal/topology"
)

func (e *EinoEngine) ListSkills() []*skills.Skill {
	if e.skillLoader == nil {
		return nil
	}
	e.skillLoader.Load() // Refresh
	return e.skillLoader.GetSkills()
}

func (e *EinoEngine) ListSkillCommands(ctx context.Context) ([]SkillCommand, error) {
	// 1. Basic tools
	searchTool := &tools.SearchToolWrapper{}
	fetchTool := &tools.FetchToolWrapper{}
	grokipediaTool := tools.CreateGrokipediaTool()
	cmdTool := tools.NewCmdTool(e.storageBaseDir)
	skillRemoveTool := tools.NewSkillRemoveTool(&config.Config{StorageDir: e.storageBaseDir}, nil)
	taskMgrTool := tools.NewTaskManagerTool(nil, session.DefaultSessionID)
	fileManagerTool := tools.NewFileManagerTool(e.storageBaseDir, nil)

	allBase := []tool.BaseTool{
		searchTool, fetchTool, grokipediaTool, cmdTool,
		skillRemoveTool, taskMgrTool, fileManagerTool,
	}

	var res []SkillCommand
	for _, t := range allBase {
		info, err := t.Info(ctx)
		if err == nil {
			res = append(res, SkillCommand{Name: info.Name, Description: info.Desc})
		}
	}

	// 2. Skill loader tools
	if e.skillLoader != nil {
		skillUseTool := skills.NewUseTool(e.skillLoader)

		if info, err := skillUseTool.Info(ctx); err == nil {
			res = append(res, SkillCommand{Name: info.Name, Description: info.Desc})
		}

		for _, t := range e.skillLoader.GetExtraTools() {
			if info, err := t.Info(ctx); err == nil {
				res = append(res, SkillCommand{Name: info.Name, Description: info.Desc})
			}
		}
	}

	return res, nil
}

func (e *EinoEngine) ListRemoteSkills(ctx context.Context) ([]string, error) {
	return nil, fmt.Errorf("remote skill listing is removed; use /learn skill")
}

func (e *EinoEngine) InstallSkill(ctx context.Context, name string) (string, error) {
	return "", fmt.Errorf("manual skill installation is removed; use /learn skill")
}

func (e *EinoEngine) RemoveSkill(name string) error {
	err := skillmanager.RemoveSkill(name, e.storageBaseDir)
	if err != nil {
		return err
	}
	if e.skillLoader != nil {
		e.skillLoader.Load() // Reload after removal
	}
	return nil
}

func (e *EinoEngine) GetSkill(name string) (*skills.Skill, error) {
	if e.skillLoader == nil {
		return nil, fmt.Errorf("skill loader not initialized")
	}
	e.skillLoader.Load() // Refresh
	skill, ok := e.skillLoader.GetSkill(name)
	if !ok {
		return nil, fmt.Errorf("skill %q not found", name)
	}
	return skill, nil
}

// LocalInstallSkill installs a local skill from content.
func (e *EinoEngine) LocalInstallSkill(ctx context.Context, name, content string) error {
	dir := filepath.Join(e.storageBaseDir, "skills")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	safeName := filepath.Base(name)
	if safeName != name || strings.ContainsAny(safeName, "./\\") {
		return fmt.Errorf("invalid skill name %q", name)
	}
	path := filepath.Join(dir, safeName+".md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	if e.skillLoader != nil {
		e.skillLoader.Load()
	}
	return nil
}

type LocalInstallTool struct {
	e *EinoEngine
}

func NewLocalInstallTool(e *EinoEngine) tool.InvokableTool {
	return &LocalInstallTool{e}
}

func (t *LocalInstallTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "skill_local_install",
		Desc: "Install a new local skill from raw MD content with YAML frontmatter. Enables hot-swap self-modification without restart. Params: name (string), content (string, full markdown with --- frontmatter ---).",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"name": {
				Type:     schema.String,
				Desc:     "Unique skill name (no path, safe basename). Required.",
				Required: true,
			},
			"content": {
				Type:     schema.String,
				Desc:     "Full skill markdown content with YAML frontmatter (--- ... ---). Required.",
				Required: true,
			},
		}),
	}, nil
}

func (t *LocalInstallTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("invalid JSON args: %w", err)
	}
	if args.Name == "" || args.Content == "" {
		return "", fmt.Errorf("name and content required")
	}
	if err := t.e.LocalInstallSkill(ctx, args.Name, args.Content); err != nil {
		return "", err
	}
	return fmt.Sprintf("✅ Skill '%s' installed and reloaded. New tools available mid-chat.", args.Name), nil
}

type CotGraphTool struct{}

func NewCotGraphTool() tool.InvokableTool {
	return &CotGraphTool{}
}

func (t *CotGraphTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "cotgraph_analyze",
		Desc: "Parse CoT reasoning ([D][R][E]/[Thought:]) → graph (nodes=thoughts, edges=transitions). Detect cycles/loops in self-mod (retry fails).",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"input": {
				Type:     schema.String,
				Desc:     "Recent reasoning/memory text with tags.",
				Required: true,
			},
		}),
	}, nil
}

func (t *CotGraphTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		Input string `json:"input"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("invalid JSON: %w", err)
	}
	if args.Input == "" {
		return "", fmt.Errorf("input required")
	}
	result, err := cotgraph.Analyze(ctx, args.Input)
	if err != nil {
		return fmt.Sprintf("Error: %v", err), nil
	}
	return result, nil
}

type TopologyTool struct{}

func NewTopologyTool() tool.InvokableTool {
	return &TopologyTool{}
}

func (t *TopologyTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "topology_analyze",
		Desc: "Compute topology metrics (valency/degrees, diameter, cyclomatic #) on Go code call graphs. Prune redundant complex tool paths e.g. failed git chains.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"dir": {
				Type:     schema.String,
				Desc:     "Go directory to analyze (e.g. 'src/internal/engine/tools').",
				Required: true,
			},
		}),
	}, nil
}

func (t *TopologyTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		Dir string `json:"dir"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("invalid JSON args: %w", err)
	}
	if args.Dir == "" {
		return "", fmt.Errorf("dir required")
	}
	result, err := topology.Analyze(ctx, args.Dir)
	if err != nil {
		return fmt.Sprintf("Error: %v", err), nil
	}
	return result, nil
}
