package api

import (
	"io"
	"log/slog"
	"miri-main/src/internal/engine"
	"miri-main/src/internal/gateway"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func (s *Server) handlePromptStream(c *gin.Context) {
	prompt := c.Query("prompt")
	sessionID := c.Query("session_id")
	modelReq := c.Query("model")

	if prompt == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "prompt query param required"})
		return
	}

	opts := engine.Options{
		Model: modelReq,
	}

	gw := c.MustGet("gateway").(*gateway.Gateway)
	stream, err := gw.PrimaryAgent.DelegatePromptStreamWithOptions(c.Request.Context(), sessionID, prompt, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Stream(func(w io.Writer) bool {
		chunk, ok := <-stream
		if !ok {
			return false
		}
		c.SSEvent("message", chunk)
		return true
	})
}

func (s *Server) handleWebsocket(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)

	channel := c.Query("channel")
	device := c.Query("device")
	sessionID := c.Query("session_id")
	clientID := c.Query("client_id")
	streamReq := c.Query("stream") == "true"

	if channel != "" && device != "" {
		slog.Info("channel WS connected", "channel", channel, "device", device)
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		}
		if _, ok := c.Get("miri_ws_key"); ok {
			requested := c.GetHeader("Sec-WebSocket-Protocol")
			if requested != "" {
				// Subprotocols must be a list of individual protocols.
				// The browser sends them as a comma-separated string in the header.
				parts := strings.Split(requested, ",")
				for i := range parts {
					parts[i] = strings.TrimSpace(parts[i])
				}
				upgrader.Subprotocols = parts
			}
		}
		ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			slog.Error("ws upgrade failed", "error", err)
			return
		}
		defer ws.Close()

		for {
			var msg struct {
				Prompt string `json:"prompt"`
			}
			if err := ws.ReadJSON(&msg); err != nil {
				break
			}

			if streamReq {
				stream, err := gw.PrimaryAgent.DelegatePromptStream(device, msg.Prompt)
				if err != nil {
					ws.WriteJSON(gin.H{"error": err.Error()})
					continue
				}
				for chunk := range stream {
					if err := ws.WriteJSON(gin.H{"response": chunk, "stream": true}); err != nil {
						break
					}
				}
				ws.WriteJSON(gin.H{"stream": false}) // End of stream
			} else {
				resp, err := gw.ChannelChat(channel, device, msg.Prompt)
				if err != nil {
					ws.WriteJSON(gin.H{"error": err.Error()})
					continue
				}

				if err := ws.WriteJSON(gin.H{"response": resp}); err != nil {
					break
				}
			}
		}
		return
	}

	if sessionID == "" {
		sessionID = gw.CreateNewSession(clientID)
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	if _, ok := c.Get("miri_ws_key"); ok {
		// Browser sends multiple sub-protocols, we must split them
		// to satisfy the handshake, as we already verified it in authMiddleware
		requested := c.GetHeader("Sec-WebSocket-Protocol")
		if requested != "" {
			parts := strings.Split(requested, ",")
			for i := range parts {
				parts[i] = strings.TrimSpace(parts[i])
			}
			upgrader.Subprotocols = parts
		}
	}
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		slog.Error("ws upgrade failed", "error", err)
		return
	}
	defer ws.Close()

	// Immediately send session history
	sess := gw.GetSession(sessionID)
	if sess != nil {
		if err := ws.WriteJSON(gin.H{"type": "history", "session": sess}); err != nil {
			slog.Error("failed to send history", "error", err)
		}
	}

	for {
		var msg struct {
			Prompt  string          `json:"prompt"`
			Options *engine.Options `json:"options,omitempty"`
			Stream  *bool           `json:"stream,omitempty"`
		}
		if err := ws.ReadJSON(&msg); err != nil {
			break
		}

		isStreaming := streamReq
		if msg.Stream != nil {
			isStreaming = *msg.Stream
		}

		opts := engine.Options{}
		if msg.Options != nil {
			opts = *msg.Options
		}

		if isStreaming {
			stream, err := gw.PrimaryAgent.DelegatePromptStreamWithOptions(c.Request.Context(), sessionID, msg.Prompt, opts)
			if err != nil {
				ws.WriteJSON(gin.H{"error": err.Error()})
				continue
			}
			for chunk := range stream {
				if err := ws.WriteJSON(gin.H{"response": chunk, "stream": true}); err != nil {
					break
				}
			}
			ws.WriteJSON(gin.H{"stream": false})
		} else {
			response, err := gw.PrimaryAgent.DelegatePromptWithOptions(c.Request.Context(), sessionID, msg.Prompt, opts)
			if err != nil {
				ws.WriteJSON(gin.H{"error": err.Error()})
				continue
			}

			if err := ws.WriteJSON(gin.H{"response": response}); err != nil {
				break
			}
		}
	}
}
