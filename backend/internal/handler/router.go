package handler

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/middleware"
	"github.com/ai-efficiency/backend/internal/oauth"
	"github.com/ai-efficiency/backend/internal/quotareset"
	"github.com/ai-efficiency/backend/internal/repo"
	"github.com/ai-efficiency/backend/internal/toolusage"
	"github.com/ai-efficiency/backend/internal/usersetup"
	"github.com/ai-efficiency/backend/internal/web"
	"github.com/ai-efficiency/backend/internal/webhook"
	"github.com/ai-efficiency/backend/internal/workitems"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

var prAttributionService prAttributionSettler
var prUsageService prUsageRefresher
var ginDefaultNotFoundBody = []byte("404 page not found")

// RouterOptions supplies production dependencies while SetupRouter preserves its legacy call shape.
type RouterOptions struct {
	DirectoryService       DirectoryAdminService
	WorkItemsCache         *workitems.CountsCache
	WorkItemsRevisionStore *workitems.RevisionStore
	WebhookHTTPClient      *http.Client
	RequestLogger          *zap.Logger
	Release                string
	RequestTimeout         time.Duration
}

type RouterRuntimeOptions = RouterOptions

func SetPRAttributionService(service prAttributionSettler) {
	prAttributionService = service
}

func SetPRUsageService(service prUsageRefresher) {
	prUsageService = service
}

// SetupRouter creates and configures the Gin router with all route groups.
func SetupRouter(
	entClient *ent.Client,
	sqlDB *sql.DB,
	authService *auth.Service,
	repoService *repo.Service,
	webhookHandler *webhook.Handler,
	syncService prSyncer,
	settingsHandler *SettingsHandler,
	encryptionKey string,
	publicURL string,
	corsMiddleware gin.HandlerFunc,
	oauthHandler *oauth.Handler,
	providerHandler *ProviderHandler,
	adminSettingsHandler *AdminSettingsHandler,
	checkpointHandler *CheckpointHandler,
	healthHandler *HealthHandler,
	runtimeOptions ...RouterRuntimeOptions,
) *gin.Engine {
	var options RouterOptions
	if len(runtimeOptions) > 0 {
		options = runtimeOptions[0]
	}
	return setupRouter(
		entClient,
		sqlDB,
		authService,
		repoService,
		webhookHandler,
		syncService,
		settingsHandler,
		encryptionKey,
		publicURL,
		corsMiddleware,
		oauthHandler,
		providerHandler,
		adminSettingsHandler,
		checkpointHandler,
		healthHandler,
		options,
	)
}

// SetupRouterWithOptions configures the router with explicit production dependencies.
func SetupRouterWithOptions(
	entClient *ent.Client,
	sqlDB *sql.DB,
	authService *auth.Service,
	repoService *repo.Service,
	webhookHandler *webhook.Handler,
	syncService prSyncer,
	settingsHandler *SettingsHandler,
	encryptionKey string,
	publicURL string,
	corsMiddleware gin.HandlerFunc,
	oauthHandler *oauth.Handler,
	providerHandler *ProviderHandler,
	adminSettingsHandler *AdminSettingsHandler,
	checkpointHandler *CheckpointHandler,
	healthHandler *HealthHandler,
	options RouterOptions,
) *gin.Engine {
	return setupRouter(
		entClient,
		sqlDB,
		authService,
		repoService,
		webhookHandler,
		syncService,
		settingsHandler,
		encryptionKey,
		publicURL,
		corsMiddleware,
		oauthHandler,
		providerHandler,
		adminSettingsHandler,
		checkpointHandler,
		healthHandler,
		options,
	)
}

func setupRouter(
	entClient *ent.Client,
	sqlDB *sql.DB,
	authService *auth.Service,
	repoService *repo.Service,
	webhookHandler *webhook.Handler,
	syncService prSyncer,
	settingsHandler *SettingsHandler,
	encryptionKey string,
	publicURL string,
	corsMiddleware gin.HandlerFunc,
	oauthHandler *oauth.Handler,
	providerHandler *ProviderHandler,
	adminSettingsHandler *AdminSettingsHandler,
	checkpointHandler *CheckpointHandler,
	healthHandler *HealthHandler,
	options RouterOptions,
) *gin.Engine {
	r := gin.New()
	// Keep canonical redirects inside the correlation and telemetry chain.
	r.RedirectTrailingSlash = false
	r.RemoveExtraSlash = true
	r.Use(middleware.RequestTelemetry(options.RequestLogger, options.Release))
	r.Use(middleware.Recovery(options.RequestLogger, options.Release))
	if options.RequestTimeout > 0 {
		r.Use(middleware.RequestTimeout(options.RequestTimeout))
	}
	r.Use(corsMiddleware)
	r.Use(web.RedirectCanonicalBrowserPath())
	if web.HasEmbeddedFrontend() {
		r.Use(web.ServeEmbeddedFrontend())
	}
	// Finalize Gin's default body before request telemetry unwinds.
	r.NoRoute(func(c *gin.Context) {
		c.Data(http.StatusNotFound, gin.MIMEPlain, ginDefaultNotFoundBody)
	})

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
	var offboardingCounter interface {
		CountOffboardingCandidates(context.Context, int) (int, error)
	}
	if options.DirectoryService != nil {
		offboardingCounter = options.DirectoryService
	}
	workItemsService := workitems.NewService(entClient, offboardingCounter)
	if providerHandler != nil {
		workItemsService = workitems.NewService(entClient, offboardingCounter, userSetupService)
	}
	workItemsService.WithCountsCache(options.WorkItemsCache)
	workItemsHandler := NewWorkItemsHandler(workItemsService)
	var quotaResetHandler *QuotaResetHandler
	if providerHandler != nil {
		adminUsersHandler = NewAdminUsersHandler(entClient, encryptionKey, providerHandler)
		adminUsersHandler.logger = providerHandler.logger
		quotaResetService := quotareset.NewService(
			entClient,
			providerHandler,
			quotareset.NewApproverResolver(entClient),
			quotareset.NewWebhookNotifier(entClient, encryptionKey, publicURL, options.WebhookHTTPClient),
			options.WorkItemsRevisionStore,
		)
		quotaResetHandler = NewQuotaResetHandler(quotaResetService)
	}

	api := r.Group("/api/v1")

	// Health check — no auth
	api.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok", "service": "ai-efficiency"})
	})
	if healthHandler != nil {
		api.GET("/health/live", healthHandler.Live)
		api.GET("/health/ready", healthHandler.Ready)
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

	if healthHandler != nil {
		systemGroup := protected.Group("/system")
		systemGroup.Use(auth.RequireAdmin())
		{
			systemGroup.GET("/version", healthHandler.Version)
			systemGroup.POST("/version/check", healthHandler.CheckVersion)
		}
	}

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
		repoGroup.GET("/inventory", repoHandler.Inventory)
		repoGroup.POST("/direct", repoHandler.CreateDirect)
		repoGroup.POST("/ensure-remote", repoHandler.EnsureRemote)
		repoGroup.POST("/resolve-remote", repoHandler.ResolveRemote)
		repoGroup.POST("/hook-eligible", repoHandler.HookEligible)
		repoGroup.POST("/auto-bind-unbound", auth.RequireAdmin(), repoHandler.AutoBindUnbound)
		repoGroup.POST("/repair-webhooks", auth.RequireAdmin(), repoHandler.RepairFailedWebhooks)
		repoGroup.POST("/:id/repair-webhook", auth.RequireAdmin(), repoHandler.RepairWebhook)
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

	RegisterWorkItemsRoutes(protected, workItemsHandler)

	teamUsageHandler := NewTeamUsageHandler(newTeamUsageService(entClient, sqlDB, providerHandler))

	userGroup := protected.Group("/user")
	{
		userGroup.GET("/providers", userSetupHandler.ListProviders)
		if quotaResetHandler != nil {
			userGroup.GET("/quota-reset/options", quotaResetHandler.Options)
			userGroup.POST("/quota-reset/requests", quotaResetHandler.CreateRequest)
			userGroup.GET("/quota-reset/requests", quotaResetHandler.ListMine)
			userGroup.POST("/quota-reset/requests/:id/cancel", quotaResetHandler.Cancel)
			userGroup.GET("/quota-reset/approvals", quotaResetHandler.ListApprovals)
			userGroup.POST("/quota-reset/approvals/:id/approve", quotaResetHandler.Approve)
			userGroup.POST("/quota-reset/approvals/:id/reject", quotaResetHandler.Reject)
			userGroup.POST("/quota-reset/approvals/:id/retry-reset", quotaResetHandler.RetryReset)
		}
		userGroup.GET("/team-usage/scope", teamUsageHandler.Scope)
		userGroup.GET("/team-usage/subjects", teamUsageHandler.Subjects)
		userGroup.GET("/team-usage/subjects/:user_id/usage/dashboard", teamUsageHandler.SubjectDashboard)
		userGroup.PUT("/team-usage/subjects/:user_id/groups/:group_id/rate-multiplier", teamUsageHandler.UpdateMultiplier)
		userGroup.GET("/team-usage/overview", teamUsageHandler.Overview)
		userGroup.GET("/team-usage/audit", teamUsageHandler.Audit)
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
		adminUsersGroup.GET("/departments", adminUsersHandler.ListDepartments)
		adminUsersGroup.GET("/subscription-options", adminUsersHandler.ListSubscriptionOptions)
		adminUsersGroup.POST("/subscription-jobs", adminUsersHandler.StartSubscriptionJob)
		adminUsersGroup.GET("/subscription-jobs/latest", adminUsersHandler.GetLatestSubscriptionJob)
		adminUsersGroup.GET("/subscription-jobs/:id", adminUsersHandler.GetSubscriptionJob)
		adminUsersGroup.POST("/subscriptions/batch", adminUsersHandler.ManageSubscriptions)
		adminUsersGroup.POST("/:id/disable-access", adminUsersHandler.DisableAccess)
		adminUsersGroup.POST("/:id/relay-password/reveal", adminUsersHandler.RevealRelayPassword)
		adminUsersGroup.POST("/:id/subscriptions", adminUsersHandler.AssignSubscription)
	}

	adminTeamUsageGroup := protected.Group("/admin/team-usage")
	adminTeamUsageGroup.Use(auth.RequireAdmin())
	{
		adminTeamUsageGroup.GET("/audit", teamUsageHandler.AdminAudit)
	}

	if quotaResetHandler != nil {
		adminQuotaResetGroup := protected.Group("/admin/quota-reset")
		adminQuotaResetGroup.Use(auth.RequireAdmin())
		{
			adminQuotaResetGroup.GET("/approver-candidates", quotaResetHandler.ListApproverCandidates)
			adminQuotaResetGroup.GET("/approver-configs", quotaResetHandler.ListApproverConfigs)
			adminQuotaResetGroup.PUT("/approver-configs", quotaResetHandler.SaveApproverConfigs)
			adminQuotaResetGroup.GET("/requests", quotaResetHandler.ListAdmin)
			adminQuotaResetGroup.POST("/requests/:id/approve", quotaResetHandler.AdminApprove)
			adminQuotaResetGroup.POST("/requests/:id/reject", quotaResetHandler.AdminReject)
			adminQuotaResetGroup.POST("/requests/:id/retry-reset", quotaResetHandler.AdminRetryReset)
			adminQuotaResetGroup.GET("/notification-settings", quotaResetHandler.GetNotificationSettings)
			adminQuotaResetGroup.PUT("/notification-settings", quotaResetHandler.UpdateNotificationSettings)
			adminQuotaResetGroup.POST("/notification-settings/test", quotaResetHandler.TestNotificationSettings)
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

	if options.DirectoryService != nil {
		directoryGroup := protected.Group("/admin/directory")
		directoryGroup.Use(auth.RequireAdmin())
		RegisterDirectoryRoutes(directoryGroup, NewDirectoryHandler(options.DirectoryService))
	}

	// Settings — admin only
	if settingsHandler != nil {
		settingsGroup := protected.Group("/settings")
		settingsGroup.Use(auth.RequireAdmin())
		{
			settingsGroup.GET("/llm", settingsHandler.GetLLMConfig)
			settingsGroup.PUT("/llm", settingsHandler.UpdateLLMConfig)
			settingsGroup.POST("/llm/test", settingsHandler.TestLLMConnection)
		}
	}

	return r
}
