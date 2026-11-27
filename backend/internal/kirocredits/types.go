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
	defaultTimeout        = 8 * time.Second
	defaultCacheTTL       = 60 * time.Second
	defaultKeyTTL         = 30 * time.Second
	maxRecordPages        = 200
	defaultCollectBudget  = 6 * time.Second
	defaultPerCallTimeout = 2500 * time.Millisecond
	defaultWarmTimezone   = "Asia/Shanghai"
	// defaultLongCollectBudget covers windows longer than a week. A heavy key
	// needs dozens of 500-record pages for a 30-day range, which cannot finish
	// inside the short batch budget.
	defaultLongCollectBudget = 30 * time.Second
	// defaultWarmLongInterval refreshes the long preset off the request path.
	defaultWarmLongInterval = 20 * time.Minute
	// longWindowSpan is the span above which a window is treated as long.
	longWindowSpan = 7 * 24 * time.Hour
	// warmShortBudget / warmLongBudget bound one background refresh. They are
	// far larger than the request budgets on purpose: the warmer may spend
	// minutes paginating a heavy key so the request path never has to.
	warmShortBudget = 90 * time.Second
	warmLongBudget  = 240 * time.Second
	recordPageSize        = 500
	maxResponseBytes      = 8 << 20
	modernKeyNameBase     = "sub2api-"
	legacyKeyNameBase     = "sub2api-user-"
)

// Params is the dashboard range a caller wants credits for. Dates are
// YYYY-MM-DD in Timezone; the timezone also decides day buckets.
type Params struct {
	StartDate   string
	EndDate     string
	Timezone    string
	Granularity string
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
	// Error carries a short reason ("timeout" / "error") when this member's
	// credits could not be read, so the UI can explain the gap instead of
	// rendering a bare placeholder.
	Error string `json:"error,omitempty"`
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

	// RequestedUsers/CountedUsers expose fan-out coverage so the UI can label
	// an incomplete total as a lower bound instead of showing it as final.
	RequestedUsers int `json:"requested_users"`
	CountedUsers   int `json:"counted_users"`

	Partial bool `json:"partial"`
}
