package grokipedia

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Search grokipedia.com and return a summary of the article.
func Search(ctx context.Context, query string) (string, error) {
	slug := normalizeQuery(query)
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

	// 1. Try to extract summary from meta description
	// <meta name="description" content="..."/>
	reMeta := regexp.MustCompile(`<meta\s+name="description"\s+content="([^"]+)"`)
	matchMeta := reMeta.FindStringSubmatch(html)
	if len(matchMeta) > 1 {
		summary := matchMeta[1]
		// Unescape common HTML entities
		summary = strings.ReplaceAll(summary, "&#x27;", "'")
		summary = strings.ReplaceAll(summary, "&quot;", "\"")
		summary = strings.ReplaceAll(summary, "&amp;", "&")
		return fmt.Sprintf("Source: Grokipedia (%s)\n\n%s", strings.ReplaceAll(slug, "_", " "), summary), nil
	}

	// 2. Fallback: extract text from data-tts-block="true" spans
	reTTS := regexp.MustCompile(`(?s)<span\s+data-tts-block="true"[^>]*>(.*?)</span>`)
	matchesTTS := reTTS.FindAllStringSubmatch(html, -1)
	if len(matchesTTS) > 0 {
		var content strings.Builder
		for _, m := range matchesTTS {
			if len(m) > 1 {
				// Strip inner HTML tags
				text := stripTags(m[1])
				content.WriteString(text)
				content.WriteString("\n\n")
			}
		}
		res := strings.TrimSpace(content.String())
		if res != "" {
			return fmt.Sprintf("Source: Grokipedia (%s)\n\n%s", strings.ReplaceAll(slug, "_", " "), res), nil
		}
	}

	return "Found Grokipedia page but couldn't retrieve summary.", nil
}

func normalizeQuery(query string) string {
	// Simple normalization: spaces to underscores.
	// We try to match Grokipedia's slug format.
	// If it contains "programming language", it's likely lowercase.
	query = strings.TrimSpace(query)

	// Handle special cases based on sample
	if strings.Contains(strings.ToLower(query), "go programming language") {
		return "Go_programming_language"
	}

	parts := strings.Fields(query)
	for i, p := range parts {
		if len(p) > 0 {
			if i == 0 {
				// Always capitalize first word
				parts[i] = strings.ToUpper(p[:1]) + p[1:]
			} else if isProperNoun(p) {
				parts[i] = strings.ToUpper(p[:1]) + p[1:]
			} else {
				// For other words, try to keep original casing if it's already capitalized,
				// otherwise maybe lowercase? Wikipedia usually keeps original.
				// But Grokipedia sample showed lowercase for "programming_language".
			}
		}
	}
	return strings.Join(parts, "_")
}

func isProperNoun(s string) bool {
	// Very simple heuristic
	if len(s) == 0 {
		return false
	}
	return s[0] >= 'A' && s[0] <= 'Z'
}

func stripTags(h string) string {
	re := regexp.MustCompile("<[^>]*>")
	return re.ReplaceAllString(h, "")
}
