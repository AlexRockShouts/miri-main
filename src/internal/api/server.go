package api

import (
	"context"
	"log/slog"
	"miri-main/src/internal/gateway"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
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
	s.Engine.Use(s.corsMiddleware())
	s.Engine.Use(s.injectMiddleware())
	s.Engine.Use(s.authMiddleware())
	s.setupRoutesRest()
	s.setupRoutesWebSocket()
	s.setupRoutesAdmin()
	return s
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
		admin.GET("/sessions/:id/history", s.handleGetSessionHistory)

		// Skill management
		admin.GET("/skills", s.handleListSkills)
		admin.GET("/skills/remote", s.handleListRemoteSkills)
		admin.POST("/skills", s.handleInstallSkill)
		admin.DELETE("/skills/:name", s.handleRemoveSkill)
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
		if provided == "" && (c.Request.Header.Get("Upgrade") == "websocket" || c.Request.Header.Get("Connection") == "Upgrade") {
			protocol := c.GetHeader("Sec-WebSocket-Protocol")
			// Support miri-key, <value> (two tokens)
			const label = "miri-key"
			if strings.Contains(protocol, label) {
				parts := strings.Split(protocol, ",")
				for i, p := range parts {
					p = strings.TrimSpace(p)
					if p == label && i+1 < len(parts) {
						provided = strings.TrimSpace(parts[i+1])
						// Save the exact protocol the client sent
						c.Set("miri_ws_key", protocol)
						break
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
	slog.Info("server stopped")

	return nil
}
