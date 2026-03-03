package api

import (
	"io"
	"log/slog"
	"miri-main/src/internal/engine"
	"miri-main/src/internal/gateway"
	"miri-main/src/internal/session"
	"net/http"
	"strings"
	"time"

	"slices"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func (s *Server) handlePromptStream(c *gin.Context) {
	var q PromptQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		s.sendError(c, http.StatusBadRequest, err.Error())
		return
	}

	opts := engine.Options{
		Model: q.Model,
	}

	promptsTotal.Inc()

	gw := c.MustGet("gateway").(*gateway.Gateway)
	stream, err := gw.PrimaryAgent.DelegatePromptStreamWithOptions(c.Request.Context(), session.DefaultSessionID, q.Prompt, opts)
	if err != nil {
		s.sendError(c, http.StatusInternalServerError, err.Error())
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
	const (
		writeWait      = 10 * time.Second
		pongWait       = 60 * time.Second
		pingPeriod     = (pongWait * 9) / 10
		maxMessageSize = 524288
	)

	gw := c.MustGet("gateway").(*gateway.Gateway)

	channel := c.Query("channel")
	device := c.Query("device")
	streamReq := c.Query("stream") == "true"

	if channel != "" && device != "" {
		slog.Info("channel WS connected", "channel", channel, "device", device)
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
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

		ws.SetReadLimit(int64(maxMessageSize))
		pingTicker := time.NewTicker(pingPeriod)
		defer pingTicker.Stop()
		ws.SetReadDeadline(time.Now().Add(pongWait))
		ws.SetPongHandler(func(appData string) error {
			ws.SetReadDeadline(time.Now().Add(pongWait))
			return nil
		})

		for {
			ws.SetReadDeadline(time.Now().Add(pongWait))
			var msg struct {
				Prompt string `json:"prompt"`
			}
			if err := ws.ReadJSON(&msg); err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					slog.Warn("channel WS unexpected close", "err", err)
				}
				break
			}

			if streamReq {
				stream, err := gw.PrimaryAgent.DelegatePromptStream(device, msg.Prompt)
				if err != nil {
					s.sendWSError(ws, http.StatusInternalServerError, err.Error())
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
					s.sendWSError(ws, http.StatusInternalServerError, err.Error())
					continue
				}

				if err := ws.WriteJSON(gin.H{"response": resp}); err != nil {
					break
				}
			}

			// Send ping if ticker fired during processing
			select {
			case <-pingTicker.C:
				ws.SetWriteDeadline(time.Now().Add(writeWait))
				if err := ws.WriteMessage(websocket.PingMessage, nil); err != nil {
					slog.Error("channel WS ping failed", "err", err)
					return
				}
			default:
			}
		}
		return
	}

	sessionID := session.DefaultSessionID

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
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

	ws.SetReadLimit(int64(maxMessageSize))
	pingTicker := time.NewTicker(pingPeriod)
	defer pingTicker.Stop()
	ws.SetReadDeadline(time.Now().Add(pongWait))
	ws.SetPongHandler(func(appData string) error {
		ws.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	s.wsMu.Lock()
	s.wsSessions[sessionID] = append(s.wsSessions[sessionID], ws)
	s.wsMu.Unlock()

	defer func() {
		s.wsMu.Lock()
		defer s.wsMu.Unlock()
		conns := s.wsSessions[sessionID]
		s.wsSessions[sessionID] = slices.DeleteFunc(conns, func(c *websocket.Conn) bool {
			return c == ws
		})
		if len(s.wsSessions[sessionID]) == 0 {
			delete(s.wsSessions, sessionID)
		}
	}()

	for {
		ws.SetReadDeadline(time.Now().Add(pongWait))
		var msg struct {
			Prompt  string          `json:"prompt"`
			Options *engine.Options `json:"options,omitempty"`
			Stream  *bool           `json:"stream,omitempty"`
		}
		if err := ws.ReadJSON(&msg); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Warn("default session WS unexpected close", "session", sessionID, "err", err)
			}
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
				s.sendWSError(ws, http.StatusInternalServerError, err.Error())
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
				s.sendWSError(ws, http.StatusInternalServerError, err.Error())
				continue
			}

			if err := ws.WriteJSON(gin.H{"response": response}); err != nil {
				break
			}
		}

		// Send ping if ticker fired during processing
		select {
		case <-pingTicker.C:
			ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				slog.Error("default session WS ping failed", "session", sessionID, "err", err)
				return
			}
		default:
		}
	}
}
