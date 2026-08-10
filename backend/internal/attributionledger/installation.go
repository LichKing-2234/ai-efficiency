package attributionledger

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/attributionusagebucket"
	"github.com/ai-efficiency/backend/ent/reportinginstallation"
	"github.com/google/uuid"
)

var (
	ErrInstallationForbidden = errors.New("reporting installation does not belong to authenticated user")
	ErrReporterDisabled      = errors.New("compact reporting is not enabled for this installation")
	ErrOTLPDisabled          = errors.New("Codex OTLP is not enabled for this installation")
)

type InstallationService struct {
	client *ent.Client
}

func NewInstallationService(client *ent.Client) *InstallationService {
	return &InstallationService{client: client}
}

func (s *InstallationService) Readiness(ctx context.Context, userID int) (ReportingReadiness, error) {
	var result ReportingReadiness
	if s == nil || s.client == nil || userID <= 0 {
		return result, fmt.Errorf("read reporting readiness: client and user are required")
	}

	total, err := s.client.ReportingInstallation.Query().
		Where(reportinginstallation.UserIDEQ(userID)).
		Count(ctx)
	if err != nil {
		return result, fmt.Errorf("read reporting readiness: count installations: %w", err)
	}
	result.InstallationCount = total
	if total == 0 {
		result.State = ReportingReadinessNotEnrolled
		return result, nil
	}

	active, err := s.client.ReportingInstallation.Query().
		Where(
			reportinginstallation.UserIDEQ(userID),
			reportinginstallation.StatusEQ(reportinginstallation.StatusActive),
		).
		Count(ctx)
	if err != nil {
		return result, fmt.Errorf("read reporting readiness: count active installations: %w", err)
	}
	if active == 0 {
		result.State = ReportingReadinessRevoked
		return result, nil
	}

	enabled, err := s.client.ReportingInstallation.Query().
		Where(
			reportinginstallation.UserIDEQ(userID),
			reportinginstallation.StatusEQ(reportinginstallation.StatusActive),
			reportinginstallation.ReportingEnabledEQ(true),
		).
		Count(ctx)
	if err != nil {
		return result, fmt.Errorf("read reporting readiness: count enabled installations: %w", err)
	}
	result.EnabledInstallationCount = enabled
	if enabled == 0 {
		result.State = ReportingReadinessDisabled
		return result, nil
	}

	latest, err := s.client.AttributionUsageBucket.Query().
		Where(attributionusagebucket.UserIDEQ(userID)).
		Order(ent.Desc(attributionusagebucket.FieldCreatedAt)).
		First(ctx)
	if ent.IsNotFound(err) {
		result.State = ReportingReadinessWaitingForData
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("read reporting readiness: query latest bucket: %w", err)
	}
	latestAt := latest.CreatedAt
	result.State = ReportingReadinessActive
	result.LatestBucketAt = &latestAt
	return result, nil
}

func (s *InstallationService) Ensure(ctx context.Context, userID int, installationID, label, clientVersion string) (InstallationCredentials, error) {
	var result InstallationCredentials
	if s == nil || s.client == nil || userID <= 0 {
		return result, fmt.Errorf("ensure reporting installation: client and user are required")
	}
	installationID = strings.TrimSpace(installationID)
	if _, err := uuid.Parse(installationID); err != nil {
		return result, fmt.Errorf("ensure reporting installation: invalid installation_id")
	}
	existing, err := s.client.ReportingInstallation.Query().
		Where(reportinginstallation.InstallationIDEQ(installationID)).
		Only(ctx)
	if err == nil {
		if existing.UserID != userID {
			return result, fmt.Errorf("ensure reporting installation: %w", ErrInstallationForbidden)
		}
		if existing.Status != reportinginstallation.StatusActive {
			reporterToken, err := newScopedToken("aer_")
			if err != nil {
				return result, fmt.Errorf("ensure reporting installation: create reporter credential: %w", err)
			}
			otlpToken, err := newScopedToken("aeo_")
			if err != nil {
				return result, fmt.Errorf("ensure reporting installation: create OTLP credential: %w", err)
			}
			if err := existing.Update().
				SetLabel(strings.TrimSpace(label)).
				SetClientVersion(strings.TrimSpace(clientVersion)).
				SetReporterTokenHash(hashToken(reporterToken)).
				SetOtlpTokenHash(hashToken(otlpToken)).
				SetReportingEnabled(false).
				SetOtelEnabled(false).
				SetStatus(reportinginstallation.StatusActive).
				SetLastSeenAt(time.Now().UTC()).
				Exec(ctx); err != nil {
				return result, fmt.Errorf("ensure reporting installation: reactivate installation: %w", err)
			}
			return InstallationCredentials{
				InstallationID: installationID,
				ReporterToken:  reporterToken,
				OTLPToken:      otlpToken,
			}, nil
		}
		if err := existing.Update().
			SetLabel(strings.TrimSpace(label)).
			SetClientVersion(strings.TrimSpace(clientVersion)).
			SetLastSeenAt(time.Now().UTC()).
			Exec(ctx); err != nil {
			return result, fmt.Errorf("ensure reporting installation: update existing installation: %w", err)
		}
		return InstallationCredentials{
			InstallationID:   installationID,
			ReportingEnabled: existing.ReportingEnabled,
			OTelEnabled:      existing.OtelEnabled,
		}, nil
	}
	if !ent.IsNotFound(err) {
		return result, fmt.Errorf("ensure reporting installation: query: %w", err)
	}

	reporterToken, err := newScopedToken("aer_")
	if err != nil {
		return result, fmt.Errorf("ensure reporting installation: create reporter credential: %w", err)
	}
	otlpToken, err := newScopedToken("aeo_")
	if err != nil {
		return result, fmt.Errorf("ensure reporting installation: create OTLP credential: %w", err)
	}
	now := time.Now().UTC()
	_, err = s.client.ReportingInstallation.Create().
		SetInstallationID(installationID).
		SetUserID(userID).
		SetLabel(strings.TrimSpace(label)).
		SetClientVersion(strings.TrimSpace(clientVersion)).
		SetReporterTokenHash(hashToken(reporterToken)).
		SetOtlpTokenHash(hashToken(otlpToken)).
		SetLastSeenAt(now).
		Save(ctx)
	if err != nil {
		return result, fmt.Errorf("ensure reporting installation: create: %w", err)
	}
	return InstallationCredentials{
		InstallationID: installationID,
		ReporterToken:  reporterToken,
		OTLPToken:      otlpToken,
		Created:        true,
	}, nil
}

func (s *InstallationService) SetEnabled(ctx context.Context, userID int, installationID string, reportingEnabled, otelEnabled *bool) (InstallationCredentials, error) {
	row, err := s.client.ReportingInstallation.Query().
		Where(reportinginstallation.InstallationIDEQ(strings.TrimSpace(installationID))).
		Only(ctx)
	if err != nil {
		return InstallationCredentials{}, fmt.Errorf("set reporting installation flags: query installation: %w", err)
	}
	if row.UserID != userID {
		return InstallationCredentials{}, fmt.Errorf("set reporting installation flags: %w", ErrInstallationForbidden)
	}
	if row.Status != reportinginstallation.StatusActive {
		return InstallationCredentials{}, fmt.Errorf("reporting installation is revoked")
	}
	update := row.Update().SetLastSeenAt(time.Now().UTC())
	if reportingEnabled != nil {
		update.SetReportingEnabled(*reportingEnabled)
		row.ReportingEnabled = *reportingEnabled
	}
	if otelEnabled != nil {
		update.SetOtelEnabled(*otelEnabled)
		row.OtelEnabled = *otelEnabled
	}
	if err := update.Exec(ctx); err != nil {
		return InstallationCredentials{}, fmt.Errorf("update reporting installation: %w", err)
	}
	return InstallationCredentials{
		InstallationID:   row.InstallationID,
		ReportingEnabled: row.ReportingEnabled,
		OTelEnabled:      row.OtelEnabled,
	}, nil
}

// Rotate replaces both installation-scoped credentials. The plaintext values
// are returned once to the authenticated owner; only hashes remain in the DB.
func (s *InstallationService) Rotate(ctx context.Context, userID int, installationID string) (InstallationCredentials, error) {
	row, err := s.client.ReportingInstallation.Query().
		Where(reportinginstallation.InstallationIDEQ(strings.TrimSpace(installationID))).
		Only(ctx)
	if err != nil {
		return InstallationCredentials{}, fmt.Errorf("rotate reporting installation credentials: query installation: %w", err)
	}
	if row.UserID != userID {
		return InstallationCredentials{}, fmt.Errorf("rotate reporting installation credentials: %w", ErrInstallationForbidden)
	}
	if row.Status != reportinginstallation.StatusActive {
		return InstallationCredentials{}, fmt.Errorf("reporting installation is revoked")
	}
	reporterToken, err := newScopedToken("aer_")
	if err != nil {
		return InstallationCredentials{}, fmt.Errorf("rotate reporting installation credentials: create reporter credential: %w", err)
	}
	otlpToken, err := newScopedToken("aeo_")
	if err != nil {
		return InstallationCredentials{}, fmt.Errorf("rotate reporting installation credentials: create OTLP credential: %w", err)
	}
	if err := row.Update().
		SetReporterTokenHash(hashToken(reporterToken)).
		SetOtlpTokenHash(hashToken(otlpToken)).
		SetLastSeenAt(time.Now().UTC()).
		Exec(ctx); err != nil {
		return InstallationCredentials{}, fmt.Errorf("rotate reporting installation credentials: %w", err)
	}
	return InstallationCredentials{
		InstallationID:   row.InstallationID,
		ReporterToken:    reporterToken,
		OTLPToken:        otlpToken,
		ReportingEnabled: row.ReportingEnabled,
		OTelEnabled:      row.OtelEnabled,
	}, nil
}

func (s *InstallationService) AuthenticateReporter(ctx context.Context, token string) (InstallationPrincipal, error) {
	return s.authenticate(ctx, token, false)
}

func (s *InstallationService) AuthenticateOTLP(ctx context.Context, token string) (InstallationPrincipal, error) {
	return s.authenticate(ctx, token, true)
}

func (s *InstallationService) authenticate(ctx context.Context, token string, otlp bool) (InstallationPrincipal, error) {
	var principal InstallationPrincipal
	token = strings.TrimSpace(token)
	if token == "" {
		return principal, errors.New("missing installation token")
	}
	query := s.client.ReportingInstallation.Query()
	if otlp {
		query = query.Where(reportinginstallation.OtlpTokenHashEQ(hashToken(token)))
	} else {
		query = query.Where(reportinginstallation.ReporterTokenHashEQ(hashToken(token)))
	}
	row, err := query.Only(ctx)
	if err != nil || row.Status != reportinginstallation.StatusActive {
		return principal, errors.New("invalid installation token")
	}
	if otlp && !row.OtelEnabled {
		return principal, ErrOTLPDisabled
	}
	if !otlp && !row.ReportingEnabled {
		return principal, ErrReporterDisabled
	}
	_ = row.Update().SetLastSeenAt(time.Now().UTC()).Exec(ctx)
	return InstallationPrincipal{DatabaseID: row.ID, InstallationID: row.InstallationID, UserID: row.UserID}, nil
}

func newScopedToken(prefix string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate installation token: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}
