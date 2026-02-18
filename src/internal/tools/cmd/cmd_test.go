package cmd

import (
	"context"
	"testing"
	"time"
)

func TestExecute(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		cmd        string
		ctxTimeout time.Duration
		wantStdout string
		wantStderr string
		wantCode   int
		wantErr    bool
	}{
		{
			name:       "echo success",
			cmd:        `echo "hello"`,
			ctxTimeout: 5 * time.Second,
			wantStdout: "hello\n",
			wantStderr: "",
			wantCode:   0,
			wantErr:    false,
		},
		{
			name:       "exit fail",
			cmd:        `exit 1`,
			ctxTimeout: 5 * time.Second,
			wantStdout: "",
			wantStderr: "",
			wantCode:   1,
			wantErr:    true,
		},
		{
			name:       "stderr",
			cmd:        `echo "err" >&2; exit 0`,
			ctxTimeout: 5 * time.Second,
			wantStdout: "",
			wantStderr: "err\n",
			wantCode:   0,
			wantErr:    false,
		},
		{
			name:       "timeout",
			cmd:        `sleep 15`,
			ctxTimeout: 1 * time.Second,
			wantStdout: "",
			wantStderr: "",
			wantCode:   0, // may vary
			wantErr:    true,
		},
		{
			name:       "empty",
			cmd:        "",
			ctxTimeout: 1 * time.Second,
			wantStdout: "",
			wantStderr: "",
			wantCode:   0,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		tt := tt // capture
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), tt.ctxTimeout)
			defer cancel()

			stdout, stderr, code, err := Execute(ctx, tt.cmd)

			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if stdout != tt.wantStdout {
					t.Errorf("Execute() stdout = %q, want %q", stdout, tt.wantStdout)
				}
				if stderr != tt.wantStderr {
					t.Errorf("Execute() stderr = %q, want %q", stderr, tt.wantStderr)
				}
				if code != tt.wantCode {
					t.Errorf("Execute() code = %d, want %d", code, tt.wantCode)
				}
			}
		})
	}
}
