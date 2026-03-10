package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type PinchTabTool struct {
	StorageDir string
	binaryPath string
	basePort   int
	httpClient *http.Client
}

func NewPinchTabTool(storageDir string) *PinchTabTool {
	toolDir := filepath.Join(storageDir, "tools")
	if err := os.MkdirAll(toolDir, 0755); err != nil {
		slog.Error("failed to create tools dir", "path", toolDir, "error", err)
	}
	binPath := filepath.Join(toolDir, "pinchtab")
	pt := &PinchTabTool{
		StorageDir: storageDir,
		binaryPath: binPath,
		basePort:   9867,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
	if err := pt.ensureBinary(); err != nil {
		slog.Warn("failed to ensure pinchtab binary", "error", err)
	}
	pt.ensureServerRunning(context.Background())
	return pt
}

func (pt *PinchTabTool) ensureBinary() error {
	if _, err := os.Stat(pt.binaryPath); err == nil {
		return nil
	}
	slog.Info("downloading pinchtab binary to", "path", pt.binaryPath)
	return pt.downloadBinary()
}

func (pt *PinchTabTool) downloadBinary() error {
	apiURL := "https://api.github.com/repos/pinchtab/pinchtab/releases/latest"
	resp, err := http.Get(apiURL)
	if err != nil {
		return fmt.Errorf("fetch releases: %w", err)
	}
	defer resp.Body.Close()
	var rel struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return fmt.Errorf("decode releases: %w", err)
	}
	goos, goarch := runtime.GOOS, runtime.GOARCH
	assetPattern := fmt.Sprintf("pinchtab-%s-%s", goos, goarch)
	var dlURL string
	for _, asset := range rel.Assets {
		if strings.Contains(asset.Name, assetPattern) {
			dlURL = asset.URL
			break
		}
	}
	if dlURL == "" {
		return fmt.Errorf("no asset matching %s-%s in tag %s", goos, goarch, rel.TagName)
	}
	dlResp, err := http.Get(dlURL)
	if err != nil {
		return fmt.Errorf("download %s: %w", dlURL, err)
	}
	defer dlResp.Body.Close()
	f, err := os.Create(pt.binaryPath)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, dlResp.Body); err != nil {
		return err
	}
	if err := os.Chmod(pt.binaryPath, 0755); err != nil {
		return err
	}
	slog.Info("pinchtab binary downloaded successfully")
	return nil
}

func (pt *PinchTabTool) ensureServerRunning(ctx context.Context) error {
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", pt.basePort)
	resp, err := pt.httpClient.Get(healthURL)
	if err == nil && resp.StatusCode == 200 {
		return nil
	}
	slog.Info("starting pinchtab server on port", "port", pt.basePort)
	c := exec.CommandContext(ctx, pt.binaryPath)
	c.Env = append(os.Environ(),
		fmt.Sprintf("BRIDGE_PORT=%d", pt.basePort),
		"BRIDGE_HEADLESS=true",
	)
	if err := c.Start(); err != nil {
		return fmt.Errorf("start server: %w", err)
	}
	// poll for ready
	for i := 0; i < 30; i++ {
		time.Sleep(time.Second)
		resp, err = pt.httpClient.Get(healthURL)
		if err == nil && resp.StatusCode == 200 {
			slog.Info("pinchtab server ready")
			return nil
		}
	}
	return fmt.Errorf("pinchtab server startup timeout after 30s")
}

func (pt *PinchTabTool) GetInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "pinchtab_browser",
		Desc: "PinchTab headless Chrome automation. Token-efficient accessibility tree snapshots. Commands: nav <url>, snap [-i] [-c], click <ref>, type <ref> <text>, fill <ref> <text>, press <key>, text, ss, eval <js>, pdf, tabs. Refs from snap. Auto-downloads binary, starts server.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"command": {
				Type:     schema.String,
				Desc:     "PinchTab CLI subcommand and args, e.g. \"nav https://pinchtab.com\", \"snap -i -c\", \"click e5\", \"text\". See `pinchtab --help` for full list.",
				Required: true,
			},
		}),
	}
}

func (pt *PinchTabTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return pt.GetInfo(), nil
}

func (pt *PinchTabTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (resStr string, err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in pinchtab_browser tool", "recover", r)
			resStr = fmt.Sprintf(`{"stdout":"","stderr":"internal error: tool panicked","exit_code":-1,"error":"%v"}`, r)
			err = nil
		}
	}()
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		slog.Error("failed to unmarshal pinchtab args", "error", err, "input", argumentsInJSON)
		return "", err
	}
	if args.Command == "" {
		return `{"stdout":"","stderr":"command required","exit_code":1,"error":"command required"}`, nil
	}
	// basic sanitization
	if strings.ContainsAny(args.Command, ";` $|&") {
		return `{"stdout":"","stderr":"invalid characters in command","exit_code":1,"error":"invalid chars"}`, nil
	}
	if err := pt.ensureServerRunning(ctx); err != nil {
		slog.Error("pinchtab server ensure failed", "error", err)
		return fmt.Sprintf(`{"stdout":"","stderr":"server error: %s","exit_code":1,"error":"%s"}`, err, err), nil
	}
	fields := strings.Fields(args.Command)
	if len(fields) == 0 {
		return `{"stdout":"","stderr":"empty command","exit_code":1,"error":"empty command"}`, nil
	}
	slog.Info("pinchtab executing", "command", args.Command)
	cmd := exec.CommandContext(ctx, pt.binaryPath, fields...)
	cmd.Dir = pt.StorageDir
	var stdoutB, stderrB bytes.Buffer
	cmd.Stdout = &stdoutB
	cmd.Stderr = &stderrB
	runErr := cmd.Run()
	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	stdout := stdoutB.String()
	stderr := stderrB.String()
	const maxOutput = 16384
	if len(stdout) > maxOutput {
		stdout = stdout[:maxOutput] + "\n... (truncated)"
	}
	if len(stderr) > maxOutput {
		stderr = stderr[:maxOutput] + "\n... (truncated)"
	}
	res := struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exit_code"`
		Error    string `json:"error,omitempty"`
		Result   string `json:"result,omitempty"`
	}{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: exitCode,
	}
	if runErr != nil {
		res.Error = runErr.Error()
		slog.Warn("pinchtab command failed", "command", args.Command, "error", runErr, "exit_code", exitCode)
	} else if exitCode != 0 {
		slog.Warn("pinchtab non-zero exit", "command", args.Command, "exit_code", exitCode)
	} else {
		res.Result = stdout
		slog.Debug("pinchtab success", "command", args.Command)
	}
	b, _ := json.Marshal(res)
	resStr = string(b)
	return resStr, nil
}
