package channels

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	"net/http"
	"slices"
)

type Whatsapp struct {
	mu         sync.Mutex
	client     *whatsmeow.Client
	dbpath     string
	msgHandler func(string, string)
	allowlist  []string
	blocklist  []string
}

func NewWhatsapp(storageDir string, allowlist, blocklist []string) *Whatsapp {
	home, err := user.Current()
	if err != nil {
		slog.Error("failed to get user home dir", "error", err)
		return nil
	}

	baseDir := storageDir
	if strings.HasPrefix(storageDir, "~/") {
		baseDir = filepath.Join(home.HomeDir, storageDir[2:])
	}

	whatsappDir := filepath.Join(baseDir, "whatsapp")
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

	w := &Whatsapp{
		mu:        sync.Mutex{},
		client:    client,
		dbpath:    dsn,
		allowlist: allowlist,
		blocklist: blocklist,
	}
	client.AddEventHandler(func(ev interface{}) {
		switch v := ev.(type) {
		case *events.Message:
			if v.Info.IsGroup || v.Info.IsFromMe {
				return
			}
			text := v.Message.GetConversation()
			if text == "" {
				return
			}
			chat := v.Info.Chat.String()
			sender := v.Info.Sender.String()

			// 1) If sender/chat is in blocklist -> silently ignore
			if slices.Contains(w.blocklist, chat) || slices.Contains(w.blocklist, sender) {
				return
			}

			// 2) If sender/chat is in allowlist -> deliver to agent
			if slices.Contains(w.allowlist, chat) || slices.Contains(w.allowlist, sender) {
				w.mu.Lock()
				handler := w.msgHandler
				w.mu.Unlock()
				if handler != nil {
					go handler(chat, text)
				}
				return
			}

			// 3) Otherwise -> send default auto-reply and do not deliver to agent
			jid, err := types.ParseJID(chat)
			if err != nil {
				slog.Warn("whatsapp default reply: invalid JID", "chat", chat, "error", err)
				return
			}
			_, err = w.client.SendMessage(context.Background(), jid, &waProto.Message{Conversation: proto.String("thanks for contact, we call you back !")})
			if err != nil {
				slog.Error("whatsapp default reply send failed", "chat", chat, "error", err)
			}
		}
	})

	// Start client if logged in
	if w.client.Store.ID != nil {
		go func() {
			if err := w.client.Connect(); err != nil {
				slog.Error("whatsapp connect failed", "error", err)
			}
		}()
	} else {
		slog.Info("whatsapp not logged in, use /channels/whatsapp/enroll to get QR")
	}

	return w
}

func (w *Whatsapp) Name() string {
	return "whatsapp"
}

func (w *Whatsapp) Status() map[string]any {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.client == nil {
		return map[string]any{"connected": false, "logged_in": false}
	}
	connected := w.client.IsConnected()
	loggedIn := w.client.Store.ID != nil
	return map[string]any{
		"connected": connected,
		"logged_in": loggedIn,
	}
}

func (w *Whatsapp) Enroll(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.client == nil {
		return fmt.Errorf("whatsapp client not initialized")
	}

	client := w.client

	if client.Store.ID != nil {
		slog.Info("whatsapp: logging out existing session")
		if err := client.Logout(ctx); err != nil {
			return fmt.Errorf("whatsapp logout: %w", err)
		}
	}

	if client.IsConnected() {
		slog.Info("whatsapp: disconnecting")
		client.Disconnect()
	}

	slog.Info("whatsapp: starting QR enrollment")

	go func() {
		qrChan, _ := client.GetQRChannel(context.Background())
		for evt := range qrChan {
			if evt.Event == "code-ok" {
				slog.Info("whatsapp: login successful")
				break
			}
			slog.Info("whatsapp QR code", "code", evt.Code)
			qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
		}
	}()

	go func() {
		if err := client.Connect(); err != nil {
			slog.Error("whatsapp connect failed during enroll", "error", err)
		}
	}()

	return nil
}

func (w *Whatsapp) ListDevices(ctx context.Context) ([]string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}
	if w.client.Store.ID == nil {
		return nil, fmt.Errorf("not logged in")
	}
	return []string{w.client.Store.ID.String()}, nil
}

func (w *Whatsapp) Send(ctx context.Context, deviceID string, msg string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.client == nil {
		return fmt.Errorf("client not initialized")
	}
	jid, err := types.ParseJID(deviceID)
	if err != nil {
		return fmt.Errorf("invalid JID %s: %w", deviceID, err)
	}
	_, err = w.client.SendMessage(ctx, jid, &waProto.Message{
		Conversation: proto.String(msg),
	})
	return err
}

func (w *Whatsapp) SendFile(ctx context.Context, deviceJID string, filePath string, caption string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.client == nil {
		return fmt.Errorf("client not initialized")
	}

	jid, err := types.ParseJID(deviceJID)
	if err != nil {
		return fmt.Errorf("invalid JID %s: %w", deviceJID, err)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	// Upload the media to WhatsApp servers
	resp, err := w.client.Upload(ctx, data, whatsmeow.MediaDocument)
	if err != nil {
		return fmt.Errorf("failed to upload media: %w", err)
	}

	fileName := filepath.Base(filePath)

	msg := &waProto.Message{
		DocumentMessage: &waProto.DocumentMessage{
			URL:           proto.String(resp.URL),
			DirectPath:    proto.String(resp.DirectPath),
			MediaKey:      resp.MediaKey,
			Mimetype:      proto.String(http.DetectContentType(data)),
			FileLength:    proto.Uint64(uint64(len(data))),
			FileSHA256:    resp.FileSHA256,
			FileEncSHA256: resp.FileEncSHA256,
			Title:         proto.String(fileName),
			FileName:      proto.String(fileName),
			Caption:       proto.String(caption),
		},
	}

	_, err = w.client.SendMessage(ctx, jid, msg)
	return err
}

func (w *Whatsapp) SetMessageHandler(handler func(deviceJID, message string)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.msgHandler = handler
}

func (w *Whatsapp) Poll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.client.Store.ID != nil && !w.client.IsConnected() {
		slog.Info("whatsapp loop reconnect")
		go func() {
			if err := w.client.Connect(); err != nil {
				slog.Error("whatsapp reconnect failed", "error", err)
			}
		}()
	}
}
