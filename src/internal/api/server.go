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

	"io/fs"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Server struct {
	Gateway *gateway.Gateway
	Engine  *gin.Engine

	// Static assets for the dashboard
	DashboardFS fs.FS

	// WebSocket session tracking
	wsMu       sync.RWMutex
	wsSessions map[string][]*websocket.Conn
}

var (
	requestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Number of http requests",
		},
		[]string{"method", "endpoint", "status"},
	)

	requestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "http_request_duration_seconds",
			Help: "Duration of http requests in seconds",
		},
		[]string{"method", "endpoint"},
	)

	promptsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "miri_prompts_total",
			Help: "Total number of prompts processed",
		},
	)
)

func init() {
	prometheus.MustRegister(requestsTotal)
	prometheus.MustRegister(requestDuration)
	prometheus.MustRegister(promptsTotal)
}

func NewServer(gw *gateway.Gateway) *Server {
	e := gin.Default()
	s := &Server{
		Gateway:    gw,
		Engine:     e,
		wsSessions: make(map[string][]*websocket.Conn),
	}
	s.Engine.Use(s.corsMiddleware())
	s.Engine.Use(s.recoveryMiddleware())
	s.Engine.Use(s.injectMiddleware())
	s.setupRoutesRest()
	s.setupRoutesWebSocket()
	s.setupRoutesAdmin()
	s.setupRoutesStatic()
	s.Engine.GET("/metrics", gin.WrapH(promhttp.Handler()))
	s.Gateway.SetTaskReportHandler(s.handleTaskReport)
	return s
}

func (s *Server) setupRoutesStatic() {
	if s.DashboardFS == nil {
		return
	}

	s.Engine.NoRoute(func(c *gin.Context) {
		if !strings.HasPrefix(c.Request.URL.Path, "/api/") && c.Request.URL.Path != "/ws" && c.Request.URL.Path != "/metrics" {
			c.FileFromFS("index.html", http.FS(s.DashboardFS))
			return
		}
	})

	// Static assets
	// We need to handle the _app directory and other static files
	staticFiles := []string{"_app", "favicon.png", "robots.txt"}
	for _, file := range staticFiles {
		s.Engine.Any("/"+file+"/*any", func(c *gin.Context) {
			http.FileServer(http.FS(s.DashboardFS)).ServeHTTP(c.Writer, c.Request)
		})
	}
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
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

func metricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		c.Next()
		code := strconv.Itoa(c.Writer.Status())
		requestsTotal.WithLabelValues(c.Request.Method, path, code).Inc()
		requestDuration.WithLabelValues(c.Request.Method, path).Observe(time.Since(start).Seconds())
	}
}

func (s *Server) setupRoutesAdmin() {
	admin := s.Engine.Group("/api/admin/v1", s.adminMiddleware())
	{
		admin.GET("/health", s.handleAdminHealth)

		// Administrative endpoints moved from /api/v1
		admin.GET("/config", s.handleGetConfig)
		admin.POST("/config", s.handleUpdateConfig)
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

		//brain
		admin.GET("/brain/facts", s.handleGetBrainFacts)
		admin.GET("/brain/summaries", s.handleGetBrainSummaries)
		admin.GET("/brain/topology", s.handleGetBrainTopology)

		// human
		admin.GET("/human", s.handleGetHuman)
		admin.POST("/human", s.handleSaveHuman)
		admin.GET("/human/pending", s.handleListHumanPending)
		admin.POST("/human/response/:id", s.handleHumanResponse)

		// Sub-agent management
		admin.GET("/subagents", s.handleListSubAgentRuns)
		admin.GET("/subagents/:id", s.handleGetSubAgentRun)
		admin.GET("/subagents/:id/transcript", s.handleGetSubAgentTranscript)
		admin.DELETE("/subagents/:id", s.handleCancelSubAgentRun)
	}
}

func (s *Server) setupRoutesWebSocket() {
	s.Engine.GET("/ws", s.authMiddleware(), s.handleWebsocket)
}

func (s *Server) setupRoutesRest() {
	v1 := s.Engine.Group("/api/v1", s.authMiddleware())
	{
		v1.POST("/prompt", s.handlePrompt)
		v1.GET("/prompt/stream", s.handlePromptStream)
		v1.POST("/interaction", s.handleInteraction)
		v1.GET("/files/*filepath", s.handleGetFile)
		v1.POST("/files/upload", s.handleUploadFile)
		v1.GET("/files", s.handleListFiles)
		v1.DELETE("/files", s.handleDeleteFile)
		v1.GET("/sessions/:id/cost", s.handleGetSessionCost)
		// Sub-agent spawn (user-facing)
		v1.POST("/subagents", s.handleSpawnSubAgent)
		v1.GET("/subagents/:id", s.handleGetSubAgentRun)
		v1.GET("/subagents/:id/transcript", s.handleGetSubAgentTranscript)
		// Dream mode: offline CoT simulation
		v1.POST("/dream", s.handleDream)
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
			s.sendError(c, http.StatusUnauthorized, "Invalid or missing server key")
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
			s.sendError(c, http.StatusUnauthorized, "Admin access denied")
			return
		}

		providedUser, providedPass, ok := c.Request.BasicAuth()
		if !ok || providedUser != user || providedPass != pass {
			c.Header("WWW-Authenticate", `Basic realm="Admin Restricted"`)
			s.sendError(c, http.StatusUnauthorized, "Admin access denied")
			return
		}

		c.Next()
	}
}

func (s *Server) sendError(c *gin.Context, code int, msg string) {
	c.JSON(code, APIError{
		Code:    code,
		Message: msg,
	})
	c.Abort()
}

func (s *Server) sendWSError(ws *websocket.Conn, code int, msg string) error {
	return ws.WriteJSON(APIError{
		Code:    code,
		Message: msg,
	})
}

func (s *Server) recoveryMiddleware() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered interface{}) {
		slog.Error("panic recovered", "panic", recovered)
		s.sendError(c, http.StatusInternalServerError, "Internal server error")
	})
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
