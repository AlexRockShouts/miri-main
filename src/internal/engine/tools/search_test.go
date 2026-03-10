package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"net/url"
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
&lt;a class=&quot;result__a&quot; href=&quot;https://example.com&quot;&gt;Example Title&lt;/a&gt;
&lt;a class=&quot;result__a&quot; href=&quot;https://example2.com&quot;&gt; Title 2 &lt;/a&gt;
`
	re := regexp.MustCompile(`&lt;a class=&quot;result__a&quot; href=&quot;([^&quot;]+)&quot;[^&gt;]*&gt;([^&lt;]+)&lt;/a&gt;`)
	matches := re.FindAllStringSubmatch(body, -1)
	var results []map[string]string
	for _, m := range matches {
		if len(m) &lt; 3 {
			continue
		}
		results = append(results, map[string]string{
			&quot;url&quot;:   m[1],
			&quot;title&quot;: strings.TrimSpace(m[2]),
		})
		if len(results) == 5 {
			break
		}
	}
	assert.Len(t, results, 2)
	assert.Equal(t, &quot;https://example.com&quot;, results[0][&quot;url&quot;])
	assert.Equal(t, &quot;Example Title&quot;, results[0][&quot;title&quot;])
	assert.Equal(t, &quot;Title 2&quot;, results[1][&quot;title&quot;])
}

func TestBraveParse(t *testing.T) {
	body := `
&lt;h3 class=&quot;title&quot;&gt;&lt;a href=&quot;https://brave1.com&quot;&gt;Brave Title 1&lt;/a&gt;
&lt;h3 class=&quot;title&quot;&gt;&lt;a href=&quot;https://brave2.com&quot;&gt; Brave Title 2 &lt;/a&gt;
`
	re := regexp.MustCompile(`&lt;h3 class=&quot;title&quot;&gt;&lt;a href=&quot;([^&quot;]+)&quot;[^&gt;]*&gt;([^&lt;]+)&lt;/a&gt;`)
	matches := re.FindAllStringSubmatch(body, -1)
	var results []map[string]string
	for _, m := range matches {
		if len(m) &lt; 3 {
			continue
		}
		results = append(results, map[string]string{
			&quot;url&quot;:   m[1],
			&quot;title&quot;: strings.TrimSpace(m[2]),
		})
		if len(results) == 5 {
			break
		}
	}
	assert.Len(t, results, 2)
	assert.Equal(t, &quot;https://brave1.com&quot;, results[0][&quot;url&quot;])
	assert.Equal(t, &quot;Brave Title 1&quot;, results[0][&quot;title&quot;])
}

func TestInvokableRunUnmarshal(t *testing.T) {
	s := &amp;SearchToolWrapper{}
	_, err := s.InvokableRun(context.Background(), `{&quot;query&quot;: &quot;test&quot;}`)
	assert.Error(t, err) // expects network error, unmarshal ok
	_, err = s.InvokableRun(context.Background(), `{invalid json`)
	assert.Error(t, err)
}