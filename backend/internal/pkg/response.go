package pkg

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// PagedResponse represents a paginated API response.
type PagedResponse struct {
	Total    int64       `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
	Items    interface{} `json:"items"`
}

type apiResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Details interface{} `json:"details,omitempty"`
}

// Success sends a 200 JSON response with data.
func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, apiResponse{
		Code: http.StatusOK,
		Data: data,
	})
}

// Created sends a 201 JSON response with data.
func Created(c *gin.Context, data interface{}) {
	c.JSON(http.StatusCreated, apiResponse{
		Code: http.StatusCreated,
		Data: data,
	})
}

// Error sends an error JSON response with the given status code and message.
func Error(c *gin.Context, code int, msg string) {
	c.JSON(code, apiResponse{
		Code:    code,
		Message: msg,
	})
}

// ErrorWithDetails sends an error JSON response with additional details.
func ErrorWithDetails(c *gin.Context, code int, msg string, details interface{}) {
	c.JSON(code, apiResponse{
		Code:    code,
		Message: msg,
		Details: details,
	})
}

// Paged sends a paginated JSON response.
func Paged(c *gin.Context, total int64, page, pageSize int, items interface{}) {
	Success(c, PagedResponse{
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		Items:    items,
	})
}
