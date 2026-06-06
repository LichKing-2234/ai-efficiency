# 用户用量趋势页面实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。

**目标：** 新增 `/user/usage` 页面，展示当前用户的 AI 用量汇总、趋势折线、模型分布。

**架构：** 前端调用 `/api/v1/user/usage/{stats,trend,models}` → 后端解密用户 relay 密码 → relay 层用用户 JWT 调 sub2api dashboard 接口 → 透传聚合数据回前端。

**技术栈：** Go (Gin + Ent), Vue 3 (Vite + TailwindCSS + Pinia), axios, sub2api user API

---

## 文件结构

### 后端

| 文件 | 职责 |
|---|---|
| `backend/internal/relay/types.go` | 新增用量 dashboard 相关 struct |
| `backend/internal/relay/provider.go` | Provider 接口新增 3 个方法 |
| `backend/internal/relay/sub2api.go` | 实现 3 个方法（loginSessionToken + HTTP 调用） |
| `backend/internal/relay/sub2api_test.go` | relay 层单元测试 |
| `backend/internal/handler/user_usage.go` | 新增 handler（Stats/Trend/Models） |
| `backend/internal/handler/user_usage_test.go` | handler 层单元测试 |
| `backend/internal/handler/router.go` | 注册 3 个路由 |

### 前端

| 文件 | 职责 |
|---|---|
| `frontend/src/api/userUsage.ts` | API 封装（3 个函数） |
| `frontend/src/types/usage.ts` | 用量相关类型定义 |
| `frontend/src/views/user/UsageView.vue` | 用量页面主组件 |
| `frontend/src/components/user/usage/UsageStatsCards.vue` | 统计卡片行 |
| `frontend/src/components/user/usage/UsageTrendChart.vue` | 趋势折线图 |
| `frontend/src/components/user/usage/UsageModelChart.vue` | 模型分布图 |
| `frontend/src/router/index.ts` | 新增路由 |

---

## 任务 1：Relay 层 — 类型定义

**文件：**
- 修改：`backend/internal/relay/types.go`

- [ ] **步骤 1：在 types.go 末尾新增用量 dashboard 类型**

```go
// UserUsageStats represents aggregated usage statistics for a user.
type UserUsageStats struct {
	TotalRequests         int64   `json:"total_requests"`
	TotalInputTokens      int64   `json:"total_input_tokens"`
	TotalOutputTokens     int64   `json:"total_output_tokens"`
	TotalCacheReadTokens  int64   `json:"total_cache_read_tokens"`
	TotalCacheWriteTokens int64   `json:"total_cache_creation_tokens"`
	TotalTokens           int64   `json:"total_tokens"`
	TotalCost             float64 `json:"total_cost"`
	TotalActualCost       float64 `json:"total_actual_cost"`
	TodayRequests        int64   `json:"today_requests"`
	TodayInputTokens      int64   `json:"today_input_tokens"`
	TodayOutputTokens     int64   `json:"today_output_tokens"`
	TodayTokens           int64   `json:"today_tokens"`
	TodayCost             float64 `json:"today_cost"`
	TodayActualCost       float64 `json:"today_actual_cost"`
	AverageDurationMs     int64   `json:"average_duration_ms"`
}

// UsageTrendParams defines parameters for usage trend queries.
type UsageTrendParams struct {
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	Granularity string `json:"granularity"`
	Timezone    string `json:"timezone"`
}

// UsageTrendDataPoint represents a single data point in usage trend.
type UsageTrendDataPoint struct {
	Date         string  `json:"date"`
	Requests     int64   `json:"requests"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	TotalTokens  int64   `json:"total_tokens"`
	Cost         float64 `json:"cost"`
	ActualCost   float64 `json:"actual_cost"`
}

// UsageTrendResponse wraps trend data with metadata.
type UsageTrendResponse struct {
	Trend       []UsageTrendDataPoint `json:"trend"`
	StartDate   string                `json:"start_date"`
	EndDate     string                `json:"end_date"`
	Granularity string                `json:"granularity"`
}

// UsageModelParams defines parameters for model breakdown queries.
type UsageModelParams struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Timezone  string `json:"timezone"`
}

// UsageModelStat represents usage statistics for a single model.
type UsageModelStat struct {
	Model        string  `json:"model"`
	Requests     int64   `json:"requests"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	TotalTokens  int64   `json:"total_tokens"`
	Cost         float64 `json:"cost"`
	ActualCost   float64 `json:"actual_cost"`
}

// UsageModelResponse wraps model breakdown data with metadata.
type UsageModelResponse struct {
	Models    []UsageModelStat `json:"models"`
	StartDate string           `json:"start_date"`
	EndDate   string           `json:"end_date"`
}
```

- [ ] **步骤 2：验证编译通过**

运行：`cd /Users/admin/ai-efficiency/backend && go build ./...`
预期：编译成功，无错误

- [ ] **步骤 3：Commit**

```bash
git add backend/internal/relay/types.go
git commit -m "feat(relay): add user usage dashboard types"
```

---

## 任务 2：Relay 层 — Provider 接口扩展

**文件：**
- 修改：`backend/internal/relay/provider.go:41-62`

- [ ] **步骤 1：在 Provider 接口末尾新增 3 个方法签名**

```go
type Provider interface {
	// ... existing methods ...

	GetUserUsageStats(ctx context.Context, login, password string) (*UserUsageStats, error)
	GetUserUsageTrend(ctx context.Context, login, password string, params UsageTrendParams) (*UsageTrendResponse, error)
	GetUserUsageModels(ctx context.Context, login, password string, params UsageModelParams) (*UsageModelResponse, error)
}
```

- [ ] **步骤 2：验证编译失败（预期行为，因为 sub2api 实现还未添加）**

运行：`cd /Users/admin/ai-efficiency/backend && go build ./...`
预期：编译错误，提示 `sub2apiRelay does not implement Provider`

- [ ] **步骤 3：Commit**

```bash
git add backend/internal/relay/provider.go
git commit -m "feat(relay): extend Provider interface with user usage methods"
```

---

## 任务 3：Relay 层 — sub2api 实现

**文件：**
- 修改：`backend/internal/relay/sub2api.go`（在文件末尾添加）

- [ ] **步骤 1：实现 GetUserUsageStats**

```go
func (s *sub2apiRelay) GetUserUsageStats(ctx context.Context, login, password string) (*UserUsageStats, error) {
	token, _, err := s.loginSessionToken(ctx, login, password)
	if err != nil {
		return nil, fmt.Errorf("relay: login for usage stats: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.adminURL+"/api/v1/usage/dashboard/stats", nil)
	if err != nil {
		return nil, fmt.Errorf("relay: create usage stats request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("relay: fetch usage stats: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrInvalidCredentials
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: usage stats: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Code int            `json:"code"`
		Data UserUsageStats `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: decode usage stats: %w", err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("relay: usage stats: code %d", result.Code)
	}

	return &result.Data, nil
}
```

- [ ] **步骤 2：实现 GetUserUsageTrend**

```go
func (s *sub2apiRelay) GetUserUsageTrend(ctx context.Context, login, password string, params UsageTrendParams) (*UsageTrendResponse, error) {
	token, _, err := s.loginSessionToken(ctx, login, password)
	if err != nil {
		return nil, fmt.Errorf("relay: login for usage trend: %w", err)
	}

	u, err := url.Parse(s.adminURL + "/api/v1/usage/dashboard/trend")
	if err != nil {
		return nil, fmt.Errorf("relay: parse usage trend url: %w", err)
	}
	q := u.Query()
	if params.StartDate != "" {
		q.Set("start_date", params.StartDate)
	}
	if params.EndDate != "" {
		q.Set("end_date", params.EndDate)
	}
	if params.Granularity != "" {
		q.Set("granularity", params.Granularity)
	}
	if params.Timezone != "" {
		q.Set("timezone", params.Timezone)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("relay: create usage trend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("relay: fetch usage trend: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrInvalidCredentials
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: usage trend: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Code int                `json:"code"`
		Data UsageTrendResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: decode usage trend: %w", err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("relay: usage trend: code %d", result.Code)
	}

	return &result.Data, nil
}
```

- [ ] **步骤 3：实现 GetUserUsageModels**

```go
func (s *sub2apiRelay) GetUserUsageModels(ctx context.Context, login, password string, params UsageModelParams) (*UsageModelResponse, error) {
	token, _, err := s.loginSessionToken(ctx, login, password)
	if err != nil {
		return nil, fmt.Errorf("relay: login for usage models: %w", err)
	}

	u, err := url.Parse(s.adminURL + "/api/v1/usage/dashboard/models")
	if err != nil {
		return nil, fmt.Errorf("relay: parse usage models url: %w", err)
	}
	q := u.Query()
	if params.StartDate != "" {
		q.Set("start_date", params.StartDate)
	}
	if params.EndDate != "" {
		q.Set("end_date", params.EndDate)
	}
	if params.Timezone != "" {
		q.Set("timezone", params.Timezone)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("relay: create usage models request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("relay: fetch usage models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrInvalidCredentials
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: usage models: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Code int                `json:"code"`
		Data UsageModelResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: decode usage models: %w", err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("relay: usage models: code %d", result.Code)
	}

	return &result.Data, nil
}
```

- [ ] **步骤 4：验证编译通过**

运行：`cd /Users/admin/ai-efficiency/backend && go build ./...`
预期：编译成功

- [ ] **步骤 5：Commit**

```bash
git add backend/internal/relay/sub2api.go
git commit -m "feat(relay): implement user usage dashboard methods"
```

---

## 任务 4：Relay 层 — 单元测试

**文件：**
- 修改：`backend/internal/relay/sub2api_test.go`（在文件末尾添加）

- [ ] **步骤 1：编写 GetUserUsageStats 测试**

```go
func TestGetUserUsageStats(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":0,"data":{"access_token":"test-jwt"}}`)
	})
	mux.HandleFunc("/api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":0,"data":{"id":1,"username":"alice","email":"alice@example.com"}}`)
	})
	mux.HandleFunc("/api/v1/usage/dashboard/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-jwt" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"code":0,"data":{"total_requests":100,"total_tokens":50000,"total_actual_cost":1.5,"today_requests":10,"today_tokens":5000,"today_actual_cost":0.15,"average_duration_ms":250}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	relay := &sub2apiRelay{
		adminURL: server.URL,
		client:   server.Client(),
	}

	stats, err := relay.GetUserUsageStats(context.Background(), "alice@example.com", "test-password")
	if err != nil {
		t.Fatalf("GetUserUsageStats() unexpected error: %v", err)
	}
	if stats.TotalRequests != 100 {
		t.Errorf("TotalRequests = %d, want 100", stats.TotalRequests)
	}
	if stats.TotalTokens != 50000 {
		t.Errorf("TotalTokens = %d, want 50000", stats.TotalTokens)
	}
	if stats.TodayRequests != 10 {
		t.Errorf("TodayRequests = %d, want 10", stats.TodayRequests)
	}
}

func TestGetUserUsageStatsLoginFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	relay := &sub2apiRelay{
		adminURL: server.URL,
		client:   server.Client(),
	}

	_, err := relay.GetUserUsageStats(context.Background(), "alice@example.com", "wrong-password")
	if err == nil {
		t.Fatal("GetUserUsageStats() expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("error = %v, want ErrInvalidCredentials", err)
	}
}
```

- [ ] **步骤 2：编写 GetUserUsageTrend 测试**

```go
func TestGetUserUsageTrend(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":0,"data":{"access_token":"test-jwt"}}`)
	})
	mux.HandleFunc("/api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":0,"data":{"id":1,"username":"alice","email":"alice@example.com"}}`)
	})
	mux.HandleFunc("/api/v1/usage/dashboard/trend", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-jwt" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("start_date") != "2026-06-01" {
			t.Errorf("start_date = %s, want 2026-06-01", r.URL.Query().Get("start_date"))
		}
		if r.URL.Query().Get("end_date") != "2026-06-07" {
			t.Errorf("end_date = %s, want 2026-06-07", r.URL.Query().Get("end_date"))
		}
		if r.URL.Query().Get("granularity") != "day" {
			t.Errorf("granularity = %s, want day", r.URL.Query().Get("granularity"))
		}
		fmt.Fprint(w, `{"code":0,"data":{"trend":[{"date":"2026-06-01","requests":20,"total_tokens":8000,"actual_cost":0.24}],"start_date":"2026-06-01","end_date":"2026-06-07","granularity":"day"}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	relay := &sub2apiRelay{
		adminURL: server.URL,
		client:   server.Client(),
	}

	trend, err := relay.GetUserUsageTrend(context.Background(), "alice@example.com", "test-password", UsageTrendParams{
		StartDate:   "2026-06-01",
		EndDate:     "2026-06-07",
		Granularity: "day",
	})
	if err != nil {
		t.Fatalf("GetUserUsageTrend() unexpected error: %v", err)
	}
	if len(trend.Trend) != 1 {
		t.Fatalf("Trend length = %d, want 1", len(trend.Trend))
	}
	if trend.Trend[0].Date != "2026-06-01" {
		t.Errorf("Trend[0].Date = %s, want 2026-06-01", trend.Trend[0].Date)
	}
	if trend.Trend[0].TotalTokens != 8000 {
		t.Errorf("Trend[0].TotalTokens = %d, want 8000", trend.Trend[0].TotalTokens)
	}
}
```

- [ ] **步骤 3：编写 GetUserUsageModels 测试**

```go
func TestGetUserUsageModels(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":0,"data":{"access_token":"test-jwt"}}`)
	})
	mux.HandleFunc("/api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":0,"data":{"id":1,"username":"alice","email":"alice@example.com"}}`)
	})
	mux.HandleFunc("/api/v1/usage/dashboard/models", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-jwt" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"code":0,"data":{"models":[{"model":"gpt-4","requests":50,"total_tokens":25000,"actual_cost":0.75},{"model":"claude-3","requests":30,"total_tokens":15000,"actual_cost":0.45}],"start_date":"2026-06-01","end_date":"2026-06-07"}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	relay := &sub2apiRelay{
		adminURL: server.URL,
		client:   server.Client(),
	}

	models, err := relay.GetUserUsageModels(context.Background(), "alice@example.com", "test-password", UsageModelParams{
		StartDate: "2026-06-01",
		EndDate:   "2026-06-07",
	})
	if err != nil {
		t.Fatalf("GetUserUsageModels() unexpected error: %v", err)
	}
	if len(models.Models) != 2 {
		t.Fatalf("Models length = %d, want 2", len(models.Models))
	}
	if models.Models[0].Model != "gpt-4" {
		t.Errorf("Models[0].Model = %s, want gpt-4", models.Models[0].Model)
	}
	if models.Models[0].TotalTokens != 25000 {
		t.Errorf("Models[0].TotalTokens = %d, want 25000", models.Models[0].TotalTokens)
	}
}
```

- [ ] **步骤 4：运行测试验证通过**

运行：`cd /Users/admin/ai-efficiency/backend && go test ./internal/relay -run TestGetUserUsage -v`
预期：所有测试 PASS

- [ ] **步骤 5：Commit**

```bash
git add backend/internal/relay/sub2api_test.go
git commit -m "test(relay): add user usage dashboard unit tests"
```

---

## 任务 5：Handler 层 — UserUsageHandler 实现

**文件：**
- 创建：`backend/internal/handler/user_usage.go`

- [ ] **步骤 1：创建 handler 文件**

```go
package handler

import (
	"net/http"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/gin-gonic/gin"
)

type UserUsageHandler struct {
	entClient     *ent.Client
	relayProvider relay.Provider
	encryptionKey string
}

func NewUserUsageHandler(entClient *ent.Client, relayProvider relay.Provider, encryptionKey string) *UserUsageHandler {
	return &UserUsageHandler{
		entClient:     entClient,
		relayProvider: relayProvider,
		encryptionKey: encryptionKey,
	}
}

func (h *UserUsageHandler) Stats(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	u, err := h.entClient.User.Get(c.Request.Context(), uc.UserID)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "fetch user: "+err.Error())
		return
	}

	if u.RelayAuthPassword == nil || strings.TrimSpace(*u.RelayAuthPassword) == "" {
		pkg.Success(c, nil)
		return
	}

	password, err := pkg.Decrypt(strings.TrimSpace(*u.RelayAuthPassword), h.encryptionKey)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "decrypt relay password: "+err.Error())
		return
	}

	login := firstNonEmptyString(u.Email, u.Username)
	if login == "" {
		pkg.Error(c, http.StatusUnprocessableEntity, "user has no email or username")
		return
	}

	stats, err := h.relayProvider.GetUserUsageStats(c.Request.Context(), login, password)
	if err != nil {
		pkg.Error(c, http.StatusBadGateway, "get usage stats: "+err.Error())
		return
	}

	pkg.Success(c, stats)
}

func (h *UserUsageHandler) Trend(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	u, err := h.entClient.User.Get(c.Request.Context(), uc.UserID)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "fetch user: "+err.Error())
		return
	}

	if u.RelayAuthPassword == nil || strings.TrimSpace(*u.RelayAuthPassword) == "" {
		pkg.Success(c, &relay.UsageTrendResponse{})
		return
	}

	password, err := pkg.Decrypt(strings.TrimSpace(*u.RelayAuthPassword), h.encryptionKey)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "decrypt relay password: "+err.Error())
		return
	}

	params := relay.UsageTrendParams{
		StartDate:   c.Query("start_date"),
		EndDate:     c.Query("end_date"),
		Granularity: c.DefaultQuery("granularity", "day"),
		Timezone:    c.Query("timezone"),
	}

	login := firstNonEmptyString(u.Email, u.Username)
	if login == "" {
		pkg.Error(c, http.StatusUnprocessableEntity, "user has no email or username")
		return
	}

	trend, err := h.relayProvider.GetUserUsageTrend(c.Request.Context(), login, password, params)
	if err != nil {
		pkg.Error(c, http.StatusBadGateway, "get usage trend: "+err.Error())
		return
	}

	pkg.Success(c, trend)
}

func (h *UserUsageHandler) Models(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	u, err := h.entClient.User.Get(c.Request.Context(), uc.UserID)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "fetch user: "+err.Error())
		return
	}

	if u.RelayAuthPassword == nil || strings.TrimSpace(*u.RelayAuthPassword) == "" {
		pkg.Success(c, &relay.UsageModelResponse{})
		return
	}

	password, err := pkg.Decrypt(strings.TrimSpace(*u.RelayAuthPassword), h.encryptionKey)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "decrypt relay password: "+err.Error())
		return
	}

	params := relay.UsageModelParams{
		StartDate: c.Query("start_date"),
		EndDate:   c.Query("end_date"),
		Timezone:  c.Query("timezone"),
	}

	login := firstNonEmptyString(u.Email, u.Username)
	if login == "" {
		pkg.Error(c, http.StatusUnprocessableEntity, "user has no email or username")
		return
	}

	models, err := h.relayProvider.GetUserUsageModels(c.Request.Context(), login, password, params)
	if err != nil {
		pkg.Error(c, http.StatusBadGateway, "get usage models: "+err.Error())
		return
	}

	pkg.Success(c, models)
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
```

- [ ] **步骤 2：验证编译通过**

运行：`cd /Users/admin/ai-efficiency/backend && go build ./...`
预期：编译成功（`firstNonEmptyString` 可能与其他文件冲突，如有则删除重复定义）

- [ ] **步骤 3：Commit**

```bash
git add backend/internal/handler/user_usage.go
git commit -m "feat(handler): add UserUsageHandler for dashboard endpoints"
```

---

## 任务 6：Handler 层 — 路由注册

**文件：**
- 修改：`backend/internal/handler/router.go:178-187`

- [ ] **步骤 1：在 userGroup 路由块内注册用量路由**

在 `userGroup := protected.Group("/user")` 块内，现有路由之后添加：

```go
if providerHandler != nil {
	// ... existing routes ...
}

// User usage dashboard
if providerHandler != nil && providerHandler.relayProvider != nil {
	userUsageHandler := NewUserUsageHandler(entClient, providerHandler.relayProvider, encryptionKey)
	userGroup.GET("/usage/stats", userUsageHandler.Stats)
	userGroup.GET("/usage/trend", userUsageHandler.Trend)
	userGroup.GET("/usage/models", userUsageHandler.Models)
}
```

注意：需要确认 `providerHandler.relayProvider` 字段是否存在。如果不存在，需要在 `ProviderHandler` 中添加该字段或使用其他方式获取 relay provider。

- [ ] **步骤 2：验证编译通过**

运行：`cd /Users/admin/ai-efficiency/backend && go build ./...`
预期：编译成功

- [ ] **步骤 3：Commit**

```bash
git add backend/internal/handler/router.go
git commit -m "feat(handler): register user usage dashboard routes"
```

---

## 任务 7：Handler 层 — 单元测试

**文件：**
- 创建：`backend/internal/handler/user_usage_test.go`

- [ ] **步骤 1：编写 mock relay provider**

```go
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/ai-efficiency/backend/internal/relay"
)

type stubRelayProvider struct {
	statsResult  *relay.UserUsageStats
	statsErr     error
	trendResult  *relay.UsageTrendResponse
	trendErr     error
	modelsResult *relay.UsageModelResponse
	modelsErr    error
	lastLogin    string
	lastPassword string
}

func (s *stubRelayProvider) GetUserUsageStats(ctx context.Context, login, password string) (*relay.UserUsageStats, error) {
	s.lastLogin = login
	s.lastPassword = password
	return s.statsResult, s.statsErr
}

func (s *stubRelayProvider) GetUserUsageTrend(ctx context.Context, login, password string, params relay.UsageTrendParams) (*relay.UsageTrendResponse, error) {
	s.lastLogin = login
	s.lastPassword = password
	return s.trendResult, s.trendErr
}

func (s *stubRelayProvider) GetUserUsageModels(ctx context.Context, login, password string, params relay.UsageModelParams) (*relay.UsageModelResponse, error) {
	s.lastLogin = login
	s.lastPassword = password
	return s.modelsResult, s.modelsErr
}

// Stub other required Provider interface methods
func (s *stubRelayProvider) Ping(ctx context.Context) error { return nil }
func (s *stubRelayProvider) Name() string                   { return "stub" }
func (s *stubRelayProvider) Authenticate(ctx context.Context, username, password string) (*relay.User, error) {
	return nil, nil
}
func (s *stubRelayProvider) GetUser(ctx context.Context, userID int64) (*relay.User, error) {
	return nil, nil
}
func (s *stubRelayProvider) ListAllowedGroupsForUser(ctx context.Context, userID int64) ([]relay.Group, error) {
	return nil, nil
}
func (s *stubRelayProvider) FindUserByEmail(ctx context.Context, email string) (*relay.User, error) {
	return nil, nil
}
func (s *stubRelayProvider) FindUserByUsername(ctx context.Context, username string) (*relay.User, error) {
	return nil, nil
}
func (s *stubRelayProvider) CreateUser(ctx context.Context, req relay.CreateUserRequest) (*relay.User, error) {
	return nil, nil
}
func (s *stubRelayProvider) UpdateUser(ctx context.Context, userID int64, req relay.UpdateUserRequest) (*relay.User, error) {
	return nil, nil
}
func (s *stubRelayProvider) ChatCompletion(ctx context.Context, req relay.ChatCompletionRequest) (*relay.ChatCompletionResponse, error) {
	return nil, nil
}
func (s *stubRelayProvider) ChatCompletionWithTools(ctx context.Context, req relay.ChatCompletionRequest, tools []relay.ToolDef) (*relay.ChatCompletionWithToolsResponse, error) {
	return nil, nil
}
func (s *stubRelayProvider) GetUsageStats(ctx context.Context, userID int64, from, to time.Time) (*relay.UsageStats, error) {
	return nil, nil
}
func (s *stubRelayProvider) ListUserAPIKeys(ctx context.Context, userID int64) ([]relay.APIKey, error) {
	return nil, nil
}
func (s *stubRelayProvider) CreateUserAPIKey(ctx context.Context, userID int64, req relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error) {
	return nil, nil
}
func (s *stubRelayProvider) UpdateUserAPIKeyStatus(ctx context.Context, keyID int64, status string) error {
	return nil
}
func (s *stubRelayProvider) RevokeUserAPIKey(ctx context.Context, keyID int64) error { return nil }
func (s *stubRelayProvider) ListUsageLogsByAPIKeyExact(ctx context.Context, apiKeyID int64, from, to time.Time) ([]relay.UsageLog, error) {
	return nil, nil
}
```

- [ ] **步骤 2：编写 Stats 端点测试**

```go
func TestUserUsageStatsReturnsData(t *testing.T) {
	env := setupTestEnv(t)
	stub := &stubRelayProvider{
		statsResult: &relay.UserUsageStats{
			TotalRequests:     100,
			TotalTokens:       50000,
			TotalActualCost:   1.5,
			TodayRequests:    10,
			TodayTokens:       5000,
			TodayActualCost:   0.15,
			AverageDurationMs: 250,
		},
	}

	// Create user with relay password
	encryptedPassword, _ := pkg.Encrypt("test-password", env.encryptionKey)
	user, _ := env.entClient.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetRelayUserID(5).
		SetRelayAuthPassword(encryptedPassword).
		Save(context.Background())

	handler := NewUserUsageHandler(env.entClient, stub, env.encryptionKey)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/user/usage/stats", nil)
	c.Set(auth.UserContextKey, &auth.UserContext{UserID: user.ID})

	handler.Stats(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var result struct {
		Data *relay.UserUsageStats `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if result.Data.TotalRequests != 100 {
		t.Errorf("TotalRequests = %d, want 100", result.Data.TotalRequests)
	}
}

func TestUserUsageStatsNoPassword(t *testing.T) {
	env := setupTestEnv(t)
	stub := &stubRelayProvider{}

	user, _ := env.entClient.User.Create().
		SetUsername("bob").
		SetEmail("bob@example.com").
		Save(context.Background())

	handler := NewUserUsageHandler(env.entClient, stub, env.encryptionKey)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/user/usage/stats", nil)
	c.Set(auth.UserContextKey, &auth.UserContext{UserID: user.ID})

	handler.Stats(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var result struct {
		Data interface{} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if result.Data != nil {
		t.Errorf("Data = %v, want nil", result.Data)
	}
}
```

- [ ] **步骤 3：运行测试验证通过**

运行：`cd /Users/admin/ai-efficiency/backend && go test ./internal/handler -run TestUserUsage -v`
预期：所有测试 PASS

- [ ] **步骤 4：Commit**

```bash
git add backend/internal/handler/user_usage_test.go
git commit -m "test(handler): add user usage dashboard handler tests"
```

---

## 任务 8：前端 — 类型定义

**文件：**
- 修改：`frontend/src/types/index.ts`（或创建新文件 `frontend/src/types/usage.ts`）

- [ ] **步骤 1：新增用量相关类型**

```typescript
export interface UserUsageStats {
  total_requests: number
  total_input_tokens: number
  total_output_tokens: number
  total_cache_read_tokens: number
  total_cache_creation_tokens: number
  total_tokens: number
  total_cost: number
  total_actual_cost: number
  today_requests: number
  today_input_tokens: number
  today_output_tokens: number
  today_tokens: number
  today_cost: number
  today_actual_cost: number
  average_duration_ms: number
}

export interface UsageTrendDataPoint {
  date: string
  requests: number
  input_tokens: number
  output_tokens: number
  total_tokens: number
  cost: number
  actual_cost: number
}

export interface UsageTrendResponse {
  trend: UsageTrendDataPoint[]
  start_date: string
  end_date: string
  granularity: string
}

export interface UsageModelStat {
  model: string
  requests: number
  input_tokens: number
  output_tokens: number
  total_tokens: number
  cost: number
  actual_cost: number
}

export interface UsageModelResponse {
  models: UsageModelStat[]
  start_date: string
  end_date: string
}
```

- [ ] **步骤 2：Commit**

```bash
git add frontend/src/types/usage.ts
git commit -m "feat(frontend): add usage dashboard types"
```

---

## 任务 9：前端 — API 封装

**文件：**
- 创建：`frontend/src/api/userUsage.ts`

- [ ] **步骤 1：创建 API 文件**

```typescript
import client from './client'
import type {
  ApiResponse,
  UserUsageStats,
  UsageTrendResponse,
  UsageModelResponse,
} from '@/types'

export function getUserUsageStats() {
  return client.get<ApiResponse<UserUsageStats | null>>('/user/usage/stats')
}

export function getUserUsageTrend(params: {
  start_date?: string
  end_date?: string
  granularity?: 'day' | 'hour'
  timezone?: string
}) {
  return client.get<ApiResponse<UsageTrendResponse>>('/user/usage/trend', { params })
}

export function getUserUsageModels(params: {
  start_date?: string
  end_date?: string
  timezone?: string
}) {
  return client.get<ApiResponse<UsageModelResponse>>('/user/usage/models', { params })
}
```

- [ ] **步骤 2：Commit**

```bash
git add frontend/src/api/userUsage.ts
git commit -m "feat(frontend): add user usage API client"
```

---

## 任务 10：前端 — 统计卡片组件

**文件：**
- 创建：`frontend/src/components/user/usage/UsageStatsCards.vue`

- [ ] **步骤 1：创建统计卡片组件**

```vue
<template>
  <div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
    <div class="card p-4">
      <div class="flex items-center gap-3">
        <div class="rounded-lg bg-blue-100 p-2 dark:bg-blue-900/30">
          <svg class="h-5 w-5 text-blue-600 dark:text-blue-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
          </svg>
        </div>
        <div>
          <p class="text-xs font-medium text-gray-500 dark:text-gray-400">总请求数</p>
          <p class="text-xl font-bold text-gray-900 dark:text-white">
            {{ stats?.total_requests?.toLocaleString() || '0' }}
          </p>
          <p class="text-xs text-gray-500 dark:text-gray-400">
            今日: {{ stats?.today_requests?.toLocaleString() || '0' }}
          </p>
        </div>
      </div>
    </div>

    <div class="card p-4">
      <div class="flex items-center gap-3">
        <div class="rounded-lg bg-amber-100 p-2 dark:bg-amber-900/30">
          <svg class="h-5 w-5 text-amber-600 dark:text-amber-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 21a4 4 0 01-4-4V5a2 2 0 012-2h4a2 2 0 012 2v12a4 4 0 01-4 4zm0 0h12a2 2 0 002-2v-4a2 2 0 00-2-2h-2.343M11 7.343l1.657-1.657a2 2 0 012.828 0l2.829 2.829a2 2 0 010 2.828l-8.486 8.485M7 17h.01" />
          </svg>
        </div>
        <div>
          <p class="text-xs font-medium text-gray-500 dark:text-gray-400">总 Token</p>
          <p class="text-xl font-bold text-gray-900 dark:text-white">
            {{ formatTokens(stats?.total_tokens || 0) }}
          </p>
          <p class="text-xs text-gray-500 dark:text-gray-400">
            入: {{ formatTokens(stats?.total_input_tokens || 0) }} / 出: {{ formatTokens(stats?.total_output_tokens || 0) }}
          </p>
        </div>
      </div>
    </div>

    <div class="card p-4">
      <div class="flex items-center gap-3">
        <div class="rounded-lg bg-green-100 p-2 dark:bg-green-900/30">
          <svg class="h-5 w-5 text-green-600 dark:text-green-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8c-1.657 0-3 .895-3 2s1.343 2 3 2 3 .895 3 2-1.343 2-3 2m0-8c1.11 0 2.08.402 2.599 1M12 8V7m0 1v8m0 0v1m0-1c-1.11 0-2.08-.402-2.599-1M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
        </div>
        <div class="min-w-0 flex-1">
          <p class="text-xs font-medium text-gray-500 dark:text-gray-400">总费用</p>
          <p class="text-xl font-bold text-green-600 dark:text-green-400">
            ${{ (stats?.total_actual_cost || 0).toFixed(4) }}
          </p>
          <p class="text-xs text-gray-500 dark:text-gray-400">
            实际 / <span class="line-through">${{ (stats?.total_cost || 0).toFixed(4) }}</span> 标准
          </p>
        </div>
      </div>
    </div>

    <div class="card p-4">
      <div class="flex items-center gap-3">
        <div class="rounded-lg bg-purple-100 p-2 dark:bg-purple-900/30">
          <svg class="h-5 w-5 text-purple-600 dark:text-purple-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
        </div>
        <div>
          <p class="text-xs font-medium text-gray-500 dark:text-gray-400">平均响应时间</p>
          <p class="text-xl font-bold text-gray-900 dark:text-white">
            {{ formatDuration(stats?.average_duration_ms || 0) }}
          </p>
          <p class="text-xs text-gray-500 dark:text-gray-400">每请求</p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { UserUsageStats } from '@/types'

defineProps<{
  stats: UserUsageStats | null
}>()

function formatTokens(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M'
  if (n >= 1_000) return (n / 1_000).toFixed(1) + 'K'
  return n.toLocaleString()
}

function formatDuration(ms: number): string {
  if (ms < 1000) return ms + 'ms'
  return (ms / 1000).toFixed(2) + 's'
}
</script>
```

- [ ] **步骤 2：Commit**

```bash
git add frontend/src/components/user/usage/UsageStatsCards.vue
git commit -m "feat(frontend): add UsageStatsCards component"
```

---

## 任务 11：前端 — 趋势图组件

**文件：**
- 创建：`frontend/src/components/user/usage/UsageTrendChart.vue`

- [ ] **步骤 1：创建趋势图组件**

```vue
<template>
  <div class="card">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h3 class="text-lg font-semibold text-gray-900 dark:text-white">用量趋势</h3>
    </div>
    <div class="p-6">
      <div v-if="loading" class="flex items-center justify-center py-12">
        <div class="h-8 w-8 animate-spin rounded-full border-4 border-primary-600 border-t-transparent"></div>
      </div>
      <div v-else-if="data.length === 0" class="py-8 text-center text-gray-500 dark:text-gray-400">
        暂无趋势数据
      </div>
      <div v-else class="space-y-2">
        <div
          v-for="point in data"
          :key="point.date"
          class="flex items-center gap-4"
        >
          <span class="w-20 text-sm text-gray-600 dark:text-gray-400">{{ point.date }}</span>
          <div class="flex-1">
            <div class="h-6 rounded bg-gray-100 dark:bg-dark-800">
              <div
                class="h-6 rounded bg-gradient-to-r from-blue-500 to-blue-600"
                :style="{ width: getBarWidth(point.total_tokens) }"
              ></div>
            </div>
          </div>
          <span class="w-24 text-right text-sm font-medium text-gray-900 dark:text-white">
            {{ formatTokens(point.total_tokens) }}
          </span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { UsageTrendDataPoint } from '@/types'

const props = defineProps<{
  data: UsageTrendDataPoint[]
  loading: boolean
}>()

function formatTokens(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M'
  if (n >= 1_000) return (n / 1_000).toFixed(1) + 'K'
  return n.toLocaleString()
}

function getBarWidth(tokens: number): string {
  const max = Math.max(...props.data.map((d) => d.total_tokens), 1)
  const pct = (tokens / max) * 100
  return Math.max(pct, 2) + '%'
}
</script>
```

- [ ] **步骤 2：Commit**

```bash
git add frontend/src/components/user/usage/UsageTrendChart.vue
git commit -m "feat(frontend): add UsageTrendChart component"
```

---

## 任务 12：前端 — 模型分布图组件

**文件：**
- 创建：`frontend/src/components/user/usage/UsageModelChart.vue`

- [ ] **步骤 1：创建模型分布组件**

```vue
<template>
  <div class="card">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h3 class="text-lg font-semibold text-gray-900 dark:text-white">模型分布</h3>
    </div>
    <div class="p-6">
      <div v-if="loading" class="flex items-center justify-center py-12">
        <div class="h-8 w-8 animate-spin rounded-full border-4 border-primary-600 border-t-transparent"></div>
      </div>
      <div v-else-if="data.length === 0" class="py-8 text-center text-gray-500 dark:text-gray-400">
        暂无模型数据
      </div>
      <div v-else class="space-y-3">
        <div
          v-for="model in data"
          :key="model.model"
          class="flex items-center justify-between rounded-lg bg-gray-50 p-3 dark:bg-dark-800/50"
        >
          <div class="flex items-center gap-3">
            <div class="h-3 w-3 rounded-full" :style="{ backgroundColor: getColor(model.model) }"></div>
            <span class="text-sm font-medium text-gray-900 dark:text-white">{{ model.model }}</span>
          </div>
          <div class="text-right">
            <p class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ formatTokens(model.total_tokens) }}
            </p>
            <p class="text-xs text-gray-500 dark:text-gray-400">
              {{ getPercentage(model.total_tokens) }}% · ${{ model.actual_cost.toFixed(4) }}
            </p>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { UsageModelStat } from '@/types'

const props = defineProps<{
  data: UsageModelStat[]
  loading: boolean
}>()

const colors = [
  '#3b82f6',
  '#10b981',
  '#f59e0b',
  '#ef4444',
  '#8b5cf6',
  '#ec4899',
  '#14b8a6',
  '#f97316',
]

function getColor(model: string): string {
  const idx = model.split('').reduce((acc, c) => acc + c.charCodeAt(0), 0) % colors.length
  return colors[idx]
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M'
  if (n >= 1_000) return (n / 1_000).toFixed(1) + 'K'
  return n.toLocaleString()
}

function getPercentage(tokens: number): string {
  const total = props.data.reduce((sum, m) => sum + m.total_tokens, 0)
  if (total === 0) return '0'
  return ((tokens / total) * 100).toFixed(1)
}
</script>
```

- [ ] **步骤 2：Commit**

```bash
git add frontend/src/components/user/usage/UsageModelChart.vue
git commit -m "feat(frontend): add UsageModelChart component"
```

---

## 任务 13：前端 — 用量页面主组件

**文件：**
- 创建：`frontend/src/views/user/UsageView.vue`

- [ ] **步骤 1：创建用量页面主组件**

```vue
<template>
  <div class="space-y-6 p-6">
    <div class="flex items-center justify-between">
      <h1 class="text-2xl font-bold text-gray-900 dark:text-white">我的用量</h1>
      <div class="flex gap-2">
        <button
          v-for="range in dateRanges"
          :key="range.label"
          :class="[
            'rounded-lg px-3 py-1.5 text-sm font-medium transition-colors',
            selectedRange === range.label
              ? 'bg-primary-600 text-white'
              : 'bg-gray-100 text-gray-700 hover:bg-gray-200 dark:bg-dark-800 dark:text-gray-300 dark:hover:bg-dark-700',
          ]"
          @click="selectRange(range)"
        >
          {{ range.label }}
        </button>
      </div>
    </div>

    <UsageStatsCards :stats="stats" />

    <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
      <UsageTrendChart :data="trendData" :loading="trendLoading" />
      <UsageModelChart :data="modelData" :loading="modelLoading" />
    </div>

    <div v-if="!stats && !trendLoading && !modelLoading" class="card p-12 text-center">
      <h3 class="text-lg font-semibold text-gray-900 dark:text-white">请先完成 AI 服务配置</h3>
      <p class="mt-2 text-gray-500 dark:text-gray-400">
        您需要先配置 relay 账户才能查看用量数据
      </p>
      <router-link
        to="/user"
        class="mt-4 inline-block rounded-lg bg-primary-600 px-4 py-2 text-white hover:bg-primary-700"
      >
        前往配置
      </router-link>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { getUserUsageStats, getUserUsageTrend, getUserUsageModels } from '@/api/userUsage'
import type { UserUsageStats, UsageTrendDataPoint, UsageModelStat } from '@/types'
import UsageStatsCards from '@/components/user/usage/UsageStatsCards.vue'
import UsageTrendChart from '@/components/user/usage/UsageTrendChart.vue'
import UsageModelChart from '@/components/user/usage/UsageModelChart.vue'

const stats = ref<UserUsageStats | null>(null)
const trendData = ref<UsageTrendDataPoint[]>([])
const modelData = ref<UsageModelStat[]>([])
const trendLoading = ref(false)
const modelLoading = ref(false)

const dateRanges = [
  { label: '今日', days: 1 },
  { label: '7 天', days: 7 },
  { label: '30 天', days: 30 },
]

const selectedRange = ref('30 天')

function getDateRange(days: number) {
  const end = new Date()
  const start = new Date()
  start.setDate(end.getDate() - days + 1)
  return {
    start_date: start.toISOString().split('T')[0],
    end_date: end.toISOString().split('T')[0],
  }
}

function selectRange(range: { label: string; days: number }) {
  selectedRange.value = range.label
  loadData(range.days)
}

async function loadData(days: number) {
  const { start_date, end_date } = getDateRange(days)

  try {
    const statsRes = await getUserUsageStats()
    stats.value = statsRes.data.data
  } catch (err) {
    console.error('Failed to load stats:', err)
  }

  trendLoading.value = true
  try {
    const trendRes = await getUserUsageTrend({ start_date, end_date, granularity: 'day' })
    trendData.value = trendRes.data.data.trend || []
  } catch (err) {
    console.error('Failed to load trend:', err)
    trendData.value = []
  } finally {
    trendLoading.value = false
  }

  modelLoading.value = true
  try {
    const modelRes = await getUserUsageModels({ start_date, end_date })
    modelData.value = modelRes.data.data.models || []
  } catch (err) {
    console.error('Failed to load models:', err)
    modelData.value = []
  } finally {
    modelLoading.value = false
  }
}

onMounted(() => loadData(30))
</script>
```

- [ ] **步骤 2：Commit**

```bash
git add frontend/src/views/user/UsageView.vue
git commit -m "feat(frontend): add user usage dashboard page"
```

---

## 任务 14：前端 — 路由注册

**文件：**
- 修改：`frontend/src/router/index.ts:54-58`

- [ ] **步骤 1：在 router 中添加用量页面路由**

在 `/user` 路由之后添加：

```typescript
{
  path: '/user/usage',
  name: 'UserUsage',
  component: () => import('@/views/user/UsageView.vue'),
},
```

- [ ] **步骤 2：验证前端编译通过**

运行：`cd /Users/admin/ai-efficiency/frontend && pnpm build`
预期：编译成功，无错误

- [ ] **步骤 3：Commit**

```bash
git add frontend/src/router/index.ts
git commit -m "feat(frontend): add /user/usage route"
```

---

## 任务 15：集成测试与手动验证

**文件：** 无新增文件

- [ ] **步骤 1：运行后端完整测试**

运行：`cd /Users/admin/ai-efficiency/backend && go test ./...`
预期：所有测试 PASS

- [ ] **步骤 2：运行前端开发服务器**

运行：`cd /Users/admin/ai-efficiency/frontend && pnpm dev`
预期：开发服务器启动成功，无编译错误

- [ ] **步骤 3：手动验证页面（需要已配置 relay 的用户登录）**

1. 访问 `http://localhost:5173/user/usage`
2. 验证统计卡片显示数据
3. 验证趋势图显示近 30 天数据
4. 验证模型分布图显示数据
5. 切换日期范围（今日/7天/30天），验证数据刷新

- [ ] **步骤 4：手动验证空状态（未配置 relay 的用户）**

1. 使用未配置 relay 的账户登录
2. 访问 `/user/usage`
3. 验证显示"请先完成 AI 服务配置"引导卡片

- [ ] **步骤 5：最终 Commit**

```bash
git add -A
git commit -m "feat: complete user usage dashboard implementation"
```

---

## 自检

**1. 规格覆盖度：**
- ✓ 汇总统计（任务 1-3 类型，任务 5 handler，任务 10 组件）
- ✓ 趋势折线（任务 1-3 类型，任务 5 handler，任务 11 组件）
- ✓ 模型分布（任务 1-3 类型，任务 5 handler，任务 12 组件）
- ✓ 日期范围选择（任务 13 主组件）
- ✓ 空状态处理（任务 5 handler 返回 null，任务 13 前端引导卡片）
- ✓ 错误处理（任务 5 handler 502，任务 7 测试）

**2. 占位符扫描：** 无 TODO / 待定 / "添加适当的错误处理"

**3. 类型一致性：** 
- `UserUsageStats` / `UsageTrendResponse` / `UsageModelResponse` 在 relay、handler、前端三层命名一致
- `firstNonEmptyString` 函数在 handler 中定义一次（需确认无重复）

---

**计划已完成并保存到实现文档中。两种执行方式：**

**1. 子代理驱动（推荐）** - 每个任务调度一个新的子代理，任务间进行审查，快速迭代

**2. 内联执行** - 在当前会话中使用 executing-plans 执行任务，批量执行并设有检查点

**选哪种方式？**
