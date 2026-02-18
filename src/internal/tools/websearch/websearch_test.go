package websearch

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

func TestSearchParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		wantLen   int
		wantURL   string
		wantTitle string
	}{
		{
			name:      "single result",
			body:      `<a class="result__a" href="https://example.com">Example Title</a>`,
			wantLen:   1,
			wantURL:   "https://example.com",
			wantTitle: "Example Title",
		},
		{
			name:    "no match",
			body:    `<html>no results</html>`,
			wantLen: 0,
		},
		{
			name:      "multiple top5",
			body:      `<a class="result__a" href="https://a.com">A</a><a class="result__a" href="https://b.com">B</a><a class="result__a" href="https://c.com">C</a><a class="result__a" href="https://d.com">D</a><a class="result__a" href="https://e.com">E</a><a class="result__a" href="https://f.com">F</a>`,
			wantLen:   6,
			wantURL:   "https://a.com",
			wantTitle: "A",
		},
	}

	re := regexp.MustCompile(`<a class="result__a" href="([^"]+)"[^>]*>([^<]+)</a>`)

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			matches := re.FindAllStringSubmatch(tt.body, -1)
			if len(matches) != tt.wantLen {
				t.Errorf("got %d matches, want %d", len(matches), tt.wantLen)
			}
			if tt.wantLen > 0 {
				m := matches[0]
				if len(m) != 3 {
					t.Errorf("match len %d want 3", len(m))
				}
				if m[1] != tt.wantURL {
					t.Errorf("url = %q want %q", m[1], tt.wantURL)
				}
				if tt.wantTitle != "" && strings.TrimSpace(m[2]) != tt.wantTitle {
					t.Errorf("title = %q want %q", strings.TrimSpace(m[2]), tt.wantTitle)
				}
			}
		})
	}
}

func TestSearch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	results, err := Search(ctx, "golang")
	if err != nil {
		t.Errorf("Search() error = %v", err)
		return
	}
	t.Logf("got %d results from DDG (may be 0 if UI changed)", len(results))
}
