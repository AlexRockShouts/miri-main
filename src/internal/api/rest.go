package api

import (
	"encoding/json"
	"log/slog"
	"miri-main/src/internal/config"
	"miri-main/src/internal/engine"
	"miri-main/src/internal/gateway"
	"miri-main/src/internal/session"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

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

type promptRequest struct {
	Prompt      string          `json:"prompt"`
	Model       string          `json:"model,omitempty"`
	Temperature *float32        `json:"temperature,omitempty"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
	Options     *engine.Options `json:"options,omitempty"`
}

func (s *Server) handlePrompt(c *gin.Context) {
	var req promptRequest
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
	if req.Temperature != nil {
		opts.Temperature = req.Temperature
	}
	if req.MaxTokens != nil {
		opts.MaxTokens = req.MaxTokens
	}

	gw := c.MustGet("gateway").(*gateway.Gateway)
	response, err := gw.PrimaryAgent.DelegatePromptWithOptions(c.Request.Context(), session.DefaultSessionID, req.Prompt, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"response": response})
}

func (s *Server) handleSaveHuman(c *gin.Context) {
	type humanReq struct {
		Content string `json:"content" binding:"required"`
	}
	var req humanReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	gw := c.MustGet("gateway").(*gateway.Gateway)
	if err := gw.SaveHuman(req.Content); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "human markdown saved"})
}

func (s *Server) handleGetHuman(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	content, err := gw.GetHuman()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"content": content})
}

func (s *Server) handleListSkills(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	skills := gw.ListSkills()

	// Return name, description, version, tags
	type skillShort struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Version     string   `json:"version"`
		Tags        []string `json:"tags"`
	}

	res := make([]skillShort, 0, len(skills))
	for _, s := range skills {
		// Use a simple map conversion if we don't want to import the skills package here
		// or if ListSkills returns any. Since we know the structure of Skill:
		if m, ok := s.(interface {
			GetName() string
			GetDescription() string
			GetVersion() string
			GetTags() []string
		}); ok {
			res = append(res, skillShort{
				Name:        m.GetName(),
				Description: m.GetDescription(),
				Version:     m.GetVersion(),
				Tags:        m.GetTags(),
			})
		} else {
			// Fallback to manual extraction or JSON marshal/unmarshal
			data, _ := json.Marshal(s)
			var ss skillShort
			json.Unmarshal(data, &ss)
			res = append(res, ss)
		}
	}
	c.JSON(http.StatusOK, res)
}

func (s *Server) handleListSkillCommands(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	commands, err := gw.ListSkillCommands(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, commands)
}

func (s *Server) handleGetSkill(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "skill name is required"})
		return
	}

	gw := c.MustGet("gateway").(*gateway.Gateway)
	skill, err := gw.GetSkill(name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, skill)
}

func (s *Server) handleRemoveSkill(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "skill name is required"})
		return
	}

	gw := c.MustGet("gateway").(*gateway.Gateway)
	if err := gw.RemoveSkill(name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "skill removed"})
}

type interactionRequest struct {
	Action string `json:"action" binding:"required,oneof=status"`
}

func (s *Server) handleInteraction(c *gin.Context) {
	var req interactionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	gw := c.MustGet("gateway").(*gateway.Gateway)

	switch req.Action {
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid action; must be 'status'"})
	}
}

func (s *Server) handleListSessions(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	ids := gw.ListSessions()
	c.JSON(http.StatusOK, ids)
}

func (s *Server) handleListTasks(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	tasks, err := gw.ListTasks()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, tasks)
}

func (s *Server) handleGetTask(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	id := c.Param("id")
	task, err := gw.GetTask(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	c.JSON(http.StatusOK, task)
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

func (s *Server) handleGetSessionStats(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	id := c.Param("id")
	sess := gw.GetSession(id)
	if sess == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"session_id":    sess.ID,
		"total_tokens":  sess.TotalTokens,
		"prompt_tokens": sess.PromptTokens,
		"output_tokens": sess.OutputTokens,
		"total_cost":    sess.TotalCost,
	})
}

func (s *Server) handleGetSessionHistory(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	id := c.Param("id")
	sess := gw.GetSession(id)
	if sess == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	history := []session.Message{}
	if gw.PrimaryAgent != nil && gw.PrimaryAgent.Eng != nil {
		buf := gw.PrimaryAgent.Eng.(interface {
			GetHistory(sessionID string) any
		}).GetHistory(id)
		if buf != nil {
			history = buf.([]session.Message)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"messages":     history,
		"total_tokens": sess.TotalTokens,
	})
}
func (s *Server) handleGetSessionSkills(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	id := c.Param("id")
	sess := gw.GetSession(id)
	if sess == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	history := []session.Message{}
	if gw.PrimaryAgent != nil && gw.PrimaryAgent.Eng != nil {
		if h := gw.PrimaryAgent.Eng.(interface {
			GetHistory(sessionID string) any
		}).GetHistory(id); h != nil {
			history = h.([]session.Message)
		}
	}

	res := []string{}
	if id == session.DefaultSessionID {
		res = append(res, "learn", "skill_creator")
	}
	// Also check responses for "Skill '...' loaded successfully"
	for _, msg := range history {
		if strings.Contains(msg.Response, "loaded successfully") {
			// Try to extract skill name
			// "Skill 'file-organizer' loaded successfully."
			start := strings.Index(msg.Response, "Skill '")
			if start != -1 {
				rest := msg.Response[start+7:]
				end := strings.Index(rest, "'")
				if end != -1 {
					skillName := rest[:end]
					res = append(res, skillName)
				}
			}
		}
	}
	// Deduplicate
	unique := make(map[string]bool)
	final := []string{}
	for _, r := range res {
		if !unique[r] {
			unique[r] = true
			final = append(final, r)
		}
	}
	c.JSON(http.StatusOK, final)
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

func (s *Server) handleGetFile(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	storageDir := gw.Config.StorageDir

	// Ensure the storage path is absolute and clean
	if strings.HasPrefix(storageDir, "~") {
		home, _ := os.UserHomeDir()
		storageDir = filepath.Join(home, storageDir[1:])
	}

	// Limit access to the storage directory
	subPath := c.Param("filepath")
	cleanSubPath := filepath.Clean("/" + subPath)
	fullPath := filepath.Join(storageDir, cleanSubPath)

	// Security check: ensure the file is within the storage directory
	absStorage, _ := filepath.Abs(storageDir)
	absFile, err := filepath.Abs(fullPath)
	if err != nil || !strings.HasPrefix(absFile, absStorage) {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}

	// Check if file exists and is not a directory
	info, err := os.Stat(absFile)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "file not found in storage folder"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	if info.IsDir() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot download directory"})
		return
	}

	c.File(absFile)
}

func (s *Server) handleUploadFile(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	storageDir := gw.Config.StorageDir

	if strings.HasPrefix(storageDir, "~") {
		home, _ := os.UserHomeDir()
		storageDir = filepath.Join(home, storageDir[1:])
	}

	uploadDir := filepath.Join(storageDir, "uploads")
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		slog.Error("failed to create upload directory", "path", uploadDir, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create upload directory"})
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		slog.Warn("no file uploaded", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "no file uploaded"})
		return
	}

	// Security: clean the filename
	filename := filepath.Base(file.Filename)
	dst := filepath.Join(uploadDir, filename)

	if err := c.SaveUploadedFile(file, dst); err != nil {
		slog.Error("failed to save uploaded file", "filename", filename, "destination", dst, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save file"})
		return
	}

	slog.Info("file uploaded successfully", "filename", filename, "path", dst)

	// Return the path relative to storage root for use with file_manager
	relPath := filepath.Join("uploads", filename)

	c.JSON(http.StatusOK, gin.H{
		"status":   "file uploaded",
		"filename": filename,
		"path":     relPath,
		"download": "/api/v1/files/" + relPath,
	})
}
