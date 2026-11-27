package kirocredits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// adminKey is one kiro2cc admin API key object. Key values are secrets and are
// never logged or returned to callers.
type adminKey struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

type modelUsage struct {
	Model        string  `json:"model"`
	Requests     int64   `json:"requests"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	Credits      float64 `json:"credits"`
	CreditsSaved float64 `json:"creditsSaved"`
}

type usageSummary struct {
	APIKeyID          int64        `json:"apiKeyId"`
	TotalRequests     int64        `json:"totalRequests"`
	TotalInputTokens  int64        `json:"totalInputTokens"`
	TotalOutputTokens int64        `json:"totalOutputTokens"`
	TotalCredits      float64      `json:"totalCredits"`
	TotalCreditsSaved float64      `json:"totalCreditsSaved"`
	ByModel           []modelUsage `json:"byModel"`
}

type usageRecord struct {
	Model        string    `json:"model"`
	InputTokens  int64     `json:"inputTokens"`
	OutputTokens int64     `json:"outputTokens"`
	CreditsUsed  *float64  `json:"creditsUsed"`
	CreditsSaved *float64  `json:"creditsSaved"`
	CreatedAt    time.Time `json:"createdAt"`
}

type usageRecordsPage struct {
	Records    []usageRecord `json:"records"`
	Total      int           `json:"total"`
	Page       int           `json:"page"`
	PageSize   int           `json:"pageSize"`
	TotalPages int           `json:"totalPages"`
}

// errAdminRouteMissing marks a 404 from the admin API so callers can fall back
// to the older record-pagination path on kiro2cc builds without the endpoint.
var errAdminRouteMissing = errors.New("kiro2cc admin route missing")

// usageRangeDay is one day bucket from the range aggregation.
type usageRangeDay struct {
	Date         string  `json:"date"`
	Requests     int64   `json:"requests"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	Cost         float64 `json:"cost"`
	Credits      float64 `json:"credits"`
	CreditsSaved float64 `json:"creditsSaved"`
}

// usageRangeModel is one model bucket from the range aggregation.
type usageRangeModel struct {
	Model        string  `json:"model"`
	Requests     int64   `json:"requests"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	Cost         float64 `json:"cost"`
	Credits      float64 `json:"credits"`
	CreditsSaved float64 `json:"creditsSaved"`
}

// usageRangeSummary is kiro2cc's daily-store aggregation for one key and range.
type usageRangeSummary struct {
	APIKeyID          int64             `json:"apiKeyId"`
	FromDate          string            `json:"fromDate"`
	ToDate            string            `json:"toDate"`
	TotalRequests     int64             `json:"totalRequests"`
	TotalInputTokens  int64             `json:"totalInputTokens"`
	TotalOutputTokens int64             `json:"totalOutputTokens"`
	TotalCost         float64           `json:"totalCost"`
	TotalCredits      float64           `json:"totalCredits"`
	TotalCreditsSaved float64           `json:"totalCreditsSaved"`
	ByDay             []usageRangeDay   `json:"byDay"`
	ByModel           []usageRangeModel `json:"byModel"`
}

// adminClient is a read-only kiro2cc admin API client. Every method issues a
// GET; there is intentionally no method that can mutate kiro2cc state.
type adminClient struct {
	baseURL     string
	adminToken  string
	httpClient  *http.Client
	callTimeout time.Duration
}

// newAdminClient builds the read-only kiro2cc admin client. callTimeout bounds
// a single HTTP attempt; timeout still bounds the whole client. Zero keeps the
// historical behaviour (no per-call deadline).
func newAdminClient(baseURL, adminToken string, timeout, callTimeout time.Duration) *adminClient {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &adminClient{
		baseURL:     strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		adminToken:  strings.TrimSpace(adminToken),
		httpClient:  &http.Client{Timeout: timeout},
		callTimeout: callTimeout,
	}
}

func (c *adminClient) listKeys(ctx context.Context) ([]adminKey, error) {
	var keys []adminKey
	if err := c.get(ctx, "/api/admin/api-keys", nil, &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

func (c *adminClient) usageSummary(ctx context.Context, keyID int64) (*usageSummary, error) {
	var summary usageSummary
	path := "/api/admin/api-keys/" + strconv.FormatInt(keyID, 10) + "/usage"
	if err := c.get(ctx, path, nil, &summary); err != nil {
		return nil, err
	}
	return &summary, nil
}

// usageRange aggregates one key over an inclusive CST date range from kiro2cc's
// daily store: one request instead of one page per 500 records.
func (c *adminClient) usageRange(ctx context.Context, keyID int64, startDate, endDate string) (*usageRangeSummary, error) {
	query := url.Values{}
	query.Set("start", startDate)
	query.Set("end", endDate)
	var summary usageRangeSummary
	path := "/api/admin/api-keys/" + strconv.FormatInt(keyID, 10) + "/usage/range"
	if err := c.get(ctx, path, query, &summary); err != nil {
		return nil, err
	}
	return &summary, nil
}

func (c *adminClient) usageRecords(ctx context.Context, keyID int64, page, pageSize int) (*usageRecordsPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = recordPageSize
	}
	query := url.Values{}
	query.Set("page", strconv.Itoa(page))
	query.Set("page_size", strconv.Itoa(pageSize))
	var records usageRecordsPage
	path := "/api/admin/api-keys/" + strconv.FormatInt(keyID, 10) + "/usage/records"
	if err := c.get(ctx, path, query, &records); err != nil {
		return nil, err
	}
	return &records, nil
}

func (c *adminClient) get(ctx context.Context, path string, query url.Values, out any) error {
	if c == nil {
		return fmt.Errorf("kiro2cc admin client is not configured")
	}
	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	if c.callTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.callTimeout)
		defer cancel()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("build kiro2cc request: %w", err)
	}
	if c.adminToken != "" {
		request.Header.Set("x-api-key", c.adminToken)
	}
	request.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("call kiro2cc admin API: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		_ = response.Body.Close()
	}()
	if response.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: %s", errAdminRouteMissing, path)
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("kiro2cc admin API %s returned %d", path, response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes))
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("decode kiro2cc admin API %s: %w", path, err)
	}
	return nil
}
