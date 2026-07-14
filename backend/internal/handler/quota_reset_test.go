package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/quotaresetrequest"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestdecision"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestnode"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/quotareset"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type fakeQuotaResetService struct {
	optionsFn                    func(context.Context, int) (*quotareset.OptionsResponse, error)
	createFn                     func(context.Context, quotareset.CreateRequestInput) (*ent.QuotaResetRequest, error)
	cancelFn                     func(context.Context, int, int) (*ent.QuotaResetRequest, error)
	approveFn                    func(context.Context, quotareset.DecisionInput) (*ent.QuotaResetRequest, error)
	rejectFn                     func(context.Context, quotareset.DecisionInput) (*ent.QuotaResetRequest, error)
	retryResetFn                 func(context.Context, quotareset.DecisionInput) (*ent.QuotaResetRequest, error)
	listAdminFn                  func(context.Context, int, quotareset.ListParams) (*quotareset.RequestListResponse, error)
	listApproverCandidatesFn     func(context.Context, quotareset.ApproverCandidateParams) (*quotareset.ApproverCandidateListResponse, error)
	listApproverConfigsFn        func(context.Context) (*quotareset.ApproverConfigListResponse, error)
	saveApproverConfigsFn        func(context.Context, quotareset.SaveApproverConfigsInput) (*quotareset.ApproverConfigListResponse, error)
	listApprovalChainsFn         func(context.Context) (*quotareset.ApprovalChainListResponse, error)
	saveApprovalChainsFn         func(context.Context, quotareset.SaveApprovalChainsInput) (*quotareset.ApprovalChainListResponse, error)
	listApprovalChainOptionsFn   func(context.Context) (*quotareset.ApprovalChainOptionsResponse, error)
	getRequestSummaryFn          func(context.Context, int, int, bool) (*quotareset.RequestSummary, error)
	getNotificationSettingsFn    func(context.Context) (*quotareset.NotificationSettings, error)
	updateNotificationSettingsFn func(context.Context, quotareset.UpdateNotificationSettingsInput) (*quotareset.NotificationSettings, error)
	testNotificationSettingsFn   func(context.Context, int) (*quotareset.NotificationTestResult, error)
}

func (f *fakeQuotaResetService) Options(ctx context.Context, userID int) (*quotareset.OptionsResponse, error) {
	if f.optionsFn != nil {
		return f.optionsFn(ctx, userID)
	}
	return &quotareset.OptionsResponse{}, nil
}

func (f *fakeQuotaResetService) CreateRequest(ctx context.Context, input quotareset.CreateRequestInput) (*ent.QuotaResetRequest, error) {
	if f.createFn == nil {
		return &ent.QuotaResetRequest{ID: 99, Status: quotaresetrequest.StatusPending}, nil
	}
	return f.createFn(ctx, input)
}

func (f *fakeQuotaResetService) Cancel(ctx context.Context, actorUserID, requestID int) (*ent.QuotaResetRequest, error) {
	if f.cancelFn != nil {
		return f.cancelFn(ctx, actorUserID, requestID)
	}
	return &ent.QuotaResetRequest{ID: 99, Status: quotaresetrequest.StatusCancelled}, nil
}

func (f *fakeQuotaResetService) Approve(ctx context.Context, input quotareset.DecisionInput) (*ent.QuotaResetRequest, error) {
	if f.approveFn == nil {
		return &ent.QuotaResetRequest{ID: input.RequestID, Status: quotaresetrequest.StatusApprovedResetSucceeded}, nil
	}
	return f.approveFn(ctx, input)
}

func (f *fakeQuotaResetService) Reject(ctx context.Context, input quotareset.DecisionInput) (*ent.QuotaResetRequest, error) {
	if f.rejectFn == nil {
		return &ent.QuotaResetRequest{ID: input.RequestID, Status: quotaresetrequest.StatusRejected}, nil
	}
	return f.rejectFn(ctx, input)
}

func (f *fakeQuotaResetService) RetryReset(ctx context.Context, input quotareset.DecisionInput) (*ent.QuotaResetRequest, error) {
	if f.retryResetFn != nil {
		return f.retryResetFn(ctx, input)
	}
	return &ent.QuotaResetRequest{ID: 99, Status: quotaresetrequest.StatusApprovedResetSucceeded}, nil
}

func (f *fakeQuotaResetService) ListMine(context.Context, int, quotareset.ListParams) (*quotareset.RequestListResponse, error) {
	return &quotareset.RequestListResponse{}, nil
}

func (f *fakeQuotaResetService) ListApprovals(context.Context, int, quotareset.ListParams) (*quotareset.RequestListResponse, error) {
	return &quotareset.RequestListResponse{}, nil
}

func (f *fakeQuotaResetService) ListAdmin(ctx context.Context, actorUserID int, params quotareset.ListParams) (*quotareset.RequestListResponse, error) {
	if f.listAdminFn != nil {
		return f.listAdminFn(ctx, actorUserID, params)
	}
	return &quotareset.RequestListResponse{}, nil
}

func (f *fakeQuotaResetService) ListApproverCandidates(ctx context.Context, params quotareset.ApproverCandidateParams) (*quotareset.ApproverCandidateListResponse, error) {
	if f.listApproverCandidatesFn != nil {
		return f.listApproverCandidatesFn(ctx, params)
	}
	return &quotareset.ApproverCandidateListResponse{}, nil
}

func (f *fakeQuotaResetService) ListApproverConfigs(ctx context.Context) (*quotareset.ApproverConfigListResponse, error) {
	if f.listApproverConfigsFn != nil {
		return f.listApproverConfigsFn(ctx)
	}
	return &quotareset.ApproverConfigListResponse{}, nil
}

func (f *fakeQuotaResetService) SaveApproverConfigs(ctx context.Context, input quotareset.SaveApproverConfigsInput) (*quotareset.ApproverConfigListResponse, error) {
	if f.saveApproverConfigsFn != nil {
		return f.saveApproverConfigsFn(ctx, input)
	}
	return &quotareset.ApproverConfigListResponse{}, nil
}

func (f *fakeQuotaResetService) ListApprovalChains(ctx context.Context) (*quotareset.ApprovalChainListResponse, error) {
	if f.listApprovalChainsFn != nil {
		return f.listApprovalChainsFn(ctx)
	}
	return &quotareset.ApprovalChainListResponse{Items: []quotareset.ApprovalChain{}}, nil
}

func (f *fakeQuotaResetService) SaveApprovalChains(ctx context.Context, input quotareset.SaveApprovalChainsInput) (*quotareset.ApprovalChainListResponse, error) {
	if f.saveApprovalChainsFn != nil {
		return f.saveApprovalChainsFn(ctx, input)
	}
	return &quotareset.ApprovalChainListResponse{Items: []quotareset.ApprovalChain{}}, nil
}

func (f *fakeQuotaResetService) ListApprovalChainOptions(ctx context.Context) (*quotareset.ApprovalChainOptionsResponse, error) {
	if f.listApprovalChainOptionsFn != nil {
		return f.listApprovalChainOptionsFn(ctx)
	}
	return &quotareset.ApprovalChainOptionsResponse{
		Groups:      []quotareset.ApprovalChainGroupOption{},
		Departments: []quotareset.ApprovalChainDepartmentOption{},
	}, nil
}

func (f *fakeQuotaResetService) GetRequestSummary(ctx context.Context, requestID, viewerUserID int, admin bool) (*quotareset.RequestSummary, error) {
	if f.getRequestSummaryFn != nil {
		return f.getRequestSummaryFn(ctx, requestID, viewerUserID, admin)
	}
	return &quotareset.RequestSummary{ID: requestID}, nil
}

func (f *fakeQuotaResetService) GetNotificationSettings(ctx context.Context) (*quotareset.NotificationSettings, error) {
	if f.getNotificationSettingsFn != nil {
		return f.getNotificationSettingsFn(ctx)
	}
	return &quotareset.NotificationSettings{}, nil
}

func (f *fakeQuotaResetService) UpdateNotificationSettings(ctx context.Context, input quotareset.UpdateNotificationSettingsInput) (*quotareset.NotificationSettings, error) {
	if f.updateNotificationSettingsFn != nil {
		return f.updateNotificationSettingsFn(ctx, input)
	}
	return &quotareset.NotificationSettings{}, nil
}

func (f *fakeQuotaResetService) TestNotificationSettings(ctx context.Context, actorUserID int) (*quotareset.NotificationTestResult, error) {
	if f.testNotificationSettingsFn != nil {
		return f.testNotificationSettingsFn(ctx, actorUserID)
	}
	return &quotareset.NotificationTestResult{Delivered: true}, nil
}

func TestQuotaResetCreateRequestPassesActorAndBody(t *testing.T) {
	env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		createFn: func(_ context.Context, input quotareset.CreateRequestInput) (*ent.QuotaResetRequest, error) {
			if input.RequesterUserID != 1 || input.GroupID != "42" || input.Reason != "Need reset" {
				t.Fatalf("input = %+v", input)
			}
			return &ent.QuotaResetRequest{ID: 99, Status: quotaresetrequest.StatusPending}, nil
		},
		getRequestSummaryFn: func(_ context.Context, requestID, viewerUserID int, admin bool) (*quotareset.RequestSummary, error) {
			if requestID != 99 || viewerUserID != 1 || admin {
				t.Fatalf("summary args = request %d viewer %d admin %t", requestID, viewerUserID, admin)
			}
			return &quotareset.RequestSummary{ID: requestID, Status: quotaresetrequest.StatusPending.String()}, nil
		},
	})
	rec := performQuotaResetRequest(env.router, http.MethodPost, "/api/v1/user/quota-reset/requests", env.userToken, `{"group_id":"42","reason":"Need reset"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"pending"`) {
		t.Fatalf("response = %d %s", rec.Code, rec.Body.String())
	}
}

func TestQuotaResetAdminApproveUsesAdminFlag(t *testing.T) {
	env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		approveFn: func(_ context.Context, input quotareset.DecisionInput) (*ent.QuotaResetRequest, error) {
			if input.ActorUserID != 2 || input.RequestID != 99 || !input.Admin {
				t.Fatalf("input = %+v", input)
			}
			return &ent.QuotaResetRequest{ID: 99, Status: quotaresetrequest.StatusApprovedResetSucceeded}, nil
		},
		getRequestSummaryFn: func(_ context.Context, requestID, viewerUserID int, admin bool) (*quotareset.RequestSummary, error) {
			if requestID != 99 || viewerUserID != 2 || !admin {
				t.Fatalf("summary args = request %d viewer %d admin %t", requestID, viewerUserID, admin)
			}
			return &quotareset.RequestSummary{ID: requestID, Status: quotaresetrequest.StatusApprovedResetSucceeded.String()}, nil
		},
	})
	rec := performQuotaResetRequest(env.router, http.MethodPost, "/api/v1/admin/quota-reset/requests/99/approve", env.adminToken, `{}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"approved_reset_succeeded"`) {
		t.Fatalf("response = %d %s", rec.Code, rec.Body.String())
	}
}

func TestQuotaResetApproveRequiresNodeAndCommentForV2(t *testing.T) {
	t.Run("forwards v2 node and decision comment", func(t *testing.T) {
		var got quotareset.DecisionInput
		env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
			approveFn: func(_ context.Context, input quotareset.DecisionInput) (*ent.QuotaResetRequest, error) {
				got = input
				return &ent.QuotaResetRequest{ID: input.RequestID, Status: quotaresetrequest.StatusPending}, nil
			},
			getRequestSummaryFn: func(_ context.Context, requestID, viewerUserID int, admin bool) (*quotareset.RequestSummary, error) {
				return &quotareset.RequestSummary{ID: requestID, Status: quotaresetrequest.StatusPending.String()}, nil
			},
		})

		rec := performQuotaResetRequest(env.router, http.MethodPost, "/api/v1/user/quota-reset/approvals/123/approve", env.userToken, `{"request_node_id":456,"decision_reason":"Approved for the release investigation."}`)

		if rec.Code != http.StatusOK {
			t.Fatalf("response = %d %s", rec.Code, rec.Body.String())
		}
		if got.ActorUserID != 1 || got.RequestID != 123 || got.RequestNodeID != 456 || got.DecisionReason != "Approved for the release investigation." || got.Admin {
			t.Fatalf("decision input = %+v", got)
		}
	})

	t.Run("keeps legacy reason compatibility", func(t *testing.T) {
		var got quotareset.DecisionInput
		env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
			approveFn: func(_ context.Context, input quotareset.DecisionInput) (*ent.QuotaResetRequest, error) {
				got = input
				return &ent.QuotaResetRequest{ID: input.RequestID, Status: quotaresetrequest.StatusApprovedResetSucceeded}, nil
			},
		})

		rec := performQuotaResetRequest(env.router, http.MethodPost, "/api/v1/user/quota-reset/approvals/123/approve", env.userToken, `{"reason":"Legacy approval reason"}`)

		if rec.Code != http.StatusOK {
			t.Fatalf("response = %d %s", rec.Code, rec.Body.String())
		}
		if got.RequestNodeID != 0 || got.DecisionReason != "Legacy approval reason" {
			t.Fatalf("legacy decision input = %+v", got)
		}
	})

	t.Run("maps missing v2 comment validation", func(t *testing.T) {
		env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
			approveFn: func(_ context.Context, input quotareset.DecisionInput) (*ent.QuotaResetRequest, error) {
				if input.DecisionReason == "" {
					return nil, quotareset.ErrDecisionRequired
				}
				return &ent.QuotaResetRequest{ID: input.RequestID}, nil
			},
		})

		rec := performQuotaResetRequest(env.router, http.MethodPost, "/api/v1/user/quota-reset/approvals/123/approve", env.userToken, `{"request_node_id":456}`)

		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), quotareset.ErrDecisionRequired.Error()) {
			t.Fatalf("response = %d %s", rec.Code, rec.Body.String())
		}
	})
}

func TestQuotaResetWorkflowAdvancedReturnsLatestSummaryDetails(t *testing.T) {
	latest := &quotareset.RequestSummary{
		ID:     123,
		Status: quotaresetrequest.StatusPending.String(),
		Workflow: &quotareset.WorkflowSummary{
			Version:     quotareset.WorkflowVersionV2,
			CurrentNode: &quotareset.WorkflowNodeSummary{ID: 457},
			Nodes:       []quotareset.WorkflowNodeSummary{},
			Decisions:   []quotareset.WorkflowDecisionSummary{},
		},
	}
	advanced := &quotareset.WorkflowAdvancedError{RequestID: 123, Latest: latest}
	env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		approveFn: func(context.Context, quotareset.DecisionInput) (*ent.QuotaResetRequest, error) {
			return nil, advanced
		},
	})

	rec := performQuotaResetRequest(env.router, http.MethodPost, "/api/v1/user/quota-reset/approvals/123/approve", env.userToken, `{"request_node_id":456,"decision_reason":"Approved for the release investigation."}`)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	var body struct {
		Message string `json:"message"`
		Details struct {
			Request *quotareset.RequestSummary `json:"request"`
		} `json:"details"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Message != quotareset.ErrWorkflowAdvanced.Error() || body.Details.Request == nil || body.Details.Request.ID != 123 || body.Details.Request.Workflow == nil || body.Details.Request.Workflow.CurrentNode == nil || body.Details.Request.Workflow.CurrentNode.ID != 457 {
		t.Fatalf("stale response = %+v", body)
	}

	nilLatestEnv := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		approveFn: func(context.Context, quotareset.DecisionInput) (*ent.QuotaResetRequest, error) {
			return nil, &quotareset.WorkflowAdvancedError{RequestID: 124}
		},
	})
	nilLatest := performQuotaResetRequest(nilLatestEnv.router, http.MethodPost, "/api/v1/user/quota-reset/approvals/124/approve", nilLatestEnv.userToken, `{"request_node_id":456,"decision_reason":"Approved for the release investigation."}`)
	if nilLatest.Code != http.StatusConflict {
		t.Fatalf("nil-latest status = %d, want %d; body = %s", nilLatest.Code, http.StatusConflict, nilLatest.Body.String())
	}
	var nilBody struct {
		Message string                     `json:"message"`
		Details map[string]json.RawMessage `json:"details"`
	}
	if err := json.Unmarshal(nilLatest.Body.Bytes(), &nilBody); err != nil {
		t.Fatalf("decode nil-latest response: %v", err)
	}
	requestDetails, ok := nilBody.Details["request"]
	if nilBody.Message != quotareset.ErrWorkflowAdvanced.Error() || !ok || string(requestDetails) != "null" {
		t.Fatalf("nil-latest response = %+v", nilBody)
	}
}

func TestQuotaResetRetryWorkflowAdvancedReturnsLatestSummaryDetails(t *testing.T) {
	env := newQuotaResetHandlerTestEnvWithServiceFactory(t, func(client *ent.Client) quotaResetService {
		return quotareset.NewService(client, nil, nil, nil)
	})
	ctx := context.Background()
	requester := env.client.User.Create().
		SetUsername("requester").
		SetEmail("requester@example.com").
		SetAuthSource("sub2api_sso").
		SetRole(entuser.RoleUser).
		SaveX(ctx)
	request := env.client.QuotaResetRequest.Create().
		SetRequesterUserID(requester.ID).
		SetRequesterRelayUserID(1001).
		SetProviderID(1).
		SetGroupID("42").
		SetGroupName("Group Alpha").
		SetGroupPlatform("openai").
		SetReason("Investigate a release regression").
		SetWorkflowVersion(quotareset.WorkflowVersionV2).
		SetRequesterDisplayNameSnapshot("Requester Example").
		SetRequesterEmailSnapshot("requester@example.com").
		SetRequesterDepartmentPaths([]string{"Department Alpha"}).
		SetRequesterNotificationIds(map[string]string{}).
		SetStatus(quotaresetrequest.StatusPending).
		SetResolvedApproverUserIds([]int{}).
		SetMatchedDepartmentPaths([]map[string]any{}).
		SaveX(ctx)
	node := env.client.QuotaResetRequestNode.Create().
		SetRequestID(request.ID).
		SetPosition(1).
		SetNodeType(quotaresetrequestnode.NodeTypeConfiguredDepartment).
		SetLabel("Department Beta").
		SetDepartmentSnapshots([]map[string]any{}).
		SetStatus(quotaresetrequestnode.StatusActive).
		SaveX(ctx)
	decision := env.client.QuotaResetRequestDecision.Create().
		SetRequestID(request.ID).
		SetRequestNodeID(node.ID).
		SetActorUserID(env.userID).
		SetActorDisplayName("Member Example").
		SetDecision(quotaresetrequestdecision.DecisionApprove).
		SetComment("Approved before a stale retry attempt").
		SaveX(ctx)
	env.client.QuotaResetRequest.UpdateOneID(request.ID).
		SetCurrentNodeID(node.ID).
		SetWorkflowCompletedByDecisionID(decision.ID).
		SaveX(ctx)

	rec := performQuotaResetRequest(env.router, http.MethodPost, fmt.Sprintf("/api/v1/user/quota-reset/approvals/%d/retry-reset", request.ID), env.userToken, `{}`)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	var body struct {
		Message string `json:"message"`
		Details struct {
			Request *quotareset.RequestSummary `json:"request"`
		} `json:"details"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	var workflow *quotareset.WorkflowSummary
	if body.Details.Request != nil {
		workflow = body.Details.Request.Workflow
	}
	if body.Message != quotareset.ErrWorkflowAdvanced.Error() || body.Details.Request == nil || workflow == nil || workflow.CurrentNode == nil || workflow.CurrentNode.ID != node.ID || workflow.CanApprove || workflow.CanReject || workflow.CanRetry {
		t.Fatalf("stale retry response = %+v", body)
	}
}

func TestQuotaResetStaleDecisionRejectsUnrelatedViewerWithoutDetails(t *testing.T) {
	env := newQuotaResetHandlerTestEnvWithServiceFactory(t, func(client *ent.Client) quotaResetService {
		return quotareset.NewService(client, nil, nil, nil)
	})
	ctx := context.Background()
	requester := env.client.User.Create().
		SetUsername("request-owner").
		SetEmail("request-owner@example.com").
		SetAuthSource("sub2api_sso").
		SetRole(entuser.RoleUser).
		SaveX(ctx)
	request := env.client.QuotaResetRequest.Create().
		SetRequesterUserID(requester.ID).
		SetRequesterRelayUserID(1001).
		SetProviderID(1).
		SetGroupID("42").
		SetGroupName("Group Confidential").
		SetGroupPlatform("openai").
		SetReason("Confidential release investigation").
		SetWorkflowVersion(quotareset.WorkflowVersionV2).
		SetRequesterDisplayNameSnapshot("Request Owner").
		SetRequesterEmailSnapshot("request-owner@example.com").
		SetRequesterDepartmentPaths([]string{"Department Confidential"}).
		SetRequesterNotificationIds(map[string]string{}).
		SetStatus(quotaresetrequest.StatusPending).
		SetResolvedApproverUserIds([]int{}).
		SetMatchedDepartmentPaths([]map[string]any{}).
		SaveX(ctx)
	node := env.client.QuotaResetRequestNode.Create().
		SetRequestID(request.ID).
		SetPosition(0).
		SetNodeType(quotaresetrequestnode.NodeTypeConfiguredDepartment).
		SetLabel("Department Confidential").
		SetDepartmentSnapshots([]map[string]any{}).
		SetStatus(quotaresetrequestnode.StatusActive).
		SaveX(ctx)
	env.client.QuotaResetRequest.UpdateOneID(request.ID).SetCurrentNodeID(node.ID).SaveX(ctx)

	for _, action := range []string{"approve", "reject"} {
		rec := performQuotaResetRequest(env.router, http.MethodPost, fmt.Sprintf("/api/v1/user/quota-reset/approvals/%d/%s", request.ID, action), env.userToken, `{"request_node_id":9999,"decision_reason":"Unauthorized stale decision"}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s status = %d, want %d; body = %s", action, rec.Code, http.StatusForbidden, rec.Body.String())
		}
		body := rec.Body.String()
		for _, forbidden := range []string{"request-owner@example.com", "Group Confidential", "Confidential release investigation", "Department Confidential", `"details"`, `"workflow"`} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s response leaked %q: %s", action, forbidden, body)
			}
		}
	}
}

func TestQuotaResetLegacyResolvedApproverMutationReturnsSummary(t *testing.T) {
	env := newQuotaResetHandlerTestEnvWithServiceFactory(t, func(client *ent.Client) quotaResetService {
		return quotareset.NewService(client, nil, nil, nil)
	})
	ctx := context.Background()
	requester := env.client.User.Create().
		SetUsername("legacy-requester").
		SetEmail("legacy-requester@example.com").
		SetAuthSource("sub2api_sso").
		SetRole(entuser.RoleUser).
		SaveX(ctx)
	request := env.client.QuotaResetRequest.Create().
		SetRequesterUserID(requester.ID).
		SetRequesterRelayUserID(1001).
		SetProviderID(1).
		SetGroupID("42").
		SetGroupName("Group Alpha").
		SetGroupPlatform("openai").
		SetReason("Legacy approval compatibility").
		SetStatus(quotaresetrequest.StatusPending).
		SetResolvedApproverUserIds([]int{env.userID}).
		SetMatchedDepartmentPaths([]map[string]any{}).
		SaveX(ctx)

	rec := performQuotaResetRequest(env.router, http.MethodPost, fmt.Sprintf("/api/v1/user/quota-reset/approvals/%d/reject", request.ID), env.userToken, `{"decision_reason":"Rejected through the legacy contract"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"rejected"`) || !strings.Contains(rec.Body.String(), `"id":`+strconv.Itoa(request.ID)) {
		t.Fatalf("legacy response = %d %s", rec.Code, rec.Body.String())
	}
}

func TestQuotaResetMutationResponsesUseViewerAwareSummary(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		path         string
		body         string
		admin        bool
		requestID    int
		viewerUserID int
	}{
		{name: "create", method: http.MethodPost, path: "/api/v1/user/quota-reset/requests", body: `{"group_id":"42","reason":"Need reset"}`, requestID: 99, viewerUserID: 1},
		{name: "cancel", method: http.MethodPost, path: "/api/v1/user/quota-reset/requests/99/cancel", body: `{}`, requestID: 99, viewerUserID: 1},
		{name: "approve", method: http.MethodPost, path: "/api/v1/user/quota-reset/approvals/99/approve", body: `{"request_node_id":456,"decision_reason":"Approved"}`, requestID: 99, viewerUserID: 1},
		{name: "reject", method: http.MethodPost, path: "/api/v1/user/quota-reset/approvals/99/reject", body: `{"request_node_id":456,"decision_reason":"Rejected"}`, requestID: 99, viewerUserID: 1},
		{name: "retry", method: http.MethodPost, path: "/api/v1/user/quota-reset/approvals/99/retry-reset", body: `{}`, requestID: 99, viewerUserID: 1},
		{name: "admin approve", method: http.MethodPost, path: "/api/v1/admin/quota-reset/requests/99/approve", body: `{"request_node_id":456,"decision_reason":"Approved"}`, admin: true, requestID: 99, viewerUserID: 2},
		{name: "admin reject", method: http.MethodPost, path: "/api/v1/admin/quota-reset/requests/99/reject", body: `{"request_node_id":456,"decision_reason":"Rejected"}`, admin: true, requestID: 99, viewerUserID: 2},
		{name: "admin retry", method: http.MethodPost, path: "/api/v1/admin/quota-reset/requests/99/retry-reset", body: `{}`, admin: true, requestID: 99, viewerUserID: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summaryCalls := 0
			env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
				getRequestSummaryFn: func(_ context.Context, requestID, viewerUserID int, admin bool) (*quotareset.RequestSummary, error) {
					summaryCalls++
					if requestID != tt.requestID || viewerUserID != tt.viewerUserID || admin != tt.admin {
						t.Fatalf("summary args = request %d viewer %d admin %t", requestID, viewerUserID, admin)
					}
					return &quotareset.RequestSummary{ID: requestID, RequesterDisplayName: "Summary Viewer"}, nil
				},
			})
			token := env.userToken
			if tt.admin {
				token = env.adminToken
			}

			rec := performQuotaResetRequest(env.router, tt.method, tt.path, token, tt.body)

			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"requester_display_name":"Summary Viewer"`) {
				t.Fatalf("response = %d %s", rec.Code, rec.Body.String())
			}
			if summaryCalls != 1 {
				t.Fatalf("summary calls = %d, want 1", summaryCalls)
			}
		})
	}
}

func TestQuotaResetListAdminForwardsAuthenticatedActorID(t *testing.T) {
	env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		listAdminFn: func(_ context.Context, actorUserID int, params quotareset.ListParams) (*quotareset.RequestListResponse, error) {
			if actorUserID != 2 || params.Page != 3 || params.PageSize != 7 || params.Status != "pending" {
				t.Fatalf("actor/params = %d / %+v", actorUserID, params)
			}
			return &quotareset.RequestListResponse{Page: 3, PageSize: 7}, nil
		},
	})
	rec := performQuotaResetRequest(env.router, http.MethodGet, "/api/v1/admin/quota-reset/requests?page=3&page_size=7&status=pending", env.adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("response = %d %s", rec.Code, rec.Body.String())
	}
}

func TestQuotaResetSaveApproverConfigsPassesMode(t *testing.T) {
	env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		saveApproverConfigsFn: func(_ context.Context, input quotareset.SaveApproverConfigsInput) (*quotareset.ApproverConfigListResponse, error) {
			if input.ActorUserID != 2 || input.Mode != quotareset.ApproverConfigSaveModeReplaceAll || len(input.Items) != 1 {
				t.Fatalf("input = %+v", input)
			}
			return &quotareset.ApproverConfigListResponse{}, nil
		},
	})
	rec := performQuotaResetRequest(env.router, http.MethodPut, "/api/v1/admin/quota-reset/approver-configs", env.adminToken, `{"mode":"replace_all","items":[{"department_external_id":"dept-alpha","approver_user_id":7,"enabled":true}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("response = %d %s", rec.Code, rec.Body.String())
	}
}

func TestQuotaResetSaveApproverConfigsMapsReferencedConflict(t *testing.T) {
	detail := `enabled approval chains reference departments without approver configs: provider_id=1 group_id=42 group_name="Group Alpha"`
	env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		saveApproverConfigsFn: func(context.Context, quotareset.SaveApproverConfigsInput) (*quotareset.ApproverConfigListResponse, error) {
			return nil, fmt.Errorf("save approver configs: %w: %s", quotareset.ErrApproverConfigReferenced, detail)
		},
	})

	rec := performQuotaResetRequest(env.router, http.MethodPut, "/api/v1/admin/quota-reset/approver-configs", env.adminToken, `{"mode":"replace_all","items":[]}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	wantMessage := "save approver configs: " + quotareset.ErrApproverConfigReferenced.Error() + ": " + detail
	var body struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if body.Message != wantMessage {
		t.Fatalf("message = %q, want %q", body.Message, wantMessage)
	}
}

func TestQuotaResetApproverCandidatesAcceptSearchAndPagination(t *testing.T) {
	env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		listApproverCandidatesFn: func(_ context.Context, params quotareset.ApproverCandidateParams) (*quotareset.ApproverCandidateListResponse, error) {
			if params.SourceID != 3 || params.Query != "Alice" || params.Page != 2 || params.PageSize != 15 {
				t.Fatalf("params = %+v", params)
			}
			return &quotareset.ApproverCandidateListResponse{Items: []quotareset.ApproverCandidate{{
				UserID:                    12,
				Username:                  "lead-alpha",
				Email:                     "lead-alpha@example.com",
				DisplayName:               "Lead Alpha",
				DirectoryMemberExternalID: "member-alpha-lead",
			}}}, nil
		},
	})
	rec := performQuotaResetRequest(env.router, http.MethodGet, "/api/v1/admin/quota-reset/approver-candidates?source_id=3&department_external_id=department-alpha&q=%20Alice%20&page=2&page_size=15", env.adminToken, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"lead-alpha@example.com"`) {
		t.Fatalf("response = %d %s", rec.Code, rec.Body.String())
	}

	serviceValidated := false
	env = newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		listApproverCandidatesFn: func(_ context.Context, params quotareset.ApproverCandidateParams) (*quotareset.ApproverCandidateListResponse, error) {
			serviceValidated = true
			if params.SourceID != 0 || params.Query != "alice" || params.Page != 0 || params.PageSize != 0 {
				t.Fatalf("params = %+v", params)
			}
			return nil, fmt.Errorf("%w: source_id is required", quotareset.ErrInvalidApproverConfig)
		},
	})
	rec = performQuotaResetRequest(env.router, http.MethodGet, "/api/v1/admin/quota-reset/approver-candidates?source_id=invalid&q=%20alice%20", env.adminToken, "")
	if rec.Code != http.StatusBadRequest || !serviceValidated {
		t.Fatalf("service validation response = %d %s; called = %t", rec.Code, rec.Body.String(), serviceValidated)
	}
}

func TestQuotaResetListApproverCandidatesForwardsMaxIntPage(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		listApproverCandidatesFn: func(_ context.Context, params quotareset.ApproverCandidateParams) (*quotareset.ApproverCandidateListResponse, error) {
			if params.SourceID != 3 || params.Page != maxInt || params.PageSize != 20 {
				t.Fatalf("params = %+v", params)
			}
			return &quotareset.ApproverCandidateListResponse{
				Items:    []quotareset.ApproverCandidate{},
				Page:     params.Page,
				PageSize: params.PageSize,
				Total:    1,
			}, nil
		},
	})
	path := "/api/v1/admin/quota-reset/approver-candidates?source_id=3&page=" + strconv.Itoa(maxInt) + "&page_size=20"

	rec := performQuotaResetRequest(env.router, http.MethodGet, path, env.adminToken, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"page":`+strconv.Itoa(maxInt)) || !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("response = %d %s", rec.Code, rec.Body.String())
	}
}

func TestQuotaResetApprovalChainRoutesListOptionsAndSave(t *testing.T) {
	actualRouter := setupTestEnvWithProvider(t).router
	wantRoutes := map[string]bool{
		http.MethodGet + " /api/v1/admin/quota-reset/approval-chains":        false,
		http.MethodPut + " /api/v1/admin/quota-reset/approval-chains":        false,
		http.MethodGet + " /api/v1/admin/quota-reset/approval-chain-options": false,
	}
	for _, route := range actualRouter.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := wantRoutes[key]; ok {
			wantRoutes[key] = true
		}
	}
	for route, registered := range wantRoutes {
		if !registered {
			t.Errorf("route %s is not registered by SetupRouter", route)
		}
	}

	listCalls := 0
	optionsCalls := 0
	saveCalls := 0
	env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		listApprovalChainsFn: func(context.Context) (*quotareset.ApprovalChainListResponse, error) {
			listCalls++
			return &quotareset.ApprovalChainListResponse{Items: []quotareset.ApprovalChain{{ID: 10, GroupID: "group-alpha"}}}, nil
		},
		listApprovalChainOptionsFn: func(context.Context) (*quotareset.ApprovalChainOptionsResponse, error) {
			optionsCalls++
			return &quotareset.ApprovalChainOptionsResponse{Groups: []quotareset.ApprovalChainGroupOption{{GroupID: "group-alpha"}}, Departments: []quotareset.ApprovalChainDepartmentOption{}}, nil
		},
		saveApprovalChainsFn: func(_ context.Context, input quotareset.SaveApprovalChainsInput) (*quotareset.ApprovalChainListResponse, error) {
			saveCalls++
			if input.ActorUserID != 2 || input.Items == nil || len(input.Items) != 0 {
				t.Fatalf("save input = %+v", input)
			}
			return &quotareset.ApprovalChainListResponse{Items: []quotareset.ApprovalChain{}}, nil
		},
	})

	forbidden := performQuotaResetRequest(env.router, http.MethodGet, "/api/v1/admin/quota-reset/approval-chains", env.userToken, "")
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("non-admin response = %d %s", forbidden.Code, forbidden.Body.String())
	}
	list := performQuotaResetRequest(env.router, http.MethodGet, "/api/v1/admin/quota-reset/approval-chains", env.adminToken, "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"group_id":"group-alpha"`) {
		t.Fatalf("list response = %d %s", list.Code, list.Body.String())
	}
	options := performQuotaResetRequest(env.router, http.MethodGet, "/api/v1/admin/quota-reset/approval-chain-options", env.adminToken, "")
	if options.Code != http.StatusOK || !strings.Contains(options.Body.String(), `"groups"`) {
		t.Fatalf("options response = %d %s", options.Code, options.Body.String())
	}
	saved := performQuotaResetRequest(env.router, http.MethodPut, "/api/v1/admin/quota-reset/approval-chains", env.adminToken, `{"items":[]}`)
	if saved.Code != http.StatusOK || !strings.Contains(saved.Body.String(), `"items":[]`) {
		t.Fatalf("save response = %d %s", saved.Code, saved.Body.String())
	}
	if listCalls != 1 || optionsCalls != 1 || saveCalls != 1 {
		t.Fatalf("chain calls = list %d options %d save %d", listCalls, optionsCalls, saveCalls)
	}
}

func TestQuotaResetNotificationSettingsUseExplicitChannelAndRedactedURL(t *testing.T) {
	var updates []quotareset.UpdateNotificationSettingsInput
	redacted := &quotareset.NotificationSettings{
		Enabled:         true,
		ChannelType:     "generic_webhook",
		TemplateVersion: 1,
		URLConfigured:   true,
		URLPreview:      "https://hooks.example.com/.../redacted",
		AuthType:        "none",
	}
	env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		getNotificationSettingsFn: func(context.Context) (*quotareset.NotificationSettings, error) {
			return redacted, nil
		},
		updateNotificationSettingsFn: func(_ context.Context, input quotareset.UpdateNotificationSettingsInput) (*quotareset.NotificationSettings, error) {
			updates = append(updates, input)
			return redacted, nil
		},
	})

	get := performQuotaResetRequest(env.router, http.MethodGet, "/api/v1/admin/quota-reset/notification-settings", env.adminToken, "")
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"url_configured":true`) || strings.Contains(get.Body.String(), `"url":`) {
		t.Fatalf("get response = %d %s", get.Code, get.Body.String())
	}
	omitted := performQuotaResetRequest(env.router, http.MethodPut, "/api/v1/admin/quota-reset/notification-settings", env.adminToken, `{"enabled":true,"channel_type":"generic_webhook","auth_type":"none"}`)
	if omitted.Code != http.StatusOK || strings.Contains(omitted.Body.String(), `"url":`) {
		t.Fatalf("omitted-url response = %d %s", omitted.Code, omitted.Body.String())
	}
	empty := performQuotaResetRequest(env.router, http.MethodPut, "/api/v1/admin/quota-reset/notification-settings", env.adminToken, `{"enabled":false,"channel_type":"generic_webhook","url":"","auth_type":"none","credential_id":null}`)
	if empty.Code != http.StatusOK || strings.Contains(empty.Body.String(), `"url":`) {
		t.Fatalf("empty-url response = %d %s", empty.Code, empty.Body.String())
	}
	if len(updates) != 2 || updates[0].ActorUserID != 2 || updates[0].ChannelType != "generic_webhook" || updates[0].URL != nil || updates[1].URL == nil || *updates[1].URL != "" {
		t.Fatalf("notification updates = %+v", updates)
	}
}

func TestQuotaResetNotificationTestReturnsCoverageWarning(t *testing.T) {
	env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		testNotificationSettingsFn: func(_ context.Context, actorUserID int) (*quotareset.NotificationTestResult, error) {
			if actorUserID != 2 {
				t.Fatalf("actor user id = %d", actorUserID)
			}
			return &quotareset.NotificationTestResult{
				Delivered:             true,
				RecipientCount:        1,
				MissingRecipientCount: 1,
				Warning:               "The test was delivered without an Enterprise WeChat mention.",
			}, nil
		},
	})

	rec := performQuotaResetRequest(env.router, http.MethodPost, "/api/v1/admin/quota-reset/notification-settings/test", env.adminToken, `{}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("response = %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data["delivered"] != true || body.Data["recipient_count"] != float64(1) || body.Data["missing_recipient_count"] != float64(1) || body.Data["warning"] == "" {
		t.Fatalf("notification test data = %#v", body.Data)
	}
	if _, leaked := body.Data["recipient_ids"]; leaked {
		t.Fatalf("notification test leaked recipient ids: %#v", body.Data)
	}
}

func TestQuotaResetDirectoryUnavailableMapsToServiceUnavailable(t *testing.T) {
	env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		listApproverCandidatesFn: func(context.Context, quotareset.ApproverCandidateParams) (*quotareset.ApproverCandidateListResponse, error) {
			return nil, fmt.Errorf("load directory snapshot: %w", quotareset.ErrDirectoryUnavailable)
		},
	})

	rec := performQuotaResetRequest(env.router, http.MethodGet, "/api/v1/admin/quota-reset/approver-candidates?source_id=3", env.adminToken, "")

	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), quotareset.ErrDirectoryUnavailable.Error()) {
		t.Fatalf("response = %d %s", rec.Code, rec.Body.String())
	}
}

func TestQuotaResetStaleApproverCandidateSourceMapsToServiceUnavailable(t *testing.T) {
	ctx := context.Background()
	serviceClient := testdb.Open(t)
	stale := createQuotaResetHandlerDirectorySource(t, ctx, serviceClient, "Stale Directory", time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC))
	current := createQuotaResetHandlerDirectorySource(t, ctx, serviceClient, "Current Directory", time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC))
	service := quotareset.NewService(serviceClient, nil, nil, nil)
	env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		listApproverCandidatesFn: service.ListApproverCandidates,
	})

	for _, sourceID := range []int{stale.ID, current.ID + 1000} {
		rec := performQuotaResetRequest(env.router, http.MethodGet, fmt.Sprintf("/api/v1/admin/quota-reset/approver-candidates?source_id=%d", sourceID), env.adminToken, "")
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), quotareset.ErrDirectoryUnavailable.Error()) {
			t.Fatalf("source %d response = %d %s, want 503 directory unavailable", sourceID, rec.Code, rec.Body.String())
		}
	}
}

type quotaResetHandlerTestEnv struct {
	router     *gin.Engine
	client     *ent.Client
	userID     int
	userToken  string
	adminToken string
}

func newQuotaResetHandlerTestEnv(t *testing.T, service *fakeQuotaResetService) *quotaResetHandlerTestEnv {
	t.Helper()
	return newQuotaResetHandlerTestEnvWithServiceFactory(t, func(*ent.Client) quotaResetService {
		return service
	})
}

func newQuotaResetHandlerTestEnvWithServiceFactory(t *testing.T, serviceFactory func(*ent.Client) quotaResetService) *quotaResetHandlerTestEnv {
	t.Helper()
	client := testdb.Open(t)
	logger := zap.NewNop()
	authSvc := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, logger)

	user := client.User.Create().
		SetUsername("member").
		SetEmail("member@example.com").
		SetAuthSource("sub2api_sso").
		SetRole(entuser.RoleUser).
		SaveX(context.Background())
	admin := client.User.Create().
		SetUsername("admin").
		SetEmail("admin@example.com").
		SetAuthSource("sub2api_sso").
		SetRole(entuser.RoleAdmin).
		SaveX(context.Background())

	userPair, err := authSvc.GenerateTokenPairForUser(&auth.UserInfo{ID: user.ID, Username: user.Username, Role: string(user.Role)})
	if err != nil {
		t.Fatalf("generate user token: %v", err)
	}
	adminPair, err := authSvc.GenerateTokenPairForUser(&auth.UserInfo{ID: admin.ID, Username: admin.Username, Role: string(admin.Role)})
	if err != nil {
		t.Fatalf("generate admin token: %v", err)
	}

	router := gin.New()
	handler := NewQuotaResetHandler(serviceFactory(client))
	userGroup := router.Group("/api/v1/user")
	userGroup.Use(auth.RequireAuth(authSvc))
	userGroup.POST("/quota-reset/requests", handler.CreateRequest)
	userGroup.POST("/quota-reset/requests/:id/cancel", handler.Cancel)
	userGroup.POST("/quota-reset/approvals/:id/approve", handler.Approve)
	userGroup.POST("/quota-reset/approvals/:id/reject", handler.Reject)
	userGroup.POST("/quota-reset/approvals/:id/retry-reset", handler.RetryReset)

	adminGroup := router.Group("/api/v1/admin/quota-reset")
	adminGroup.Use(auth.RequireAuth(authSvc), auth.RequireAdmin())
	adminGroup.GET("/requests", handler.ListAdmin)
	adminGroup.POST("/requests/:id/approve", handler.AdminApprove)
	adminGroup.POST("/requests/:id/reject", handler.AdminReject)
	adminGroup.POST("/requests/:id/retry-reset", handler.AdminRetryReset)
	adminGroup.GET("/approver-candidates", handler.ListApproverCandidates)
	adminGroup.PUT("/approver-configs", handler.SaveApproverConfigs)
	adminGroup.GET("/notification-settings", handler.GetNotificationSettings)
	adminGroup.PUT("/notification-settings", handler.UpdateNotificationSettings)
	adminGroup.POST("/notification-settings/test", handler.TestNotificationSettings)
	adminGroup.GET("/approval-chains", handler.ListApprovalChains)
	adminGroup.PUT("/approval-chains", handler.SaveApprovalChains)
	adminGroup.GET("/approval-chain-options", handler.ListApprovalChainOptions)

	return &quotaResetHandlerTestEnv{
		router:     router,
		client:     client,
		userID:     user.ID,
		userToken:  userPair.AccessToken,
		adminToken: adminPair.AccessToken,
	}
}

func createQuotaResetHandlerDirectorySource(t *testing.T, ctx context.Context, client *ent.Client, name string, completedAt time.Time) *ent.DirectorySource {
	t.Helper()
	source := client.DirectorySource.Create().
		SetName(name).
		SetDescription("Synthetic organization directory").
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		SaveX(ctx)
	run := client.DirectorySyncRun.Create().
		SetSourceID(source.ID).
		SetMode("apply").
		SetStatus("completed").
		SetPhase("completed").
		SetCompletedAt(completedAt).
		SaveX(ctx)
	return client.DirectorySource.UpdateOneID(source.ID).
		SetLastRunID(run.ID).
		SetLastSuccessfulRunID(run.ID).
		SaveX(ctx)
}

func performQuotaResetRequest(router *gin.Engine, method, path, token string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
