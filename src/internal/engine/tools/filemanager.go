package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type FileGateway interface {
	ChannelSendFile(channel, device, filePath, caption string) error
}

type FileManagerToolWrapper struct {
	StorageDir string
	gw         FileGateway
}

func NewFileManagerTool(storageDir string, gw FileGateway) *FileManagerToolWrapper {
	return &FileManagerToolWrapper{StorageDir: storageDir, gw: gw}
}

func (f *FileManagerToolWrapper) GetInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "file_manager",
		Desc: "Manage files in the local storage. You can list files and share files with the user via communication channels (WhatsApp, IRC) or by providing a download link.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"action": {
				Type:     schema.String,
				Desc:     "The action to perform: 'list', 'share'",
				Required: true,
			},
			"path": {
				Type:     schema.String,
				Desc:     "The path to the file or directory (relative to storage root)",
				Required: false,
			},
			"channel": {
				Type:     schema.String,
				Desc:     "The channel to share the file to (e.g., 'whatsapp', 'irc')",
				Required: false,
			},
			"device": {
				Type:     schema.String,
				Desc:     "The device/target ID to share the file to",
				Required: false,
			},
			"caption": {
				Type:     schema.String,
				Desc:     "Optional caption for the shared file",
				Required: false,
			},
		}),
	}
}

func (f *FileManagerToolWrapper) Info(_ context.Context) (*schema.ToolInfo, error) {
	return f.GetInfo(), nil
}

func (f *FileManagerToolWrapper) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		Action  string `json:"action"`
		Path    string `json:"path"`
		Channel string `json:"channel"`
		Device  string `json:"device"`
		Caption string `json:"caption"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		slog.Error("failed to unmarshal file manager arguments", "error", err, "arguments", argumentsInJSON)
		return "", err
	}

	storageDir := f.StorageDir
	if strings.HasPrefix(storageDir, "~") {
		home, _ := os.UserHomeDir()
		storageDir = filepath.Join(home, storageDir[1:])
	}

	switch args.Action {
	case "list":
		// List from storageDir (this includes 'generated' if it exists there)
		cleanPath := filepath.Clean("/" + args.Path)
		// Default to .generated if path is empty
		if args.Path == "" || args.Path == "/" {
			cleanPath = "/.generated"
		}
		fullPath := filepath.Join(storageDir, cleanPath)

		// Ensure the directory exists
		if err := os.MkdirAll(fullPath, 0755); err != nil {
			slog.Error("failed to create directory in file manager", "path", fullPath, "error", err)
			return "", err
		}

		entries, err := os.ReadDir(fullPath)
		if err != nil {
			slog.Error("failed to read directory in file manager", "path", fullPath, "error", err)
			return "", err
		}
		var files []string
		for _, e := range entries {
			suffix := ""
			if e.IsDir() {
				suffix = "/"
			}
			files = append(files, e.Name()+suffix)
		}
		return strings.Join(files, "\n"), nil

	case "share":
		if args.Path == "" {
			slog.Warn("path is required for file manager share action")
			return "", fmt.Errorf("path is required for share")
		}
		// Try to find the file in storageDir
		cleanPath := filepath.Clean("/" + args.Path)
		fullPath := filepath.Join(storageDir, cleanPath)

		if _, err := os.Stat(fullPath); err != nil {
			slog.Warn("file not found for sharing", "path", args.Path, "full_path", fullPath)
			return "", fmt.Errorf("file not found in storage: %s", args.Path)
		}

		res := fmt.Sprintf("File available for download at: /api/v1/files%s\n", cleanPath)

		if args.Channel != "" && args.Device != "" && f.gw != nil {
			err := f.gw.ChannelSendFile(args.Channel, args.Device, fullPath, args.Caption)
			if err != nil {
				slog.Error("failed to share file via channel", "channel", args.Channel, "device", args.Device, "path", fullPath, "error", err)
				res += fmt.Sprintf("Failed to share to %s: %v", args.Channel, err)
			} else {
				slog.Info("successfully shared file via channel", "channel", args.Channel, "device", args.Device, "path", fullPath)
				res += fmt.Sprintf("Successfully shared to %s (%s)", args.Channel, args.Device)
			}
		}

		return res, nil

	default:
		return "", fmt.Errorf("invalid action: %s", args.Action)
	}
}
