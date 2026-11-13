// Package kirocredits reads per-user Kiro credits from a kiro2cc admin API.
//
// The data is presentation-only: credits are Kiro's real upstream metering
// units, while the tokens recorded here are kiro2cc's cl100k estimates. They
// must never be summed with sub2api token usage or converted to USD.
//
// The client is deliberately read-only. It only issues GET requests against
// /api/admin/api-keys* and never creates, renames, resets or deletes keys.
package kirocredits

import "time"

// Status describes why a snapshot does or does not carry credits.
const (
	StatusOK        = "ok"
	StatusNoSource  = "no_source"
	StatusUnlinked  = "unlinked"
	StatusNoKey     = "no_key"
	StatusError     = "error"
	statusDedicated = "dedicated"
)

const (
	defaultTimeout    = 8 * time.Second
	defaultCacheTTL   = 60 * time.Second
	defaultKeyTTL     = 30 * time.Second
	maxRecordPages    = 10
	recordPageSize    = 500
	maxResponseBytes  = 8 << 20
	modernKeyNameBase = "sub2api-"
	legacyKeyNameBase = "sub2api-user-"
)

// Params is the dashboard range a caller wants credits for. Dates are
// YYYY-MM-DD in Timezone; the timezone also decides day buckets.
type Params struct {
	StartDate string
	EndDate   string
	Timezone  string
}

// ModelCredits is one model bucket inside the requested range.
type ModelCredits struct {
	Model        string  `json:"model"`
	Requests     int64   `json:"requests"`
	Credits      float64 `json:"credits"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
}

// DayCredits is one day bucket inside the requested range.
type DayCredits struct {
	Date     string  `json:"date"`
	Requests int64   `json:"requests"`
	Credits  float64 `json:"credits"`
}

// Snapshot is the credits view for one user. Available is true only when a
// kiro2cc key was found and read successfully; every other state renders as
// "no data" without failing the dashboard request.
type Snapshot struct {
	Available  bool      `json:"available"`
	Configured bool      `json:"configured"`
	KeyFound   bool      `json:"key_found"`
	Status     string    `json:"status"`
	Message    string    `json:"message,omitempty"`
	AsOf       time.Time `json:"as_of"`

	Credits      float64 `json:"credits"`
	CreditsSaved float64 `json:"credits_saved"`
	Requests     int64   `json:"requests"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`

	// Lifetime values come from kiro2cc's all-time per-key summary.
	LifetimeCredits  float64 `json:"lifetime_credits"`
	LifetimeRequests int64   `json:"lifetime_requests"`

	ByModel []ModelCredits `json:"by_model"`
	ByDay   []DayCredits   `json:"by_day"`

	// Partial means the range totals only cover the newest record pages the
	// admin API exposes; treat them as a lower bound, not an exact total.
	Partial bool `json:"partial"`
	// AmbiguousKey means more than one kiro2cc key matched this user; the
	// highest key ID was used.
	AmbiguousKey bool `json:"ambiguous_key,omitempty"`
}

// UserRef pairs an AI Efficiency user with the Relay user ID that owns the
// gateway-provisioned kiro2cc key.
type UserRef struct {
	UserID      int   `json:"user_id"`
	RelayUserID int64 `json:"relay_user_id"`
}

// UserCredits is one user's credits inside the requested range.
type UserCredits struct {
	UserID       int            `json:"user_id"`
	RelayUserID  int64          `json:"relay_user_id"`
	KeyFound     bool           `json:"key_found"`
	Credits      float64        `json:"credits"`
	CreditsSaved float64        `json:"credits_saved"`
	Requests     int64          `json:"requests"`
	InputTokens  int64          `json:"input_tokens"`
	OutputTokens int64          `json:"output_tokens"`
	ByModel      []ModelCredits `json:"by_model"`
	ByDay        []DayCredits   `json:"by_day"`
	Partial      bool           `json:"partial"`
}

// TeamCredits aggregates a set of users for one dashboard range. It is
// presentation-only, exactly like Snapshot: credits never mix with tokens.
type TeamCredits struct {
	Available  bool      `json:"available"`
	Configured bool      `json:"configured"`
	Status     string    `json:"status"`
	Message    string    `json:"message,omitempty"`
	AsOf       time.Time `json:"as_of"`

	Credits      float64 `json:"credits"`
	CreditsSaved float64 `json:"credits_saved"`
	Requests     int64   `json:"requests"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	KeyCount     int     `json:"key_count"`

	ByModel []ModelCredits `json:"by_model"`
	ByDay   []DayCredits   `json:"by_day"`
	Users   []UserCredits  `json:"users"`

	Partial bool `json:"partial"`
}
