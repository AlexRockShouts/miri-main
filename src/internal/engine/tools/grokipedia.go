package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type GrokipediaToolWrapper struct{}

func (g *GrokipediaToolWrapper) GetInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "grokipedia",
		Desc: "Lookup facts and summaries on the internet (Wikipedia). Use for general knowledge, biographies, history, and science.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "The topic or fact to look up",
				Required: true,
			},
		}),
	}
}

func (g *GrokipediaToolWrapper) Info(_ context.Context) (*schema.ToolInfo, error) {
	return g.GetInfo(), nil
}

func (g *GrokipediaToolWrapper) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		slog.Error("failed to unmarshal grokipedia arguments", "error", err, "arguments", argumentsInJSON)
		return "", err
	}
	result, err := grokipediaSearch(ctx, args.Query)
	if err != nil {
		slog.Error("failed to search grokipedia", "query", args.Query, "error", err)
		return "", err
	}
	return result, nil
}

func grokipediaSearch(ctx context.Context, query string) (string, error) {
	slug := grokipediaNormalizeQuery(query)
	articleURL := fmt.Sprintf("https://grokipedia.com/page/%s", slug)
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", articleURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "MiriBot/1.0 (https://github.com/mirjamagento/miri)")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Sprintf("No results found on Grokipedia for %q.", query), nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code from Grokipedia: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	html := string(body)
	reMeta := regexp.MustCompile(`<meta\s+name="description"\s+content="([^"]+)"`)
	if m := reMeta.FindStringSubmatch(html); len(m) > 1 {
		summary := m[1]
		summary = strings.ReplaceAll(summary, "&#x27;", "'")
		summary = strings.ReplaceAll(summary, "&quot;", "\"")
		summary = strings.ReplaceAll(summary, "&amp;", "&")
		return fmt.Sprintf("Source: Grokipedia (%s)\n\n%s", strings.ReplaceAll(slug, "_", " "), summary), nil
	}
	reTTS := regexp.MustCompile(`(?s)<span\s+data-tts-block="true"[^>]*>(.*?)</span>`)
	if matches := reTTS.FindAllStringSubmatch(html, -1); len(matches) > 0 {
		var content strings.Builder
		for _, m := range matches {
			if len(m) > 1 {
				content.WriteString(grokipediaStripTags(m[1]))
				content.WriteString("\n\n")
			}
		}
		if res := strings.TrimSpace(content.String()); res != "" {
			return fmt.Sprintf("Source: Grokipedia (%s)\n\n%s", strings.ReplaceAll(slug, "_", " "), res), nil
		}
	}
	return "Found Grokipedia page but couldn't retrieve summary.", nil
}

func grokipediaNormalizeQuery(query string) string {
	query = strings.TrimSpace(query)
	if strings.Contains(strings.ToLower(query), "go programming language") {
		return "Go_programming_language"
	}
	parts := strings.Fields(query)
	for i, p := range parts {
		if len(p) > 0 {
			if i == 0 {
				parts[i] = strings.ToUpper(p[:1]) + p[1:]
			} else if grokipediaIsProperNoun(p) {
				parts[i] = strings.ToUpper(p[:1]) + p[1:]
			}
		}
	}
	return strings.Join(parts, "_")
}

func grokipediaIsProperNoun(s string) bool {
	if len(s) == 0 {
		return false
	}
	return s[0] >= 'A' && s[0] <= 'Z'
}

func grokipediaStripTags(h string) string {
	re := regexp.MustCompile("<[^>]*>")
	return re.ReplaceAllString(h, "")
}
