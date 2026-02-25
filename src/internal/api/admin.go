package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *Server) handleAdminHealth(c *gin.Context) {
	c.JSON(http.StatusOK, adminHealthResponse{
		Status:  "ok",
		Message: "Admin API is operational",
	})
}

type adminHealthResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}
