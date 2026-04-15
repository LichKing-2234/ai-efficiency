package handler

import (
	"net/http"

	"github.com/ai-efficiency/backend/internal/pkg"
	repomodule "github.com/ai-efficiency/backend/internal/repo"
	"github.com/gin-gonic/gin"
)

func handleRepoBindingError(c *gin.Context, err error) bool {
	if !repomodule.IsRepoUnbound(err) {
		return false
	}

	pkg.ErrorWithDetails(c, http.StatusConflict, "repo is not bound to an scm provider", gin.H{
		"error_code": "repo_unbound",
	})
	return true
}
