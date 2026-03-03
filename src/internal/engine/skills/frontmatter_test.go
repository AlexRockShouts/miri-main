package skills

import (
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	// Case 1: No frontmatter (should be treated as body, not an error)
	content := "# Test Skill\nNo frontmatter here."
	yamlPart, bodyPart, err := parseFrontmatter(content)
	if err != nil {
		t.Errorf("Unexpected error for missing frontmatter: %v", err)
	}
	if yamlPart != "" {
		t.Errorf("Expected empty yamlPart, got '%s'", yamlPart)
	}
	if bodyPart != content {
		t.Errorf("Expected bodyPart to be full content, got '%s'", bodyPart)
	}

	// Case 2: Standard YAML frontmatter
	content2 := "---\nname: test\n---\nBody"
	yamlPart, bodyPart, err = parseFrontmatter(content2)
	if err != nil {
		t.Errorf("Unexpected error for valid frontmatter: %v", err)
	}
	if yamlPart != "name: test" {
		t.Errorf("Expected yamlPart 'name: test', got '%s'", yamlPart)
	}
	if bodyPart != "Body" {
		t.Errorf("Expected bodyPart 'Body', got '%s'", bodyPart)
	}

	// Case 3: agentskill.sh format
	content3 := "# --- agentskill.sh ---\n# name: shell-test\n# ---\necho hello"
	yamlPart, bodyPart, err = parseFrontmatter(content3)
	if err != nil {
		t.Errorf("Unexpected error for agentskill.sh frontmatter: %v", err)
	}
	if yamlPart != "name: shell-test" {
		t.Errorf("Expected yamlPart 'name: shell-test', got '%s'", yamlPart)
	}
	if bodyPart != "echo hello" {
		t.Errorf("Expected bodyPart 'echo hello', got '%s'", bodyPart)
	}
}
