package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"

	"github.com/ai-efficiency/backend/internal/deployment"
	"github.com/gin-gonic/gin"
)

type dockerComposeRunner struct {
	composeFile string
	envFile     string
}

func NewDockerComposeRunner(composeFile, envFile string) *dockerComposeRunner {
	return &dockerComposeRunner{
		composeFile: composeFile,
		envFile:     envFile,
	}
}

func (r *dockerComposeRunner) Run(ctx context.Context, args ...string) error {
	baseArgs := []string{"compose", "--env-file", r.envFile, "-f", r.composeFile}
	cmd := exec.CommandContext(ctx, "docker", append(baseArgs, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run docker compose %v: %w", args, err)
	}
	return nil
}

func main() {
	composeFile := mustEnv("AE_UPDATER_COMPOSE_FILE")
	envFile := mustEnv("AE_UPDATER_ENV_FILE")
	serviceName := mustEnv("AE_UPDATER_SERVICE_NAME")
	stateDir := mustEnv("AE_DEPLOYMENT_STATE_DIR")

	runner := NewDockerComposeRunner(composeFile, envFile)
	server := deployment.NewUpdaterServer(deployment.UpdaterConfig{
		ComposeFile: composeFile,
		EnvFile:     envFile,
		ServiceName: serviceName,
		StateDir:    stateDir,
	}, runner)

	if mode := os.Getenv("AE_SERVER_MODE"); mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Recovery())

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	router.GET("/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, deployment.UpdateStatus{Phase: "idle"})
	})

	router.POST("/apply", func(c *gin.Context) {
		var req deployment.ApplyRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		status, err := server.Apply(c.Request.Context(), req)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, status)
	})

	router.POST("/rollback", func(c *gin.Context) {
		status, err := server.Rollback(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, status)
	})

	if err := router.Run(":8090"); err != nil {
		panic(err)
	}
}

func mustEnv(key string) string {
	value := os.Getenv(key)
	if value == "" {
		panic(fmt.Sprintf("required env var %s is not set", key))
	}
	return value
}
