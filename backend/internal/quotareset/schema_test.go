package quotareset

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/quotaresetnotificationsetting"
	"github.com/ai-efficiency/backend/ent/quotaresetrequest"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestevent"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestnode"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestWorkflowSchemasRoundTrip(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	chain := client.QuotaResetApprovalChain.Create().
		SetProviderID(provider.ID).SetGroupID("42").SetGroupName("Group Alpha").SetEnabled(true).
		SetCreatedByUserID(requester.ID).SetUpdatedByUserID(requester.ID).SaveX(ctx)
	client.QuotaResetApprovalChainNode.Create().
		SetChainID(chain.ID).SetPosition(0).SetDirectorySourceID(1).
		SetDepartmentExternalID("department-alpha").SetDepartmentDisplayPath("Department Alpha").SaveX(ctx)
	request := client.QuotaResetRequest.Create().
		SetRequesterUserID(requester.ID).SetRequesterRelayUserID(1001).
		SetProviderID(provider.ID).SetGroupID("42").SetGroupName("Group Alpha").SetGroupPlatform("openai").
		SetReason("Need a reset for a build investigation").SetWorkflowVersion(2).
		SetRequesterDisplayNameSnapshot("Alice").SetRequesterEmailSnapshot("alice@example.com").
		SetRequesterDepartmentPaths([]string{"Department Alpha"}).
		SetRequesterNotificationIds(map[string]string{"wecom": "alice-wecom"}).SaveX(ctx)
	node := client.QuotaResetRequestNode.Create().
		SetRequestID(request.ID).SetPosition(0).SetNodeType("requester_departments").
		SetLabel("Requester departments").
		SetDepartmentSnapshots([]map[string]any{{"external_id": "department-alpha", "display_path": "Department Alpha"}}).
		SetStatus(quotaresetrequestnode.StatusActive).SaveX(ctx)
	activeRequest := client.QuotaResetRequest.UpdateOneID(request.ID).SetCurrentNodeID(node.ID).SaveX(ctx)
	if activeRequest.CurrentNodeID == nil || *activeRequest.CurrentNodeID != node.ID {
		t.Fatalf("active request current node id = %v, want %d", activeRequest.CurrentNodeID, node.ID)
	}
	client.QuotaResetRequestNodeApprover.Create().
		SetRequestNodeID(node.ID).SetUserID(approver.ID).SetDisplayName("Bob").SetEmail("bob@example.org").
		SetSource("configured").SetSourceDepartmentExternalIds([]string{"department-alpha"}).
		SetNotificationIds(map[string]string{"wecom": "bob-wecom"}).SaveX(ctx)
	decision := client.QuotaResetRequestDecision.Create().
		SetRequestID(request.ID).SetRequestNodeID(node.ID).SetActorUserID(approver.ID).
		SetActorDisplayName("Bob").SetDecision("approve").
		SetComment("Approved for the current investigation").SetAdminOverride(false).SaveX(ctx)
	approvedNode := client.QuotaResetRequestNode.UpdateOneID(node.ID).
		SetStatus(quotaresetrequestnode.StatusApproved).SetSatisfiedByDecisionID(decision.ID).SaveX(ctx)
	completedRequest := client.QuotaResetRequest.UpdateOneID(request.ID).
		ClearCurrentNodeID().SetWorkflowCompletedByDecisionID(decision.ID).
		SetStatus(quotaresetrequest.StatusApprovedResetSucceeded).SaveX(ctx)
	if got := client.QuotaResetRequestNodeApprover.Query().CountX(ctx); got != 1 {
		t.Fatalf("node approver count = %d, want 1", got)
	}
	if got := client.QuotaResetRequestDecision.Query().OnlyX(ctx).Comment; got != "Approved for the current investigation" {
		t.Fatalf("decision comment = %q", got)
	}
	if got := approvedNode.Status; got != quotaresetrequestnode.StatusApproved {
		t.Fatalf("node status = %q, want %q", got, quotaresetrequestnode.StatusApproved)
	}
	if completedRequest.CurrentNodeID != nil {
		t.Fatalf("current node id = %d, want nil", *completedRequest.CurrentNodeID)
	}
	if completedRequest.WorkflowCompletedByDecisionID == nil {
		t.Fatal("workflow completed decision id = nil, want decision id")
	}
	if got := *completedRequest.WorkflowCompletedByDecisionID; got != decision.ID {
		t.Fatalf("workflow completed decision id = %d, want %d", got, decision.ID)
	}
	if got := completedRequest.Status; got != quotaresetrequest.StatusApprovedResetSucceeded {
		t.Fatalf("request status = %q, want %q", got, quotaresetrequest.StatusApprovedResetSucceeded)
	}
}

func TestQuotaResetRequestCreationFactsAreImmutable(t *testing.T) {
	updateTypes := []reflect.Type{
		reflect.TypeOf((*ent.QuotaResetRequestUpdate)(nil)),
		reflect.TypeOf((*ent.QuotaResetRequestUpdateOne)(nil)),
	}
	creationFields := []string{
		"RequesterUserID",
		"RequesterRelayUserID",
		"ProviderID",
		"GroupID",
		"GroupName",
		"GroupPlatform",
		"Reason",
		"WorkflowVersion",
		"RequesterDisplayNameSnapshot",
		"RequesterEmailSnapshot",
		"RequesterDepartmentPaths",
		"RequesterNotificationIds",
		"ResolvedApproverUserIds",
		"MatchedDepartmentPaths",
		"CreatedAt",
	}
	updateOperations := []string{"Set", "SetNillable", "Add", "Append", "Clear"}
	stateMethods := []string{
		"SetStatus",
		"SetCurrentNodeID",
		"ClearCurrentNodeID",
		"SetWorkflowCompletedByDecisionID",
		"ClearWorkflowCompletedByDecisionID",
		"SetApprovedByUserID",
		"ClearApprovedByUserID",
		"SetRejectedByUserID",
		"ClearRejectedByUserID",
		"SetDecisionReason",
		"SetDecidedAt",
		"ClearDecidedAt",
		"SetResetError",
		"SetResetStartedAt",
		"ClearResetStartedAt",
		"SetResetCompletedAt",
		"ClearResetCompletedAt",
		"SetUpdatedAt",
	}
	for _, updateType := range updateTypes {
		for _, fieldName := range creationFields {
			for _, operation := range updateOperations {
				methodName := operation + fieldName
				if _, ok := updateType.MethodByName(methodName); ok {
					t.Errorf("%s exposes creation-fact method %s", updateType, methodName)
				}
			}
		}
		for _, methodName := range stateMethods {
			if _, ok := updateType.MethodByName(methodName); !ok {
				t.Errorf("%s is missing required state method %s", updateType, methodName)
			}
		}
	}
}

func TestQuotaResetRequestCreationFactsIgnoreDirectUpdateMutation(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := client.QuotaResetRequest.Create().
		SetRequesterUserID(requester.ID).SetRequesterRelayUserID(1001).
		SetProviderID(provider.ID).SetGroupID("42").SetGroupName("Group Alpha").SetGroupPlatform("openai").
		SetReason("Original reason").SetWorkflowVersion(2).
		SetRequesterDisplayNameSnapshot("Alice").SetRequesterEmailSnapshot("alice@example.com").
		SetRequesterDepartmentPaths([]string{"Department Alpha"}).
		SetRequesterNotificationIds(map[string]string{"wecom": "alice-wecom"}).
		SetResolvedApproverUserIds([]int{requester.ID}).
		SetMatchedDepartmentPaths([]map[string]any{{"external_id": "department-alpha"}}).SaveX(ctx)
	update := client.QuotaResetRequest.UpdateOneID(request.ID)
	mutation := update.Mutation()
	mutation.SetReason("Tampered reason")
	mutation.SetWorkflowVersion(99)
	mutation.SetRequesterEmailSnapshot("tampered@example.net")
	mutation.SetRequesterNotificationIds(map[string]string{"wecom": "tampered-wecom"})
	updated := update.SaveX(ctx)
	if got := updated.Reason; got != "Original reason" {
		t.Fatalf("reason after direct mutation = %q, want original", got)
	}
	if got := updated.WorkflowVersion; got != 2 {
		t.Fatalf("workflow version after direct mutation = %d, want 2", got)
	}
	if got := updated.RequesterEmailSnapshot; got != "alice@example.com" {
		t.Fatalf("requester email after direct mutation = %q, want original", got)
	}
	if got := updated.RequesterNotificationIds["wecom"]; got != "alice-wecom" {
		t.Fatalf("requester notification id after direct mutation = %q, want original", got)
	}
}

func TestQuotaResetRequestJSONSnapshotClearsAreRejected(t *testing.T) {
	const wantError = "quotaresetrequest: JSON creation snapshots cannot be cleared"

	ctx := context.Background()
	clearMethods := []string{
		"ClearRequesterDepartmentPaths",
		"ClearRequesterNotificationIds",
		"ClearResolvedApproverUserIds",
		"ClearMatchedDepartmentPaths",
	}
	for _, updateType := range []reflect.Type{
		reflect.TypeOf((*ent.QuotaResetRequestUpdate)(nil)),
		reflect.TypeOf((*ent.QuotaResetRequestUpdateOne)(nil)),
	} {
		for _, methodName := range clearMethods {
			if _, ok := updateType.MethodByName(methodName); ok {
				t.Errorf("%s exposes %s", updateType, methodName)
			}
		}
	}
	mutationType := reflect.TypeOf((*ent.QuotaResetRequestMutation)(nil))
	for _, methodName := range clearMethods {
		if _, ok := mutationType.MethodByName(methodName); !ok {
			t.Errorf("%s does not expose hook-protected %s", mutationType, methodName)
		}
	}
	updateCases := []struct {
		name string
		run  func(*testing.T, *ent.Client, int, string) error
	}{
		{
			name: "update one direct mutation",
			run: func(t *testing.T, client *ent.Client, requestID int, clearMethod string) error {
				update := client.QuotaResetRequest.UpdateOneID(requestID)
				invokeRequestSnapshotClearMethods(t, update.Mutation(), []string{clearMethod})
				_, err := update.Save(ctx)
				return err
			},
		},
		{
			name: "bulk update direct mutation",
			run: func(t *testing.T, client *ent.Client, requestID int, clearMethod string) error {
				update := client.QuotaResetRequest.Update().
					Where(quotaresetrequest.IDEQ(requestID))
				invokeRequestSnapshotClearMethods(t, update.Mutation(), []string{clearMethod})
				_, err := update.Save(ctx)
				return err
			},
		},
	}

	for _, test := range updateCases {
		for _, clearMethod := range clearMethods {
			t.Run(fmt.Sprintf("%s/%s", test.name, clearMethod), func(t *testing.T) {
				client := testdb.Open(t)
				requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
				provider := createQuotaResetRelayProvider(t, ctx, client)
				request := client.QuotaResetRequest.Create().
					SetRequesterUserID(requester.ID).SetRequesterRelayUserID(1001).
					SetProviderID(provider.ID).SetGroupID("42").
					SetGroupName("Group Alpha").SetGroupPlatform("openai").
					SetReason("Request with immutable JSON snapshots").SetWorkflowVersion(2).
					SetRequesterDepartmentPaths([]string{"Department Alpha"}).
					SetRequesterNotificationIds(map[string]string{"wecom": "alice-wecom"}).
					SetResolvedApproverUserIds([]int{requester.ID}).
					SetMatchedDepartmentPaths([]map[string]any{{"external_id": "department-alpha"}}).
					SaveX(ctx)

				err := test.run(t, client, request.ID, clearMethod)
				if err == nil || err.Error() != wantError {
					t.Errorf("clear snapshots error = %v, want %q", err, wantError)
				}
				stored := client.QuotaResetRequest.GetX(ctx, request.ID)
				if !reflect.DeepEqual(stored.RequesterDepartmentPaths, request.RequesterDepartmentPaths) {
					t.Errorf("requester department paths = %#v, want %#v", stored.RequesterDepartmentPaths, request.RequesterDepartmentPaths)
				}
				if !reflect.DeepEqual(stored.RequesterNotificationIds, request.RequesterNotificationIds) {
					t.Errorf("requester notification ids = %#v, want %#v", stored.RequesterNotificationIds, request.RequesterNotificationIds)
				}
				if !reflect.DeepEqual(stored.ResolvedApproverUserIds, request.ResolvedApproverUserIds) {
					t.Errorf("resolved approver user ids = %#v, want %#v", stored.ResolvedApproverUserIds, request.ResolvedApproverUserIds)
				}
				if !reflect.DeepEqual(stored.MatchedDepartmentPaths, request.MatchedDepartmentPaths) {
					t.Errorf("matched department paths = %#v, want %#v", stored.MatchedDepartmentPaths, request.MatchedDepartmentPaths)
				}
			})
		}
	}
}

func invokeRequestSnapshotClearMethods(t *testing.T, target any, methods []string) {
	t.Helper()
	value := reflect.ValueOf(target)
	for _, methodName := range methods {
		method := value.MethodByName(methodName)
		if !method.IsValid() {
			t.Fatalf("%T does not expose %s", target, methodName)
		}
		method.Call(nil)
	}
}

func TestWorkflowSchemaDefaultsAndLifecycleEvent(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	legacyRequest := client.QuotaResetRequest.Create().
		SetRequesterUserID(requester.ID).SetRequesterRelayUserID(1001).
		SetProviderID(provider.ID).SetGroupID("42").SetGroupName("Group Alpha").SetGroupPlatform("openai").
		SetReason("Legacy request without workflow fields").SaveX(ctx)
	legacyRequest = client.QuotaResetRequest.GetX(ctx, legacyRequest.ID)
	if got := legacyRequest.WorkflowVersion; got != 1 {
		t.Fatalf("legacy workflow version = %d, want 1", got)
	}

	setting := client.QuotaResetNotificationSetting.Create().SaveX(ctx)
	setting = client.QuotaResetNotificationSetting.GetX(ctx, setting.ID)
	if got := setting.ChannelType; got != quotaresetnotificationsetting.ChannelTypeGenericWebhook {
		t.Fatalf("notification channel type = %q, want %q", got, quotaresetnotificationsetting.ChannelTypeGenericWebhook)
	}
	if setting.ChannelTypeConfigured {
		t.Fatal("notification channel type configured = true, want false")
	}
	if got := setting.TemplateVersion; got != 1 {
		t.Fatalf("notification template version = %d, want 1", got)
	}

	eventTypes := []quotaresetrequestevent.EventType{
		quotaresetrequestevent.EventTypeWorkflowSnapshotted,
		quotaresetrequestevent.EventTypeNodeActivated,
		quotaresetrequestevent.EventTypeNodeApproved,
		quotaresetrequestevent.EventTypeNodeSatisfiedByPriorApproval,
		quotaresetrequestevent.EventTypeNodeSkippedNoApprover,
		quotaresetrequestevent.EventTypeAdminFallbackActivated,
	}
	for _, eventType := range eventTypes {
		t.Run(string(eventType), func(t *testing.T) {
			event := client.QuotaResetRequestEvent.Create().
				SetRequestID(legacyRequest.ID).
				SetEventType(eventType).
				SaveX(ctx)
			if got := event.EventType; got != eventType {
				t.Fatalf("event type = %q, want %q", got, eventType)
			}
		})
	}
	if got := client.QuotaResetRequestEvent.Query().
		Where(quotaresetrequestevent.RequestIDEQ(legacyRequest.ID)).
		CountX(ctx); got != len(eventTypes) {
		t.Fatalf("lifecycle event count = %d, want %d", got, len(eventTypes))
	}
}

func TestWorkflowSnapshotJSONFieldsAreNonNullableAndNotClearable(t *testing.T) {
	clearMethods := []struct {
		mutationType reflect.Type
		method       string
	}{
		{reflect.TypeOf((*ent.QuotaResetRequestNodeMutation)(nil)), "ClearDepartmentSnapshots"},
		{reflect.TypeOf((*ent.QuotaResetRequestNodeApproverMutation)(nil)), "ClearSourceDepartmentExternalIds"},
		{reflect.TypeOf((*ent.QuotaResetRequestNodeApproverMutation)(nil)), "ClearNotificationIds"},
	}
	for _, clearMethod := range clearMethods {
		if _, ok := clearMethod.mutationType.MethodByName(clearMethod.method); ok {
			t.Errorf("%s exposes %s", clearMethod.mutationType, clearMethod.method)
		}
	}

	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := client.QuotaResetRequest.Create().
		SetRequesterUserID(requester.ID).SetRequesterRelayUserID(1001).
		SetProviderID(provider.ID).SetGroupID("42").SetGroupName("Group Alpha").SetGroupPlatform("openai").
		SetReason("Request with default workflow snapshots").SetWorkflowVersion(2).SaveX(ctx)
	node := client.QuotaResetRequestNode.Create().
		SetRequestID(request.ID).SetPosition(0).SetNodeType("requester_departments").SaveX(ctx)
	nodeApprover := client.QuotaResetRequestNodeApprover.Create().
		SetRequestNodeID(node.ID).SetUserID(approver.ID).SetSource("configured").SaveX(ctx)
	node = client.QuotaResetRequestNode.GetX(ctx, node.ID)
	nodeApprover = client.QuotaResetRequestNodeApprover.GetX(ctx, nodeApprover.ID)
	if node.DepartmentSnapshots == nil {
		t.Error("department snapshots = nil, want non-null empty array")
	}
	if nodeApprover.SourceDepartmentExternalIds == nil {
		t.Error("source department external ids = nil, want non-null empty array")
	}
	if nodeApprover.NotificationIds == nil {
		t.Error("notification ids = nil, want non-null empty object")
	}
}

func TestWorkflowSnapshotJSONFieldsRejectExplicitNil(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	sourceApprover := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	notificationApprover := createQuotaResetUser(t, ctx, client, "carol", "carol@example.net", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := client.QuotaResetRequest.Create().
		SetRequesterUserID(requester.ID).SetRequesterRelayUserID(1001).
		SetProviderID(provider.ID).SetGroupID("42").SetGroupName("Group Alpha").SetGroupPlatform("openai").
		SetReason("Request with explicit nil workflow snapshots").SetWorkflowVersion(2).SaveX(ctx)
	approverNode := client.QuotaResetRequestNode.Create().
		SetRequestID(request.ID).SetPosition(2).SetNodeType("requester_departments").SaveX(ctx)

	tests := []struct {
		name   string
		create func() (bool, error)
	}{
		{
			name: "department snapshots nil container",
			create: func() (bool, error) {
				node, err := client.QuotaResetRequestNode.Create().
					SetRequestID(request.ID).SetPosition(0).SetNodeType("requester_departments").
					SetDepartmentSnapshots(nil).Save(ctx)
				if err != nil {
					return false, err
				}
				node, err = client.QuotaResetRequestNode.Get(ctx, node.ID)
				return node != nil && node.DepartmentSnapshots == nil, err
			},
		},
		{
			name: "department snapshots nil element",
			create: func() (bool, error) {
				node, err := client.QuotaResetRequestNode.Create().
					SetRequestID(request.ID).SetPosition(1).SetNodeType("requester_departments").
					SetDepartmentSnapshots([]map[string]any{nil}).Save(ctx)
				if err != nil {
					return false, err
				}
				node, err = client.QuotaResetRequestNode.Get(ctx, node.ID)
				return node != nil && len(node.DepartmentSnapshots) == 1 && node.DepartmentSnapshots[0] == nil, err
			},
		},
		{
			name: "source department external ids nil container",
			create: func() (bool, error) {
				approver, err := client.QuotaResetRequestNodeApprover.Create().
					SetRequestNodeID(approverNode.ID).SetUserID(sourceApprover.ID).SetSource("configured").
					SetSourceDepartmentExternalIds(nil).Save(ctx)
				if err != nil {
					return false, err
				}
				approver, err = client.QuotaResetRequestNodeApprover.Get(ctx, approver.ID)
				return approver != nil && approver.SourceDepartmentExternalIds == nil, err
			},
		},
		{
			name: "notification ids nil container",
			create: func() (bool, error) {
				approver, err := client.QuotaResetRequestNodeApprover.Create().
					SetRequestNodeID(approverNode.ID).SetUserID(notificationApprover.ID).SetSource("configured").
					SetNotificationIds(nil).Save(ctx)
				if err != nil {
					return false, err
				}
				approver, err = client.QuotaResetRequestNodeApprover.Get(ctx, approver.ID)
				return approver != nil && approver.NotificationIds == nil, err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			persistedInvalidValue, err := test.create()
			if err == nil {
				t.Fatalf("explicit nil value accepted; persisted invalid JSON = %t, want validation error", persistedInvalidValue)
			}
			if !ent.IsValidationError(err) {
				t.Fatalf("explicit nil error = %v, want validation error", err)
			}
		})
	}
}

func TestQuotaResetRequestSnapshotJSONFieldsRejectExplicitNil(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	newRequest := func(groupID string) *ent.QuotaResetRequestCreate {
		return client.QuotaResetRequest.Create().
			SetRequesterUserID(requester.ID).SetRequesterRelayUserID(1001).
			SetProviderID(provider.ID).SetGroupID(groupID).SetGroupName("Group Alpha").SetGroupPlatform("openai").
			SetReason("Request with explicit nil request snapshots").SetWorkflowVersion(2)
	}
	tests := []struct {
		name   string
		create func() (bool, error)
	}{
		{
			name: "requester department paths nil container",
			create: func() (bool, error) {
				request, err := newRequest("42").SetRequesterDepartmentPaths(nil).Save(ctx)
				if err != nil {
					return false, err
				}
				request, err = client.QuotaResetRequest.Get(ctx, request.ID)
				return request != nil && request.RequesterDepartmentPaths == nil, err
			},
		},
		{
			name: "requester notification ids nil container",
			create: func() (bool, error) {
				request, err := newRequest("43").SetRequesterNotificationIds(nil).Save(ctx)
				if err != nil {
					return false, err
				}
				request, err = client.QuotaResetRequest.Get(ctx, request.ID)
				return request != nil && request.RequesterNotificationIds == nil, err
			},
		},
		{
			name: "resolved approver user ids nil container",
			create: func() (bool, error) {
				request, err := newRequest("44").SetResolvedApproverUserIds(nil).Save(ctx)
				if err != nil {
					return false, err
				}
				request, err = client.QuotaResetRequest.Get(ctx, request.ID)
				return request != nil && request.ResolvedApproverUserIds == nil, err
			},
		},
		{
			name: "matched department paths nil container",
			create: func() (bool, error) {
				request, err := newRequest("45").SetMatchedDepartmentPaths(nil).Save(ctx)
				if err != nil {
					return false, err
				}
				request, err = client.QuotaResetRequest.Get(ctx, request.ID)
				return request != nil && request.MatchedDepartmentPaths == nil, err
			},
		},
		{
			name: "matched department paths nil element",
			create: func() (bool, error) {
				request, err := newRequest("46").SetMatchedDepartmentPaths([]map[string]any{nil}).Save(ctx)
				if err != nil {
					return false, err
				}
				request, err = client.QuotaResetRequest.Get(ctx, request.ID)
				return request != nil && len(request.MatchedDepartmentPaths) == 1 && request.MatchedDepartmentPaths[0] == nil, err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			persistedInvalidValue, err := test.create()
			if err == nil {
				t.Fatalf("explicit nil value accepted; persisted invalid JSON = %t, want validation error", persistedInvalidValue)
			}
			if !ent.IsValidationError(err) {
				t.Fatalf("explicit nil error = %v, want validation error", err)
			}
		})
	}
}

func TestWorkflowSnapshotJSONDefaultMapsAreIsolated(t *testing.T) {
	t.Run("requester notification ids", func(t *testing.T) {
		ctx := context.Background()
		client := testdb.Open(t)
		requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
		provider := createQuotaResetRelayProvider(t, ctx, client)
		createRequest := func(groupID string) *ent.QuotaResetRequest {
			return client.QuotaResetRequest.Create().
				SetRequesterUserID(requester.ID).SetRequesterRelayUserID(1001).
				SetProviderID(provider.ID).SetGroupID(groupID).SetGroupName("Group Alpha").SetGroupPlatform("openai").
				SetReason("Request with isolated default snapshots").SetWorkflowVersion(2).SaveX(ctx)
		}
		first := createRequest("42")
		if first.RequesterNotificationIds == nil {
			t.Fatal("first requester notification ids = nil, want non-null empty map")
		}
		first.RequesterNotificationIds["wecom"] = "leaked-wecom"
		second := createRequest("43")
		if second.RequesterNotificationIds == nil {
			t.Fatal("second requester notification ids = nil, want non-null empty map")
		}
		if got := second.RequesterNotificationIds["wecom"]; got != "" {
			t.Fatalf("second requester notification id = %q, want isolated empty default", got)
		}
	})

	t.Run("node approver notification ids", func(t *testing.T) {
		ctx := context.Background()
		client := testdb.Open(t)
		requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
		firstApprover := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
		secondApprover := createQuotaResetUser(t, ctx, client, "carol", "carol@example.net", nil, "user")
		provider := createQuotaResetRelayProvider(t, ctx, client)
		request := client.QuotaResetRequest.Create().
			SetRequesterUserID(requester.ID).SetRequesterRelayUserID(1001).
			SetProviderID(provider.ID).SetGroupID("42").SetGroupName("Group Alpha").SetGroupPlatform("openai").
			SetReason("Request with isolated approver defaults").SetWorkflowVersion(2).SaveX(ctx)
		node := client.QuotaResetRequestNode.Create().
			SetRequestID(request.ID).SetPosition(0).SetNodeType("requester_departments").SaveX(ctx)
		first := client.QuotaResetRequestNodeApprover.Create().
			SetRequestNodeID(node.ID).SetUserID(firstApprover.ID).SetSource("configured").SaveX(ctx)
		if first.NotificationIds == nil {
			t.Fatal("first node approver notification ids = nil, want non-null empty map")
		}
		first.NotificationIds["wecom"] = "leaked-wecom"
		second := client.QuotaResetRequestNodeApprover.Create().
			SetRequestNodeID(node.ID).SetUserID(secondApprover.ID).SetSource("configured").SaveX(ctx)
		if second.NotificationIds == nil {
			t.Fatal("second node approver notification ids = nil, want non-null empty map")
		}
		if got := second.NotificationIds["wecom"]; got != "" {
			t.Fatalf("second node approver notification id = %q, want isolated empty default", got)
		}
	})
}

func TestWorkflowSchemaRejectsMultipleActiveNodesPerRequest(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := client.QuotaResetRequest.Create().
		SetRequesterUserID(requester.ID).SetRequesterRelayUserID(1001).
		SetProviderID(provider.ID).SetGroupID("42").SetGroupName("Group Alpha").SetGroupPlatform("openai").
		SetReason("Request with one active workflow node").SetWorkflowVersion(2).SaveX(ctx)
	for position := 0; position < 2; position++ {
		client.QuotaResetRequestNode.Create().
			SetRequestID(request.ID).SetPosition(position).SetNodeType("configured_department").
			SetStatus(quotaresetrequestnode.StatusQueued).SaveX(ctx)
	}
	if got := client.QuotaResetRequestNode.Query().
		Where(
			quotaresetrequestnode.RequestIDEQ(request.ID),
			quotaresetrequestnode.StatusEQ(quotaresetrequestnode.StatusQueued),
		).
		CountX(ctx); got != 2 {
		t.Fatalf("queued node count = %d, want 2", got)
	}
	client.QuotaResetRequestNode.Create().
		SetRequestID(request.ID).SetPosition(2).SetNodeType("requester_departments").
		SetStatus(quotaresetrequestnode.StatusActive).SaveX(ctx)

	_, err := client.QuotaResetRequestNode.Create().
		SetRequestID(request.ID).SetPosition(3).SetNodeType("configured_department").
		SetStatus(quotaresetrequestnode.StatusActive).Save(ctx)
	if !ent.IsConstraintError(err) {
		t.Fatalf("second active node error = %v, want constraint error", err)
	}
}
