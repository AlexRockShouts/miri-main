package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"

	"miri-main/src/internal/tools/cmd"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"gopkg.in/yaml.v3"
)

type Skill struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Version     string   `yaml:"version"`
	Tags        []string `yaml:"tags"`
	Directory   string   // Path to the skill folder
	FullContent string   // Content of SKILL.md
}

type SkillLoader struct {
	SkillsDir  string
	ScriptsDir string
	skills     map[string]*Skill
	extraTools []tool.BaseTool
}

func NewSkillLoader(skillsDir, scriptsDir string) *SkillLoader {
	return &SkillLoader{
		SkillsDir:  skillsDir,
		ScriptsDir: scriptsDir,
		skills:     make(map[string]*Skill),
	}
}

func (l *SkillLoader) Load() error {
	if err := l.loadSkills(); err != nil {
		return err
	}
	return l.loadScripts()
}

func (l *SkillLoader) loadSkills() error {
	if _, err := os.Stat(l.SkillsDir); os.IsNotExist(err) {
		return nil
	}

	// Clear existing skills to allow refresh
	l.skills = make(map[string]*Skill)

	return filepath.WalkDir(l.SkillsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != l.SkillsDir {
			skill, err := l.parseSkillDir(path)
			if err != nil {
				slog.Warn("Failed to parse skill directory", "path", path, "error", err)
				return nil // Continue walking
			}
			if skill != nil {
				l.skills[skill.Name] = skill
				slog.Info("Loaded skill", "name", skill.Name, "version", skill.Version)
			}
			return filepath.SkipDir // Don't descend into skill subdirectories
		}
		return nil
	})
}

func (l *SkillLoader) parseSkillDir(dir string) (*Skill, error) {
	skillFile := filepath.Join(dir, "SKILL.md")
	content, err := os.ReadFile(skillFile)
	if err != nil {
		return nil, nil // Not a skill directory
	}

	// Simple YAML frontmatter parser
	parts := strings.SplitN(string(content), "---", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid skill format: missing frontmatter in %s", skillFile)
	}

	var skill Skill
	if err := yaml.Unmarshal([]byte(parts[1]), &skill); err != nil {
		return nil, fmt.Errorf("failed to parse frontmatter: %w", err)
	}

	if skill.Name == "" {
		skill.Name = filepath.Base(dir)
	}
	skill.Directory = dir
	skill.FullContent = strings.TrimSpace(parts[2])

	return &skill, nil
}

func (l *SkillLoader) GetSkill(name string) (*Skill, bool) {
	// Try exact match
	if s, ok := l.skills[name]; ok {
		return s, true
	}

	// Try with name variations (hyphen/underscore)
	normalized := strings.ReplaceAll(name, "_", "-")
	if s, ok := l.skills[normalized]; ok {
		return s, true
	}

	normalized = strings.ReplaceAll(name, "-", "_")
	if s, ok := l.skills[normalized]; ok {
		return s, true
	}

	// Try case-insensitive exact match
	for _, s := range l.skills {
		if strings.EqualFold(s.Name, name) {
			return s, true
		}
	}

	// Try case-insensitive normalized match
	normalized = strings.ReplaceAll(strings.ToLower(name), "_", "-")
	for _, s := range l.skills {
		if strings.ReplaceAll(strings.ToLower(s.Name), "_", "-") == normalized {
			return s, true
		}
	}

	return nil, false
}

func (l *SkillLoader) GetSkills() []*Skill {
	res := make([]*Skill, 0, len(l.skills))
	for _, s := range l.skills {
		res = append(res, s)
	}
	return res
}

func (l *SkillLoader) loadScripts() error {
	if _, err := os.Stat(l.ScriptsDir); os.IsNotExist(err) {
		return nil
	}

	// Clear existing extra tools to allow refresh
	l.extraTools = nil

	return filepath.WalkDir(l.ScriptsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			ext := filepath.Ext(path)
			if ext == ".sh" || ext == ".py" || ext == ".js" {
				t := l.inferTool(path)
				l.extraTools = append(l.extraTools, t)
				slog.Info("Inferred tool from script", "path", path)
			}
		}
		return nil
	})
}

func (l *SkillLoader) inferTool(path string) tool.BaseTool {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	// Replace non-alphanumeric with underscore for tool name
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, name)

	return &scriptTool{
		name: name,
		path: path,
	}
}

type scriptTool struct {
	name string
	path string
}

func (s *scriptTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: s.name,
		Desc: fmt.Sprintf("Execute the script at %s. Takes a single 'args' string argument.", s.path),
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"args": {
				Type:     schema.String,
				Desc:     "Arguments to pass to the script",
				Required: false,
			},
		}),
	}, nil
}

func (s *scriptTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		Args string `json:"args"`
	}
	_ = json.Unmarshal([]byte(argumentsInJSON), &args)

	var command string
	ext := filepath.Ext(s.path)
	switch ext {
	case ".sh":
		command = fmt.Sprintf("sh %s %s", s.path, args.Args)
	case ".py":
		command = fmt.Sprintf("python3 %s %s", s.path, args.Args)
	case ".js":
		command = fmt.Sprintf("node %s %s", s.path, args.Args)
	default:
		command = fmt.Sprintf("%s %s", s.path, args.Args)
	}

	stdout, stderr, exitCode, err := cmd.Execute(ctx, command)
	res := map[string]any{
		"stdout":    stdout,
		"stderr":    stderr,
		"exit_code": exitCode,
	}
	if err != nil {
		res["error"] = err.Error()
	}
	b, _ := json.Marshal(res)
	return string(b), nil
}

func (l *SkillLoader) GetExtraTools() []tool.BaseTool {
	return l.extraTools
}

// SearchTool implements a tool to search for available skills.
type SearchTool struct {
	loader *SkillLoader
}

func NewSearchTool(loader *SkillLoader) tool.InvokableTool {
	return &SearchTool{loader: loader}
}

func (t *SearchTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "skill_search",
		Desc: "Search for available skills and capabilities. Returns matching skill names and descriptions.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "The search query to match against skill names, descriptions, or tags.",
				Required: false,
			},
		}),
	}, nil
}

func (t *SearchTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		Query string `json:"query"`
	}
	_ = json.Unmarshal([]byte(argumentsInJSON), &args)

	query := strings.ToLower(args.Query)
	var matches []map[string]any

	for _, s := range t.loader.skills {
		match := false
		if query == "" {
			match = true
		} else {
			name := strings.ToLower(s.Name)
			desc := strings.ToLower(s.Description)

			if strings.Contains(query, "*") || strings.Contains(query, "?") {
				match, _ = path.Match(query, name)
				if !match {
					match, _ = path.Match(query, desc)
				}
				if !match {
					for _, tag := range s.Tags {
						match, _ = path.Match(query, strings.ToLower(tag))
						if match {
							break
						}
					}
				}
			} else {
				if strings.Contains(name, query) || strings.Contains(desc, query) {
					match = true
				} else {
					for _, tag := range s.Tags {
						if strings.Contains(strings.ToLower(tag), query) {
							match = true
							break
						}
					}
				}
			}
		}

		if match {
			matches = append(matches, map[string]any{
				"name":        s.Name,
				"description": s.Description,
				"version":     s.Version,
				"tags":        s.Tags,
			})
		}
	}

	b, _ := json.Marshal(matches)
	return string(b), nil
}

// UseTool implements a tool to "activate" a skill by loading its content into the session context.
type UseTool struct {
	loader *SkillLoader
}

func NewUseTool(loader *SkillLoader) tool.InvokableTool {
	return &UseTool{loader: loader}
}

func (t *UseTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "skill_use",
		Desc: "Load a specific skill's instructions and capabilities into the current context.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"skill_name": {
				Type:     schema.String,
				Desc:     "The exact name of the skill to load.",
				Required: true,
			},
		}),
	}, nil
}

func (t *UseTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		SkillName string `json:"skill_name"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", err
	}

	skill, ok := t.loader.GetSkill(args.SkillName)
	if !ok {
		return fmt.Sprintf("Skill %q not found locally. Check available skills with `/skill_search`.", args.SkillName), nil
	}

	// We'll use a callback or context-based injection to actually add the content to the prompt.
	// For now, return a success message that will be seen by the agent.
	// The actual injection happens in the EinoEngine loop.
	return fmt.Sprintf("Skill %q loaded successfully. You now have access to its instructions and tools.", skill.Name), nil
}
