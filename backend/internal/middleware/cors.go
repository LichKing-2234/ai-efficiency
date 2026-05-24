package middleware

import (
	"net/url"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// CORS returns a CORS middleware with sensible defaults.
func CORS(allowOrigins []string) gin.HandlerFunc {
	allowOrigins = compactOrigins(allowOrigins)
	if len(allowOrigins) == 0 {
		allowOrigins = []string{"http://localhost:5173", "http://localhost:8081"}
	}
	allowOrigins = expandLoopbackOrigins(allowOrigins)
	return cors.New(cors.Config{
		AllowOrigins:     allowOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "Accept"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	})
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
