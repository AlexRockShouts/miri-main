package tools

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type SearchToolWrapper struct{}

func (s *SearchToolWrapper) GetInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "web_search",
		Desc: "Search the web for current information.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "The search query",
				Required: true,
			},
		}),
	}
}

func (s *SearchToolWrapper) Info(_ context.Context) (*schema.ToolInfo, error) {
	return s.GetInfo(), nil
}

func (s *SearchToolWrapper) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", err
	}
	results, err := webSearch(ctx, args.Query)
	if err != nil {
		slog.Error("failed to perform web search", "query", args.Query, "error", err)
		return "", err
	}
	b, _ := json.Marshal(results)
	return string(b), nil
}

func webSearch(ctx context.Context, query string) ([]map[string]string, error) {
	u := "https://duckduckgo.com/html/?q=" + url.QueryEscape(query)
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{DialContext: dialer.DialContext}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	httpReq, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("User-Agent", "Miri-AI-Agent/1.0")
	resp, err := client.Do(httpReq)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		if shouldFallback(err) {
			return braveSearch(ctx, query)
		}
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	re := regexp.MustCompile(`<a class="result__a" href="([^"]+)"[^>]*>([^<]+)</a>`)
	matches := re.FindAllStringSubmatch(string(body), -1)
	var results []map[string]string
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		results = append(results, map[string]string{
			"url":   m[1],
			"title": strings.TrimSpace(m[2]),
		})
		if len(results) == 5 {
			break
		}
	}
	return results, nil
}

func shouldFallback(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "dial tcp") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "deadline exceeded")
}

func braveSearch(ctx context.Context, query string) ([]map[string]string, error) {
	u := "https://search.brave.com/search?q=" + url.QueryEscape(query)
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{DialContext: dialer.DialContext}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	httpReq, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("User-Agent", "Miri-AI-Agent/1.0")
	resp, err := client.Do(httpReq)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	re := regexp.MustCompile(`<h3 class="title"><a href="([^"]+)"[^>]*>([^<]+)</a>`)
	matches := re.FindAllStringSubmatch(string(body), -1)
	var results []map[string]string
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		results = append(results, map[string]string{
			"url":   m[1],
			"title": strings.TrimSpace(m[2]),
		})
		if len(results) == 5 {
			break
		}
	}
	return results, nil
}
