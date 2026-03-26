package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	// ContextKeyUser is the key for UserContext in gin.Context.
	ContextKeyUser = "user_context"
)

// UserContext holds the authenticated user's information in request context.
type UserContext struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// RequireAuth returns a Gin middleware that validates JWT tokens.
func RequireAuth(svc *Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenStr := extractToken(c)
		if tokenStr == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    http.StatusUnauthorized,
				"message": "missing or invalid authorization header",
			})
			return
		}

		claims, err := svc.ValidateAccessToken(tokenStr)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    http.StatusUnauthorized,
				"message": "invalid or expired token",
			})
			return
		}

		userID, _ := claims["user_id"].(float64)
		username, _ := claims["username"].(string)
		role, _ := claims["role"].(string)

		c.Set(ContextKeyUser, &UserContext{
			UserID:   int(userID),
			Username: username,
			Role:     role,
		})
		c.Next()
	}
}

// RequireAdmin returns a Gin middleware that requires admin role.
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		uc := GetUserContext(c)
		if uc == nil || uc.Role != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    http.StatusForbidden,
				"message": "admin access required",
			})
			return
		}
		c.Next()
	}
}

// GetUserContext extracts the UserContext from gin.Context.
func GetUserContext(c *gin.Context) *UserContext {
	val, exists := c.Get(ContextKeyUser)
	if !exists {
		return nil
	}
	uc, ok := val.(*UserContext)
	if !ok {
		return nil
	}
	return uc
}

func extractToken(c *gin.Context) string {
	auth := c.GetHeader("Authorization")
	if auth == "" {
		return ""
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return parts[1]
}
