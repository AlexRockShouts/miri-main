package clawhub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const baseURL = "https://clawhub.ai/api/v1"

// SkillSummary represents a skill in the list endpoint
type SkillSummary struct {
	Slug        string            `json:"slug"`
	DisplayName string            `json:"displayName"`
	Summary     string            `json:"summary"`
	Tags        map[string]string `json:"tags,omitempty"`
	CreatedAt   int64             `json:"createdAt,omitempty"`
	UpdatedAt   int64             `json:"updatedAt,omitempty"`
}

// SkillListResponse is the response from /skills
type SkillListResponse struct {
	Items []SkillSummary `json:"items"`
}

// SkillOwner represents owner data in detail
type SkillOwner struct {
	Handle      string `json:"handle"`
	UserID      string `json:"userId"`
	DisplayName string `json:"displayName"`
	Image       string `json:"image"`
}

// SkillDetailResponse represents /skills/{slug}
type SkillDetailResponse struct {
	Skill struct {
		Slug        string            `json:"slug"`
		DisplayName string            `json:"displayName"`
		Summary     string            `json:"summary"`
		Tags        map[string]string `json:"tags"`
		CreatedAt   int64             `json:"createdAt"`
		UpdatedAt   int64             `json:"updatedAt"`
	} `json:"skill"`
	LatestVersion struct {
		Version   string `json:"version"`
		CreatedAt int64  `json:"createdAt"`
		Changelog string `json:"changelog"`
	} `json:"latestVersion"`
	Owner      *SkillOwner `json:"owner"`
	Moderation any         `json:"moderation"`
}

var httpClient = &http.Client{Timeout: 20 * time.Second}

// ListSkills fetches the list of skills from ClawHub
func ListSkills(ctx context.Context) ([]SkillSummary, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/skills", nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("clawhub list: status %d", resp.StatusCode)
	}
	var out SkillListResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// GetSkill fetches the detail for a given slug
func GetSkill(ctx context.Context, slug string) (*SkillDetailResponse, error) {
	if slug == "" {
		return nil, fmt.Errorf("empty slug")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/skills/"+slug, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("clawhub get %s: status %d", slug, resp.StatusCode)
	}
	var out SkillDetailResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// InstallSkill is unavailable in the rollback state and returns an error.
// Kept to satisfy callers in the engine during rollback.
func InstallSkill(_ context.Context, _ string, _ string) (string, error) {
	return "", fmt.Errorf("InstallSkill is not available in the current rollback state")
}
