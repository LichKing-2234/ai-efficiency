package handler

import (
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/gin-gonic/gin"
)

func isAdminUser(c *gin.Context) bool {
	uc := auth.GetUserContext(c)
	return uc != nil && uc.Role == "admin"
}
