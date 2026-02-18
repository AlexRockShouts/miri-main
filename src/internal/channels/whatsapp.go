package channels

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

type Whatsapp struct {
	mu     sync.Mutex
	client *whatsmeow.Client
	dbpath string
	chatFn func(device, prompt string) (string, error)
}

func NewWhatsapp(storageDir string, chatFn func(device, prompt string) (string, error)) *Whatsapp {
	whatsappDir := filepath.Join(storageDir, "whatsapp")
	if err := os.MkdirAll(whatsappDir, 0755); err != nil {
		slog.Error("failed to create whatsapp dir", "error", err)
		return nil
	}
	dbpath := filepath.Join(whatsappDir, "whatsapp.db")
	dsn := "file:" + dbpath + "?_foreign_keys=on"

	ctx := context.Background()
	container, err := sqlstore.New(ctx, "sqlite3", dsn, nil)
	if err != nil {
		slog.Error("failed to connect to whatsapp store", "error", err)
		return nil
	}

	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		slog.Error("failed to get device store", "error", err)
		return nil
	}

	client := whatsmeow.NewClient(deviceStore, nil)
	client.EnableAutoReconnect = true
	client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Message:
			if !v.Info.IsGroup && !v.Info.IsFromMe {
				prompt := v.Message.GetConversation()
				if prompt != "" {
					slog.Info("whatsapp inbound", "jid", v.Info.Sender.String(), "prompt", prompt[:50])
					resp, err := chatFn(v.Info.Sender.String(), prompt)
					if err != nil {
						slog.Error("whatsapp chat failed", "jid", v.Info.Sender.String(), "error", err)
						return
					}
					// send response
					_, err = client.SendMessage(ctx, v.Info.Sender, &waProto.Message{
						Conversation: proto.String(resp),
					})
					if err != nil {
						slog.Error("whatsapp send failed", "jid", v.Info.Sender.String(), "error", err)
					}
				}
			}
		}
	})

	// Start client if logged in
	if client.Store.ID != nil {
		go func() {
			if err := client.Connect(); err != nil {
				slog.Error("whatsapp connect failed", "error", err)
			}
		}()
	} else {
		slog.Info("whatsapp not logged in, use /channels/whatsapp/enroll to get QR")
	}

	return &Whatsapp{
		client: client,
		dbpath: dsn,
		chatFn: chatFn,
	}
}

func (w *Whatsapp) Name() string {
	return "whatsapp"
}

func (w *Whatsapp) Status() map[string]any {
	w.mu.Lock()
	defer w.mu.Unlock()
	connected := w.client.IsConnected()
	loggedIn := w.client.Store.ID != nil
	return map[string]any{
		"connected": connected,
		"logged_in": loggedIn,
	}
}

func (w *Whatsapp) Enroll(ctx context.Context) error {
	w.mu.Lock()
	client := w.client
	w.mu.Unlock()
	if err := client.Logout(ctx); err != nil {
		return fmt.Errorf("whatsapp logout: %w", err)
	}
	slog.Info("whatsapp logging out, starting new connect for QR")
	go func() {
		qrChan, _ := client.GetQRChannel(ctx)
		for evt := range qrChan {
			if evt.Event == "code-ok" {
				slog.Info("whatsapp login successful")
				break
			}
			slog.Info("whatsapp QR code", "code", evt.Code)
			qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
		}
	}()
	return client.Connect()
}

func (w *Whatsapp) ListDevices(ctx context.Context) ([]string, error) {
	w.mu.Lock()
	client := w.client
	w.mu.Unlock()
	if client.Store.ID == nil {
		return nil, fmt.Errorf("not logged in")
	}
	return []string{client.Store.PushName}, nil
}

func (w *Whatsapp) Send(ctx context.Context, deviceID string, msg string) error {
	w.mu.Lock()
	client := w.client
	w.mu.Unlock()
	jid, err := types.ParseJID(deviceID)
	if err != nil {
		return fmt.Errorf("invalid JID %s: %w", deviceID, err)
	}
	_, err = client.SendMessage(ctx, jid, &waProto.Message{
		Conversation: proto.String(msg),
	})
	return err
}
