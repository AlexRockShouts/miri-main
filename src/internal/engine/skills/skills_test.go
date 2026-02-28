package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkillLoader_FlatFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skill-loader-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a flat skill file
	skillContent := "---\nname: my-skill\ndescription: A test skill\n---\nBody of the skill."
	skillFile := filepath.Join(tmpDir, "my-skill.md")
	if err := os.WriteFile(skillFile, []byte(skillContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a resource directory with a script
	scriptDir := filepath.Join(tmpDir, "my-skill", "scripts")
	if err := os.MkdirAll(scriptDir, 0755); err != nil {
		t.Fatal(err)
	}
	scriptFile := filepath.Join(scriptDir, "test_script.py")
	if err := os.WriteFile(scriptFile, []byte("#!/usr/bin/env python3\nprint('hello')"), 0755); err != nil {
		t.Fatal(err)
	}

	loader := NewSkillLoader(tmpDir, "scripts")
	if err := loader.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	skills := loader.GetSkills()
	if len(skills) != 1 {
		t.Fatalf("Expected 1 skill, got %d", len(skills))
	}

	skill := skills[0]
	if skill.Name != "my-skill" {
		t.Errorf("Expected skill name 'my-skill', got %q", skill.Name)
	}

	// Check if scripts were loaded
	tools := loader.GetExtraTools()
	found := false
	for _, tool := range tools {
		if info, err := tool.Info(nil); err == nil && info.Name == "test_script" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Extra tool 'test_script' not found; script inference failed for flat skill file")
	}
}

func TestSkillLoader_LegacyDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skill-loader-legacy-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a legacy directory-based skill
	skillDir := filepath.Join(tmpDir, "legacy-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	skillContent := "---\nname: legacy-skill\ndescription: A legacy skill\n---\nLegacy body."
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatal(err)
	}

	loader := NewSkillLoader(tmpDir, "scripts")
	if err := loader.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	skills := loader.GetSkills()
	if len(skills) != 1 {
		t.Fatalf("Expected 1 skill, got %d", len(skills))
	}

	skill := skills[0]
	if skill.Name != "legacy-skill" {
		t.Errorf("Expected skill name 'legacy-skill', got %q", skill.Name)
	}
}
