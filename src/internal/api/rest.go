package api

import (
	"encoding/json"
	"log/slog"
	"miri-main/src/internal/config"
	"miri-main/src/internal/engine"
	"miri-main/src/internal/gateway"
	"miri-main/src/internal/session"
	"miri-main/src/internal/storage"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func (s *Server) handleGetConfig(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	c.JSON(http.StatusOK, gw.Config)
}

func (s *Server) handleUpdateConfig(c *gin.Context) {
	var cfg config.Config
	if err := c.ShouldBindJSON(&cfg); err != nil {
		s.sendError(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := config.Save(&cfg); err != nil {
		s.sendError(c, http.StatusInternalServerError, err.Error())
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
		s.sendError(c, http.StatusBadRequest, err.Error())
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

	promptsTotal.Inc()

	gw := c.MustGet("gateway").(*gateway.Gateway)
	response, err := gw.PrimaryAgent.DelegatePromptWithOptions(c.Request.Context(), session.DefaultSessionID, req.Prompt, opts)
	if err != nil {
		s.sendError(c, http.StatusInternalServerError, err.Error())
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
		s.sendError(c, http.StatusBadRequest, err.Error())
		return
	}

	gw := c.MustGet("gateway").(*gateway.Gateway)
	if err := gw.SaveHuman(req.Content); err != nil {
		s.sendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "human markdown saved"})
}

func (s *Server) handleGetHuman(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	content, err := gw.GetHuman()
	if err != nil {
		s.sendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"content": content})
}

func (s *Server) handleListSkills(c *gin.Context) {
	var pq PaginationQuery
	if err := c.ShouldBindQuery(&pq); err != nil {
		s.sendError(c, http.StatusBadRequest, err.Error())
		return
	}
	limit := pq.Limit
	if limit == 0 {
		limit = 50
	}
	offset := pq.Offset
	if offset < 0 {
		offset = 0
	}
	gw := c.MustGet("gateway").(*gateway.Gateway)
	allSkills := gw.ListSkills()
	total := len(allSkills)
	type skillShort struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Version     string   `json:"version"`
		Tags        []string `json:"tags"`
	}
	var res []skillShort
	if offset < total {
		end := min(offset+limit, total)
		sliced := allSkills[offset:end]
		res = make([]skillShort, 0, len(sliced))
		for _, sk := range sliced {
			res = append(res, skillShort{
				Name:        sk.Name,
				Description: sk.Description,
				Version:     sk.Version,
				Tags:        sk.Tags,
			})
		}
	}
	c.JSON(http.StatusOK, PaginatedResponse{
		Data:   res,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

func (s *Server) handleListSkillCommands(c *gin.Context) {
	var pq PaginationQuery
	if err := c.ShouldBindQuery(&pq); err != nil {
		s.sendError(c, http.StatusBadRequest, err.Error())
		return
	}
	limit := pq.Limit
	if limit == 0 {
		limit = 50
	}
	offset := pq.Offset
	if offset < 0 {
		offset = 0
	}
	gw := c.MustGet("gateway").(*gateway.Gateway)
	allCommands, err := gw.ListSkillCommands(c.Request.Context())
	if err != nil {
		s.sendError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, Paginate(allCommands, offset, limit))
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
		s.sendError(c, http.StatusBadRequest, err.Error())
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
	var pq PaginationQuery
	if err := c.ShouldBindQuery(&pq); err != nil {
		s.sendError(c, http.StatusBadRequest, err.Error())
		return
	}
	limit := pq.Limit
	if limit == 0 {
		limit = 50
	}
	offset := pq.Offset
	if offset < 0 {
		offset = 0
	}
	gw := c.MustGet("gateway").(*gateway.Gateway)
	all := gw.ListSessions()
	c.JSON(http.StatusOK, Paginate(all, offset, limit))
}

func (s *Server) handleListTasks(c *gin.Context) {
	var pq PaginationQuery
	if err := c.ShouldBindQuery(&pq); err != nil {
		s.sendError(c, http.StatusBadRequest, err.Error())
		return
	}
	limit := pq.Limit
	if limit == 0 {
		limit = 50
	}
	offset := pq.Offset
	if offset < 0 {
		offset = 0
	}
	gw := c.MustGet("gateway").(*gateway.Gateway)
	allTasks, err := gw.ListTasks()
	if err != nil {
		s.sendError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, Paginate(allTasks, offset, limit))
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

func (s *Server) handleGetSessionCost(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	id := c.Param("id")
	sess := gw.GetSession(id)
	if sess == nil {
		s.sendError(c, http.StatusNotFound, "session not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{"total_cost": sess.TotalCost})
}

func (s *Server) handleGetSessionHistory(c *gin.Context) {
	var pq PaginationQuery
	if err := c.ShouldBindQuery(&pq); err != nil {
		s.sendError(c, http.StatusBadRequest, err.Error())
		return
	}
	limit := pq.Limit
	if limit == 0 {
		limit = 50
	}
	offset := pq.Offset
	if offset < 0 {
		offset = 0
	}
	gw := c.MustGet("gateway").(*gateway.Gateway)
	id := c.Param("id")
	sess := gw.GetSession(id)
	if sess == nil {
		s.sendError(c, http.StatusNotFound, "session not found")
		return
	}
	allHistory := []session.Message{}
	if gw.PrimaryAgent != nil && gw.PrimaryAgent.Eng != nil {
		if h := gw.PrimaryAgent.Eng.GetHistory(id); h != nil {
			allHistory = h
		}
	}
	total := len(allHistory)
	end := offset + limit
	if end > total {
		end = total
	}
	sliced := allHistory[offset:end]
	c.JSON(http.StatusOK, gin.H{
		"messages":       sliced,
		"total_messages": total,
		"total_tokens":   sess.TotalTokens,
		"limit":          limit,
		"offset":         offset,
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
		if h := gw.PrimaryAgent.Eng.GetHistory(id); h != nil {
			history = h
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
		s.sendError(c, http.StatusBadRequest, err.Error())
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

func (s *Server) handleGetBrainFacts(c *gin.Context) {
	var pq PaginationQuery
	if err := c.ShouldBindQuery(&pq); err != nil {
		s.sendError(c, http.StatusBadRequest, err.Error())
		return
	}
	limit := pq.Limit
	if limit == 0 {
		limit = 50
	}
	offset := pq.Offset
	if offset < 0 {
		offset = 0
	}
	allFacts, err := s.Gateway.PrimaryAgent.Eng.GetBrainFacts(c.Request.Context())
	if err != nil {
		s.sendError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, Paginate(allFacts, offset, limit))
}

func (s *Server) handleGetBrainSummaries(c *gin.Context) {
	var pq PaginationQuery
	if err := c.ShouldBindQuery(&pq); err != nil {
		s.sendError(c, http.StatusBadRequest, err.Error())
		return
	}
	limit := pq.Limit
	if limit == 0 {
		limit = 50
	}
	offset := pq.Offset
	if offset < 0 {
		offset = 0
	}
	allSummaries, err := s.Gateway.PrimaryAgent.Eng.GetBrainSummaries(c.Request.Context())
	if err != nil {
		s.sendError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, Paginate(allSummaries, offset, limit))
}

func (s *Server) handleGetBrainTopology(c *gin.Context) {
	sessionID := c.Query("session_id")
	topology, err := s.Gateway.PrimaryAgent.Eng.GetBrainTopology(c.Request.Context(), sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, topology)
}

func (s *Server) handleListHumanPending(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	pendingDir := filepath.Join(gw.Config.StorageDir, "human_pending")
	sessionID := c.Query("session_id")

	type HumanPending struct {
		ID        string    `json:"id"`
		Question  string    `json:"question"`
		SessionID string    `json:"session_id"`
		Created   time.Time `json:"created,omitempty"`
	}

	var pendings []HumanPending

	entries, err := os.ReadDir(pendingDir)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusOK, []HumanPending{})
			return
		}
		s.sendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		id := strings.TrimSuffix(entry.Name(), ".json")
		filePath := filepath.Join(pendingDir, entry.Name())

		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		type pendingInfo struct {
			Status    string    `json:"status"`
			Question  string    `json:"question"`
			SessionID string    `json:"session_id"`
			Created   time.Time `json:"created"`
		}
		var h pendingInfo
		if err := json.Unmarshal(data, &h); err != nil {
			continue
		}

		if h.Status != "pending" {
			continue
		}

		if sessionID != "" && h.SessionID != sessionID {
			continue
		}

		pendings = append(pendings, HumanPending{
			ID:        id,
			Question:  h.Question,
			SessionID: h.SessionID,
			Created:   h.Created,
		})
	}

	c.JSON(http.StatusOK, pendings)
}

func (s *Server) handleHumanResponse(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		s.sendError(c, http.StatusBadRequest, "id required")
		return
	}

	gw := c.MustGet("gateway").(*gateway.Gateway)
	pendingDir := filepath.Join(gw.Config.StorageDir, "human_pending")
	filePath := filepath.Join(pendingDir, id+".json")

	data, err := os.ReadFile(filePath)
	if err != nil {
		s.sendError(c, http.StatusNotFound, "human pending not found")
		return
	}

	var h map[string]any
	if err := json.Unmarshal(data, &h); err != nil {
		s.sendError(c, http.StatusInternalServerError, "invalid pending JSON")
		return
	}

	if h["status"] != "pending" {
		s.sendError(c, http.StatusBadRequest, "not pending")
		return
	}

	type req struct {
		Response string `json:"response" binding:"required"`
	}
	var r req
	if err := c.ShouldBindJSON(&r); err != nil {
		s.sendError(c, http.StatusBadRequest, err.Error())
		return
	}

	h["status"] = "answered"
	h["response"] = r.Response

	newData, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		s.sendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	if err := os.WriteFile(filePath, newData, 0644); err != nil {
		s.sendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "responded", "id": id})
}

// handleSpawnSubAgent POST /api/v1/subagents
func (s *Server) handleSpawnSubAgent(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	var req struct {
		Role          string `json:"role" binding:"required"`
		Goal          string `json:"goal" binding:"required"`
		Model         string `json:"model"`
		ParentSession string `json:"parent_session"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		s.sendError(c, http.StatusBadRequest, err.Error())
		return
	}
	if req.ParentSession == "" {
		req.ParentSession = session.DefaultSessionID
	}
	id, err := gw.SpawnSubAgent(c.Request.Context(), req.Role, req.Goal, req.Model, req.ParentSession)
	if err != nil {
		s.sendError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"id": id, "status": "pending"})
}

// handleListSubAgentRuns GET /api/admin/v1/subagents
func (s *Server) handleListSubAgentRuns(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	parentSession := c.Query("session")
	runs, err := gw.Storage.ListSubAgentRuns(parentSession)
	if err != nil {
		s.sendError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if runs == nil {
		runs = []*storage.SubAgentRun{}
	}
	c.JSON(http.StatusOK, runs)
}

// handleGetSubAgentRun GET /api/admin/v1/subagents/:id
func (s *Server) handleGetSubAgentRun(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	run, err := gw.Storage.LoadSubAgentRun(c.Param("id"))
	if err != nil {
		s.sendError(c, http.StatusNotFound, "sub-agent run not found")
		return
	}
	c.JSON(http.StatusOK, run)
}

// handleGetSubAgentTranscript GET /api/admin/v1/subagents/:id/transcript
func (s *Server) handleGetSubAgentTranscript(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	msgs, err := gw.Storage.LoadSubAgentTranscript(c.Param("id"))
	if err != nil {
		s.sendError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if msgs == nil {
		msgs = []map[string]string{}
	}
	c.JSON(http.StatusOK, msgs)
}

// handleCancelSubAgentRun DELETE /api/admin/v1/subagents/:id
func (s *Server) handleCancelSubAgentRun(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	if err := gw.CancelSubAgent(c.Param("id")); err != nil {
		s.sendError(c, http.StatusBadRequest, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "canceled"})
}
