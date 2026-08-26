package directoryfacts

import (
	"context"
	"time"
)

const (
	DepartmentRepresentativeIDsKey = "representative_external_ids"
	MemberLeaderDepartmentIDsKey   = "leader_department_ids"
)

type Snapshot struct {
	SourceID int
	RunID    int
}

type Reader interface {
	Current(context.Context) (View, bool, error)
	At(Snapshot) View
	LocalUsers(context.Context, *Snapshot, LocalUserQuery) (LocalUserPage, error)
}

type View interface {
	Snapshot() Snapshot
	Load(context.Context, Query) (*Facts, error)
	DepartmentPage(context.Context, DepartmentPageQuery) (DepartmentPage, error)
	DepartmentAggregates(context.Context, []string) (map[string]DepartmentAggregate, error)
}

type Query struct {
	AllDepartments              bool
	DepartmentIDs               []string
	IncludeDepartmentAncestors  bool
	AllMembers                  bool
	ActiveMembersOnly           bool
	MemberUserIDs               []int
	MemberEmails                []string
	MemberExternalIDs           []string
	RepresentativeDepartmentIDs []string
	IncludeMemberships          bool
	AllUsers                    bool
	UserIDs                     []int
	UserEmails                  []string
	KnownUsers                  []User
	MatchUsersForMembers        bool
}

type Department struct {
	ID                        int
	ExternalID                string
	ParentExternalID          *string
	EffectiveParentExternalID *string
	Name                      string
	Path                      string
	Metadata                  map[string]any
}

type Member struct {
	ID                   int
	ExternalID           string
	EmailNormalized      string
	DisplayName          string
	DepartmentExternalID string
	Status               string
	MatchedUserID        *int
	Metadata             map[string]any
}

type Membership struct {
	ID                   int
	DirectoryMemberID    int
	DepartmentExternalID string
}

type User struct {
	ID              int
	Username        string
	Email           string
	Role            string
	RelayUserID     *int
	TokenValidAfter *time.Time
	RelayDisabledAt *time.Time
}

type DepartmentStats struct {
	MemberCount      int
	MatchedUserCount int
}

type DepartmentPageQuery struct {
	Search   string
	ParentID *string
	Offset   int
	Limit    int
}

type DepartmentPage struct {
	Items []Department
	Total int
}

type DepartmentAggregate struct {
	ChildCount                 int
	MemberCount                int
	MatchedUserCount           int
	SubtreeMemberCount         int
	SubtreeMatchedUserCount    int
	RepresentativeCount        int
	MatchedRepresentativeCount int
}

type LocalUserQuery struct {
	Search       string
	DepartmentID string
	AccessStatus string
	Page         int
	Offset       int
	Limit        int
	IncludeTotal bool
}

type LocalUserPage struct {
	IDs   []int
	Total int
}
