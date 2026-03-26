package resilience

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"
)

// RetryOpts configures the retry behaviour.
type RetryOpts struct {
	MaxAttempts int           // total attempts (including the first); 0 defaults to 3
	BaseDelay   time.Duration // initial back-off; 0 defaults to 1s
	MaxDelay    time.Duration // cap per attempt; 0 defaults to 8s
}

func (o RetryOpts) maxAttempts() int {
	if o.MaxAttempts <= 0 {
		return 3
	}
	return o.MaxAttempts
}

func (o RetryOpts) baseDelay() time.Duration {
	if o.BaseDelay <= 0 {
		return time.Second
	}
	return o.BaseDelay
}

func (o RetryOpts) maxDelay() time.Duration {
	if o.MaxDelay <= 0 {
		return 8 * time.Second
	}
	return o.MaxDelay
}

// IsRetryable returns true for errors that are typically transient.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// HTTP 429 / 500 / 502 / 503 / 504 are retryable
	for _, code := range []int{
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	} {
		if strings.Contains(msg, fmt.Sprintf("%d", code)) {
			return true
		}
	}
	// Common transient network errors
	for _, substr := range []string{
		"connection reset",
		"connection refused",
		"EOF",
		"timeout",
		"temporary failure",
	} {
		if strings.Contains(strings.ToLower(msg), substr) {
			return true
		}
	}
	return false
}

// Retry calls fn up to opts.MaxAttempts times with exponential back-off and
// jitter. Only retryable errors (as determined by IsRetryable) trigger a retry;
// non-retryable errors are returned immediately.
func Retry[T any](ctx context.Context, fn func(context.Context) (T, error), opts RetryOpts) (T, error) {
	maxAttempts := opts.maxAttempts()
	base := opts.baseDelay()
	cap := opts.maxDelay()

	var lastErr error
	var zero T

	for attempt := range maxAttempts {
		result, err := fn(ctx)
		if err == nil {
			return result, nil
		}
		lastErr = err

		if !IsRetryable(err) {
			return zero, err
		}

		if attempt == maxAttempts-1 {
			break
		}

		// Exponential back-off with full jitter
		delay := time.Duration(float64(base) * math.Pow(2, float64(attempt)))
		delay = min(delay, cap)
		jitter := time.Duration(rand.Int64N(int64(delay) + 1))

		slog.Warn("Retryable error, backing off",
			"attempt", attempt+1,
			"max_attempts", maxAttempts,
			"delay", jitter,
			"error", err,
		)

		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(jitter):
		}
	}

	return zero, fmt.Errorf("all %d attempts failed: %w", maxAttempts, lastErr)
}
