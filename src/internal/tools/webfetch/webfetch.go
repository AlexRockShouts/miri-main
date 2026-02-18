package webfetch

import (
	"context"
	"io"
	"net/http"
	"time"
)

func Fetch(ctx context.Context, urlStr string, maxBytes int) (statusCode int, headers http.Header, body string, err error) {
	if maxBytes == 0 {
		maxBytes = 1024 * 1024 // 1MB default
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return 0, nil, "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, "", err
	}
	defer resp.Body.Close()

	bodyBytes := make([]byte, maxBytes)
	n, readErr := resp.Body.Read(bodyBytes)
	if readErr != nil && readErr != io.EOF {
		return 0, nil, "", readErr
	}

	bodyStr := string(bodyBytes[:n])
	if n == maxBytes {
		bodyStr += " [truncated]"
	}

	return resp.StatusCode, resp.Header.Clone(), bodyStr, nil
}