package handler

import (
	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/oauth"
	"github.com/ai-efficiency/backend/internal/repo"
	"github.com/ai-efficiency/backend/internal/toolusage"
	"github.com/ai-efficiency/backend/internal/usersetup"
	"github.com/ai-efficiency/backend/internal/web"
	"github.com/ai-efficiency/backend/internal/webhook"
	"github.com/gin-gonic/gin"
)

var prAttributionService prAttributionSettler
var prUsageService prUsageRefresher

func SetPRAttributionService(service prAttributionSettler) {
	prAttributionService = service
}

func SetPRUsageService(service prUsageRefresher) {
	prUsageService = service
}

// SetupRouter creates and configures the Gin router with all route groups.
func SetupRouter(
	entClient *ent.Client,
	authService *auth.Service,
	repoService *repo.Service,
	webhookHandler *webhook.Handler,
	syncService prSyncer,
	settingsHandler *SettingsHandler,
	encryptionKey string,
	corsMiddleware gin.HandlerFunc,
	oauthHandler *oauth.Handler,
	providerHandler *ProviderHandler,
	adminSettingsHandler *AdminSettingsHandler,
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
	authHandler := NewAuthHandler(authService, entClient, adminSettingsHandler)
	credentialHandler := NewCredentialHandler(entClient, encryptionKey)
	scmProviderHandler := NewSCMProviderHandler(entClient, encryptionKey)
	repoHandler := NewRepoHandler(repoService)
	prHandler := NewPRHandler(entClient, repoService, syncService, prAttributionService, prUsageService)
	efficiencyHandler := NewEfficiencyHandler(entClient)
	toolUsageHandler := NewToolUsageHandler(toolusage.NewService(entClient))
	eventsHandler := NewEventsHandler(toolusage.NewQueryService(entClient))
	userSetupService := usersetup.NewService(entClient, providerHandler, encryptionKey)
	userSetupHandler := NewUserSetupHandler(userSetupService)
	adminUsersHandler := NewAdminUsersHandler(entClient, encryptionKey)
	if providerHandler != nil {
		adminUsersHandler = NewAdminUsersHandler(entClient, encryptionKey, providerHandler)
		adminUsersHandler.logger = providerHandler.logger
	}

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
		authGroup.GET("/options", authHandler.Options)
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
	}

	// Repos
	repoGroup := protected.Group("/repos")
	{
		repoGroup.GET("", repoHandler.List)
		repoGroup.POST("", repoHandler.Create)
		repoGroup.POST("/direct", repoHandler.CreateDirect)
		repoGroup.POST("/ensure-remote", repoHandler.EnsureRemote)
		repoGroup.POST("/resolve-remote", repoHandler.ResolveRemote)
		repoGroup.POST("/hook-eligible", repoHandler.HookEligible)
		repoGroup.POST("/auto-bind-unbound", auth.RequireAdmin(), repoHandler.AutoBindUnbound)
		repoGroup.GET("/:id", repoHandler.Get)
		repoGroup.PUT("/:id", repoHandler.Update)
		repoGroup.DELETE("/:id", repoHandler.Delete)
		repoGroup.GET("/:id/prs", prHandler.ListByRepo)
		repoGroup.POST("/:id/sync-prs", prHandler.SyncPRs)
		repoGroup.GET("/:id/pr-sync-job/latest", prHandler.GetLatestSyncJobForRepo)
	}

	// PRs
	prGroup := protected.Group("/prs")
	{
		prGroup.GET("/:id", prHandler.Get)
		prGroup.POST("/:id/refresh-usage", prHandler.RefreshUsage)
		prGroup.POST("/:id/settle", prHandler.Settle)
	}

	prSyncJobGroup := protected.Group("/pr-sync-jobs")
	{
		prSyncJobGroup.GET("/:id", prHandler.GetSyncJob)
	}

	// Efficiency
	effGroup := protected.Group("/efficiency")
	{
		effGroup.GET("/dashboard", efficiencyHandler.Dashboard)
	}

	toolUsageGroup := protected.Group("/tool-usage-events")
	toolUsageGroup.POST("", toolUsageHandler.Create)
	toolUsageGroup.POST("/batch", toolUsageHandler.CreateBatch)

	eventsGroup := protected.Group("/events")
	{
		eventsGroup.GET("/summary", eventsHandler.Summary)
		eventsGroup.GET("/users", eventsHandler.Users)
		eventsGroup.GET("", eventsHandler.List)
		eventsGroup.GET("/:id", eventsHandler.Get)
	}

	userGroup := protected.Group("/user")
	{
		userGroup.GET("/providers", userSetupHandler.ListProviders)
		if providerHandler != nil {
			userGroup.GET("/providers/:id/groups/:group_id/models", providerHandler.Models)
			userGroup.POST("/providers/:id/test", providerHandler.Test)

			// User usage dashboard
			userUsageHandler := NewUserUsageHandler(entClient, providerHandler, encryptionKey)
			userGroup.GET("/usage/dashboard", userUsageHandler.Dashboard)
		}
		userGroup.POST("/providers/:id/groups/:group_id/credential", userSetupHandler.CreateGroupCredential)
		userGroup.POST("/providers/:id/groups/:group_id/credential/regenerate", userSetupHandler.RegenerateGroupCredential)
	}

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

	adminUsersGroup := protected.Group("/admin/users")
	adminUsersGroup.Use(auth.RequireAdmin())
	{
		adminUsersGroup.GET("", adminUsersHandler.List)
		adminUsersGroup.GET("/subscription-options", adminUsersHandler.ListSubscriptionOptions)
		adminUsersGroup.POST("/subscription-jobs", adminUsersHandler.StartSubscriptionJob)
		adminUsersGroup.GET("/subscription-jobs/latest", adminUsersHandler.GetLatestSubscriptionJob)
		adminUsersGroup.GET("/subscription-jobs/:id", adminUsersHandler.GetSubscriptionJob)
		adminUsersGroup.POST("/subscriptions/batch", adminUsersHandler.ManageSubscriptions)
		adminUsersGroup.POST("/:id/relay-password/reveal", adminUsersHandler.RevealRelayPassword)
		adminUsersGroup.POST("/:id/subscriptions", adminUsersHandler.AssignSubscription)
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
