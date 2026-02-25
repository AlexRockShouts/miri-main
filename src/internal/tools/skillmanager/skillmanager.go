package skillmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var BaseURL = "https://agentskill.sh"

// SearchAndInstall executes the skill installation from agentskill.sh
func SearchAndInstall(ctx context.Context, skillName, storageDir string) (stdout, stderr string, exitCode int, err error) {
	ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()

	skillsDir := filepath.Join(storageDir, "skills")

	// Check if already installed
	if _, err := os.Stat(filepath.Join(skillsDir, skillName)); err == nil {
		return "Skill already installed locally.", "", 0, nil
	}

	if _, err := os.Stat(skillsDir); os.IsNotExist(err) {
		if err := os.MkdirAll(skillsDir, 0755); err != nil {
			return "", "", 0, fmt.Errorf("failed to create skills directory: %w", err)
		}
	}

	// Try multiple variations of the name
	candidates := []string{skillName}
	if strings.Contains(skillName, "_") {
		candidates = append(candidates, strings.ReplaceAll(skillName, "_", "-"))
	}
	if strings.Contains(skillName, "/") {
		parts := strings.Split(skillName, "/")
		candidates = append(candidates, parts[len(parts)-1])
		if strings.Contains(parts[len(parts)-1], "_") {
			candidates = append(candidates, strings.ReplaceAll(parts[len(parts)-1], "_", "-"))
		}
	}

	var ghFallback map[string]string // github info for direct fallback download if install script 404s

	// Also try fetching the remote list to see if we can find a better name/slug
	if remoteSkills, err := ListRemoteSkills(ctx); err == nil {
		if data, ok := remoteSkills.(map[string]any); ok {
			if list, ok := data["data"].([]any); ok {
				for _, s := range list {
					if skill, ok := s.(map[string]any); ok {
						slug, _ := skill["slug"].(string)
						name, _ := skill["name"].(string)
						ghPath, _ := skill["githubPath"].(string)
						owner, _ := skill["githubOwner"].(string)
						repo, _ := skill["githubRepo"].(string)
						branch, _ := skill["githubBranch"].(string)

						// Match against slug, name, or slug with underscores replaced
						if slug == skillName || name == skillName || strings.ReplaceAll(slug, "-", "_") == skillName || strings.ReplaceAll(name, "-", "_") == skillName {
							if name != "" && name != skillName {
								candidates = append(candidates, name)
							}
							if slug != "" && slug != skillName {
								candidates = append(candidates, slug)
							}
							if owner != "" && repo != "" && ghPath != "" {
								if branch == "" {
									branch = "main"
								}
								ghFallback = map[string]string{
									"owner":  owner,
									"repo":   repo,
									"path":   ghPath,
									"branch": branch,
									"name":   name,
								}
								if ghFallback["name"] == "" {
									ghFallback["name"] = slug
								}
							}

							if ghPath != "" {
								ghParts := strings.Split(ghPath, "/")
								for i := len(ghParts) - 1; i >= 0; i-- {
									p := strings.TrimSuffix(ghParts[i], ".md")
									p = strings.TrimSuffix(p, "SKILL")
									if p != "" && p != "SKILL" && p != "skills" {
										candidates = append(candidates, p)
										break
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// Deduplicate candidates
	uniqueCandidates := make([]string, 0, len(candidates))
	seen := make(map[string]bool)
	for _, c := range candidates {
		if c != "" && !seen[c] {
			uniqueCandidates = append(uniqueCandidates, c)
			seen[c] = true
		}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	var lastErr error
	var finalURL string

	for _, name := range uniqueCandidates {
		url := fmt.Sprintf("%s/install/%s", BaseURL, name)
		req, _ := http.NewRequestWithContext(ctx, "HEAD", url, nil)
		resp, headErr := client.Do(req)
		if headErr == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				finalURL = url
				break
			}
			lastErr = fmt.Errorf("skill %q not found (404)", name)
		} else {
			lastErr = headErr
		}
	}

	if finalURL == "" {
		if ghFallback != nil {
			// Try to download directly from GitHub as a fallback
			rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s",
				ghFallback["owner"], ghFallback["repo"], ghFallback["branch"], ghFallback["path"])

			slog.Info("agentskill.sh install script not found, trying GitHub fallback", "url", rawURL)

			resp, err := http.Get(rawURL)
			if err == nil && resp.StatusCode == http.StatusOK {
				defer resp.Body.Close()

				// Clean name for directory
				safeName := strings.ReplaceAll(ghFallback["name"], "/", "-")
				skillDir := filepath.Join(skillsDir, safeName)
				if err := os.MkdirAll(skillDir, 0755); err != nil {
					return "", "", 0, fmt.Errorf("failed to create skill directory: %w", err)
				}

				f, err := os.Create(filepath.Join(skillDir, "SKILL.md"))
				if err != nil {
					return "", "", 0, fmt.Errorf("failed to create SKILL.md: %w", err)
				}
				defer f.Close()

				_, err = io.Copy(f, resp.Body)
				if err != nil {
					return "", "", 0, fmt.Errorf("failed to save SKILL.md: %w", err)
				}

				return fmt.Sprintf("Skill %q installed via direct GitHub download (fallback).", ghFallback["name"]), "", 0, nil
			}
		}

		return "", fmt.Sprintf("Skill %q not found on %s after trying variations: %v. Verify the skill name or try /skill_list_remote.", skillName, BaseURL, uniqueCandidates), 1, lastErr
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", "curl -fsSL "+finalURL+" | sh")
	cmd.Env = append(os.Environ(), "MIRI_SKILLS_DIR="+skillsDir)

	stdoutB := &bytes.Buffer{}
	stderrB := &bytes.Buffer{}
	cmd.Stdout = stdoutB
	cmd.Stderr = stderrB

	err = cmd.Run()

	exitCode = 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		exitCode = exitErr.ExitCode()
	}

	return stdoutB.String(), stderrB.String(), exitCode, err
}

// SearchAndInstallStream executes the skill installation and streams combined stdout/stderr.
func SearchAndInstallStream(ctx context.Context, skillName, storageDir string) (io.ReadCloser, error) {
	skillsDir := filepath.Join(storageDir, "skills")
	if _, err := os.Stat(skillsDir); os.IsNotExist(err) {
		if err := os.MkdirAll(skillsDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create skills directory: %w", err)
		}
	}

	url := fmt.Sprintf("%s/install/%s", BaseURL, skillName)
	cmd := exec.CommandContext(ctx, "sh", "-c", "curl -fsSL "+url+" | sh")
	cmd.Env = append(os.Environ(), "MIRI_SKILLS_DIR="+skillsDir)

	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		pw.Close()
		return nil, err
	}

	go func() {
		err := cmd.Wait()
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				pw.CloseWithError(fmt.Errorf("exit code %d: %w", exitErr.ExitCode(), err))
			} else {
				pw.CloseWithError(err)
			}
		} else {
			pw.Close()
		}
	}()

	return pr, nil
}

// ListRemoteSkills fetches the list of available skills from agentskill.sh
func ListRemoteSkills(ctx context.Context) (any, error) {
	url := fmt.Sprintf("%s/api/skills", BaseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch remote skills: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}

// RemoveSkill deletes the specified skill from the local skills directory
func RemoveSkill(skillName, storageDir string) error {
	skillsDir := filepath.Join(storageDir, "skills")
	// Safety check: ensure skillName doesn't contain path traversal
	if filepath.Base(skillName) != skillName {
		return fmt.Errorf("invalid skill name: %s", skillName)
	}

	skillPath := filepath.Join(skillsDir, skillName)
	if _, err := os.Stat(skillPath); err == nil {
		return os.RemoveAll(skillPath)
	}

	// Try with name variations (hyphen/underscore)
	normalized := strings.ReplaceAll(skillName, "_", "-")
	skillPath = filepath.Join(skillsDir, normalized)
	if _, err := os.Stat(skillPath); err == nil {
		return os.RemoveAll(skillPath)
	}

	normalized = strings.ReplaceAll(skillName, "-", "_")
	skillPath = filepath.Join(skillsDir, normalized)
	if _, err := os.Stat(skillPath); err == nil {
		return os.RemoveAll(skillPath)
	}

	return fmt.Errorf("skill not found: %s", skillName)
}
