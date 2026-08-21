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
	"github.com/ai-efficiency/backend/ent/reportinginstallation"
	"github.com/google/uuid"
)

var (
	ErrInstallationForbidden = errors.New("reporting installation does not belong to authenticated user")
	ErrReporterDisabled      = errors.New("compact reporting is not enabled for this installation")
)

type InstallationService struct {
	client   *ent.Client
	protocol ProtocolContract
}

func NewInstallationService(client *ent.Client, protocol ProtocolContract) *InstallationService {
	return &InstallationService{client: client, protocol: protocol}
}

func (s *InstallationService) SetupState(ctx context.Context, userID int) (ReportingSetupState, error) {
	if s == nil || s.client == nil || userID <= 0 {
		return "", fmt.Errorf("load reporting setup state: client and user are required")
	}
	rows, err := s.client.ReportingInstallation.Query().
		Where(reportinginstallation.UserIDEQ(userID)).
		Select(reportinginstallation.FieldStatus, reportinginstallation.FieldReportingEnabled).
		All(ctx)
	if err != nil {
		return "", fmt.Errorf("load reporting setup state: %w", err)
	}
	if len(rows) == 0 {
		return ReportingSetupNotEnrolled, nil
	}
	hasActive := false
	for _, row := range rows {
		if row.Status == reportinginstallation.StatusActive {
			hasActive = true
			if row.ReportingEnabled {
				return ReportingSetupWaiting, nil
			}
		}
	}
	if hasActive {
		return ReportingSetupDisabled, nil
	}
	return ReportingSetupRevoked, nil
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
			return result, fmt.Errorf("ensure reporting installation: installation is revoked")
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
			Protocol:         s.protocol,
		}, nil
	}
	if !ent.IsNotFound(err) {
		return result, fmt.Errorf("ensure reporting installation: query: %w", err)
	}

	reporterToken, err := newScopedToken("aer_")
	if err != nil {
		return result, fmt.Errorf("ensure reporting installation: create reporter credential: %w", err)
	}
	now := time.Now().UTC()
	_, err = s.client.ReportingInstallation.Create().
		SetInstallationID(installationID).
		SetUserID(userID).
		SetLabel(strings.TrimSpace(label)).
		SetClientVersion(strings.TrimSpace(clientVersion)).
		SetReporterTokenHash(hashToken(reporterToken)).
		SetLastSeenAt(now).
		Save(ctx)
	if err != nil {
		return result, fmt.Errorf("ensure reporting installation: create: %w", err)
	}
	return InstallationCredentials{
		InstallationID: installationID,
		ReporterToken:  reporterToken,
		Created:        true,
		Protocol:       s.protocol,
	}, nil
}

func (s *InstallationService) SetEnabled(ctx context.Context, userID int, installationID string, reportingEnabled *bool) (InstallationCredentials, error) {
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
	if err := update.Exec(ctx); err != nil {
		return InstallationCredentials{}, fmt.Errorf("update reporting installation: %w", err)
	}
	return InstallationCredentials{
		InstallationID:   row.InstallationID,
		ReportingEnabled: row.ReportingEnabled,
		Protocol:         s.protocol,
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
	if err := row.Update().
		SetReporterTokenHash(hashToken(reporterToken)).
		SetLastSeenAt(time.Now().UTC()).
		Exec(ctx); err != nil {
		return InstallationCredentials{}, fmt.Errorf("rotate reporting installation credentials: %w", err)
	}
	return InstallationCredentials{
		InstallationID:   row.InstallationID,
		ReporterToken:    reporterToken,
		ReportingEnabled: row.ReportingEnabled,
		Protocol:         s.protocol,
	}, nil
}

func (s *InstallationService) AuthenticateReporter(ctx context.Context, token string) (InstallationPrincipal, error) {
	var principal InstallationPrincipal
	token = strings.TrimSpace(token)
	if token == "" {
		return principal, errors.New("missing installation token")
	}
	row, err := s.client.ReportingInstallation.Query().Where(reportinginstallation.ReporterTokenHashEQ(hashToken(token))).Only(ctx)
	if err != nil || row.Status != reportinginstallation.StatusActive {
		return principal, errors.New("invalid installation token")
	}
	if !row.ReportingEnabled {
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
