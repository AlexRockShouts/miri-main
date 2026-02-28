package api

import (
	"context"
	"log/slog"
	"miri-main/src/internal/gateway"
	"miri-main/src/internal/session"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type Server struct {
	Gateway *gateway.Gateway
	Engine  *gin.Engine

	// WebSocket session tracking
	wsMu       sync.RWMutex
	wsSessions map[string][]*websocket.Conn
}

func NewServer(gw *gateway.Gateway) *Server {
	e := gin.Default()
	s := &Server{
		Gateway:    gw,
		Engine:     e,
		wsSessions: make(map[string][]*websocket.Conn),
	}
	s.Engine.Use(s.corsMiddleware())
	s.Engine.Use(s.injectMiddleware())
	s.Engine.Use(s.authMiddleware())
	s.setupRoutesRest()
	s.setupRoutesWebSocket()
	s.setupRoutesAdmin()
	s.Gateway.SetTaskReportHandler(s.handleTaskReport)
	return s
}

func (s *Server) handleTaskReport(sessionID, taskName, taskID, message string) {
	if sessionID == "" {
		sessionID = session.DefaultSessionID
	}

	s.wsMu.RLock()
	conns, ok := s.wsSessions[sessionID]
	s.wsMu.RUnlock()

	if !ok || len(conns) == 0 {
		return
	}

	slog.Info("Broadcasting task report to session", "session_id", sessionID, "task_id", taskID, "conns", len(conns))

	for _, conn := range conns {
		err := conn.WriteJSON(gin.H{
			"response":   message,
			"source":     "task",
			"task_id":    taskID,
			"task_name":  taskName,
			"session_id": sessionID,
		})
		if err != nil {
			slog.Warn("Failed to send task report to websocket", "session_id", sessionID, "error", err)
		}
	}
}

func (s *Server) corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With, X-Server-Key")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

func (s *Server) setupRoutesAdmin() {
	admin := s.Engine.Group("/api/admin/v1", s.adminMiddleware())
	{
		admin.GET("/health", s.handleAdminHealth)

		// Administrative endpoints moved from /api/v1
		admin.GET("/config", s.handleGetConfig)
		admin.POST("/config", s.handleUpdateConfig)
		admin.POST("/human", s.handleSaveHumanInfo)
		admin.GET("/human", s.handleListHumanInfo)
		admin.POST("/channels", s.handleChannels)
		admin.GET("/sessions", s.handleListSessions)
		admin.GET("/sessions/:id", s.handleGetSession)
		admin.GET("/sessions/:id/stats", s.handleGetSessionStats)
		admin.GET("/sessions/:id/history", s.handleGetSessionHistory)
		admin.GET("/sessions/:id/skills", s.handleGetSessionSkills)

		// Skill management
		admin.GET("/skills", s.handleListSkills)
		admin.GET("/skills/commands", s.handleListSkillCommands)
		admin.GET("/skills/:name", s.handleGetSkill)
		admin.DELETE("/skills/:name", s.handleRemoveSkill)

		// Task management
		admin.GET("/tasks", s.handleListTasks)
		admin.GET("/tasks/:id", s.handleGetTask)
	}
}

func (s *Server) setupRoutesWebSocket() {
	s.Engine.GET("/ws", s.handleWebsocket)
}

func (s *Server) setupRoutesRest() {
	v1 := s.Engine.Group("/api/v1")
	{
		v1.POST("/prompt", s.handlePrompt)
		v1.GET("/prompt/stream", s.handlePromptStream)
		v1.POST("/interaction", s.handleInteraction)
		v1.GET("/files/*filepath", s.handleGetFile)
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
		// Skip server key auth for admin endpoints
		if strings.HasPrefix(c.Request.URL.Path, "/api/admin/v1") {
			c.Next()
			return
		}

		// Skip auth for OPTIONS requests (CORS preflight)
		if c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}

		gw := c.MustGet("gateway").(*gateway.Gateway)
		key := gw.Config.Server.Key
		if key == "" {
			c.Next()
			return
		}
		// Check for X-Server-Key header
		provided := c.GetHeader("X-Server-Key")

		// Check for Sec-WebSocket-Protocol header if X-Server-Key is missing
		isWebSocket := false
		if strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
			conn := strings.ToLower(c.GetHeader("Connection"))
			if strings.Contains(conn, "upgrade") {
				isWebSocket = true
			}
		}

		// If it's a potential WebSocket request, we MUST NOT consume the request body
		// or do anything that might disrupt the handshake if we are going to Next()
		if isWebSocket {
			// Save the protocol header if present, so handleWebsocket can use it for handshake
			if protocol := c.GetHeader("Sec-WebSocket-Protocol"); protocol != "" {
				c.Set("miri_ws_key", protocol)
			}

			if provided == "" {
				// Try token query parameter first as it's the most reliable across proxies
				provided = c.Query("token")

				if provided == "" {
					protocol := c.GetHeader("Sec-WebSocket-Protocol")
					// Support miri-key, <value> (two tokens)
					const label = "miri-key"
					if strings.Contains(protocol, label) {
						parts := strings.Split(protocol, ",")
						for i, p := range parts {
							p = strings.TrimSpace(p)
							if p == label && i+1 < len(parts) {
								provided = strings.TrimSpace(parts[i+1])
								break
							}
						}
					}
				}
			}
		}

		if provided != key {
			slog.Warn("unauthorized request", "path", c.Request.URL.Path, "remote", c.ClientIP(), "provided", provided != "")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or missing server key"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func (s *Server) adminMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		gw := c.MustGet("gateway").(*gateway.Gateway)
		user := gw.Config.Server.AdminUser
		pass := gw.Config.Server.AdminPass

		// If no admin credentials set, deny all admin access
		if user == "" || pass == "" {
			c.Header("WWW-Authenticate", `Basic realm="Admin Restricted"`)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		providedUser, providedPass, ok := c.Request.BasicAuth()
		if !ok || providedUser != user || providedPass != pass {
			c.Header("WWW-Authenticate", `Basic realm="Admin Restricted"`)
			c.AbortWithStatus(http.StatusUnauthorized)
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

	// Trigger brain compaction on shutdown
	if s.Gateway != nil && s.Gateway.PrimaryAgent != nil {
		slog.Info("triggering engine shutdown...")
		s.Gateway.PrimaryAgent.Shutdown(ctxShut)
	}

	slog.Info("server stopped")

	return nil
}
