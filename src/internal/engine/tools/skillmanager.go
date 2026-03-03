package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"miri-main/src/internal/config"
	"miri-main/src/internal/tools/skillmanager"
	"path"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type SkillInstallToolWrapper struct {
	Config      *config.Config
	OnInstalled func()
}

func NewSkillInstallTool(cfg *config.Config, onInstalled func()) *SkillInstallToolWrapper {
	return &SkillInstallToolWrapper{Config: cfg, OnInstalled: onInstalled}
}

func (s *SkillInstallToolWrapper) GetInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "skill_install",
		Desc: "Search and install a new skill from agentskill.sh into the local skills directory.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"skill_name": {
				Type:     schema.String,
				Desc:     "The name of the skill to install (e.g., 'web-analysis', 'notion-capture')",
				Required: true,
			},
		}),
	}
}

func (s *SkillInstallToolWrapper) Info(_ context.Context) (*schema.ToolInfo, error) {
	return s.GetInfo(), nil
}

func (s *SkillInstallToolWrapper) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		SkillName string `json:"skill_name"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		slog.Error("failed to unmarshal skill install arguments", "error", err, "arguments", argumentsInJSON)
		return "", err
	}
	slog.Debug("installing skill", "skill", args.SkillName)
	stdout, stderr, exitCode, err := skillmanager.SearchAndInstall(ctx, args.SkillName, s.Config.StorageDir)

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
		slog.Error("error occurred during skill install", "error", err, "stderr", stderr)
		res["error"] = err.Error()
	}

	slog.Debug("skill installation complete", "skill", args.SkillName, "exit_code", exitCode)
	if exitCode == 0 && s.OnInstalled != nil {
		s.OnInstalled()
	}
	b, _ := json.Marshal(res)
	return string(b), nil
}

func (s *SkillInstallToolWrapper) StreamableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (*schema.StreamReader[string], error) {
	var args struct {
		SkillName string `json:"skill_name"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		slog.Error("failed to unmarshal skill install (stream) arguments", "error", err, "arguments", argumentsInJSON)
		return nil, err
	}
	slog.Debug("installing skill (streaming)", "skill", args.SkillName)

	rc, err := skillmanager.SearchAndInstallStream(ctx, args.SkillName, s.Config.StorageDir)
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

type SkillRemoteListToolWrapper struct{}

func (s *SkillRemoteListToolWrapper) GetInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "skill_list_remote",
		Desc: "Get a list of all available skills that can be installed from agentskill.sh.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "The search query to match against remote skill names, slugs, or descriptions. Supports wildcards like *.",
				Required: false,
			},
		}),
	}
}

func (s *SkillRemoteListToolWrapper) Info(_ context.Context) (*schema.ToolInfo, error) {
	return s.GetInfo(), nil
}

func (s *SkillRemoteListToolWrapper) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		slog.Error("failed to unmarshal skill list remote arguments", "error", err, "arguments", argumentsInJSON)
	}

	allSkills, err := skillmanager.ListRemoteSkills(ctx)
	if err != nil {
		return "", err
	}

	if args.Query == "" {
		b, _ := json.Marshal(allSkills)
		return string(b), nil
	}

	matches := s.filterSkills(allSkills, strings.ToLower(args.Query))
	b, _ := json.Marshal(matches)
	return string(b), nil
}

func (s *SkillRemoteListToolWrapper) filterSkills(list []skillmanager.RemoteSkill, query string) []skillmanager.RemoteSkill {
	var matches []skillmanager.RemoteSkill
	isWildcard := strings.Contains(query, "*") || strings.Contains(query, "?")

	for _, skill := range list {
		name := strings.ToLower(skill.Name)
		slug := strings.ToLower(skill.Slug)
		desc := strings.ToLower(skill.Description)

		match := false
		if isWildcard {
			match, _ = path.Match(query, name)
			if !match {
				match, _ = path.Match(query, slug)
			}
			if !match {
				match, _ = path.Match(query, desc)
			}
		} else {
			match = strings.Contains(name, query) || strings.Contains(slug, query) || strings.Contains(desc, query)
		}

		if match {
			matches = append(matches, skill)
		}
	}
	return matches
}

type SkillRemoveToolWrapper struct {
	Config    *config.Config
	OnRemoved func()
}

func NewSkillRemoveTool(cfg *config.Config, onRemoved func()) *SkillRemoveToolWrapper {
	return &SkillRemoveToolWrapper{Config: cfg, OnRemoved: onRemoved}
}

func (s *SkillRemoveToolWrapper) GetInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "skill_remove",
		Desc: "Uninstall and remove a skill from the local skills directory.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"skill_name": {
				Type:     schema.String,
				Desc:     "The name of the skill to remove",
				Required: true,
			},
		}),
	}
}

func (s *SkillRemoveToolWrapper) Info(_ context.Context) (*schema.ToolInfo, error) {
	return s.GetInfo(), nil
}

func (s *SkillRemoveToolWrapper) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		SkillName string `json:"skill_name"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		slog.Error("failed to unmarshal skill remove arguments", "error", err, "arguments", argumentsInJSON)
		return "", err
	}
	err := skillmanager.RemoveSkill(args.SkillName, s.Config.StorageDir)
	if err != nil {
		slog.Error("failed to remove skill", "skill", args.SkillName, "error", err)
		return "", err
	}
	if s.OnRemoved != nil {
		s.OnRemoved()
	}
	return "Skill removed successfully", nil
}
