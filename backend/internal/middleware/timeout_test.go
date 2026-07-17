package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRequestTimeoutAddsAndReleasesContextDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const budget = 35 * time.Second

	router := gin.New()
	router.Use(RequestTimeout(budget))
	var requestContext context.Context
	router.GET("/bounded", func(c *gin.Context) {
		requestContext = c.Request.Context()
		deadline, ok := requestContext.Deadline()
		if !ok {
			t.Fatal("request context has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > budget {
			t.Fatalf("request deadline remaining = %s, want within (0, %s]", remaining, budget)
		}
		c.Status(http.StatusNoContent)
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/bounded", nil))

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if requestContext == nil {
		t.Fatal("handler did not receive request context")
	}
	if !contextCanceledSoon(requestContext, time.Second) {
		t.Fatal("request timeout context was not released after the handler completed")
	}
}

func contextCanceledSoon(ctx context.Context, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return true
	case <-timer.C:
		return false
	}
}
