package telemetry

import (
	"fmt"
	"net/http"
)

func HTTPStatusClass(status int) string {
	if status < http.StatusContinue || status > 599 {
		return "unknown"
	}
	return fmt.Sprintf("%dxx", status/100)
}
