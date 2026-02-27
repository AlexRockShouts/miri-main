package channels

import (
	"context"
)

type Channel interface {
	Name() string
	Status() map[string]any
	Enroll(ctx context.Context) error
	ListDevices(ctx context.Context) ([]string, error)
	Send(ctx context.Context, deviceID string, msg string) error
	SendFile(ctx context.Context, deviceID string, filePath string, caption string) error
}

type Processor interface {
	ChannelChat(channel, device, prompt string) (string, error)
}
