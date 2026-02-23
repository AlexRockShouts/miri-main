package api

import (
	"context"
	"io"
	"log/slog"
	"miri-main/src/internal/config"
	"miri-main/src/internal/engine"
	"miri-main/src/internal/gateway"
	"miri-main/src/internal/storage"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type Server struct {
	Gateway *gateway.Gateway
	Engine  *gin.Engine
}

func NewServer(gw *gateway.Gateway) *Server {
	e := gin.Default()
	s := &Server{
		Gateway: gw,
		Engine:  e,
	}
	s.Engine.Use(s.injectMiddleware())
	s.Engine.Use(s.authMiddleware())
	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	s.Engine.GET("/config", s.handleGetConfig)
	s.Engine.GET("/prompt/stream", s.handlePromptStream)
	s.Engine.POST("/config", s.handleUpdateConfig)
	s.Engine.POST("/prompt", s.handlePrompt)
	s.Engine.POST("/human", s.handleSaveHumanInfo)
	s.Engine.GET("/human", s.handleListHumanInfo)
	s.Engine.POST("/interaction", s.handleInteraction)
	s.Engine.GET("/ws", s.handleWebsocket)
	s.Engine.POST("/channels", s.handleChannels)
	s.Engine.GET("/sessions", s.handleListSessions)
	s.Engine.GET("/sessions/:id", s.handleGetSession)
	s.Engine.GET("/sessions/:id/history", s.handleGetSessionHistory)
}

func (s *Server) handleGetConfig(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	c.JSON(http.StatusOK, gw.Config)
}

func (s *Server) handleUpdateConfig(c *gin.Context) {
	var cfg config.Config
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := config.Save(&cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	gw := c.MustGet("gateway").(*gateway.Gateway)
	gw.UpdateConfig(&cfg)
	c.JSON(http.StatusOK, gin.H{"status": "config updated"})
}

func (s *Server) handlePrompt(c *gin.Context) {
	var req struct {
		Prompt      string          `json:"prompt"`
		SessionID   string          `json:"session_id,omitempty"`
		Model       string          `json:"model,omitempty"`
		Engine      string          `json:"engine,omitempty"`
		Temperature *float32        `json:"temperature,omitempty"`
		MaxTokens   *int            `json:"max_tokens,omitempty"`
		Options     *engine.Options `json:"options,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Merge flat options into engine.Options if provided
	opts := engine.Options{}
	if req.Options != nil {
		opts = *req.Options
	}
	if req.Model != "" {
		opts.Model = req.Model
	}
	if req.Engine != "" {
		opts.Engine = req.Engine
	}
	if req.Temperature != nil {
		opts.Temperature = req.Temperature
	}
	if req.MaxTokens != nil {
		opts.MaxTokens = req.MaxTokens
	}

	gw := c.MustGet("gateway").(*gateway.Gateway)
	response, err := gw.PrimaryAgent.DelegatePromptWithOptions(c.Request.Context(), req.SessionID, req.Prompt, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"response": response})
}

func (s *Server) handlePromptStream(c *gin.Context) {
	prompt := c.Query("prompt")
	sessionID := c.Query("session_id")
	engineReq := c.Query("engine")
	modelReq := c.Query("model")

	if prompt == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "prompt query param required"})
		return
	}

	opts := engine.Options{
		Engine: engineReq,
		Model:  modelReq,
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

func (s *Server) handleSaveHumanInfo(c *gin.Context) {
	var info storage.HumanInfo
	if err := c.ShouldBindJSON(&info); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	gw := c.MustGet("gateway").(*gateway.Gateway)
	if err := gw.SaveHumanInfo(&info); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "human info saved"})
}

func (s *Server) handleListHumanInfo(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	infos, err := gw.ListHumanInfo()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, infos)
}

type ChannelActionReq struct {
	Channel string `json:"channel" binding:"required"`
	Action  string `json:"action" binding:"required,oneof=status enroll send devices chat"`
	Device  string `json:"device,omitempty"`
	Message string `json:"message,omitempty"`
	Prompt  string `json:"prompt,omitempty"`
}

func (s *Server) handleChannels(c *gin.Context) {
	var req ChannelActionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	gw := c.MustGet("gateway").(*gateway.Gateway)
	ctx := c.Request.Context()

	switch req.Action {
	case "status":
		status := gw.ChannelStatus(req.Channel)
		c.JSON(http.StatusOK, status)
	case "enroll":
		if err := gw.ChannelEnroll(req.Channel, ctx); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "enroll started, check logs for QR"})
	case "devices":
		devs, err := gw.ChannelListDevices(req.Channel)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"devices": devs})
	case "send":
		if req.Device == "" || req.Message == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "device and message required for send"})
			return
		}
		if err := gw.ChannelSend(req.Channel, req.Device, req.Message); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "sent"})
	case "chat":
		if req.Device == "" || req.Prompt == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "device and prompt required for chat"})
			return
		}
		resp, err := gw.ChannelChat(req.Channel, req.Device, req.Prompt)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"response": resp})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid action"})
	}
}

func (s *Server) handleInteraction(c *gin.Context) {
	type SendReq struct {
		JID     string `json:"jid" binding:"required"`
		Message string `json:"message" binding:"required"`
	}

	type InteractionReq struct {
		Action   string `json:"action" binding:"required,oneof=new status"`
		ClientID string `json:"client_id,omitempty"`
	}
	var req InteractionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	gw := c.MustGet("gateway").(*gateway.Gateway)

	switch req.Action {
	case "new":
		if req.ClientID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "client_id required for new"})
			return
		}
		sessionID := gw.CreateNewSession(req.ClientID)
		c.JSON(http.StatusOK, gin.H{"session_id": sessionID})
	case "status":
		chs := map[string]map[string]any{}
		for k, ch := range gw.Channels {
			chs[k] = ch.Status()
		}
		c.JSON(http.StatusOK, gin.H{
			"primary_model": gw.PrimaryAgent.PrimaryModel(),
			"num_subagents": gw.NumSubAgents(),
			"sessions":      gw.ListSessions(),
			"channels":      chs,
		})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid action; must be 'new' or 'status'"})
	}
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

	if sessionID == "" && clientID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id or client_id query param required"})
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

	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		slog.Error("ws upgrade failed", "error", err)
		return
	}
	defer ws.Close()

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

func (s *Server) injectMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("gateway", s.Gateway)
		c.Next()
	}
}

func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		gw := c.MustGet("gateway").(*gateway.Gateway)
		key := gw.Config.Server.Key
		if key == "" {
			c.Next()
			return
		}
		provided := c.GetHeader("X-Server-Key")
		if provided != key {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or missing server key"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func (s *Server) Run(addr string) error {
	return s.Engine.Run(addr)
}

func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Engine,
		ReadHeaderTimeout: 60 * time.Second,
		ReadTimeout:       600 * time.Second,
		WriteTimeout:      600 * time.Second,
		IdleTimeout:       1200 * time.Second,
	}

	go func() {
		slog.Info("server listening", "addr", addr)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed && err != nil {
			slog.Error("server ListenAndServe error", "error", err)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down server...")

	ctxShut, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctxShut); err != nil {
		slog.Error("server graceful shutdown error", "error", err)
	}
	slog.Info("server stopped")

	return nil
}

func (s *Server) handleListSessions(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	ids := gw.ListSessions()
	c.JSON(http.StatusOK, ids)
}

func (s *Server) handleGetSession(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	id := c.Param("id")
	sess := gw.GetSession(id)
	if sess == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	c.JSON(http.StatusOK, sess)
}

func (s *Server) handleGetSessionHistory(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	id := c.Param("id")
	sess := gw.GetSession(id)
	if sess == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"messages":     sess.Messages,
		"total_tokens": sess.TotalTokens,
	})
}
