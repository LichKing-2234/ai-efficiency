package middleware

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/ai-efficiency/backend/internal/telemetry"
	"github.com/gin-gonic/gin"
)

// CORS returns a CORS middleware with sensible defaults.
func CORS(allowOrigins []string) gin.HandlerFunc {
	allowOrigins = compactOrigins(allowOrigins)
	if len(allowOrigins) == 0 {
		allowOrigins = []string{"http://localhost:5173", "http://localhost:8081"}
	}
	allowOrigins = expandLoopbackOrigins(allowOrigins)

	allowAll := false
	allowedSet := make(map[string]struct{}, len(allowOrigins))
	for _, origin := range allowOrigins {
		if origin == "*" {
			allowAll = true
			continue
		}
		allowedSet[origin] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		originAllowed := allowAll
		if origin != "" && !allowAll {
			_, originAllowed = allowedSet[origin]
		}

		if originAllowed {
			if allowAll {
				c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
			} else if origin != "" {
				c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
				c.Writer.Header().Add("Vary", "Origin")
			}
			if !allowAll {
				c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
			c.Writer.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization, Accept, "+telemetry.HeaderRequestID)
			c.Writer.Header().Set("Access-Control-Expose-Headers", "Content-Length, "+telemetry.HeaderRequestID)
			c.Writer.Header().Set("Access-Control-Max-Age", "43200")
		}

		if c.Request.Method == http.MethodOptions {
			if originAllowed {
				c.AbortWithStatus(http.StatusNoContent)
			} else {
				c.AbortWithStatus(http.StatusForbidden)
			}
			return
		}

		c.Next()
	}
}

func compactOrigins(origins []string) []string {
	var out []string
	for _, origin := range origins {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			out = append(out, origin)
		}
	}
	return out
}

func expandLoopbackOrigins(origins []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(origin string) {
		origin = strings.TrimRight(strings.TrimSpace(origin), "/")
		if origin == "" || seen[origin] {
			return
		}
		seen[origin] = true
		out = append(out, origin)
	}
	for _, origin := range origins {
		add(origin)
		u, err := url.Parse(strings.TrimSpace(origin))
		if err != nil || u.Scheme == "" || u.Host == "" {
			continue
		}
		port := u.Port()
		switch strings.ToLower(u.Hostname()) {
		case "localhost":
			u.Host = "127.0.0.1"
			if port != "" {
				u.Host += ":" + port
			}
			add(u.String())
		case "127.0.0.1":
			u.Host = "localhost"
			if port != "" {
				u.Host += ":" + port
			}
			add(u.String())
		}
	}
	return out
}
