# 用户用量趋势页面设计

**日期**: 2026-06-06
**状态**: 已批准

## 背景

用户需要在 ai-efficiency 平台查看自己的 AI 用量趋势。sub2api 用户端已有完整的用量 Dashboard 功能（汇总统计、趋势折线、模型分布），ai-efficiency 已存储用户的 relay 账户密码，可直接获取用户 JWT 调用 sub2api 用户端接口，无需走 admin API。

## 目标

在 ai-efficiency 前端新增 `/user/usage` 页面，展示当前用户的：
- 汇总统计（请求数、Token、费用、响应时间）
- 每日用量趋势折线图
- 按模型分布的 Token 占比

## 架构

```
ai-efficiency frontend (/user/usage)
    ↓ GET /api/v1/user/usage/{stats,trend,models}
ai-efficiency backend (handler/user_usage.go)
    ↓ decrypt relay_auth_password → loginSessionToken → JWT
ai-efficiency relay layer (relay/sub2api.go)
    ↓ GET /api/v1/usage/dashboard/* (user JWT auth)
sub2api (user-facing endpoints, 自带 user 隔离)
```

## 后端设计

### relay 层

**文件**: `backend/internal/relay/provider.go`

新增接口：

```go
type UserUsageStats struct {
    TotalRequests         int64   `json:"total_requests"`
    TotalInputTokens      int64   `json:"total_input_tokens"`
    TotalOutputTokens     int64   `json:"total_output_tokens"`
    TotalCacheReadTokens  int64   `json:"total_cache_read_tokens"`
    TotalCacheWriteTokens int64   `json:"total_cache_creation_tokens"`
    TotalTokens           int64   `json:"total_tokens"`
    TotalCost             float64 `json:"total_cost"`
    TotalActualCost       float64 `json:"total_actual_cost"`
    TodayRequests         int64   `json:"today_requests"`
    TodayInputTokens      int64   `json:"today_input_tokens"`
    TodayOutputTokens     int64   `json:"today_output_tokens"`
    TodayTokens           int64   `json:"today_tokens"`
    TodayCost             float64 `json:"today_cost"`
    TodayActualCost       float64 `json:"today_actual_cost"`
    AverageDurationMs     int64   `json:"average_duration_ms"`
}

type UsageTrendParams struct {
    StartDate   string `json:"start_date"`
    EndDate     string `json:"end_date"`
    Granularity string `json:"granularity"` // day or hour
    Timezone    string `json:"timezone"`
}

type UsageTrendDataPoint struct {
    Date         string  `json:"date"`
    Requests     int64   `json:"requests"`
    InputTokens  int64   `json:"input_tokens"`
    OutputTokens int64   `json:"output_tokens"`
    TotalTokens  int64   `json:"total_tokens"`
    Cost         float64 `json:"cost"`
    ActualCost   float64 `json:"actual_cost"`
}

type UsageTrendResponse struct {
    Trend       []UsageTrendDataPoint `json:"trend"`
    StartDate   string                `json:"start_date"`
    EndDate     string                `json:"end_date"`
    Granularity string                `json:"granularity"`
}

type UsageModelParams struct {
    StartDate string `json:"start_date"`
    EndDate   string `json:"end_date"`
    Timezone  string `json:"timezone"`
}

type UsageModelStat struct {
    Model        string  `json:"model"`
    Requests     int64   `json:"requests"`
    InputTokens  int64   `json:"input_tokens"`
    OutputTokens int64   `json:"output_tokens"`
    TotalTokens  int64   `json:"total_tokens"`
    Cost         float64 `json:"cost"`
    ActualCost   float64 `json:"actual_cost"`
}

type UsageModelResponse struct {
    Models    []UsageModelStat `json:"models"`
    StartDate string           `json:"start_date"`
    EndDate   string           `json:"end_date"`
}

type Provider interface {
    // ... existing methods ...

    GetUserUsageStats(ctx context.Context, login, password string) (*UserUsageStats, error)
    GetUserUsageTrend(ctx context.Context, login, password string, params UsageTrendParams) (*UsageTrendResponse, error)
    GetUserUsageModels(ctx context.Context, login, password string, params UsageModelParams) (*UsageModelResponse, error)
}
```

**实现**: `backend/internal/relay/sub2api.go`

```go
func (s *sub2apiRelay) GetUserUsageStats(ctx context.Context, login, password string) (*UserUsageStats, error) {
    token, _, err := s.loginSessionToken(ctx, login, password)
    if err != nil {
        return nil, fmt.Errorf("relay: login for usage stats: %w", err)
    }
    // GET /api/v1/usage/dashboard/stats
    // Parse response into UserUsageStats
}

func (s *sub2apiRelay) GetUserUsageTrend(ctx context.Context, login, password string, params UsageTrendParams) (*UsageTrendResponse, error) {
    token, _, err := s.loginSessionToken(ctx, login, password)
    if err != nil {
        return nil, fmt.Errorf("relay: login for usage trend: %w", err)
    }
    // GET /api/v1/usage/dashboard/trend?start_date=...&end_date=...&granularity=...
    // Parse response into UsageTrendResponse
}

func (s *sub2apiRelay) GetUserUsageModels(ctx context.Context, login, password string, params UsageModelParams) (*UsageModelResponse, error) {
    token, _, err := s.loginSessionToken(ctx, login, password)
    if err != nil {
        return nil, fmt.Errorf("relay: login for usage models: %w", err)
    }
    // GET /api/v1/usage/dashboard/models?start_date=...&end_date=...
    // Parse response into UsageModelResponse
}
```

### handler 层

**文件**: `backend/internal/handler/user_usage.go`

```go
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

// Stats handles GET /api/v1/user/usage/stats
func (h *UserUsageHandler) Stats(c *gin.Context) {
    userID := auth.UserIDFromContext(c)
    u, err := h.entClient.User.Get(c.Request.Context(), userID)
    if err != nil {
        response.Error(c, fmt.Errorf("fetch user: %w", err))
        return
    }

    if u.RelayAuthPassword == nil || strings.TrimSpace(*u.RelayAuthPassword) == "" {
        response.Success(c, nil) // No relay credentials, return empty
        return
    }

    password, err := pkg.Decrypt(strings.TrimSpace(*u.RelayAuthPassword), h.encryptionKey)
    if err != nil {
        response.Error(c, fmt.Errorf("decrypt relay password: %w", err))
        return
    }

    login := firstNonEmptyString(u.Email, u.Username)
    stats, err := h.relayProvider.GetUserUsageStats(c.Request.Context(), login, password)
    if err != nil {
        response.Error(c, fmt.Errorf("get usage stats: %w", err))
        return
    }

    response.Success(c, stats)
}

// Trend handles GET /api/v1/user/usage/trend
func (h *UserUsageHandler) Trend(c *gin.Context) {
    userID := auth.UserIDFromContext(c)
    u, err := h.entClient.User.Get(c.Request.Context(), userID)
    if err != nil {
        response.Error(c, fmt.Errorf("fetch user: %w", err))
        return
    }

    if u.RelayAuthPassword == nil || strings.TrimSpace(*u.RelayAuthPassword) == "" {
        response.Success(c, &relay.UsageTrendResponse{})
        return
    }

    password, err := pkg.Decrypt(strings.TrimSpace(*u.RelayAuthPassword), h.encryptionKey)
    if err != nil {
        response.Error(c, fmt.Errorf("decrypt relay password: %w", err))
        return
    }

    params := relay.UsageTrendParams{
        StartDate:   c.Query("start_date"),
        EndDate:     c.Query("end_date"),
        Granularity: c.DefaultQuery("granularity", "day"),
        Timezone:    c.Query("timezone"),
    }

    login := firstNonEmptyString(u.Email, u.Username)
    trend, err := h.relayProvider.GetUserUsageTrend(c.Request.Context(), login, password, params)
    if err != nil {
        response.Error(c, fmt.Errorf("get usage trend: %w", err))
        return
    }

    response.Success(c, trend)
}

// Models handles GET /api/v1/user/usage/models
func (h *UserUsageHandler) Models(c *gin.Context) {
    userID := auth.UserIDFromContext(c)
    u, err := h.entClient.User.Get(c.Request.Context(), userID)
    if err != nil {
        response.Error(c, fmt.Errorf("fetch user: %w", err))
        return
    }

    if u.RelayAuthPassword == nil || strings.TrimSpace(*u.RelayAuthPassword) == "" {
        response.Success(c, &relay.UsageModelResponse{})
        return
    }

    password, err := pkg.Decrypt(strings.TrimSpace(*u.RelayAuthPassword), h.encryptionKey)
    if err != nil {
        response.Error(c, fmt.Errorf("decrypt relay password: %w", err))
        return
    }

    params := relay.UsageModelParams{
        StartDate: c.Query("start_date"),
        EndDate:   c.Query("end_date"),
        Timezone:  c.Query("timezone"),
    }

    login := firstNonEmptyString(u.Email, u.Username)
    models, err := h.relayProvider.GetUserUsageModels(c.Request.Context(), login, password, params)
    if err != nil {
        response.Error(c, fmt.Errorf("get usage models: %w", err))
        return
    }

    response.Success(c, models)
}
```

**路由注册**: `backend/internal/handler/router.go`

```go
userGroup := protected.Group("/user")
{
    // ... existing routes ...

    if relayProvider != nil {
        userUsageHandler := NewUserUsageHandler(entClient, relayProvider, encryptionKey)
        userGroup.GET("/usage/stats", userUsageHandler.Stats)
        userGroup.GET("/usage/trend", userUsageHandler.Trend)
        userGroup.GET("/usage/models", userUsageHandler.Models)
    }
}
```

## 前端设计

### 路由

**文件**: `frontend/src/router/index.ts`

```ts
{
  path: '/user/usage',
  name: 'UserUsage',
  component: () => import('@/views/user/UsageView.vue'),
  meta: { requiresAuth: true, title: '我的用量' }
}
```

### API 封装

**文件**: `frontend/src/api/userUsage.ts`

```ts
import request from './request'
import type { UserUsageStats, UsageTrendResponse, UsageModelResponse } from '@/types'

export function getUserUsageStats(): Promise<UserUsageStats | null> {
  return request.get('/user/usage/stats')
}

export function getUserUsageTrend(params: {
  start_date?: string
  end_date?: string
  granularity?: 'day' | 'hour'
  timezone?: string
}): Promise<UsageTrendResponse> {
  return request.get('/user/usage/trend', { params })
}

export function getUserUsageModels(params: {
  start_date?: string
  end_date?: string
  timezone?: string
}): Promise<UsageModelResponse> {
  return request.get('/user/usage/models', { params })
}
```

### 页面组件

**文件**: `frontend/src/views/user/UsageView.vue`

页面结构：

1. **顶部统计卡片行**（2×2 grid）
   - 总请求数（今日请求数）
   - 总 Token（input/output 分项）
   - 总费用（actual_cost / standard_cost）
   - 平均响应时间

2. **日期范围选择器** — 快捷选项：今日 / 7 天 / 30 天 / 自定义

3. **趋势折线图** — 按天展示 token 消耗（input/output 堆叠）

4. **模型分布图** — 饼图或条形图，按 model 分组展示 token 占比

**组件拆分**:

```
UsageView.vue
├── UsageStatsCards.vue (统计卡片行)
├── DateRangePicker.vue (日期选择器)
├── UsageTrendChart.vue (趋势折线图)
└── UsageModelChart.vue (模型分布图)
```

### 空状态处理

- 用户无 `relay_auth_password` → 展示引导卡片："请先完成 AI 服务配置" + 跳转 `/user` 按钮
- 接口返回空数据 → 展示"暂无用量数据"
- 接口错误 → 展示"用量数据暂不可用" + 重试按钮

## 错误处理

| 场景 | 后端行为 | 前端行为 |
|---|---|---|
| 用户无 relay 密码 | 返回 `null` / 空对象 | 展示引导卡片，跳转配置页 |
| relay 登录失败 | 返回 401 | 提示重新登录 |
| sub2api 请求失败 | 返回 502 | 展示"暂不可用" + 重试 |
| 参数格式错误 | 返回 400 | 表单校验拦截 |

## 测试

### relay 层

- mock sub2api dashboard 响应，验证 JWT 获取 + 数据透传
- 验证登录失败时的错误处理

### handler 层

- mock relay provider，验证解密 → 调 relay → 返回
- 验证无密码时返回空数据
- 验证参数透传（start_date, end_date, granularity, timezone）

### 前端

- 无 relay 密码时的空状态展示
- 有数据时的图表渲染
- 日期范围切换时的重新请求
- 接口错误时的错误提示

## 范围控制

**包含**:
- 汇总统计、趋势折线、模型分布三个核心视图
- 日期范围选择（今日/7天/30天/自定义）
- 空状态和错误处理

**不包含**:
- 原始日志列表（方案 C）
- API Key 级别的过滤
- 导出 CSV
- 管理员查看其他用户用量

## 依赖

- sub2api 用户端 `/api/v1/usage/dashboard/{stats,trend,models}` 接口稳定
- ai-efficiency 用户 `relay_auth_password` 字段可用
