package activity

import "time"

const defaultPRSyncStaleAfter = 24 * time.Hour

type SyncCoverage struct {
	Complete                    bool `json:"complete"`
	AffectedRepositories        int  `json:"affected_repositories"`
	UnsyncedRepositories        int  `json:"unsynced_repositories"`
	StaleRepositories           int  `json:"stale_repositories"`
	PartiallySyncedRepositories int  `json:"partially_synced_repositories"`
	FailedRepositories          int  `json:"failed_repositories"`
}

type MemberIdentity struct {
	UserID                    int      `json:"user_id"`
	DirectoryMemberExternalID string   `json:"directory_member_external_id,omitempty"`
	DisplayName               string   `json:"display_name"`
	Email                     string   `json:"email"`
	DepartmentExternalIDs     []string `json:"department_external_ids"`
}

type Team struct {
	ExternalID       string  `json:"external_id"`
	ParentExternalID *string `json:"parent_external_id,omitempty"`
	Name             string  `json:"name"`
	DisplayPath      string  `json:"display_path"`
	MemberCount      int     `json:"member_count"`
}
