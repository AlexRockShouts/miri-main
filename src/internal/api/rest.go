package api

import (
	"miri-main/src/internal/config"
	"miri-main/src/internal/engine"
	"miri-main/src/internal/gateway"
	"miri-main/src/internal/storage"
	"net/http"

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
	SessionID   string          `json:"session_id,omitempty"`
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
	response, err := gw.PrimaryAgent.DelegatePromptWithOptions(c.Request.Context(), req.SessionID, req.Prompt, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"response": response})
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

func (s *Server) handleListSkills(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	skills := gw.ListSkills()
	c.JSON(http.StatusOK, skills)
}

func (s *Server) handleListRemoteSkills(c *gin.Context) {
	gw := c.MustGet("gateway").(*gateway.Gateway)
	skills, err := gw.ListRemoteSkills(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, skills)
}

func (s *Server) handleInstallSkill(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	gw := c.MustGet("gateway").(*gateway.Gateway)
	output, err := gw.InstallSkill(c.Request.Context(), req.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "output": output})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "skill installed", "output": output})
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
	Action   string `json:"action" binding:"required,oneof=new status"`
	ClientID string `json:"client_id,omitempty"`
}

func (s *Server) handleInteraction(c *gin.Context) {
	var req interactionRequest
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
