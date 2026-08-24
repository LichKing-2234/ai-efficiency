package handler

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/activity"
	"github.com/ai-efficiency/backend/internal/attributionclaim"
	"github.com/ai-efficiency/backend/internal/attributionledger"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/middleware"
	"github.com/ai-efficiency/backend/internal/oauth"
	"github.com/ai-efficiency/backend/internal/personalusage"
	"github.com/ai-efficiency/backend/internal/quotareset"
	"github.com/ai-efficiency/backend/internal/relayplanning"
	"github.com/ai-efficiency/backend/internal/repo"
	"github.com/ai-efficiency/backend/internal/representativescope"
	"github.com/ai-efficiency/backend/internal/teamusage"
	"github.com/ai-efficiency/backend/internal/telemetry"
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

// RouterOptions supplies the runtime dependencies required by the production router.
type RouterOptions struct {
	DirectoryService              DirectoryAdminService
	PersonalUsageCache            *personalusage.Cache
	WorkItemsCache                *workitems.CountsCache
	WorkItemsRevisionStore        *workitems.RevisionStore
	RepresentativeScopeCache      *representativescope.Cache
	TeamUsageSnapshotCache        *teamusage.SnapshotCache
	TeamUsageOriginCache          *teamusage.OriginCache
	TeamUsagePrewarmReader        *teamusage.PrewarmReader
	TeamUsageCursorSecret         string
	WebhookHTTPClient             *http.Client
	RequestLogger                 *zap.Logger
	RequestObserver               telemetry.RequestObserver
	WebVitalsHandler              *WebVitalsHandler
	AttributionProtocol           attributionledger.ProtocolContract
	AttributionCutoverAt          time.Time
	AttributionSetupAvailable     bool
	AttributionReadinessAvailable bool
	ActivityCache                 *activity.Cache
	Release                       string
	RequestTimeout                time.Duration
}

func validateRouterDependencies(providerHandler *ProviderHandler, options RouterOptions) error {
	missing := make([]string, 0, 14)
	if providerHandler == nil || providerHandler.runtime == nil {
		missing = append(missing, "provider runtime")
	}
	if strings.TrimSpace(options.TeamUsageCursorSecret) == "" {
		missing = append(missing, "cursor secret")
	}
	if options.DirectoryService == nil {
		missing = append(missing, "directory service")
	}
	if options.PersonalUsageCache == nil {
		missing = append(missing, "personal usage cache")
	}
	if options.WorkItemsCache == nil {
		missing = append(missing, "work items cache")
	}
	if options.WorkItemsRevisionStore == nil {
		missing = append(missing, "work items revision store")
	}
	if options.RepresentativeScopeCache == nil {
		missing = append(missing, "representative scope cache")
	}
	if options.TeamUsageSnapshotCache == nil {
		missing = append(missing, "team usage snapshot cache")
	}
	if options.TeamUsageOriginCache == nil {
		missing = append(missing, "team usage origin cache")
	}
	if options.TeamUsagePrewarmReader == nil {
		missing = append(missing, "team usage prewarm reader")
	}
	if options.WebhookHTTPClient == nil {
		missing = append(missing, "webhook HTTP client")
	}
	if options.RequestLogger == nil {
		missing = append(missing, "request logger")
	}
	if options.RequestObserver == nil {
		missing = append(missing, "request observer")
	}
	if options.WebVitalsHandler == nil {
		missing = append(missing, "Web Vitals handler")
	}
	if strings.TrimSpace(options.Release) == "" {
		missing = append(missing, "release")
	}
	if options.RequestTimeout <= 0 {
		missing = append(missing, "request timeout")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing production router dependencies: %s", strings.Join(missing, ", "))
	}
	return nil
}

func SetPRAttributionService(service prAttributionSettler) {
	prAttributionService = service
}

func SetPRUsageService(service prUsageRefresher) {
	prUsageService = service
}

// SetupRouter validates production dependencies and configures all route groups.
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
	options RouterOptions,
) (*gin.Engine, error) {
	protocol, err := attributionledger.NormalizeProtocolContract(options.AttributionProtocol)
	if err != nil {
		return nil, fmt.Errorf("initialize attribution protocol: %w", err)
	}
	options.AttributionProtocol = protocol
	if protocol.LedgerEpoch == attributionledger.LedgerEpochFormalV2 {
		if options.AttributionCutoverAt.IsZero() || options.AttributionCutoverAt.Location() != time.UTC {
			return nil, fmt.Errorf("attribution cutover requires one explicit UTC instant for formal_v2")
		}
	} else if !options.AttributionCutoverAt.IsZero() {
		return nil, fmt.Errorf("attribution cutover must be empty outside formal_v2")
	}
	if options.AttributionReadinessAvailable && !options.AttributionSetupAvailable {
		return nil, fmt.Errorf("reporting readiness capability requires setup capability")
	}
	if options.AttributionReadinessAvailable && protocol.LedgerEpoch != attributionledger.LedgerEpochFormalV2 {
		return nil, fmt.Errorf("reporting readiness capability requires the formal v2 ledger epoch")
	}
	if err := validateRouterDependencies(providerHandler, options); err != nil {
		return nil, err
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
		protocol,
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
	protocol attributionledger.ProtocolContract,
) (*gin.Engine, error) {
	r := gin.New()
	// Keep canonical redirects inside the correlation and telemetry chain.
	r.RedirectTrailingSlash = false
	r.RemoveExtraSlash = true
	r.Use(middleware.RequestTelemetry(options.RequestLogger, options.Release, options.RequestObserver))
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
		r.HEAD("/oauth/authorize", oauthHandler.Authorize)
		r.GET("/oauth/device", oauthHandler.DevicePage)
		r.HEAD("/oauth/device", oauthHandler.DevicePage)
		r.POST("/oauth/device/code", oauthHandler.DeviceCode)
		r.POST("/oauth/token", oauthHandler.Token)

		oauthAuth := r.Group("/oauth")
		oauthAuth.Use(auth.RequireAuth(authService))
		oauthAuth.POST("/authorize/approve", oauthHandler.Approve)
		oauthAuth.POST("/device/verify", oauthHandler.VerifyDevice)
	}

	// Handlers
	authHandler := NewAuthHandler(authService, entClient, adminSettingsHandler, ReportingCapabilities{
		SetupAvailable: options.AttributionSetupAvailable, ReadinessAvailable: options.AttributionReadinessAvailable,
	})
	credentialHandler := NewCredentialHandler(entClient, encryptionKey)
	scmProviderHandler := NewSCMProviderHandler(entClient, encryptionKey, repoService)
	repoHandler := NewRepoHandler(repoService)
	prHandler := NewPRHandler(entClient, repoService, syncService, prAttributionService, prUsageService)
	efficiencyHandler := NewEfficiencyHandler(entClient)
	toolUsageHandler := NewToolUsageHandler(toolusage.NewService(entClient))
	eventsHandler := NewEventsHandler(toolusage.NewQueryService(entClient))
	installationService := attributionledger.NewInstallationService(entClient, protocol)
	teamUsageService, err := newTeamUsageService(entClient, sqlDB, providerHandler, options.RepresentativeScopeCache, options.TeamUsageSnapshotCache, options.TeamUsageOriginCache, options.TeamUsagePrewarmReader, options.TeamUsageCursorSecret)
	if err != nil {
		return nil, fmt.Errorf("initialize team usage service: %w", err)
	}
	var personalUsageService *personalusage.Service
	if providerHandler != nil {
		personalUsageService = personalusage.NewService(entClient, providerHandler, encryptionKey, options.PersonalUsageCache)
	}
	activityLedgerEpoch := ""
	if protocol.LedgerEpoch == attributionledger.LedgerEpochFormalV2 {
		activityLedgerEpoch = protocol.LedgerEpoch
	}
	activityService := activity.NewService(entClient, activity.ServiceOptions{
		ScopeResolver: representativescope.NewWithCache(entClient, options.RepresentativeScopeCache),
		CursorSecret:  options.TeamUsageCursorSecret,
		V2LedgerEpoch: activityLedgerEpoch,
		V2CutoverAt:   options.AttributionCutoverAt,
		V2Denominator: &activityDenominatorResolver{personal: personalUsageService, team: teamUsageService, client: entClient, cache: options.ActivityCache},
		V2DB:          sqlDB,
	})
	activityHandler := NewActivityHandler(activityService)
	attributionHandler := NewAttributionHandler(installationService, attributionclaim.NewService(entClient, protocol), activityService)
	userSetupService := usersetup.NewService(entClient, providerHandler, encryptionKey)
	userSetupHandler := NewUserSetupHandler(userSetupService)
	adminUsersHandler := NewAdminUsersHandler(entClient, encryptionKey)
	var relayPlanningHandler *RelayPlanningHandler
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
		relayPlanningHandler = NewRelayPlanningHandler(relayplanning.NewService(entClient, providerHandler, options.TeamUsagePrewarmReader))
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

	// CLI repository discovery keeps normal authenticated access.
	repoGroup := protected.Group("/repos")
	{
		repoGroup.POST("/ensure-remote", repoHandler.EnsureRemote)
		repoGroup.POST("/resolve-remote", repoHandler.ResolveRemote)
		repoGroup.POST("/hook-eligible", repoHandler.HookEligible)
	}
	// Browser repository integration management is administrator-only.
	adminRepoGroup := protected.Group("/repos")
	adminRepoGroup.Use(auth.RequireAdmin())
	{
		adminRepoGroup.GET("", repoHandler.List)
		adminRepoGroup.POST("", repoHandler.Create)
		adminRepoGroup.GET("/inventory", repoHandler.Inventory)
		adminRepoGroup.POST("/direct", repoHandler.CreateDirect)
		adminRepoGroup.POST("/auto-bind-unbound", repoHandler.AutoBindUnbound)
		adminRepoGroup.POST("/repair-webhooks", repoHandler.RepairFailedWebhooks)
		adminRepoGroup.POST("/:id/repair-webhook", repoHandler.RepairWebhook)
		adminRepoGroup.GET("/:id", repoHandler.Get)
		adminRepoGroup.PUT("/:id", repoHandler.Update)
		adminRepoGroup.DELETE("/:id", repoHandler.Delete)
		adminRepoGroup.GET("/:id/prs", prHandler.ListByRepo)
		adminRepoGroup.POST("/:id/sync-prs", prHandler.SyncPRs)
		adminRepoGroup.GET("/:id/pr-sync-job/latest", prHandler.GetLatestSyncJobForRepo)
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

	attributionReadGroup := protected.Group("/attribution")
	{
		if options.AttributionReadinessAvailable {
			attributionReadGroup.GET("/status", attributionHandler.Status)
		}
		attributionReadGroup.POST("/installations", attributionHandler.EnsureInstallation)
		attributionReadGroup.PUT("/installations/:installation_id", attributionHandler.SetInstallationEnabled)
		attributionReadGroup.POST("/installations/:installation_id/credentials/rotate", attributionHandler.RotateInstallationCredentials)
	}

	attributionReporterGroup := api.Group("/attribution")
	attributionReporterGroup.Use(requireInstallationToken(installationService))
	{
		attributionReporterGroup.POST("/v2/claim-groups/batch", attributionHandler.CreateV2Claims)
		attributionReporterGroup.POST("/repos/resolve-remote", repoHandler.ResolveRemote)
		attributionReporterGroup.POST("/repos/ensure-remote", repoHandler.EnsureReportingRemote)
		if checkpointHandler != nil {
			attributionReporterGroup.POST("/checkpoints/commit", checkpointHandler.CompactCommit)
			attributionReporterGroup.POST("/checkpoints/rewrite", checkpointHandler.CompactRewrite)
		}
	}

	RegisterActivityRoutes(protected.Group("/activity"), activityHandler)

	RegisterWorkItemsRoutes(protected, workItemsHandler)
	RegisterWebVitalsRoutes(protected, options.WebVitalsHandler)

	teamUsageHandler := NewTeamUsageHandler(teamUsageService)

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
		userGroup.GET("/team-usage/summary", teamUsageHandler.Summary)
		userGroup.GET("/team-usage/trend", teamUsageHandler.Trend)
		userGroup.GET("/team-usage/members", teamUsageHandler.Members)
		userGroup.GET("/team-usage/organization", teamUsageHandler.Organization)
		userGroup.GET("/team-usage/audit", teamUsageHandler.Audit)
		if providerHandler != nil {
			userGroup.GET("/providers/:id/groups/:group_id/models", providerHandler.Models)
			userGroup.POST("/providers/:id/test", providerHandler.Test)

			// User usage dashboard
			userUsageHandler := NewUserUsageHandler(personalUsageService)
			userGroup.GET("/usage/dashboard", userUsageHandler.Dashboard)
			userGroup.GET("/usage/group-quotas", userUsageHandler.GroupQuotas)
			userGroup.GET("/usage/group-pool-usage", userUsageHandler.GroupPoolUsage)
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
		adminUsersGroup.GET("/department-options", adminUsersHandler.ListDepartmentOptions)
		adminUsersGroup.GET("/department-children", adminUsersHandler.ListDepartmentChildren)
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
	if relayPlanningHandler != nil {
		relayPlanningGroup := protected.Group("/admin/relay-planning")
		relayPlanningGroup.Use(auth.RequireAdmin())
		{
			relayPlanningGroup.GET("/users", relayPlanningHandler.SearchUsers)
			relayPlanningGroup.GET("/accounts", relayPlanningHandler.SearchAccounts)
			relayPlanningGroup.POST("/preview", relayPlanningHandler.Preview)
			relayPlanningGroup.POST("/execute", relayPlanningHandler.Execute)
			relayPlanningGroup.GET("/mappings", relayPlanningHandler.ListMappings)
			relayPlanningGroup.POST("/mappings/:id/renewal/preview", relayPlanningHandler.PreviewMappingRenewal)
			relayPlanningGroup.POST("/mappings/:id/renewal/execute", relayPlanningHandler.ExecuteMappingRenewal)
			relayPlanningGroup.PUT("/mappings/:id/rebind", relayPlanningHandler.Rebind)
			relayPlanningGroup.POST("/mappings/:id/accounts/adopt", relayPlanningHandler.AdoptCurrentAccounts)
			relayPlanningGroup.PUT("/mappings/:id/accounts", relayPlanningHandler.SaveDesiredAccounts)
			relayPlanningGroup.POST("/mappings/:id/replan", relayPlanningHandler.Replan)
			relayPlanningGroup.POST("/mappings/:id/replan/execute", relayPlanningHandler.ReplanExecute)
		}
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

	return r, nil
}
