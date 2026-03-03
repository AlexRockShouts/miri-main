package engine

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"

	"miri-main/src/internal/config"
	"miri-main/src/internal/engine/skills"
	"miri-main/src/internal/engine/tools"
	"miri-main/src/internal/session"
	"miri-main/src/internal/tools/skillmanager"
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
	grokipediaTool := &tools.GrokipediaToolWrapper{}
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
