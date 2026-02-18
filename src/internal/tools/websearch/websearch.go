package websearch

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

func Search(ctx context.Context, query string) ([]map[string]string, error) {
	u := "https://duckduckgo.com/html/?q=" + url.QueryEscape(query)

	client := &http.Client{Timeout: 30 * time.Second}
	httpReq, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(httpReq)
	if err != nil {
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
