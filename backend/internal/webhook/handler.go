package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/internal/efficiency"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/scm"
	"github.com/ai-efficiency/backend/internal/scm/bitbucket"
	"github.com/ai-efficiency/backend/internal/scm/github"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Handler handles incoming SCM webhook events.
type Handler struct {
	entClient *ent.Client
	labeler   *efficiency.Labeler
	logger    *zap.Logger
}

// NewHandler creates a new webhook handler.
func NewHandler(entClient *ent.Client, labeler *efficiency.Labeler, logger *zap.Logger) *Handler {
	return &Handler{
		entClient: entClient,
		labeler:   labeler,
		logger:    logger,
	}
}

// HandleGitHub handles POST /api/v1/webhooks/github
func (h *Handler) HandleGitHub(c *gin.Context) {
	// Read the raw body for signature validation (limit to 1MB)
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "failed to read body")
		return
	}

	// Parse the event type header
	eventType := c.GetHeader("X-GitHub-Event")
	deliveryID := c.GetHeader("X-GitHub-Delivery")
	if eventType == "" || deliveryID == "" {
		pkg.Error(c, http.StatusBadRequest, "missing github event headers")
		return
	}

	// Extract repo full name from payload
	var payload struct {
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid payload")
		return
	}

	repoFullName := payload.Repository.FullName
	if repoFullName == "" {
		pkg.Error(c, http.StatusBadRequest, "missing repository full_name")
		return
	}

	// Look up repo config
	rc, err := h.entClient.RepoConfig.Query().
		Where(repoconfig.FullNameEQ(repoFullName)).
		Only(c.Request.Context())
	if err != nil {
		if ent.IsNotFound(err) {
			h.logger.Warn("webhook for unknown repo", zap.String("repo", repoFullName))
			pkg.Error(c, http.StatusNotFound, "repo not configured")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "failed to find repo")
		return
	}

	// Validate signature using stored webhook secret
	secret := ""
	if rc.WebhookSecret != nil {
		secret = *rc.WebhookSecret
	}

	// Use a temporary GitHub provider to parse the event
	ghProvider, err := github.New("", "", h.logger)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to init github provider")
		return
	}

	// Reconstruct request for parsing
	parseReq := c.Request.Clone(c.Request.Context())
	parseReq.Body = io.NopCloser(
		&bodyReader{data: body},
	)

	event, err := ghProvider.ParseWebhookPayload(parseReq, secret)
	if err != nil {
		h.logger.Warn("webhook parse failed", zap.String("repo", repoFullName), zap.Error(err))
		// Store in dead letter queue
		h.storeDeadLetter(c, rc.ID, deliveryID, eventType, body, err.Error())
		pkg.Error(c, http.StatusUnauthorized, "invalid webhook signature or payload")
		return
	}

	if event == nil {
		// Unsupported event type, just acknowledge
		pkg.Success(c, gin.H{"status": "ignored"})
		return
	}

	// Dispatch event
	h.dispatch(c, rc, event)

	pkg.Success(c, gin.H{"status": "processed"})
}

// HandleBitbucket handles POST /api/v1/webhooks/bitbucket
func (h *Handler) HandleBitbucket(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "failed to read body")
		return
	}

	eventKey := c.GetHeader("X-Event-Key")
	if eventKey == "" {
		pkg.Error(c, http.StatusBadRequest, "missing X-Event-Key header")
		return
	}

	// Extract repo full name from payload
	var payload struct {
		Repository struct {
			Slug    string `json:"slug"`
			Project struct {
				Key string `json:"key"`
			} `json:"project"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid payload")
		return
	}

	repoFullName := payload.Repository.Project.Key + "/" + payload.Repository.Slug
	if repoFullName == "/" {
		pkg.Error(c, http.StatusBadRequest, "missing repository info")
		return
	}

	rc, err := h.entClient.RepoConfig.Query().
		Where(repoconfig.FullNameEQ(repoFullName)).
		Only(c.Request.Context())
	if err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "repo not configured")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "failed to find repo")
		return
	}

	secret := ""
	if rc.WebhookSecret != nil {
		secret = *rc.WebhookSecret
	}

	bbProvider, err := bitbucket.New("", "", h.logger)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to init bitbucket provider")
		return
	}

	parseReq := c.Request.Clone(c.Request.Context())
	parseReq.Body = io.NopCloser(&bodyReader{data: body})

	event, err := bbProvider.ParseWebhookPayload(parseReq, secret)
	if err != nil {
		h.logger.Warn("bitbucket webhook parse failed", zap.String("repo", repoFullName), zap.Error(err))
		h.storeDeadLetter(c, rc.ID, "", eventKey, body, err.Error())
		pkg.Error(c, http.StatusBadRequest, "invalid webhook payload")
		return
	}

	if event == nil {
		pkg.Success(c, gin.H{"status": "ignored"})
		return
	}

	h.dispatch(c, rc, event)
	pkg.Success(c, gin.H{"status": "processed"})
}

func (h *Handler) dispatch(c *gin.Context, rc *ent.RepoConfig, event *scm.WebhookEvent) {
	ctx := c.Request.Context()
	h.logger.Info("webhook event received",
		zap.String("type", string(event.Type)),
		zap.String("repo", event.RepoFullName),
		zap.String("sender", event.Sender),
	)

	switch event.Type {
	case scm.EventPROpened:
		h.handlePROpened(ctx, rc, event)
	case scm.EventPRUpdated:
		h.handlePRUpdated(ctx, rc, event)
	case scm.EventPRMerged:
		h.handlePRMerged(ctx, rc, event)
	case scm.EventPush:
		h.logger.Info("push event", zap.String("repo", event.RepoFullName))
	}
}

func (h *Handler) handlePROpened(ctx context.Context, rc *ent.RepoConfig, event *scm.WebhookEvent) {
	if event.PR == nil {
		return
	}
	pr := event.PR

	record, err := h.entClient.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(pr.ID).
		SetScmPrURL(pr.URL).
		SetAuthor(pr.Author).
		SetTitle(pr.Title).
		SetSourceBranch(pr.SourceBranch).
		SetTargetBranch(pr.TargetBranch).
		SetStatus(prrecord.StatusOpen).
		Save(ctx)
	if err != nil {
		h.logger.Error("failed to create PR record", zap.Error(err), zap.Int("pr_id", pr.ID))
		return
	}
	h.logger.Info("PR record created", zap.Int("pr_id", pr.ID), zap.String("title", pr.Title))

	// Trigger auto-labeling
	h.labelPR(ctx, record.ID)
}

func (h *Handler) handlePRUpdated(ctx context.Context, rc *ent.RepoConfig, event *scm.WebhookEvent) {
	if event.PR == nil {
		return
	}
	pr := event.PR

	// Find existing PR record
	existing, err := h.entClient.PrRecord.Query().
		Where(
			prrecord.HasRepoConfigWith(repoconfig.IDEQ(rc.ID)),
			prrecord.ScmPrIDEQ(pr.ID),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			// PR not tracked yet, create it
			h.handlePROpened(ctx, rc, event)
			return
		}
		h.logger.Error("failed to find PR record", zap.Error(err))
		return
	}

	// Update existing record
	update := h.entClient.PrRecord.UpdateOneID(existing.ID)
	if pr.Title != "" {
		update.SetTitle(pr.Title)
	}
	if pr.Author != "" {
		update.SetAuthor(pr.Author)
	}
	if err := update.Exec(ctx); err != nil {
		h.logger.Error("failed to update PR record", zap.Error(err))
	}
}

func (h *Handler) handlePRMerged(ctx context.Context, rc *ent.RepoConfig, event *scm.WebhookEvent) {
	if event.PR == nil {
		return
	}
	pr := event.PR

	// Find existing PR record
	existing, err := h.entClient.PrRecord.Query().
		Where(
			prrecord.HasRepoConfigWith(repoconfig.IDEQ(rc.ID)),
			prrecord.ScmPrIDEQ(pr.ID),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			// Create merged PR record directly
			now := time.Now()
			_, err = h.entClient.PrRecord.Create().
				SetRepoConfigID(rc.ID).
				SetScmPrID(pr.ID).
				SetScmPrURL(pr.URL).
				SetAuthor(pr.Author).
				SetTitle(pr.Title).
				SetSourceBranch(pr.SourceBranch).
				SetTargetBranch(pr.TargetBranch).
				SetStatus(prrecord.StatusMerged).
				SetMergedAt(now).
				Save(ctx)
			if err != nil {
				h.logger.Error("failed to create merged PR record", zap.Error(err))
			}
			return
		}
		h.logger.Error("failed to find PR record for merge", zap.Error(err))
		return
	}

	// Update to merged
	now := time.Now()
	cycleTime := now.Sub(existing.CreatedAt).Hours()

	if err := h.entClient.PrRecord.UpdateOneID(existing.ID).
		SetStatus(prrecord.StatusMerged).
		SetMergedAt(now).
		SetCycleTimeHours(cycleTime).
		Exec(ctx); err != nil {
		h.logger.Error("failed to update PR to merged", zap.Error(err))
	}

	h.logger.Info("PR merged", zap.Int("pr_id", pr.ID), zap.Float64("cycle_time_hours", cycleTime))

	// Trigger auto-labeling on merge
	h.labelPR(ctx, existing.ID)
}

// labelPR runs the labeler if available, logging any errors.
func (h *Handler) labelPR(ctx context.Context, prRecordID int) {
	if h.labeler == nil {
		return
	}
	if _, err := h.labeler.LabelPR(ctx, prRecordID); err != nil {
		h.logger.Warn("auto-label failed", zap.Int("pr_record_id", prRecordID), zap.Error(err))
	}
}

func (h *Handler) storeDeadLetter(c *gin.Context, repoConfigID int, deliveryID, eventType string, payload []byte, errMsg string) {
	var payloadMap map[string]interface{}
	json.Unmarshal(payload, &payloadMap)

	_, err := h.entClient.WebhookDeadLetter.Create().
		SetRepoConfigID(repoConfigID).
		SetDeliveryID(deliveryID).
		SetEventType(eventType).
		SetPayload(payloadMap).
		SetErrorMessage(errMsg).
		Save(c.Request.Context())
	if err != nil {
		h.logger.Error("failed to store dead letter", zap.Error(err))
	}
}

// bodyReader is a simple io.Reader wrapper for a byte slice.
type bodyReader struct {
	data []byte
	pos  int
}

func (r *bodyReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
