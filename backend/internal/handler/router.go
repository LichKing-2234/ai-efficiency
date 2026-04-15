package handler

import (
	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/efficiency"
	"github.com/ai-efficiency/backend/internal/oauth"
	"github.com/ai-efficiency/backend/internal/repo"
	"github.com/ai-efficiency/backend/internal/sessionbootstrap"
	"github.com/ai-efficiency/backend/internal/sessionevent"
	"github.com/ai-efficiency/backend/internal/sessionusage"
	"github.com/ai-efficiency/backend/internal/web"
	"github.com/ai-efficiency/backend/internal/webhook"
	"github.com/gin-gonic/gin"
)

var prAttributionService prAttributionSettler

func SetPRAttributionService(service prAttributionSettler) {
	prAttributionService = service
}

// SetupRouter creates and configures the Gin router with all route groups.
func SetupRouter(
	entClient *ent.Client,
	authService *auth.Service,
	repoService *repo.Service,
	analysisService analysisScanner,
	webhookHandler *webhook.Handler,
	syncService prSyncer,
	settingsHandler *SettingsHandler,
	chatHandler *ChatHandler,
	aggregator *efficiency.Aggregator,
	optimizer optimizerService,
	encryptionKey string,
	corsMiddleware gin.HandlerFunc,
	oauthHandler *oauth.Handler,
	providerHandler *ProviderHandler,
	adminSettingsHandler *AdminSettingsHandler,
	sessionBootstrapSvc *sessionbootstrap.Service,
	checkpointHandler *CheckpointHandler,
	deploymentHandler *DeploymentHandler,
) *gin.Engine {
	r := gin.New()
	r.RemoveExtraSlash = true
	r.Use(gin.Recovery())
	r.Use(corsMiddleware)
	r.Use(web.RedirectCanonicalBrowserPath())
	if web.HasEmbeddedFrontend() {
		r.Use(web.ServeEmbeddedFrontend())
	}

	// OAuth endpoints — at root /oauth/* (not under /api/v1)
	if oauthHandler != nil {
		r.GET("/oauth/authorize", oauthHandler.Authorize)
		r.GET("/oauth/device", oauthHandler.DevicePage)
		r.POST("/oauth/device/code", oauthHandler.DeviceCode)
		r.POST("/oauth/token", oauthHandler.Token)

		oauthAuth := r.Group("/oauth")
		oauthAuth.Use(auth.RequireAuth(authService))
		oauthAuth.POST("/authorize/approve", oauthHandler.Approve)
		oauthAuth.POST("/device/verify", oauthHandler.VerifyDevice)
	}

	// Handlers
	authHandler := NewAuthHandler(authService)
	credentialHandler := NewCredentialHandler(entClient, encryptionKey)
	scmProviderHandler := NewSCMProviderHandler(entClient, encryptionKey)
	repoHandler := NewRepoHandler(repoService)
	analysisHandler := NewAnalysisHandler(analysisService, optimizer, repoService)
	prHandler := NewPRHandler(entClient, repoService, syncService, prAttributionService)
	efficiencyHandler := NewEfficiencyHandler(entClient, aggregator)
	sessionHandler := NewSessionHandler(entClient, sessionBootstrapSvc)
	sessionUsageHandler := NewSessionUsageHandler(
		sessionusage.NewService(entClient),
		sessionevent.NewService(entClient),
	)

	api := r.Group("/api/v1")

	// Health check — no auth
	api.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok", "service": "ai-efficiency"})
	})
	if deploymentHandler != nil {
		api.GET("/health/live", deploymentHandler.Live)
		api.GET("/health/ready", deploymentHandler.Ready)
	}

	// Auth routes — no auth middleware
	authGroup := api.Group("/auth")
	{
		authGroup.POST("/login", authHandler.Login)
		authGroup.POST("/refresh", authHandler.Refresh)
		authGroup.GET("/me", auth.RequireAuth(authService), authHandler.Me)

		// Dev login — only available in debug mode
		if gin.Mode() == gin.DebugMode {
			authGroup.POST("/dev-login", func(c *gin.Context) {
				authHandler.DevLogin(c, entClient)
			})
		}
	}

	// Webhook routes — no auth middleware (signature-verified internally)
	webhookGroup := api.Group("/webhooks")
	{
		webhookGroup.POST("/github", webhookHandler.HandleGitHub)
		webhookGroup.POST("/bitbucket", webhookHandler.HandleBitbucket)
	}

	// Protected routes
	protected := api.Group("")
	protected.Use(auth.RequireAuth(authService))

	// SCM Providers — admin only
	scmGroup := protected.Group("/scm-providers")
	scmGroup.Use(auth.RequireAdmin())
	{
		scmGroup.GET("", scmProviderHandler.List)
		scmGroup.POST("", scmProviderHandler.Create)
		scmGroup.PUT("/:id", scmProviderHandler.Update)
		scmGroup.DELETE("/:id", scmProviderHandler.Delete)
		scmGroup.POST("/:id/test", scmProviderHandler.Test)
	}

	// Repos
	repoGroup := protected.Group("/repos")
	{
		repoGroup.GET("", repoHandler.List)
		repoGroup.POST("", repoHandler.Create)
		repoGroup.POST("/direct", repoHandler.CreateDirect)
		repoGroup.GET("/:id", repoHandler.Get)
		repoGroup.PUT("/:id", repoHandler.Update)
		repoGroup.DELETE("/:id", repoHandler.Delete)
		repoGroup.POST("/:id/scan", analysisHandler.TriggerScan)
		repoGroup.GET("/:id/scans", analysisHandler.ListScans)
		repoGroup.GET("/:id/scans/latest", analysisHandler.LatestScan)
		repoGroup.POST("/:id/optimize", analysisHandler.Optimize)
		repoGroup.POST("/:id/optimize/preview", analysisHandler.OptimizePreview)
		repoGroup.POST("/:id/optimize/confirm", analysisHandler.OptimizeConfirm)
		repoGroup.GET("/:id/prs", prHandler.ListByRepo)
		repoGroup.POST("/:id/sync-prs", prHandler.SyncPRs)
		if chatHandler != nil {
			repoGroup.POST("/:id/chat", chatHandler.Chat)
		}
	}

	// PRs
	prGroup := protected.Group("/prs")
	{
		prGroup.GET("/:id", prHandler.Get)
		prGroup.POST("/:id/settle", prHandler.Settle)
	}

	// Efficiency
	effGroup := protected.Group("/efficiency")
	{
		effGroup.GET("/dashboard", efficiencyHandler.Dashboard)
		effGroup.GET("/repos/:id/metrics", efficiencyHandler.RepoMetrics)
		effGroup.GET("/repos/:id/trend", efficiencyHandler.Trend)
		effGroup.POST("/aggregate", auth.RequireAdmin(), efficiencyHandler.Aggregate)
	}

	// Sessions (ae-cli)
	sessionGroup := protected.Group("/sessions")
	{
		sessionGroup.GET("", sessionHandler.List)
		sessionGroup.GET("/:id", sessionHandler.Get)
		sessionGroup.GET("/:id/provider-credentials", sessionHandler.ProviderCredential)
		sessionGroup.POST("/bootstrap", sessionHandler.Bootstrap)
		sessionGroup.POST("", sessionHandler.Create)
		sessionGroup.PUT("/:id", sessionHandler.Update)
		sessionGroup.POST("/:id/stop", sessionHandler.Stop)
		sessionGroup.POST("/:id/invocations", sessionHandler.AddInvocation)
	}

	sessionUsageGroup := protected.Group("/session-usage-events")
	sessionUsageGroup.POST("", sessionUsageHandler.CreateUsage)

	sessionEventGroup := protected.Group("/session-events")
	sessionEventGroup.POST("", sessionUsageHandler.CreateEvent)

	if checkpointHandler != nil {
		checkpointGroup := protected.Group("/checkpoints")
		{
			checkpointGroup.POST("/commit", checkpointHandler.Commit)
			checkpointGroup.POST("/rewrite", checkpointHandler.Rewrite)
		}
	}

	// Providers (ae-cli API key delivery)
	if providerHandler != nil {
		protected.GET("/providers", providerHandler.ListForUser)

		adminProviderGroup := protected.Group("/admin/providers")
		adminProviderGroup.Use(auth.RequireAdmin())
		{
			adminProviderGroup.GET("", providerHandler.List)
			adminProviderGroup.POST("", providerHandler.Create)
			adminProviderGroup.PUT("/:id", providerHandler.Update)
			adminProviderGroup.DELETE("/:id", providerHandler.Delete)
		}
	}

	adminCredentialGroup := protected.Group("/admin/credentials")
	adminCredentialGroup.Use(auth.RequireAdmin())
	{
		adminCredentialGroup.GET("", credentialHandler.List)
		adminCredentialGroup.POST("", credentialHandler.Create)
		adminCredentialGroup.GET("/:id", credentialHandler.Get)
		adminCredentialGroup.PUT("/:id", credentialHandler.Update)
		adminCredentialGroup.DELETE("/:id", credentialHandler.Delete)
	}

	// LDAP settings — admin only
	if adminSettingsHandler != nil {
		ldapGroup := protected.Group("/admin/settings/ldap")
		ldapGroup.Use(auth.RequireAdmin())
		{
			ldapGroup.GET("", adminSettingsHandler.GetLDAP)
			ldapGroup.PUT("", adminSettingsHandler.UpdateLDAP)
			ldapGroup.POST("/test", adminSettingsHandler.TestLDAP)
		}
	}

	// Settings — admin only
	if settingsHandler != nil || deploymentHandler != nil {
		settingsGroup := protected.Group("/settings")
		settingsGroup.Use(auth.RequireAdmin())
		{
			if settingsHandler != nil {
				settingsGroup.GET("/llm", settingsHandler.GetLLMConfig)
				settingsGroup.PUT("/llm", settingsHandler.UpdateLLMConfig)
				settingsGroup.POST("/llm/test", settingsHandler.TestLLMConnection)
			}
			if deploymentHandler != nil {
				settingsGroup.GET("/deployment", deploymentHandler.Status)
				settingsGroup.POST("/deployment/update/check", deploymentHandler.CheckForUpdate)
				settingsGroup.POST("/deployment/update/apply", deploymentHandler.ApplyUpdate)
				settingsGroup.POST("/deployment/update/rollback", deploymentHandler.RollbackUpdate)
				settingsGroup.POST("/deployment/restart", deploymentHandler.Restart)
			}
		}
	}

	return r
}
