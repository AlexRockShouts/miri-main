package tools

import (
	"context"
	"encoding/json"
	"fmt"
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
		return "", err
	}

	storageDir := f.StorageDir
	if strings.HasPrefix(storageDir, "~") {
		home, _ := os.UserHomeDir()
		storageDir = filepath.Join(home, storageDir[1:])
	}

	switch args.Action {
	case "list":
		// Force listing from the generated directory
		genDir := filepath.Join(storageDir, "generated")
		fullPath := filepath.Join(genDir, filepath.Clean("/"+args.Path))

		entries, err := os.ReadDir(fullPath)
		if err != nil {
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
			return "", fmt.Errorf("path is required for share")
		}
		// Ensure the path is within the generated directory
		genDir := filepath.Join(storageDir, "generated")
		cleanPath := filepath.Clean("/" + args.Path)
		fullPath := filepath.Join(genDir, cleanPath)

		if _, err := os.Stat(fullPath); err != nil {
			return "", fmt.Errorf("file not found in generated folder: %s", args.Path)
		}

		res := fmt.Sprintf("File available for download at: /api/v1/files%s\n", cleanPath)

		if args.Channel != "" && args.Device != "" && f.gw != nil {
			err := f.gw.ChannelSendFile(args.Channel, args.Device, fullPath, args.Caption)
			if err != nil {
				res += fmt.Sprintf("Failed to share to %s: %v", args.Channel, err)
			} else {
				res += fmt.Sprintf("Successfully shared to %s (%s)", args.Channel, args.Device)
			}
		}

		return res, nil

	default:
		return "", fmt.Errorf("invalid action: %s", args.Action)
	}
}
