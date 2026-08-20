package attributionledger

type InstallationCredentials struct {
	InstallationID   string           `json:"installation_id"`
	ReporterToken    string           `json:"reporter_token,omitempty"`
	Created          bool             `json:"created"`
	ReportingEnabled bool             `json:"reporting_enabled"`
	Protocol         ProtocolContract `json:"protocol"`
}

type InstallationPrincipal struct {
	DatabaseID     int
	InstallationID string
	UserID         int
}

type ReportingSetupState string

const (
	ReportingSetupNotEnrolled ReportingSetupState = "not_enrolled"
	ReportingSetupRevoked     ReportingSetupState = "revoked"
	ReportingSetupDisabled    ReportingSetupState = "disabled"
	ReportingSetupWaiting     ReportingSetupState = "waiting_for_data"
	ReportingSetupActive      ReportingSetupState = "active"
)
