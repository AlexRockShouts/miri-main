package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"net/url"
	"regexp"
	"strings"
)

func TestShouldFallback(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "context deadline",
			err:  context.DeadlineExceeded,
			want: true,
		},
		{
			name: "url timeout",
			err:  &url.Error{Err: context.DeadlineExceeded},
			want: true,
		},
		{
			name: "contains timeout",
			err:  errors.New("read timeout"),
			want: true,
		},
		{
			name: "connection reset by peer",
			err:  errors.New("connection reset by peer"),
			want: true,
		},
		{
			name: "dial tcp",
			err:  errors.New("dial tcp 40.114.177.156:443: i/o timeout"),
			want: true,
		},
		{
			name: "i/o timeout",
			err:  errors.New("i/o timeout"),
			want: true,
		},
		{
			name: "other error",
			err:  errors.New("parse error"),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, shouldFallback(tt.err))
		})
	}
}

func TestDDGParse(t *testing.T) {
	body := `
<a class="result__a" href="https://example.com">Example Title</a>
<a class="result__a" href="https://example2.com"> Title 2 </a>
`
	re := regexp.MustCompile(`<a class="result__a" href="([^"]+)"[^>]*>([^<]+)</a>`)
	matches := re.FindAllStringSubmatch(body, -1)
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
	assert.Len(t, results, 2)
	assert.Equal(t, "https://example.com", results[0]["url"])
	assert.Equal(t, "Example Title", results[0]["title"])
	assert.Equal(t, "Title 2", results[1]["title"])
}

func TestBraveParse(t *testing.T) {
	body := `
<h3 class="title"><a href="https://brave1.com">Brave Title 1</a>
<h3 class="title"><a href="https://brave2.com"> Brave Title 2 </a>
`
	re := regexp.MustCompile(`<h3 class="title"><a href="([^"]+)"[^>]*>([^<]+)</a>`)
	matches := re.FindAllStringSubmatch(body, -1)
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
	assert.Len(t, results, 2)
	assert.Equal(t, "https://brave1.com", results[0]["url"])
	assert.Equal(t, "Brave Title 1", results[0]["title"])
}

func TestInvokableRunUnmarshal(t *testing.T) {
	s := &SearchToolWrapper{}
	_, err := s.InvokableRun(context.Background(), `{"query": "test"}`)
	assert.NoError(t, err) // unmarshal succeeds
	_, err = s.InvokableRun(context.Background(), `{invalid json`)
	assert.Error(t, err)
}
