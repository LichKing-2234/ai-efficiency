# Multi-Stage Quota Reset Approval Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the single-step quota reset approval with an exact-department initial node, subscription-group-specific ordered department chains, reusable approvals, durable decision comments, and explicit preset webhook adapters with Enterprise WeChat mentions.

**Architecture:** Keep `backend/internal/quotareset` as the workflow owner and the relay provider as the only quota-reset boundary. Persist configured chains and immutable request-node, approver, and decision snapshots in normalized Ent tables; route legacy requests through the existing v1 behavior. Render notifications from a channel-neutral context through explicit `wecom_group_robot` and `generic_webhook` adapters, and expose backend-computed workflow permissions to the Vue UI.

**Tech Stack:** Go, Gin, Ent, PostgreSQL, Vue 3 `<script setup lang="ts">`, Pinia, Vite, Vitest, TailwindCSS, Python Playwright role checks.

**Status:** In progress

**Design:** [2026-07-10-multi-stage-quota-reset-approval-design.md](../specs/2026-07-10-multi-stage-quota-reset-approval-design.md)

---

## File Map

### Backend schemas and generated Ent code

- Create `backend/ent/schema/quota_reset_approval_chain.go`: one optional chain per provider and subscription group.
- Create `backend/ent/schema/quota_reset_approval_chain_node.go`: ordered configured department nodes.
- Create `backend/ent/schema/quota_reset_request_node.go`: immutable workflow-node snapshot and current state.
- Create `backend/ent/schema/quota_reset_request_node_approver.go`: immutable normal-approver and notification-identity snapshot.
- Create `backend/ent/schema/quota_reset_request_decision.go`: one durable manual decision per active node.
- Create `backend/ent/schema/quota_reset_json.go`: fresh JSON default factories and shared snapshot validators.
- Modify `backend/ent/schema/quota_reset_request.go`: workflow version, current-node pointer, completion decision, and requester identity snapshots.
- Modify `backend/ent/schema/quota_reset_request_event.go`: node lifecycle event types.
- Modify `backend/ent/schema/quota_reset_notification_setting.go`: explicit channel type, backfill marker, and template version.
- Regenerate `backend/ent/` with `go generate ./ent`.

### Backend quota reset module

- Create `backend/internal/quotareset/chain_config.go`: searchable approver candidates, chain options, validation, list, and atomic save.
- Create `backend/internal/quotareset/chain_config_test.go`: non-representative candidates, chain ordering, and reference guards.
- Create `backend/internal/quotareset/workflow_types.go`: v2 snapshots, summaries, permissions, notification ids, and stale-workflow error.
- Create `backend/internal/quotareset/workflow_resolver.go`: exact-department initial-node and configured-chain resolution.
- Create `backend/internal/quotareset/workflow_resolver_test.go`: resolution contract.
- Create `backend/internal/quotareset/workflow_service.go`: v2 create, approve, reject, cancel, approval reuse, and retry authorization.
- Create `backend/internal/quotareset/workflow_service_test.go`: state-machine and concurrency tests.
- Create `backend/internal/quotareset/workflow_summary.go`: viewer-aware request workflow summaries and list predicates.
- Create `backend/internal/quotareset/work_items.go`: v1/v2 actionable count queries owned by the workflow module.
- Create `backend/internal/quotareset/notification_channel.go`: neutral notification context and adapter registry.
- Create `backend/internal/quotareset/notification_generic.go`: versioned JSON renderer.
- Create `backend/internal/quotareset/notification_wecom.go`: Markdown renderer, mentions, escaping, and truncation.
- Create `backend/internal/quotareset/notification_backfill.go`: one-time explicit channel classification for existing settings.
- Modify `backend/internal/quotareset/service.go`: dispatch v1/v2 behavior and invoke the new bounded modules.
- Modify `backend/internal/quotareset/types.go`: admin configuration, workflow response, decision, and notification contracts.
- Modify `backend/internal/quotareset/errors.go`: `workflow_advanced` and typed latest-state error.
- Modify `backend/internal/quotareset/notification.go`: delivery only; remove runtime URL-shape format inference.
- Modify existing quota reset tests for v1 compatibility and v2 defaults.

### Backend API and startup

- Modify `backend/internal/handler/quota_reset.go`: node-aware decisions, chain APIs, candidate pagination, redacted notification settings, and typed errors.
- Modify `backend/internal/handler/quota_reset_test.go`: HTTP contracts and status mapping.
- Modify `backend/internal/handler/router.go`: chain and option routes.
- Modify `backend/internal/workitems/service.go` and tests: delegate quota counts to normalized workflow queries.
- Modify `backend/cmd/server/main.go`: run notification channel backfill after Ent migration.

### Frontend

- Modify `frontend/src/types/index.ts`: chain, node, decision, permissions, candidate pagination, and notification channel types.
- Modify `frontend/src/api/quotaReset.ts`: chain APIs, searchable candidates, node-aware decisions, and redacted notification settings.
- Modify `frontend/src/__tests__/quota-reset-api.test.ts`.
- Refactor `frontend/src/components/settings/QuotaResetApprovalSettings.vue` into a small orchestration parent.
- Create `frontend/src/components/settings/DepartmentApproverSettings.vue`.
- Create `frontend/src/components/settings/SubscriptionGroupApprovalChains.vue`.
- Create `frontend/src/components/settings/QuotaResetNotificationSettings.vue`.
- Modify `frontend/src/__tests__/quota-reset-approval-settings.test.ts` and `frontend/src/__tests__/settings-view.test.ts`.
- Create `frontend/src/components/quota-reset/QuotaResetWorkflowTimeline.vue`.
- Create `frontend/src/components/quota-reset/QuotaResetDecisionDialog.vue`.
- Modify `frontend/src/components/quota-reset/QuotaResetRequestList.vue` and `frontend/src/views/QuotaResetView.vue`.
- Modify `frontend/src/__tests__/quota-reset-view.test.ts`.
- Modify `frontend/src/i18n.ts` in English and Chinese.
- Create `frontend/e2e_quota_reset_workflow.py` for deterministic browser role and layout verification.

### Documentation

- Modify `docs/architecture.md` only after the code is current.
- Modify `docs/superpowers/specs/2026-07-10-multi-stage-quota-reset-approval-design.md` status to current implemented contract.
- Maintain this plan's checkboxes and status after every completed step.

## Acceptance Criteria Coverage

| Design acceptance criterion | Implementation tasks | Primary verification |
| --- | --- | --- |
| Exact direct-department first node, configured approver priority, same-department representative fallback | Tasks 2-4 | `TestWorkflowResolverUsesExactConfigWithoutWalkingParent`, `TestWorkflowResolverFallsBackToRepresentativeOfSameDepartment`, multi-department and empty-node resolver tests |
| Non-representative configured approvers | Tasks 2, 10 | Candidate/config service tests and settings picker tests |
| Ordered subscription-group department chains | Tasks 2, 3, 8, 10 | Atomic chain save/order tests, resolver tests, handler tests, and reorder UI test |
| One approval satisfies a node | Task 4 | State-machine approval tests and the one-decision-per-node constraint |
| Earlier approval satisfies every later node containing the actor | Tasks 4, 7, 11 | Non-contiguous reuse test, no-duplicate-notification test, and timeline attribution test |
| Non-empty durable comments for every v2 manual decision | Tasks 1, 4, 8, 11 | Ent constraint, service/handler validation, and decision-dialog tests |
| Admin acts only on the current node without bypassing the chain | Tasks 4, 5 | Admin override, requester self-approval, permissions, and stale-node tests |
| Existing v1 requests retain existing behavior | Tasks 4, 5, 8, 9, 11 | Legacy dispatch/count/list tests plus legacy frontend action tests |
| Notifications include requester, teams, group, reason, node, progress, prior decision, and action URL | Task 7 | Generic and Enterprise WeChat renderer assertions |
| Enterprise WeChat mentions current approvers from synchronized recipient ids | Tasks 2, 3, 7, 10 | Recipient-resolution, escaping, coverage-warning, and settings-preview tests |
| Queued, skipped, and prior-approval-satisfied nodes produce no duplicate notification | Tasks 4, 7 | Activation routing and approval-reuse delivery-count tests |
| Admin explicitly selects a preset channel type | Tasks 6, 8, 10 | Migration/validation, redacted HTTP contract, and settings tests |
| Work Items counts only actionable current work | Task 5 | v1/v2 approver, retry actor, admin, and deduplication count tests |
| Durable workflow, decision, reset, and notification audit facts | Tasks 1, 4, 7, 12 | Schema round trip, lifecycle-event assertions, notification metadata checks, and final architecture review |

---

### Task 1: Add Normalized Workflow Schemas

**Files:**
- Create: `backend/ent/schema/quota_reset_approval_chain.go`
- Create: `backend/ent/schema/quota_reset_approval_chain_node.go`
- Create: `backend/ent/schema/quota_reset_request_node.go`
- Create: `backend/ent/schema/quota_reset_request_node_approver.go`
- Create: `backend/ent/schema/quota_reset_request_decision.go`
- Create: `backend/ent/schema/quota_reset_json.go`
- Modify: `backend/ent/schema/quota_reset_request.go`
- Modify: `backend/ent/schema/quota_reset_request_event.go`
- Modify: `backend/ent/schema/quota_reset_notification_setting.go`
- Create: `backend/internal/quotareset/schema_test.go`
- Modify: `backend/internal/quotareset/service.go` and its test fixture so an absent legacy approver snapshot uses the schema default.
- Modify: `backend/internal/workitems/service_test.go` so an absent legacy approver snapshot uses the schema default.
- Generated: `backend/ent/*`

- [x] **Step 1: Write the failing schema round-trip test**

Create `backend/internal/quotareset/schema_test.go`:

```go
package quotareset

import (
	"context"
	"testing"

	"github.com/ai-efficiency/backend/ent/quotaresetrequest"
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
	if approvedNode.Status != quotaresetrequestnode.StatusApproved || completedRequest.CurrentNodeID != nil ||
		completedRequest.WorkflowCompletedByDecisionID == nil ||
		*completedRequest.WorkflowCompletedByDecisionID != decision.ID ||
		completedRequest.Status != quotaresetrequest.StatusApprovedResetSucceeded {
		t.Fatal("workflow did not persist a valid completed state")
	}
}
```

- [x] **Step 2: Run the schema test and verify it fails before generation**

Run:

```bash
cd backend && go test ./internal/quotareset -run TestWorkflowSchemasRoundTrip -count=1
```

Expected: FAIL to compile because the new Ent entities and setters do not exist.

- [x] **Step 3: Add the five new Ent schemas**

Create `backend/ent/schema/quota_reset_approval_chain.go`:

```go
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type QuotaResetApprovalChain struct{ ent.Schema }

func (QuotaResetApprovalChain) Fields() []ent.Field {
	return []ent.Field{
		field.Int("provider_id"),
		field.String("group_id").NotEmpty(),
		field.String("group_name").Default(""),
		field.Bool("enabled").Default(true),
		field.Int("created_by_user_id").Default(0),
		field.Int("updated_by_user_id").Default(0),
		field.Time("created_at").Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (QuotaResetApprovalChain) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("provider_id", "group_id").Unique(),
		index.Fields("enabled", "updated_at"),
	}
}
```

Create `backend/ent/schema/quota_reset_approval_chain_node.go`:

```go
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type QuotaResetApprovalChainNode struct{ ent.Schema }

func (QuotaResetApprovalChainNode) Fields() []ent.Field {
	return []ent.Field{
		field.Int("chain_id"),
		field.Int("position").NonNegative(),
		field.Int("directory_source_id"),
		field.String("department_external_id").NotEmpty(),
		field.String("department_display_path").Default(""),
		field.Time("created_at").Default(timeNow),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (QuotaResetApprovalChainNode) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("chain_id", "position").Unique(),
		index.Fields("chain_id", "department_external_id").Unique(),
		index.Fields("directory_source_id", "department_external_id"),
	}
}
```

Create `backend/ent/schema/quota_reset_json.go` for the JSON builders in this
task. Ent's JSON builder accepts a factory through `Default(factory)`:

```go
package schema

import (
	"errors"

	"entgo.io/ent"
)

func validatedQuotaResetJSONField[T any](jsonField ent.Field, validator func(T) error) ent.Field {
	descriptor := jsonField.Descriptor()
	descriptor.Validators = append(descriptor.Validators, validator)
	return jsonField
}

func newQuotaResetSlice[T any]() []T {
	return []T{}
}

func newQuotaResetMap[K comparable, V any]() map[K]V {
	return map[K]V{}
}

func validateQuotaResetSlice[T any](values []T) error {
	if values == nil {
		return errors.New("JSON snapshot container must not be nil")
	}
	return nil
}

func validateQuotaResetMap[K comparable, V any](values map[K]V) error {
	if values == nil {
		return errors.New("JSON snapshot container must not be nil")
	}
	return nil
}

func validateQuotaResetMapSlice(values []map[string]any) error {
	if err := validateQuotaResetSlice(values); err != nil {
		return err
	}
	for _, value := range values {
		if value == nil {
			return errors.New("JSON snapshot must not contain nil map elements")
		}
	}
	return nil
}
```

Create `backend/ent/schema/quota_reset_request_node.go`:

```go
package schema

import (
	"entgo.io/ent"
	entsql "entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type QuotaResetRequestNode struct{ ent.Schema }

func (QuotaResetRequestNode) Fields() []ent.Field {
	return []ent.Field{
		field.Int("request_id").Immutable(),
		field.Int("position").NonNegative().Immutable(),
		field.Enum("node_type").Values("requester_departments", "configured_department").Immutable(),
		field.String("label").Default("").Immutable(),
		validatedQuotaResetJSONField(
			field.JSON("department_snapshots", []map[string]any{}).Default(newQuotaResetSlice[map[string]any]).Immutable(),
			validateQuotaResetMapSlice,
		),
		field.Enum("status").Values("queued", "active", "approved", "satisfied_by_prior_approval", "skipped_no_approver", "rejected").Default("queued"),
		field.Bool("admin_fallback_required").Default(false).Immutable(),
		field.Int("satisfied_by_decision_id").Optional().Nillable(),
		field.Time("activated_at").Optional().Nillable(),
		field.Time("completed_at").Optional().Nillable(),
		field.Time("created_at").Default(timeNow).Immutable(),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (QuotaResetRequestNode) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("request_id", "position").Unique(),
		index.Fields("request_id").
			Unique().
			Annotations(entsql.IndexWhere("status = 'active'")),
		index.Fields("request_id", "status"),
		index.Fields("status", "activated_at"),
	}
}
```

Create `backend/ent/schema/quota_reset_request_node_approver.go`:

```go
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type QuotaResetRequestNodeApprover struct{ ent.Schema }

func (QuotaResetRequestNodeApprover) Fields() []ent.Field {
	return []ent.Field{
		field.Int("request_node_id").Immutable(),
		field.Int("user_id").Immutable(),
		field.String("display_name").Default("").Immutable(),
		field.String("email").Default("").Immutable(),
		field.Enum("source").Values("configured", "directory_representative").Immutable(),
		validatedQuotaResetJSONField(
			field.JSON("source_department_external_ids", []string{}).Default(newQuotaResetSlice[string]).Immutable(),
			validateQuotaResetSlice[string],
		),
		validatedQuotaResetJSONField(
			field.JSON("notification_ids", map[string]string{}).Default(newQuotaResetMap[string, string]).Immutable(),
			validateQuotaResetMap[string, string],
		),
		field.Time("created_at").Default(timeNow).Immutable(),
	}
}

func (QuotaResetRequestNodeApprover) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("request_node_id", "user_id").Unique(),
		index.Fields("user_id", "request_node_id"),
	}
}
```

Create `backend/ent/schema/quota_reset_request_decision.go`:

```go
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type QuotaResetRequestDecision struct{ ent.Schema }

func (QuotaResetRequestDecision) Fields() []ent.Field {
	return []ent.Field{
		field.Int("request_id").Immutable(),
		field.Int("request_node_id").Immutable(),
		field.Int("actor_user_id").Immutable(),
		field.String("actor_display_name").Default("").Immutable(),
		field.Enum("decision").Values("approve", "reject").Immutable(),
		field.String("comment").NotEmpty().Immutable(),
		field.Bool("admin_override").Default(false).Immutable(),
		field.Time("created_at").Default(timeNow).Immutable(),
	}
}

func (QuotaResetRequestDecision) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("request_node_id").Unique(),
		index.Fields("request_id", "created_at"),
		index.Fields("actor_user_id", "created_at"),
	}
}
```

- [x] **Step 4: Extend existing request, event, and notification schemas**

Mark every request creation fact and snapshot immutable. Ent's JSON builder uses
`Default(factory)` to create a fresh non-nil value for each create:

```go
field.Int("requester_user_id").Immutable(),
field.Int64("requester_relay_user_id").Immutable(),
field.Int("provider_id").Immutable(),
field.String("group_id").NotEmpty().Immutable(),
field.String("group_name").Default("").Immutable(),
field.String("group_platform").Default("").Immutable(),
field.String("reason").NotEmpty().Immutable(),
field.Int("workflow_version").Default(1).Immutable(),
field.Int("current_node_id").Optional().Nillable(),
field.Int("workflow_completed_by_decision_id").Optional().Nillable(),
field.String("requester_display_name_snapshot").Default("").Immutable(),
field.String("requester_email_snapshot").Default("").Immutable(),
validatedQuotaResetJSONField(
	field.JSON("requester_department_paths", []string{}).Default(newQuotaResetSlice[string]).Optional().Immutable(),
	validateQuotaResetSlice[string],
),
validatedQuotaResetJSONField(
	field.JSON("requester_notification_ids", map[string]string{}).Default(newQuotaResetMap[string, string]).Optional().Immutable(),
	validateQuotaResetMap[string, string],
),
validatedQuotaResetJSONField(
	field.JSON("resolved_approver_user_ids", []int{}).Default(newQuotaResetSlice[int]).Optional().Immutable(),
	validateQuotaResetSlice[int],
),
validatedQuotaResetJSONField(
	field.JSON("matched_department_paths", []map[string]any{}).Default(newQuotaResetSlice[map[string]any]).Optional().Immutable(),
	validateQuotaResetMapSlice,
),
field.Time("created_at").Default(timeNow).Immutable(),
```

Keep `status`, `current_node_id`, `workflow_completed_by_decision_id`, the v1
approval/rejection/reset/decision state, and `updated_at` mutable. The four
request JSON snapshots remain SQL-nullable for historical v1 rows, but every new
create gets a fresh non-nil application default. Explicit nil setters fail schema
validation; slices of maps also reject nil elements.

Add a request schema hook that blocks Ent's `Optional().Immutable()` mutation
clear bypass on both update operations:

```go
func (QuotaResetRequest) Hooks() []ent.Hook {
	return []ent.Hook{
		func(next ent.Mutator) ent.Mutator {
			return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
				for _, fieldName := range [...]string{
					"requester_department_paths",
					"requester_notification_ids",
					"resolved_approver_user_ids",
					"matched_department_paths",
				} {
					if mutation.FieldCleared(fieldName) {
						return nil, errQuotaResetRequestJSONSnapshotClear
					}
				}
				return next.Mutate(ctx, mutation)
			})
		},
	}
}
```

Add request indexes:

```go
index.Fields("workflow_version", "status", "created_at"),
index.Fields("current_node_id"),
```

Append event types:

```go
"workflow_snapshotted",
"node_activated",
"node_approved",
"node_satisfied_by_prior_approval",
"node_skipped_no_approver",
"admin_fallback_activated",
```

Add notification setting fields after `enabled`:

```go
field.Enum("channel_type").Values("generic_webhook", "wecom_group_robot").Default("generic_webhook"),
field.Bool("channel_type_configured").Default(false),
field.Int("template_version").Default(1),
```

`channel_type_configured` distinguishes auto-migrated existing rows from explicit
new admin choices.

- [x] **Step 5: Generate Ent code and run schema-sensitive tests**

Run:

```bash
cd backend && go generate ./ent
cd backend && go test ./internal/quotareset -run TestWorkflowSchemasRoundTrip -count=1
cd backend && go test ./internal/quotareset ./internal/workitems ./internal/testdb -count=1
```

Expected: PASS.

- [x] **Step 6: Commit the schema unit**

```bash
git add backend/ent backend/internal/quotareset/schema_test.go
git commit -m "feat(backend): add quota reset workflow schemas"
```

**Review follow-up quality evidence (2026-07-10):**

- [x] RED proves the pre-fix schema permits two active nodes for one request.
- [x] Structural node snapshots and all approver/decision snapshot fields are immutable in generated update APIs.
- [x] PostgreSQL enforces at most one active node per request while retaining the non-unique query indexes.
- [x] Durable tests cover legacy request defaults, existing notification defaults, new lifecycle events, and valid completed state.
- [x] An independent reviewer completed a PostgreSQL base-to-head migration with representative v1 rows; no hand-written old-DDL fixture was added.
- [x] Focused, package, full-backend, generation reproducibility, and diff checks pass.
- [x] Follow-up fixes are committed separately from `b60cdf9`.

**Final JSON immutability follow-up evidence (2026-07-13):**

- [x] RED proves optional immutable JSON snapshots still expose public `Clear*` mutation methods and can read back as null.
- [x] The three JSON snapshot fields are non-null, explicitly defaulted, immutable, and expose no public clear mutation methods.
- [x] Durable tests cover multiple queued nodes, the active-node uniqueness constraint, and all six new lifecycle event values.
- [x] Focused, package, full-backend, vet, generation reproducibility, and diff checks pass.
- [x] Final quality fixes are committed separately from `55e19bf`.

**Explicit nil validation follow-up evidence (2026-07-13):**

- [x] RED proves all three explicit-nil setters persist JSON null; department snapshots also accept a nil element map.
- [x] Ent schema validators reject nil containers and nil department-snapshot elements while allowing empty non-nil values.
- [x] Omitted setters still persist non-null empty `[]` and `{}` defaults.
- [x] Focused, package, full-backend, vet, generation reproducibility, and diff checks pass.
- [x] Explicit-nil invariant fixes are committed separately from `6c8d5b9`.

**Comprehensive request snapshot invariant follow-up evidence (2026-07-13):**

- [x] No handwritten request update mutates a creation fact or snapshot.
- [x] RED proves request creation setters remain effective, request JSON accepts explicit nil, and static notification-map defaults leak or are absent.
- [x] All request creation facts are immutable while workflow and v1 decision/reset state remains mutable.
- [x] Every request, node, and node-approver JSON snapshot uses a fresh default factory and schema-level nil validation.
- [x] Legacy v1 builders omit an absent approver snapshot so the schema factory supplies `[]`; direct explicit-nil setters remain invalid.
- [x] The completion fixture establishes and asserts a current node before clearing it.
- [x] Focused, package, full-backend, vet, generation reproducibility, and diff checks pass.
- [x] Request snapshot invariant fixes are committed separately from `402badc`.

**Legacy request JSON migration correction evidence (2026-07-13):**

- [x] First RED proves the non-optional generated mutation types omit all four `Clear*` paths; the generated migration schema simultaneously marked those columns non-null.
- [x] Second RED proves `Optional().Immutable()` exposes direct mutation `Clear*` paths on `Update` and `UpdateOne`, and both paths persist SQL `NULL` before the hook.
- [x] The request schema hook returns `quotaresetrequest: JSON creation snapshots cannot be cleared` for both update operations and leaves all stored snapshots unchanged.
- [x] Real PostgreSQL migration ran from exact pre-Task-1 commit `0c7d0be6a0764f391ce3c5fcf24c5f469855bf82` to the generated final head in isolated schema `quota_probe_sxp4aa` on `127.0.0.1:15432`.
- [x] The representative v1 request retained SQL `NULL` in `resolved_approver_user_ids`, `matched_department_paths`, and both newly added requester JSON columns; it read back with `workflow_version=1`.
- [x] The representative v1 `created` event remained readable, and the notification row migrated to `generic_webhook`, `channel_type_configured=false`, `template_version=1`.
- [x] A new Ent-created request received four non-nil empty defaults and stored no SQL `NULL`; the isolated PostgreSQL schema was dropped afterward.
- [x] Focused schema tests pass.
- [x] Package, full-backend, vet, generation reproducibility, and diff checks pass.
- [x] Migration correction is committed separately from `ddbaaf2` as `7a40383`.

**Request JSON `SetOp` bypass follow-up evidence (2026-07-13):**

- [x] RED proves `Update` and `UpdateOne` direct mutations can set each clear flag, relabel the mutation to `OpCreate` or `OpDelete`, and persist SQL `NULL` through the operation-gated hook.
- [x] Clear-flag rejection is unconditional and independent of `Mutation.Op`; every field/op/update combination returns the deterministic immutable-snapshot error and preserves stored values.
- [x] Focused schema tests preserve omitted fresh defaults, explicit-nil validation, scalar immutability, and normal mutable request state transitions.
- [x] Package, full-backend, vet, generation reproducibility, and diff checks pass.
- [x] `SetOp` bypass fix is committed separately from `2dcb6e7` as `f8d3c15`.

---

### Task 2: Broaden Department Approvers and Persist Subscription Chains

**Files:**
- Create: `backend/internal/quotareset/chain_config.go`
- Create: `backend/internal/quotareset/chain_config_test.go`
- Modify: `backend/internal/quotareset/types.go`
- Modify: `backend/internal/quotareset/errors.go`
- Modify: `backend/internal/quotareset/service.go`
- Modify: `backend/internal/quotareset/service_test.go`

- [ ] **Step 1: Write failing candidate and chain tests**

Create tests named:

```go
func TestListApproverCandidatesIncludesMatchedNonRepresentative(t *testing.T)
func TestListApproverCandidatesExcludesInactiveMembers(t *testing.T)
func TestSaveApprovalChainsPreservesOrder(t *testing.T)
func TestSaveApprovalChainsRejectsDuplicateDepartment(t *testing.T)
func TestSaveApprovalChainsRejectsDepartmentWithoutConfig(t *testing.T)
func TestSaveApproverConfigsRejectsRemovingChainReferencedDepartment(t *testing.T)
```

The non-representative assertion must create a directory-matched local peer with
no representative metadata and expect that user in the response:

```go
resp, err := svc.ListApproverCandidates(ctx, ApproverCandidateParams{
	SourceID: source.ID,
	Query: "Alice",
	Page: 1,
	PageSize: 20,
})
if err != nil {
	t.Fatalf("ListApproverCandidates() error = %v", err)
}
if len(resp.Items) != 1 || resp.Items[0].UserID != peer.ID || !resp.Items[0].WeComMentionAvailable {
	t.Fatalf("candidates = %#v, want non-representative peer with mention coverage", resp.Items)
}
```

The ordered-chain assertion must save Beta then Alpha and verify the same order
from `ListApprovalChains`. The duplicate and missing-config cases must assert
`ErrInvalidApproverConfig`. The destructive approver save case must assert
`ErrApproverConfigReferenced`.

Extend `fakeQuotaResetProvider`:

```go
groups []relay.Group

func (f *fakeQuotaResetProvider) ListPlatformGroups(context.Context) ([]relay.Group, error) {
	return append([]relay.Group(nil), f.groups...), nil
}
```

- [ ] **Step 2: Run focused tests and verify failure**

Run:

```bash
cd backend && go test ./internal/quotareset -run 'Test(ListApproverCandidatesIncludesMatchedNonRepresentative|ListApproverCandidatesExcludesInactiveMembers|SaveApprovalChains|SaveApproverConfigsRejectsRemoving)' -count=1
```

Expected: FAIL because the contracts and methods do not exist.

- [ ] **Step 3: Add chain and candidate contracts**

Add to `types.go`:

```go
type ApproverCandidateParams struct {
	SourceID int
	Query string
	Page int
	PageSize int
}

type ApproverCandidate struct {
	UserID int `json:"user_id"`
	Username string `json:"username"`
	Email string `json:"email"`
	DisplayName string `json:"display_name"`
	DirectoryMemberExternalID string `json:"directory_member_external_id"`
	DepartmentPaths []string `json:"department_paths"`
	WeComMentionAvailable bool `json:"wecom_mention_available"`
}

type ApproverCandidateListResponse struct {
	Items []ApproverCandidate `json:"items"`
	Page int `json:"page"`
	PageSize int `json:"page_size"`
	Total int `json:"total"`
}

type ApprovalChainNodeInput struct {
	DirectorySourceID int `json:"directory_source_id"`
	DepartmentExternalID string `json:"department_external_id"`
	DepartmentDisplayPath string `json:"department_display_path"`
}

type ApprovalChainInput struct {
	ProviderID int `json:"provider_id"`
	GroupID string `json:"group_id"`
	GroupName string `json:"group_name"`
	Enabled bool `json:"enabled"`
	Nodes []ApprovalChainNodeInput `json:"nodes"`
}

type SaveApprovalChainsInput struct {
	ActorUserID int
	Items []ApprovalChainInput
}

type ApprovalChain struct {
	ID int `json:"id"`
	ProviderID int `json:"provider_id"`
	GroupID string `json:"group_id"`
	GroupName string `json:"group_name"`
	Enabled bool `json:"enabled"`
	Nodes []ApprovalChainNodeInput `json:"nodes"`
}

type ApprovalChainListResponse struct {
	Items []ApprovalChain `json:"items"`
}

type ApprovalChainGroupOption struct {
	ProviderID int `json:"provider_id"`
	GroupID string `json:"group_id"`
	GroupName string `json:"group_name"`
	Platform string `json:"platform"`
}

type ApprovalChainDepartmentOption struct {
	DirectorySourceID int `json:"directory_source_id"`
	DepartmentExternalID string `json:"department_external_id"`
	DepartmentDisplayPath string `json:"department_display_path"`
	ApproverCount int `json:"approver_count"`
}

type ApprovalChainOptionsResponse struct {
	Groups []ApprovalChainGroupOption `json:"groups"`
	Departments []ApprovalChainDepartmentOption `json:"departments"`
}
```

Add to `errors.go`:

```go
ErrApproverConfigReferenced = errors.New("approver_config_referenced")
```

- [ ] **Step 4: Implement bounded candidate lookup**

Create `chain_config.go`. `ListApproverCandidates` must:

1. Resolve the current source when `SourceID` is zero.
2. Load current members, memberships, departments, and local users. Exclude a
   directory member whose status is not `active`, and exclude a local user with
   `relay_disabled_at` set.
3. Match by `matched_user_id`, then normalized email.
4. Search display name, username, and email.
5. Return every current name-based department path for each candidate.
6. Deduplicate by local user id, sort deterministically, then paginate.
7. Return WeCom mention coverage only when the allowlisted
   `metadata.wecom_userid` value or the current production-compatible
   `member.external_id` fallback is non-empty.

Define the helper contracts in the same file so the service call above does not
depend on implicit shapes:

```go
func resolveCandidateSourceID(ctx context.Context, client *ent.Client, requested int) (int, error)
func loadCandidateOrganizationFacts(ctx context.Context, client *ent.Client, sourceID int) ([]*ent.DirectoryMemberDepartment, map[string]*ent.DirectoryDepartment, error)
func matchApproverCandidates(query string, members []*ent.DirectoryMember, memberships []*ent.DirectoryMemberDepartment, departments map[string]*ent.DirectoryDepartment, users []*ent.User) []ApproverCandidate
```

Use:

```go
func (s *Service) ListApproverCandidates(ctx context.Context, params ApproverCandidateParams) (*ApproverCandidateListResponse, error) {
	page, pageSize := normalizePage(params.Page, params.PageSize)
	sourceID, err := resolveCandidateSourceID(ctx, s.client, params.SourceID)
	if err != nil {
		return nil, err
	}
	members, err := s.client.DirectoryMember.Query().
		Where(directorymember.SourceIDEQ(sourceID)).
		Order(ent.Asc(directorymember.FieldDisplayName), ent.Asc(directorymember.FieldEmailNormalized)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list approver candidate members: %w", err)
	}
	users, err := s.client.User.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list approver candidate users: %w", err)
	}
	memberships, departments, err := loadCandidateOrganizationFacts(ctx, s.client, sourceID)
	if err != nil {
		return nil, err
	}
	items := matchApproverCandidates(params.Query, members, memberships, departments, users)
	total := len(items)
	start := min((page-1)*pageSize, total)
	end := min(start+pageSize, total)
	return &ApproverCandidateListResponse{Items: items[start:end], Page: page, PageSize: pageSize, Total: total}, nil
}
```

Implement `matchApproverCandidates` as a pure helper and test it directly. Do
not reuse the representative-only `approverCandidateUserIDsByDepartment`.

Replace `validateApproverConfigs` with current-source department existence plus
candidate-user membership validation. An approver does not need to belong to or
represent the selected department.

- [ ] **Step 5: Implement chain list, options, atomic save, and reference guard**

Use a local optional relay interface:

```go
type platformGroupLister interface {
	ListPlatformGroups(context.Context) ([]relay.Group, error)
}
```

`ListApprovalChainOptions` must resolve enabled providers, keep only groups with
`strings.EqualFold(group.SubscriptionType, "subscription")`, convert
`group.ID` with `strconv.FormatInt`, and return only current departments with
enabled approver configs.

`SaveApprovalChains` must validate the complete input before opening the
transaction:

```go
seenGroups := map[string]struct{}{}
for _, item := range input.Items {
	groupKey := fmt.Sprintf("%d/%s", item.ProviderID, strings.TrimSpace(item.GroupID))
	if _, ok := allowedGroups[groupKey]; !ok {
		return nil, fmt.Errorf("%w: unknown subscription group %s", ErrInvalidApproverConfig, groupKey)
	}
	if _, duplicate := seenGroups[groupKey]; duplicate {
		return nil, fmt.Errorf("%w: duplicate subscription group %s", ErrInvalidApproverConfig, groupKey)
	}
	seenGroups[groupKey] = struct{}{}
	seenDepartments := map[string]struct{}{}
	for _, node := range item.Nodes {
		nodeKey := fmt.Sprintf("%d/%s", node.DirectorySourceID, strings.TrimSpace(node.DepartmentExternalID))
		if _, ok := allowedDepartments[nodeKey]; !ok {
			return nil, fmt.Errorf("%w: department %s has no enabled approver config", ErrInvalidApproverConfig, nodeKey)
		}
		if _, duplicate := seenDepartments[nodeKey]; duplicate {
			return nil, fmt.Errorf("%w: duplicate department %s", ErrInvalidApproverConfig, nodeKey)
		}
		seenDepartments[nodeKey] = struct{}{}
	}
}
```

Delete and recreate chains and nodes in one transaction, preserving input order.
Before `SaveApproverConfigs` deletes rows, project the complete post-save state.
For `replace_all`, that state is the submitted list. For
`replace_departments`, load existing rows, replace only the departments present
in the request, and retain untouched departments. Compare that final state with
enabled chain-node references:

```go
func (s *Service) validateChainReferencesAfterApproverSave(ctx context.Context, sourceID int, finalItems []ApproverConfigInput) error {
	remaining := map[string]struct{}{}
	for _, item := range finalItems {
		if item.Enabled {
			remaining[strings.TrimSpace(item.DepartmentExternalID)] = struct{}{}
		}
	}
	nodes, err := s.client.QuotaResetApprovalChainNode.Query().
		Where(quotaresetapprovalchainnode.DirectorySourceIDEQ(sourceID)).
		All(ctx)
	if err != nil {
		return fmt.Errorf("list approval chain references: %w", err)
	}
	for _, node := range nodes {
		if _, ok := remaining[node.DepartmentExternalID]; !ok {
			return fmt.Errorf("%w: department %s is used by an approval chain", ErrApproverConfigReferenced, node.DepartmentDisplayPath)
		}
	}
	return nil
}
```

- [ ] **Step 6: Run focused and full quota reset tests**

Run:

```bash
cd backend && go test ./internal/quotareset -run 'Test(ListApproverCandidatesIncludesMatchedNonRepresentative|ListApproverCandidatesExcludesInactiveMembers|SaveApprovalChains|SaveApproverConfigsRejectsRemoving)' -count=1
cd backend && go test ./internal/quotareset -count=1
```

Expected: PASS. Update representative-only candidate tests to assert the new
directory-matched-user contract while keeping representative resolver tests.

- [ ] **Step 7: Commit chain configuration**

```bash
git add backend/internal/quotareset
git commit -m "feat(backend): configure quota reset approval chains"
```

---

### Task 3: Resolve Immutable Multi-Stage Workflow Snapshots

**Files:**
- Create: `backend/internal/quotareset/workflow_types.go`
- Create: `backend/internal/quotareset/workflow_resolver.go`
- Create: `backend/internal/quotareset/workflow_resolver_test.go`
- Modify: `backend/internal/quotareset/service.go`

- [ ] **Step 1: Write failing resolution tests**

Create these tests in `workflow_resolver_test.go`:

```go
func TestWorkflowResolverUsesExactConfigWithoutWalkingParent(t *testing.T)
func TestWorkflowResolverFallsBackToRepresentativeOfSameDepartment(t *testing.T)
func TestWorkflowResolverMergesConfiguredAndRepresentativeDepartments(t *testing.T)
func TestWorkflowResolverUsesMembershipRowsBeforePrimaryCompatibilityFallback(t *testing.T)
func TestWorkflowResolverSkipsEmptyInitialNode(t *testing.T)
func TestWorkflowResolverConfiguredChainNeverFallsBackToRepresentative(t *testing.T)
func TestWorkflowResolverExcludesRequesterFromEveryNode(t *testing.T)
func TestWorkflowResolverExcludesInactiveConfiguredAndRepresentativeUsers(t *testing.T)
```

The parent-walk regression must configure only the parent, configure the child
organization representative, and assert the child representative is selected.
The mixed multi-department test must configure Alpha, leave Beta unconfigured,
and assert Alpha's configured user plus Beta's representative in one initial
node. The configured-chain test must leave a later department without config,
give it representative metadata, and assert zero candidates plus
`AdminFallbackRequired=true`.

- [ ] **Step 2: Run resolution tests and verify failure**

Run:

```bash
cd backend && go test ./internal/quotareset -run TestWorkflowResolver -count=1
```

Expected: FAIL because the v2 resolver does not exist.

- [ ] **Step 3: Define workflow snapshot and response types**

Create `workflow_types.go`:

```go
package quotareset

import "time"

const WorkflowVersionV2 = 2

type RequesterIdentitySnapshot struct {
	DisplayName string
	Email string
	DepartmentPaths []string
	NotificationIDs map[string]string
}

type DepartmentSnapshot struct {
	ExternalID string `json:"external_id"`
	DisplayPath string `json:"display_path"`
	Resolution string `json:"resolution"`
}

type ResolvedNodeApprover struct {
	UserID int
	DisplayName string
	Email string
	Source string
	SourceDepartmentExternalIDs []string
	NotificationIDs map[string]string
}

type ResolvedWorkflowNode struct {
	Position int
	NodeType string
	Label string
	Departments []DepartmentSnapshot
	Approvers []ResolvedNodeApprover
	InitialStatus string
	AdminFallbackRequired bool
}

type WorkflowSnapshot struct {
	Requester RequesterIdentitySnapshot
	Nodes []ResolvedWorkflowNode
}

type WorkflowNodeApproverSummary struct {
	UserID int `json:"user_id"`
	DisplayName string `json:"display_name"`
	Email string `json:"email"`
	Source string `json:"source"`
}

type WorkflowDecisionSummary struct {
	ID int `json:"id"`
	NodeID int `json:"node_id"`
	ActorUserID int `json:"actor_user_id"`
	ActorDisplayName string `json:"actor_display_name"`
	Decision string `json:"decision"`
	Comment string `json:"comment"`
	AdminOverride bool `json:"admin_override"`
	CreatedAt time.Time `json:"created_at"`
}

type WorkflowNodeSummary struct {
	ID int `json:"id"`
	Position int `json:"position"`
	NodeType string `json:"node_type"`
	Label string `json:"label"`
	Departments []DepartmentSnapshot `json:"departments"`
	Status string `json:"status"`
	AdminFallbackRequired bool `json:"admin_fallback_required"`
	Approvers []WorkflowNodeApproverSummary `json:"approvers"`
	SatisfiedByDecisionID *int `json:"satisfied_by_decision_id,omitempty"`
}

type WorkflowSummary struct {
	Version int `json:"version"`
	CurrentNode *WorkflowNodeSummary `json:"current_node,omitempty"`
	Nodes []WorkflowNodeSummary `json:"nodes"`
	Decisions []WorkflowDecisionSummary `json:"decisions"`
	CanApprove bool `json:"can_approve"`
	CanReject bool `json:"can_reject"`
	CanCancel bool `json:"can_cancel"`
	CanRetry bool `json:"can_retry"`
}
```

- [ ] **Step 4: Implement the exact-department resolver**

Create `workflow_resolver.go` with:

```go
type WorkflowResolver struct{ client *ent.Client }

func NewWorkflowResolver(client *ent.Client) *WorkflowResolver {
	return &WorkflowResolver{client: client}
}

func (r *WorkflowResolver) Resolve(ctx context.Context, requesterUserID, providerID int, groupID string) (*WorkflowSnapshot, error) {
	sourceID, ok, err := directorysync.CurrentSourceID(ctx, r.client)
	if err != nil {
		return nil, fmt.Errorf("resolve current directory source: %w", err)
	}
	if !ok {
		return nil, ErrDirectoryUnavailable
	}
	facts, err := r.loadWorkflowDirectoryFacts(ctx, sourceID, requesterUserID)
	if err != nil {
		return nil, err
	}
	initial, err := r.resolveInitialNode(ctx, facts)
	if err != nil {
		return nil, err
	}
	configured, err := r.resolveConfiguredNodes(ctx, facts, providerID, strings.TrimSpace(groupID))
	if err != nil {
		return nil, err
	}
	nodes := []ResolvedWorkflowNode{initial}
	for i := range configured {
		configured[i].Position = i + 1
		nodes = append(nodes, configured[i])
	}
	return &WorkflowSnapshot{
		Requester: RequesterIdentitySnapshot{
			DisplayName: firstNonEmptyQuotaReset(requesterMemberDisplayName(facts.requesterMember), facts.requester.Username),
			Email: facts.requester.Email,
			DepartmentPaths: facts.departmentPaths,
			NotificationIDs: notificationIDsForMember(facts.requesterMember),
		},
		Nodes: nodes,
	}, nil
}
```

`loadWorkflowDirectoryFacts` must load current departments, members,
memberships, users, exact approver configs, representative metadata, and the
requester match once. It must return an empty membership list, not an error, for
a requester unmatched in a valid snapshot. Both configured and representative
candidate helpers must exclude inactive directory members, offboarded local
users, and the requester.

Do not reuse the v1 `requesterDepartmentIDs` helper because it always unions the
legacy primary field. Add the v2 helper below: relation rows are authoritative,
and `member.department_external_id` is used only when no relation row exists.

```go
func workflowRequesterDepartmentIDs(member *ent.DirectoryMember, memberships []*ent.DirectoryMemberDepartment) []string {
	if member == nil {
		return []string{}
	}
	ids := make([]string, 0)
	for _, membership := range memberships {
		if membership != nil && membership.DirectoryMemberID == member.ID {
			ids = appendUniqueSortedString(ids, membership.DepartmentExternalID)
		}
	}
	if len(ids) == 0 {
		ids = appendUniqueSortedString(ids, member.DepartmentExternalID)
	}
	return ids
}
```

Define the helper in this file; do not change the legacy resolver's helper:

```go
func appendUniqueSortedString(values []string, candidate string) []string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return values
	}
	for _, value := range values {
		if value == candidate {
			return values
		}
	}
	values = append(values, candidate)
	sort.Strings(values)
	return values
}
```

`loadWorkflowDirectoryFacts` sets `facts.requesterDepartmentIDs` from
`workflowRequesterDepartmentIDs(facts.requesterMember, memberships)` and derives
display paths from exactly that returned list.

Implement each direct department independently:

```go
initial := ResolvedWorkflowNode{
	Position: 0,
	NodeType: "requester_departments",
	Label: "Requester departments",
	InitialStatus: "queued",
}
for _, departmentID := range facts.requesterDepartmentIDs {
	configured := usableConfiguredApprovers(facts.configsByDepartment[departmentID], facts, facts.requester.ID)
	resolution := "configured"
	candidates := configured
	if len(candidates) == 0 {
		resolution = "directory_representative"
		candidates = usableRepresentativeApprovers(facts.representatives[departmentID], facts, facts.requester.ID)
	}
	initial.Departments = append(initial.Departments, DepartmentSnapshot{
		ExternalID: departmentID,
		DisplayPath: facts.tree.DisplayPath(departmentID),
		Resolution: resolution,
	})
	mergeResolvedApprovers(&initial.Approvers, candidates, departmentID, resolution)
}
if len(initial.Approvers) == 0 {
	initial.InitialStatus = "skipped_no_approver"
}
```

Do not call `directorytree.ParentExternalID` or the v1 `resolveDepartmentPath`.

Resolve later chain nodes with exact enabled config rows only:

```go
for _, row := range chainRows {
	approvers := usableConfiguredApprovers(facts.configsByDepartment[row.DepartmentExternalID], facts, facts.requester.ID)
	nodes = append(nodes, ResolvedWorkflowNode{
		NodeType: "configured_department",
		Label: row.DepartmentDisplayPath,
		Departments: []DepartmentSnapshot{{
			ExternalID: row.DepartmentExternalID,
			DisplayPath: row.DepartmentDisplayPath,
			Resolution: "configured",
		}},
		Approvers: approvers,
		InitialStatus: "queued",
		AdminFallbackRequired: len(approvers) == 0,
	})
}
```

`notificationIDsForMember` must prefer allowlisted
`member.Metadata["wecom_userid"]` and fall back to `member.ExternalID`.
`mergeResolvedApprovers` deduplicates by local user id, unions and sorts every
source department id, and keeps `source="configured"` when the same user is
configured for one direct department and a representative for another. This
makes the single stored source deterministic while retaining all department
evidence.

- [ ] **Step 5: Initialize the resolver without changing existing constructors**

Add `workflowResolver *WorkflowResolver` to `Service` and initialize it:

```go
func NewService(client *ent.Client, providerResolver ProviderResolver, approverResolver *ApproverResolver, notifier Notifier) *Service {
	return &Service{
		client: client,
		providerResolver: providerResolver,
		approverResolver: approverResolver,
		workflowResolver: NewWorkflowResolver(client),
		notifier: notifier,
	}
}
```

- [ ] **Step 6: Run resolver and legacy tests**

Run:

```bash
cd backend && go test ./internal/quotareset -run 'Test(WorkflowResolver|ResolveApprovers)' -count=1
```

Expected: PASS. The v1 nearest-ancestor resolver remains unchanged for legacy
requests.

- [ ] **Step 7: Commit workflow resolution**

```bash
git add backend/internal/quotareset
git commit -m "feat(backend): resolve quota reset workflow snapshots"
```

---

### Task 4: Persist V2 Requests and Implement the Node State Machine

**Files:**
- Create: `backend/internal/quotareset/workflow_service.go`
- Create: `backend/internal/quotareset/workflow_service_test.go`
- Modify: `backend/internal/quotareset/service.go`
- Modify: `backend/internal/quotareset/types.go`
- Modify: `backend/internal/quotareset/errors.go`

- [ ] **Step 1: Write failing creation and transition tests**

Create `workflow_service_test.go` with:

```go
func TestCreateRequestSnapshotsV2WorkflowAndActivatesFirstReachableNode(t *testing.T)
func TestCreateRequestSkipsEmptyInitialNodeAndActivatesConfiguredNode(t *testing.T)
func TestCreateRequestWithOnlySkippedInitialNodeExecutesReset(t *testing.T)
func TestWorkflowApproveRequiresComment(t *testing.T)
func TestWorkflowApproveSatisfiesEveryLaterNodeContainingActor(t *testing.T)
func TestWorkflowApproveLeavesUnmatchedIntermediateNodeActive(t *testing.T)
func TestWorkflowAdminOverrideCompletesCurrentNodeOnly(t *testing.T)
func TestWorkflowRejectsRequesterSelfApprovalEvenForAdmin(t *testing.T)
func TestWorkflowAdminFallbackWritesActivationEvent(t *testing.T)
func TestWorkflowRejectTerminatesRequest(t *testing.T)
func TestWorkflowDecisionRejectsStaleNode(t *testing.T)
func TestWorkflowSnapshotDoesNotChangeAfterConfigMutation(t *testing.T)
func TestWorkflowRetryAllowsCompletionActorAndAdminOnly(t *testing.T)
```

The non-contiguous reuse test must create four nodes with actor A in nodes 0, 2,
and 3 and actor B in node 1. After A approves node 0, assert nodes 2 and 3 are
`satisfied_by_prior_approval`, node 1 is active, and reset has not run. After B
approves node 1, assert one reset call.

Use this assertion shape:

```go
updated, err := svc.Approve(ctx, DecisionInput{
	ActorUserID: actorA.ID,
	RequestID: request.ID,
	RequestNodeID: node0.ID,
	DecisionReason: "Approved at the first review",
})
if err != nil {
	t.Fatalf("Approve() error = %v", err)
}
if updated.Status != quotaresetrequest.StatusPending {
	t.Fatalf("status = %s, want pending", updated.Status)
}
if got := client.QuotaResetRequestNode.GetX(ctx, node1.ID).Status; got != quotaresetrequestnode.StatusActive {
	t.Fatalf("node 1 status = %s, want active", got)
}
for _, id := range []int{node2.ID, node3.ID} {
	if got := client.QuotaResetRequestNode.GetX(ctx, id).Status; got != quotaresetrequestnode.StatusSatisfiedByPriorApproval {
		t.Fatalf("node %d status = %s", id, got)
	}
}
if fake.resetCalls != 0 {
	t.Fatalf("reset calls = %d, want 0", fake.resetCalls)
}
```

- [ ] **Step 2: Run state-machine tests and verify failure**

Run:

```bash
cd backend && go test ./internal/quotareset -run 'Test(CreateRequestSnapshotsV2|CreateRequestSkipsEmpty|CreateRequestWithOnlySkipped|Workflow)' -count=1
```

Expected: FAIL because requests still use the v1 resolver and decision path.

- [ ] **Step 3: Extend decision input and stale-workflow errors**

Add to `DecisionInput`:

```go
RequestNodeID int
```

Add to `errors.go`:

```go
ErrWorkflowAdvanced = errors.New("workflow_advanced")

type WorkflowAdvancedError struct {
	RequestID int
}

func (e *WorkflowAdvancedError) Error() string {
	return fmt.Sprintf("%s: request_id=%d", ErrWorkflowAdvanced, e.RequestID)
}

func (e *WorkflowAdvancedError) Unwrap() error {
	return ErrWorkflowAdvanced
}
```

Add `fmt` to the imports.

- [ ] **Step 4: Persist a complete workflow snapshot in one transaction**

Create `workflow_service.go`. Add a `createWorkflowRequest` helper called by
`CreateRequest` after existing reason, relay mapping, active subscription, and
duplicate-request validation:

```go
func (s *Service) createWorkflowRequest(
	ctx context.Context,
	requester *ent.User,
	providerRow *ent.RelayProvider,
	subscription relay.UserSubscription,
	input CreateRequestInput,
) (*ent.QuotaResetRequest, error) {
	snapshot, err := s.workflowResolver.Resolve(ctx, requester.ID, providerRow.ID, input.GroupID)
	if err != nil {
		return nil, err
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin workflow snapshot transaction: %w", err)
	}
	defer tx.Rollback()
	create := tx.QuotaResetRequest.Create().
		SetRequesterUserID(requester.ID).
		SetRequesterRelayUserID(int64(*requester.RelayUserID)).
		SetProviderID(providerRow.ID).
		SetGroupID(input.GroupID).
		SetGroupName(subscriptionGroupName(subscription)).
		SetGroupPlatform(subscriptionGroupPlatform(subscription)).
		SetReason(strings.TrimSpace(input.Reason)).
		SetWorkflowVersion(WorkflowVersionV2).
		SetRequesterDisplayNameSnapshot(snapshot.Requester.DisplayName).
		SetRequesterEmailSnapshot(snapshot.Requester.Email).
		SetRequesterDepartmentPaths(snapshot.Requester.DepartmentPaths).
		SetRequesterNotificationIds(snapshot.Requester.NotificationIDs).
		SetResolvedApproverUserIds([]int{}).
		SetMatchedDepartmentPaths([]map[string]any{})
	request, err := create.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create v2 quota reset request: %w", err)
	}
	var activeNodeID *int
	for _, resolved := range snapshot.Nodes {
		status := quotaresetrequestnode.Status(resolved.InitialStatus)
		if status == quotaresetrequestnode.StatusQueued && activeNodeID == nil {
			status = quotaresetrequestnode.StatusActive
		}
		nodeCreate := tx.QuotaResetRequestNode.Create().
			SetRequestID(request.ID).
			SetPosition(resolved.Position).
			SetNodeType(quotaresetrequestnode.NodeType(resolved.NodeType)).
			SetLabel(resolved.Label).
			SetDepartmentSnapshots(departmentSnapshotsToMaps(resolved.Departments)).
			SetStatus(status).
			SetAdminFallbackRequired(resolved.AdminFallbackRequired)
		if status == quotaresetrequestnode.StatusActive {
			nodeCreate.SetActivatedAt(time.Now())
		}
		node, err := nodeCreate.Save(ctx)
		if err != nil {
			return nil, fmt.Errorf("create request node: %w", err)
		}
		if status == quotaresetrequestnode.StatusActive {
			id := node.ID
			activeNodeID = &id
		}
		for _, approver := range resolved.Approvers {
			if _, err := tx.QuotaResetRequestNodeApprover.Create().
				SetRequestNodeID(node.ID).
				SetUserID(approver.UserID).
				SetDisplayName(approver.DisplayName).
				SetEmail(approver.Email).
				SetSource(quotaresetrequestnodeapprover.Source(approver.Source)).
				SetSourceDepartmentExternalIds(approver.SourceDepartmentExternalIDs).
				SetNotificationIds(approver.NotificationIDs).
				Save(ctx); err != nil {
				return nil, fmt.Errorf("create request node approver: %w", err)
			}
		}
	}
	update := tx.QuotaResetRequest.UpdateOneID(request.ID)
	if activeNodeID != nil {
		update.SetCurrentNodeID(*activeNodeID)
	} else {
		update.SetStatus(quotaresetrequest.StatusApprovedResetting).ClearCurrentNodeID()
	}
	request, err = update.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("activate workflow: %w", err)
	}
	if err := writeWorkflowEvent(ctx, tx, request.ID, nil, quotaresetrequestevent.EventTypeWorkflowSnapshotted, map[string]any{
		"node_count": len(snapshot.Nodes),
	}); err != nil {
		return nil, err
	}
	storedNodes, err := tx.QuotaResetRequestNode.Query().
		Where(quotaresetrequestnode.RequestIDEQ(request.ID)).
		Order(ent.Asc(quotaresetrequestnode.FieldPosition)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load snapshotted nodes for events: %w", err)
	}
	for _, storedNode := range storedNodes {
		switch storedNode.Status {
		case quotaresetrequestnode.StatusSkippedNoApprover:
			if err := writeWorkflowEvent(ctx, tx, request.ID, nil, quotaresetrequestevent.EventTypeNodeSkippedNoApprover, map[string]any{"node_id": storedNode.ID, "position": storedNode.Position}); err != nil {
				return nil, err
			}
		case quotaresetrequestnode.StatusActive:
			if err := writeWorkflowEvent(ctx, tx, request.ID, nil, quotaresetrequestevent.EventTypeNodeActivated, map[string]any{"node_id": storedNode.ID, "position": storedNode.Position, "admin_fallback": storedNode.AdminFallbackRequired}); err != nil {
				return nil, err
			}
			if storedNode.AdminFallbackRequired {
				if err := writeWorkflowEvent(ctx, tx, request.ID, nil, quotaresetrequestevent.EventTypeAdminFallbackActivated, map[string]any{"node_id": storedNode.ID, "position": storedNode.Position}); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit workflow snapshot: %w", err)
	}
	if activeNodeID == nil {
		return s.executeReset(ctx, request.ID, requester.ID, false, false)
	}
	_ = s.notifyActiveNode(ctx, request.ID, *activeNodeID)
	return request, nil
}
```

`writeWorkflowEvent` must accept `*ent.Tx` so request, nodes, and initial events
commit together. `departmentSnapshotsToMaps` must use JSON marshal/unmarshal like
the existing path evidence converter.

Replace the v1 creation body in `CreateRequest` with:

```go
return s.createWorkflowRequest(ctx, requester, providerRow, subscription, input)
```

Do not remove v1 creation helpers yet; legacy rows still use v1 decisions and
summaries.

- [ ] **Step 5: Implement transactional approval and cross-node reuse**

Add `approveWorkflow`:

```go
func (s *Service) approveWorkflow(ctx context.Context, input DecisionInput) (*ent.QuotaResetRequest, error) {
	input.DecisionReason = strings.TrimSpace(input.DecisionReason)
	if input.DecisionReason == "" {
		return nil, ErrDecisionRequired
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin workflow approval: %w", err)
	}
	defer tx.Rollback()
	request, node, actor, normalApprover, err := lockAndAuthorizeCurrentNode(ctx, tx, input)
	if err != nil {
		return nil, err
	}
	decision, err := tx.QuotaResetRequestDecision.Create().
		SetRequestID(request.ID).
		SetRequestNodeID(node.ID).
		SetActorUserID(actor.ID).
		SetActorDisplayName(actor.Username).
		SetDecision(quotaresetrequestdecision.DecisionApprove).
		SetComment(input.DecisionReason).
		SetAdminOverride(input.Admin && !normalApprover).
		Save(ctx)
	if ent.IsConstraintError(err) {
		return nil, &WorkflowAdvancedError{RequestID: request.ID}
	}
	if err != nil {
		return nil, fmt.Errorf("store workflow approval: %w", err)
	}
	now := time.Now()
	if _, err := tx.QuotaResetRequestNode.UpdateOneID(node.ID).
		SetStatus(quotaresetrequestnode.StatusApproved).
		SetSatisfiedByDecisionID(decision.ID).
		SetCompletedAt(now).
		Save(ctx); err != nil {
		return nil, fmt.Errorf("complete current node: %w", err)
	}
	if err := writeWorkflowEvent(ctx, tx, request.ID, &actor.ID, quotaresetrequestevent.EventTypeNodeApproved, map[string]any{
		"node_id": node.ID,
		"position": node.Position,
		"admin_override": input.Admin && !normalApprover,
	}); err != nil {
		return nil, err
	}
	later, err := tx.QuotaResetRequestNode.Query().
		Where(
			quotaresetrequestnode.RequestIDEQ(request.ID),
			quotaresetrequestnode.PositionGT(node.Position),
			quotaresetrequestnode.StatusEQ(quotaresetrequestnode.StatusQueued),
		).
		Order(ent.Asc(quotaresetrequestnode.FieldPosition)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load later workflow nodes: %w", err)
	}
	for _, future := range later {
		eligible, err := tx.QuotaResetRequestNodeApprover.Query().
			Where(
				quotaresetrequestnodeapprover.RequestNodeIDEQ(future.ID),
				quotaresetrequestnodeapprover.UserIDEQ(actor.ID),
			).
			Exist(ctx)
		if err != nil {
			return nil, fmt.Errorf("check reusable approval: %w", err)
		}
		if !eligible {
			continue
		}
		if _, err := tx.QuotaResetRequestNode.UpdateOneID(future.ID).
			SetStatus(quotaresetrequestnode.StatusSatisfiedByPriorApproval).
			SetSatisfiedByDecisionID(decision.ID).
			SetCompletedAt(now).
			Save(ctx); err != nil {
			return nil, fmt.Errorf("reuse workflow approval: %w", err)
		}
		if err := writeWorkflowEvent(ctx, tx, request.ID, &actor.ID, quotaresetrequestevent.EventTypeNodeSatisfiedByPriorApproval, map[string]any{
			"node_id": future.ID,
			"decision_id": decision.ID,
		}); err != nil {
			return nil, err
		}
	}
	next, err := tx.QuotaResetRequestNode.Query().
		Where(
			quotaresetrequestnode.RequestIDEQ(request.ID),
			quotaresetrequestnode.StatusEQ(quotaresetrequestnode.StatusQueued),
		).
		Order(ent.Asc(quotaresetrequestnode.FieldPosition)).
		First(ctx)
	update := tx.QuotaResetRequest.UpdateOneID(request.ID)
	var nextNodeID *int
	switch {
	case err == nil:
		if _, err := tx.QuotaResetRequestNode.UpdateOneID(next.ID).
			SetStatus(quotaresetrequestnode.StatusActive).
			SetActivatedAt(now).
			Save(ctx); err != nil {
			return nil, fmt.Errorf("activate next workflow node: %w", err)
		}
		update.SetCurrentNodeID(next.ID)
		if err := writeWorkflowEvent(ctx, tx, request.ID, nil, quotaresetrequestevent.EventTypeNodeActivated, map[string]any{
			"node_id": next.ID,
			"position": next.Position,
			"admin_fallback": next.AdminFallbackRequired,
		}); err != nil {
			return nil, err
		}
		if next.AdminFallbackRequired {
			if err := writeWorkflowEvent(ctx, tx, request.ID, nil, quotaresetrequestevent.EventTypeAdminFallbackActivated, map[string]any{
				"node_id": next.ID,
				"position": next.Position,
			}); err != nil {
				return nil, err
			}
		}
		id := next.ID
		nextNodeID = &id
	case ent.IsNotFound(err):
		update.SetStatus(quotaresetrequest.StatusApprovedResetting).
			SetWorkflowCompletedByDecisionID(decision.ID).
			ClearCurrentNodeID()
	default:
		return nil, fmt.Errorf("load next workflow node: %w", err)
	}
	request, err = update.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("advance workflow request: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit workflow approval: %w", err)
	}
	if nextNodeID != nil {
		_ = s.notifyActiveNode(ctx, request.ID, *nextNodeID)
		return request, nil
	}
	return s.executeReset(ctx, request.ID, actor.ID, false, input.Admin)
}
```

`lockAndAuthorizeCurrentNode` must:

1. Select the request and node with `FOR UPDATE`.
2. Require workflow v2, status pending, supplied node id equal to
   `current_node_id`, and node status active.
3. Reject requester self-approval regardless of role or whether the admin route
   was used.
4. Load the actor.
5. Check normal membership in `quota_reset_request_node_approvers`.
6. Permit a current admin when `input.Admin=true`.
7. Return `WorkflowAdvancedError` for stale state and `ErrNotApprover` for
   unauthorized actors.

Use Ent selector locking:

```go
Modify(func(selector *sql.Selector) {
	selector.ForUpdate()
})
```

- [ ] **Step 6: Implement rejection, cancellation, dispatch, and retry**

`rejectWorkflow` uses the same lock and authorization helper, inserts a reject
decision, marks the node and request rejected, commits, then sends one terminal
notification.

Dispatch without changing legacy semantics:

```go
func (s *Service) Approve(ctx context.Context, input DecisionInput) (*ent.QuotaResetRequest, error) {
	request, err := s.client.QuotaResetRequest.Get(ctx, input.RequestID)
	if err != nil {
		return nil, err
	}
	if request.WorkflowVersion < WorkflowVersionV2 {
		return s.approveLegacy(ctx, input)
	}
	return s.approveWorkflow(ctx, input)
}
```

Apply the same version dispatch to reject and retry. Rename the existing bodies
to `approveLegacy`, `rejectLegacy`, and `retryResetLegacy`. Keep `Cancel` as one
shared conditional-update path because its requester authorization and pending
status contract are identical for v1 and v2; add cancellation tests for both
versions and let Task 7 build the version-appropriate notification context.

For v2 retry:

```go
if !input.Admin {
	decisionID := request.WorkflowCompletedByDecisionID
	if decisionID == nil {
		return nil, ErrNotApprover
	}
	decision, err := s.client.QuotaResetRequestDecision.Get(ctx, *decisionID)
	if err != nil {
		return nil, err
	}
	if decision.ActorUserID != input.ActorUserID {
		return nil, ErrNotApprover
	}
}
return s.executeReset(ctx, request.ID, input.ActorUserID, true, input.Admin)
```

- [ ] **Step 7: Run state-machine and race-sensitive tests**

Run:

```bash
cd backend && go test ./internal/quotareset -run 'Test(CreateRequestSnapshotsV2|CreateRequestSkipsEmpty|CreateRequestWithOnlySkipped|Workflow)' -count=1
cd backend && go test -race ./internal/quotareset -run 'TestWorkflowDecisionRejectsStaleNode' -count=1
```

Expected: PASS. PostgreSQL is required; report test-database setup failures
separately from behavioral failures.

- [ ] **Step 8: Commit the state machine**

```bash
git add backend/internal/quotareset
git commit -m "feat(backend): execute multi-stage quota reset approvals"
```

---

### Task 5: Add Viewer-Aware Summaries and Actionable Work Counts

**Files:**
- Create: `backend/internal/quotareset/workflow_summary.go`
- Create: `backend/internal/quotareset/work_items.go`
- Create: `backend/internal/quotareset/work_items_test.go`
- Modify: `backend/internal/quotareset/service.go`
- Modify: `backend/internal/quotareset/types.go`
- Modify: `backend/internal/workitems/service.go`
- Modify: `backend/internal/workitems/service_test.go`
- Modify: `backend/internal/handler/quota_reset.go`
- Modify: `backend/internal/handler/quota_reset_test.go`

- [ ] **Step 1: Write failing list, permission, and count tests**

Add:

```go
func TestListApprovalsReturnsOnlyActiveV2Assignments(t *testing.T)
func TestWorkflowSummaryReturnsOrderedNodesDecisionsAndPermissions(t *testing.T)
func TestWorkflowSummaryHidesFutureQueueFromUnrelatedApprover(t *testing.T)
func TestCountWorkItemsIncludesActiveNodeAndCompletionActorRetry(t *testing.T)
func TestCountWorkItemsAdminUsesAllPendingWithoutDoubleCounting(t *testing.T)
func TestCountWorkItemsKeepsLegacyV1Semantics(t *testing.T)
```

For the active-assignment test, put the actor in one future queued node and a
different active node. Expect zero until the future node activates. For the
retry test, set `approved_reset_failed` plus
`workflow_completed_by_decision_id` and expect only that decision actor.

- [ ] **Step 2: Run focused tests and verify failure**

Run:

```bash
cd backend && go test ./internal/quotareset ./internal/workitems -run 'Test(ListApprovalsReturnsOnlyActiveV2|WorkflowSummary|CountWorkItems)' -count=1
```

Expected: FAIL because v2 list and count queries do not exist.

- [ ] **Step 3: Add workflow and action fields to request summaries**

Add to `RequestSummary`:

```go
RequesterDepartmentPaths []string `json:"requester_department_paths"`
Workflow *WorkflowSummary `json:"workflow,omitempty"`
```

For workflow v2, populate requester display name, email, and department paths
from the immutable request snapshot fields. Keep the current live-user fallback
only for v1 rows.

Add a private viewer contract:

```go
type summaryViewer struct {
	UserID int
	Admin bool
	Requester bool
}
```

Create `workflow_summary.go` with `loadWorkflowSummary(ctx, request, viewer)`.
It must load nodes ordered by position, approvers grouped by node, and decisions
ordered by creation time. Calculate:

```go
canDecide := request.Status == quotaresetrequest.StatusPending &&
	currentNode != nil &&
	request.RequesterUserID != viewer.UserID &&
	(viewer.Admin || workflowNodeHasApprover(currentNode.ID, viewer.UserID))
canCancel := request.Status == quotaresetrequest.StatusPending &&
	request.RequesterUserID == viewer.UserID
canRetry := request.Status == quotaresetrequest.StatusApprovedResetFailed &&
	(viewer.Admin || completionDecisionActorID == viewer.UserID)
```

Only requester, admin, active normal candidates, and users with a stored manual
decision may receive workflow details. Future queued candidates alone do not
gain list visibility.

Refactor the batch summary path to carry the real viewer:

```go
func (s *Service) summaries(ctx context.Context, requests []*ent.QuotaResetRequest, viewer summaryViewer) ([]RequestSummary, error)
func (s *Service) ListAdmin(ctx context.Context, actorUserID int, params ListParams) (*RequestListResponse, error)
```

`ListMine` passes `{UserID: actorUserID, Requester: true}`, `ListApprovals`
passes `{UserID: actorUserID}`, and `ListAdmin` passes
`{UserID: actorUserID, Admin: true}`. Update the handler service interface and
`ListAdmin` handler to obtain `quotaResetActor(c)` and pass that id. This keeps
admin self-approval false in computed permissions instead of using a synthetic
viewer id.

- [ ] **Step 4: Update list predicates for v1 and v2**

Replace v2 approval listing with an `EXISTS` query over current active nodes and
node approvers while retaining the JSONB v1 predicate:

```go
return q.Where(quotaresetrequest.Or(
	quotaresetrequest.And(
		quotaresetrequest.WorkflowVersionLT(WorkflowVersionV2),
		legacyApproverJSONPredicate(actorUserID),
	),
	quotaresetrequest.And(
		quotaresetrequest.WorkflowVersionGTE(WorkflowVersionV2),
		v2ActiveApproverPredicate(actorUserID),
	),
	quotaresetrequest.And(
		quotaresetrequest.WorkflowVersionGTE(WorkflowVersionV2),
		quotaresetrequest.StatusEQ(quotaresetrequest.StatusApprovedResetFailed),
		v2CompletionActorPredicate(actorUserID),
	),
))
```

Implement the three SQL predicates in `workflow_summary.go` using parameterized
`EXISTS` subqueries; do not interpolate user input into SQL strings.

- [ ] **Step 5: Centralize Work Items quota queries in quotareset**

Create:

```go
type WorkItemCounts struct {
	Assigned int
	Admin int
}

func CountWorkItems(ctx context.Context, client *ent.Client, userID int, admin bool) (WorkItemCounts, error)
```

`Assigned` must count:

1. Actionable v1 requests containing the actor in legacy approver JSON.
2. Pending v2 requests whose active node has the actor as a normal approver.
3. Failed v2 requests whose completion decision actor is the actor.

`Admin` must count all v1/v2 pending and reset-failed requests.

In `backend/internal/workitems/service.go`, replace the two quota-specific query
methods with:

```go
quotaCounts, err := quotareset.CountWorkItems(ctx, s.client, userID, admin)
if err != nil {
	return nil, fmt.Errorf("count quota reset work items: %w", err)
}
counts.QuotaResetApprovalCount = quotaCounts.Assigned
if admin {
	counts.QuotaResetAdminCount = quotaCounts.Admin
}
```

Keep AI access and offboarding degradation and total-count deduplication exactly
as currently implemented.

- [ ] **Step 6: Run list, count, and regression tests**

Run:

```bash
cd backend && go test ./internal/quotareset ./internal/workitems -run 'Test(ListApprovalsReturnsOnlyActiveV2|WorkflowSummary|CountWorkItems|Counts)' -count=1
cd backend && go test ./internal/quotareset ./internal/workitems -count=1
cd backend && go test ./internal/handler -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit summaries and counts**

```bash
git add backend/internal/quotareset backend/internal/workitems backend/internal/handler/quota_reset.go backend/internal/handler/quota_reset_test.go
git commit -m "feat(backend): expose actionable quota reset workflow state"
```

---

### Task 6: Make Notification Channel Selection Explicit and Migratable

**Files:**
- Create: `backend/internal/quotareset/notification_backfill.go`
- Create: `backend/internal/quotareset/notification_backfill_test.go`
- Modify: `backend/internal/quotareset/types.go`
- Modify: `backend/internal/quotareset/service.go`
- Modify: `backend/internal/quotareset/service_test.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Write failing settings and backfill tests**

Add:

```go
func TestNotificationSettingsRequiresExplicitChannelType(t *testing.T)
func TestNotificationSettingsValidatesWeComEndpointAndDisallowsBearer(t *testing.T)
func TestNotificationSettingsReadRedactsRobotKey(t *testing.T)
func TestNotificationSettingsOmittedURLPreservesExistingSecret(t *testing.T)
func TestBackfillNotificationChannelsClassifiesExistingWeComOnce(t *testing.T)
func TestBackfillNotificationChannelsKeepsExplicitGenericChoice(t *testing.T)
```

The redaction test must build its synthetic endpoint without placing a complete
robot URL in source:

```go
robotURL := "https://qyapi.weixin.qq.com/cgi-bin/webhook/send" + "?" + "key=test-secret"
```

Store `robotURL` and assert the response does not contain `test-secret`, returns
`url_configured=true`, and returns a host/path preview.

- [ ] **Step 2: Run settings tests and verify failure**

Run:

```bash
cd backend && go test ./internal/quotareset -run 'Test(NotificationSettings|BackfillNotificationChannels)' -count=1
```

Expected: FAIL because explicit channel and redacted response contracts do not
exist.

- [ ] **Step 3: Replace public notification setting contracts**

Use:

```go
type NotificationSettings struct {
	Enabled bool `json:"enabled"`
	ChannelType string `json:"channel_type"`
	TemplateVersion int `json:"template_version"`
	URLConfigured bool `json:"url_configured"`
	URLPreview string `json:"url_preview"`
	AuthType string `json:"auth_type"`
	CredentialID *int `json:"credential_id,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type UpdateNotificationSettingsInput struct {
	ActorUserID int
	Enabled bool
	ChannelType string
	URL *string
	AuthType string
	CredentialID *int
}
```

`URL=nil` means preserve the existing URL. A non-nil empty URL clears it and is
valid only when notifications are disabled.

- [ ] **Step 4: Implement type-specific validation and redacted reads**

Implement:

```go
func validateNotificationEndpoint(channelType string, parsed *url.URL, authType quotaresetnotificationsetting.AuthType) error {
	switch channelType {
	case quotaresetnotificationsetting.ChannelTypeWecomGroupRobot.String():
		if !strings.EqualFold(parsed.Hostname(), "qyapi.weixin.qq.com") || parsed.Path != "/cgi-bin/webhook/send" || strings.TrimSpace(parsed.Query().Get("key")) == "" {
			return fmt.Errorf("%w: invalid Enterprise WeChat group robot URL", ErrInvalidNotification)
		}
		if authType != quotaresetnotificationsetting.AuthTypeNone {
			return fmt.Errorf("%w: Enterprise WeChat group robot does not use bearer auth", ErrInvalidNotification)
		}
	case quotaresetnotificationsetting.ChannelTypeGenericWebhook.String():
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("%w: invalid webhook URL", ErrInvalidNotification)
		}
	default:
		return fmt.Errorf("%w: channel_type is required", ErrInvalidNotification)
	}
	return nil
}

func webhookURLPreview(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return ""
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
```

On save, set `channel_type_configured=true`. `GetNotificationSettings` and save
responses must never return the raw URL.

- [ ] **Step 5: Implement one-time channel backfill**

Create:

```go
func BackfillNotificationChannelTypes(ctx context.Context, client *ent.Client) (int, error) {
	rows, err := client.QuotaResetNotificationSetting.Query().
		Where(quotaresetnotificationsetting.ChannelTypeConfigured(false)).
		All(ctx)
	if err != nil {
		return 0, fmt.Errorf("list notification settings for channel backfill: %w", err)
	}
	updated := 0
	for _, row := range rows {
		channelType := quotaresetnotificationsetting.ChannelTypeGenericWebhook
		parsed, _ := url.Parse(strings.TrimSpace(row.URL))
		if parsed != nil && strings.EqualFold(parsed.Hostname(), "qyapi.weixin.qq.com") && parsed.Path == "/cgi-bin/webhook/send" {
			channelType = quotaresetnotificationsetting.ChannelTypeWecomGroupRobot
		}
		if _, err := client.QuotaResetNotificationSetting.UpdateOneID(row.ID).
			SetChannelType(channelType).
			SetChannelTypeConfigured(true).
			Save(ctx); err != nil {
			return updated, fmt.Errorf("backfill notification setting %d: %w", row.ID, err)
		}
		updated++
	}
	return updated, nil
}
```

Call it in `backend/cmd/server/main.go` immediately after `Schema.Create` and
before router construction:

```go
if _, err := quotareset.BackfillNotificationChannelTypes(context.Background(), entClient); err != nil {
	logger.Fatal("backfill quota reset notification channels", zap.Error(err))
}
```

- [ ] **Step 6: Run settings and startup package tests**

Run:

```bash
cd backend && go test ./internal/quotareset -run 'Test(NotificationSettings|BackfillNotificationChannels)' -count=1
cd backend && go test ./cmd/server ./internal/quotareset -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit explicit channel settings**

```bash
git add backend/internal/quotareset backend/cmd/server/main.go
git commit -m "feat(backend): configure quota reset notification channels"
```

---

### Task 7: Add Generic and Enterprise WeChat Notification Adapters

**Files:**
- Create: `backend/internal/quotareset/notification_channel.go`
- Create: `backend/internal/quotareset/notification_generic.go`
- Create: `backend/internal/quotareset/notification_wecom.go`
- Modify: `backend/internal/quotareset/notification.go`
- Modify: `backend/internal/quotareset/notification_test.go`
- Modify: `backend/internal/quotareset/types.go`
- Modify: `backend/internal/quotareset/workflow_service.go`

- [ ] **Step 1: Write failing renderer, recipient, and routing tests**

Replace URL-inference assertions with:

```go
func TestGenericWebhookAdapterRendersVersionedWorkflowPayload(t *testing.T)
func TestWeComAdapterRendersMarkdownRequesterTeamReasonAndMentions(t *testing.T)
func TestWeComAdapterEscapesUserControlledMentionAndMarkdownSyntax(t *testing.T)
func TestWeComAdapterReportsMissingRecipientCoverageWithoutFailing(t *testing.T)
func TestWeComAdapterKeepsRequiredFieldsWithinByteLimit(t *testing.T)
func TestWebhookNotifierUsesExplicitChannelInsteadOfURLShape(t *testing.T)
func TestWorkflowActivationNotifiesOnlyActiveNode(t *testing.T)
func TestApprovalReuseDoesNotNotifySatisfiedLaterNodes(t *testing.T)
func TestWebhookNotifierReturnsWeComBusinessError(t *testing.T)
func TestNotificationTestReturnsMentionCoverageWarning(t *testing.T)
```

The WeCom content assertion must require:

```go
for _, want := range []string{
	"# 额度重置待审批",
	"Alice",
	"Department Alpha / Team One",
	"Group Alpha",
	"Complete a time-sensitive build investigation.",
	"2/3",
	"<@bob-wecom-id>",
	"/usage/quota-reset?request_id=123",
} {
	if !strings.Contains(content, want) {
		t.Fatalf("content = %q, want %q", content, want)
	}
}
```

The injection test uses reason `"<@all> **approve now**"` and must assert the
rendered content does not contain `<@all>`. The approval-reuse test records HTTP
delivery count and asserts one delivery for the newly active unmatched node and
zero for every auto-satisfied node.

- [ ] **Step 2: Run notification tests and verify failure**

Run:

```bash
cd backend && go test ./internal/quotareset -run 'Test(GenericWebhookAdapter|WeComAdapter|WebhookNotifierUsesExplicit|WorkflowActivationNotifies|ApprovalReuseDoesNotNotify|WebhookNotifierReturnsWeCom|NotificationTestReturnsMentionCoverageWarning)' -count=1
```

Expected: FAIL because the adapter registry and enriched context do not exist.

- [ ] **Step 3: Define a channel-neutral notification contract**

Create `notification_channel.go`:

```go
package quotareset

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type NotificationEvent string

const (
	NotificationNodeActivated NotificationEvent = "quota_reset_approval_node_activated"
	NotificationRejected NotificationEvent = "quota_reset_request_rejected"
	NotificationCancelled NotificationEvent = "quota_reset_request_cancelled"
	NotificationResetSucceeded NotificationEvent = "quota_reset_request_reset_succeeded"
	NotificationResetFailed NotificationEvent = "quota_reset_request_reset_failed"
	NotificationTest NotificationEvent = "quota_reset_notification_test"
)

type NotificationPerson struct {
	UserID int `json:"user_id"`
	DisplayName string `json:"display_name"`
	Email string `json:"email,omitempty"`
	NotificationIDs map[string]string `json:"-"`
}

type NotificationDecision struct {
	ActorDisplayName string `json:"actor_display_name"`
	Decision string `json:"decision"`
	Comment string `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
}

type NotificationNode struct {
	ID int `json:"id"`
	Position int `json:"position"`
	Total int `json:"total"`
	Label string `json:"label"`
	Approvers []NotificationPerson `json:"approvers"`
	AdminFallback bool `json:"admin_fallback"`
}

type NotificationContext struct {
	Event NotificationEvent
	OccurredAt time.Time
	RequestID int
	Status string
	Requester NotificationPerson
	Recipients []NotificationPerson
	DepartmentPaths []string
	GroupID string
	GroupName string
	GroupPlatform string
	Reason string
	CurrentNode *NotificationNode
	ApprovalHistory []NotificationDecision
	ActionURL string
}

type RenderedNotification struct {
	Body []byte
	Headers http.Header
	RecipientCount int
	MissingRecipientUserIDs []int
}

type notificationAdapter interface {
	Render(NotificationContext) (RenderedNotification, error)
	ValidateResponse(statusCode int, body []byte) error
}

func notificationAdapterFor(channelType string) (notificationAdapter, error) {
	switch channelType {
	case "generic_webhook":
		return genericWebhookAdapter{}, nil
	case "wecom_group_robot":
		return weComGroupRobotAdapter{maxBytes: 4096}, nil
	default:
		return nil, fmt.Errorf("%w: unsupported channel_type %q", ErrInvalidNotification, channelType)
	}
}

func marshalNotificationPayload(value any) ([]byte, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal notification payload: %w", err)
	}
	return body, nil
}
```

Change `Notifier` in `types.go`:

```go
type Notifier interface {
	Notify(context.Context, NotificationContext) (*NotificationDeliveryResult, error)
}

type NotificationDeliveryResult struct {
	RecipientCount int
	MissingRecipientUserIDs []int
}

type NotificationTestResult struct {
	Delivered bool `json:"delivered"`
	RecipientCount int `json:"recipient_count"`
	MissingRecipientCount int `json:"missing_recipient_count"`
	Warning string `json:"warning,omitempty"`
}
```

- [ ] **Step 4: Implement the generic JSON adapter**

Create `notification_generic.go`:

```go
package quotareset

import "net/http"

type genericWebhookAdapter struct{}

func (genericWebhookAdapter) Render(ctx NotificationContext) (RenderedNotification, error) {
	payload := map[string]any{
		"schema_version": 2,
		"event": ctx.Event,
		"request": map[string]any{
			"id": ctx.RequestID,
			"status": ctx.Status,
			"requester": map[string]any{
				"display_name": ctx.Requester.DisplayName,
				"email": ctx.Requester.Email,
				"departments": ctx.DepartmentPaths,
			},
			"subscription_group": map[string]any{
				"id": ctx.GroupID,
				"name": ctx.GroupName,
				"platform": ctx.GroupPlatform,
			},
			"reason": ctx.Reason,
		},
		"current_node": ctx.CurrentNode,
		"approval_history": ctx.ApprovalHistory,
		"action_url": ctx.ActionURL,
		"occurred_at": ctx.OccurredAt.UTC().Format(time.RFC3339),
	}
	body, err := marshalNotificationPayload(payload)
	if err != nil {
		return RenderedNotification{}, err
	}
	return RenderedNotification{Body: body, Headers: http.Header{"Content-Type": []string{"application/json"}}}, nil
}

func (genericWebhookAdapter) ValidateResponse(statusCode int, _ []byte) error {
	if statusCode < 200 || statusCode > 299 {
		return fmt.Errorf("webhook returned %d", statusCode)
	}
	return nil
}
```

Add `fmt` and `time` imports.

- [ ] **Step 5: Implement safe Enterprise WeChat Markdown rendering**

Create `notification_wecom.go` with trusted mentions separated from escaped
user content:

```go
package quotareset

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

var safeWeComUserID = regexp.MustCompile(`^[A-Za-z0-9_.@-]{1,128}$`)

type weComGroupRobotAdapter struct{ maxBytes int }

func escapeWeComUserText(value string) string {
	value = strings.TrimSpace(value)
	replacer := strings.NewReplacer(
		"<", "＜",
		">", "＞",
		"[", "［",
		"]", "］",
		"*", "＊",
		"`", "｀",
	)
	return replacer.Replace(value)
}

func weComMention(person NotificationPerson) (string, bool) {
	userID := strings.TrimSpace(person.NotificationIDs["wecom"])
	if userID == "" || !safeWeComUserID.MatchString(userID) {
		return escapeWeComUserText(person.DisplayName) + "（无法 @）", false
	}
	return "<@" + userID + ">", true
}

func (a weComGroupRobotAdapter) Render(ctx NotificationContext) (RenderedNotification, error) {
	maxBytes := a.maxBytes
	if maxBytes <= 0 {
		maxBytes = 4096
	}
	title, action := weComEventCopy(ctx.Event)
	required := []string{
		"# " + title,
		`<font color="warning">` + escapeWeComUserText(action) + `</font>`,
		"> 申请人：" + escapeWeComUserText(ctx.Requester.DisplayName),
		"> 所属团队：" + escapeWeComUserText(strings.Join(ctx.DepartmentPaths, "、")),
		"> 订阅组：" + escapeWeComUserText(ctx.GroupName),
	}
	optional := []string{"> 申请原因：" + escapeWeComUserText(ctx.Reason)}
	if ctx.CurrentNode != nil {
		required = append(required, fmt.Sprintf("> 当前节点：%d/%d · %s", ctx.CurrentNode.Position+1, ctx.CurrentNode.Total, escapeWeComUserText(ctx.CurrentNode.Label)))
	}
	if len(ctx.ApprovalHistory) > 0 {
		latest := ctx.ApprovalHistory[len(ctx.ApprovalHistory)-1]
		optional = append(optional, "> 上一审批："+escapeWeComUserText(latest.ActorDisplayName)+"："+escapeWeComUserText(latest.Comment))
	}
	missing := []int{}
	mentions := []string{}
	for _, recipient := range ctx.Recipients {
			mention, ok := weComMention(recipient)
			mentions = append(mentions, mention)
			if !ok {
				missing = append(missing, recipient.UserID)
			}
	}
	if len(mentions) > 0 {
		required = append(required, "待审批："+strings.Join(mentions, " "))
	}
	if ctx.ActionURL != "" {
		required = append(required, "[进入待处理]("+ctx.ActionURL+")")
	}
	content := fitWeComMarkdown(required, optional, maxBytes)
	payload := map[string]any{"msgtype": "markdown", "markdown": map[string]string{"content": content}}
	body, err := json.Marshal(payload)
	if err != nil {
		return RenderedNotification{}, fmt.Errorf("marshal WeCom payload: %w", err)
	}
	return RenderedNotification{
		Body: body,
		Headers: http.Header{"Content-Type": []string{"application/json"}},
		RecipientCount: len(mentions) - len(missing),
		MissingRecipientUserIDs: missing,
	}, nil
}

func fitWeComMarkdown(required, optional []string, maxBytes int) string {
	tail := ""
	if len(required) > 0 && strings.HasPrefix(required[len(required)-1], "[进入待处理]") {
		tail = required[len(required)-1]
		required = required[:len(required)-1]
	}
	lines := append([]string{}, required...)
	if tail != "" {
		lines = append(lines, tail)
	}
	if len([]byte(strings.Join(lines, "\n\n"))) > maxBytes {
		return ""
	}
	insertAt := len(lines)
	if tail != "" {
		insertAt--
	}
	for _, line := range optional {
		candidateLines := append([]string{}, lines[:insertAt]...)
		candidateLines = append(candidateLines, truncateUTF8(line, 768))
		candidateLines = append(candidateLines, lines[insertAt:]...)
		candidate := strings.Join(candidateLines, "\n\n")
		if len([]byte(candidate)) <= maxBytes {
			lines = candidateLines
			insertAt++
		}
	}
	return strings.Join(lines, "\n\n")
}

func truncateUTF8(value string, maxBytes int) string {
	if len([]byte(value)) <= maxBytes {
		return value
	}
	suffix := "…"
	runes := []rune(value)
	for len(runes) > 0 && len([]byte(string(runes)+suffix)) > maxBytes {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + suffix
}

func (weComGroupRobotAdapter) ValidateResponse(statusCode int, body []byte) error {
	if statusCode < 200 || statusCode > 299 {
		return fmt.Errorf("webhook returned %d", statusCode)
	}
	return webhookResponseBusinessError(bytes.TrimSpace(body))
}
```

Bound requester, team, group, node, and mention lines before calling
`fitWeComMarkdown` so the required set plus action link is always below 4096
bytes. Add mentions one at a time while reserving space for the action link;
recipient ids that do not fit are reported as missing coverage. Treat an empty
result from `fitWeComMarkdown` as a rendering error rather than sending a
truncated link.

Implement `weComEventCopy` as an exhaustive switch for the six defined events.
Do not concatenate an unknown event into user-visible Markdown; return a neutral
synthetic label.

- [ ] **Step 6: Refactor WebhookNotifier to use explicit adapters**

`Notify` must:

1. Load the single enabled setting.
2. Select `notificationAdapterFor(setting.ChannelType.String())`.
3. Render the neutral context.
4. POST the rendered body and headers to the saved URL.
5. Apply bearer auth only for `generic_webhook`.
6. Bound response reads to the existing 4096 bytes.
7. Call adapter `ValidateResponse`.
8. Return recipient coverage.

Remove `payloadForURL`, `isWeComRobotWebhookURL`,
`weComRobotTextContent`, and URL-based runtime format selection.

Use:

```go
rendered, err := adapter.Render(notificationContext)
if err != nil {
	return nil, err
}
request, err := http.NewRequestWithContext(ctx, http.MethodPost, setting.URL, bytes.NewReader(rendered.Body))
if err != nil {
	return nil, fmt.Errorf("create webhook request: %w", err)
}
request.Header = rendered.Headers.Clone()
```

- [ ] **Step 7: Build notification context and route node events**

Add `notificationContextForRequest(ctx, requestID, nodeID, event)` in
`workflow_summary.go`. It must load requester snapshots, current node approvers,
all decisions, action URL, and an explicit `Recipients` list:

1. Node activation: current-node normal approvers, or current admins when the
   node requires admin fallback.
2. Rejection and reset success: requester.
3. Cancellation: currently active approvers.
4. Reset failure: requester, completion decision actor, and current admins.
5. Test: triggering admin when a current Enterprise WeChat id exists.

Change the service method to:

```go
func (s *Service) TestNotificationSettings(ctx context.Context, actorUserID int) (*NotificationTestResult, error)
```

It sends only synthetic request data. Convert the internal delivery result into
counts; never return recipient ids. For `wecom_group_robot`, set
`Warning="wecom_recipient_unavailable"` when the triggering admin has no
resolvable recipient id. Generic webhook tests return an empty warning.

`notifyActiveNode` calls it only for the newly active node.

Persist result metadata:

```go
map[string]any{
	"event": string(context.Event),
	"channel_type": setting.ChannelType.String(),
	"recipient_count": result.RecipientCount,
	"missing_recipient_count": len(result.MissingRecipientUserIDs),
}
```

Do not store recipient ids or full user comments in event metadata. Approval
reuse writes node events but never calls the notifier for satisfied nodes.

For legacy v1 events, build a minimal neutral context from the existing request
and live requester lookup so the configured adapter still handles old requests.

- [ ] **Step 8: Run notification tests**

Run:

```bash
cd backend && go test ./internal/quotareset -run 'Test(GenericWebhookAdapter|WeComAdapter|WebhookNotifierUsesExplicit|WorkflowActivationNotifies|ApprovalReuseDoesNotNotify|WebhookNotifierReturnsWeCom|NotificationTestReturnsMentionCoverageWarning)' -count=1
cd backend && go test ./internal/quotareset -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit adapters and routing**

```bash
git add backend/internal/quotareset
git commit -m "feat(backend): notify quota reset workflow approvers"
```

---

### Task 8: Expose V2 Workflow and Configuration HTTP Contracts

**Files:**
- Modify: `backend/internal/handler/quota_reset.go`
- Modify: `backend/internal/handler/quota_reset_test.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/internal/quotareset/types.go`
- Modify: `backend/internal/quotareset/workflow_summary.go`

- [ ] **Step 1: Write failing handler contract tests**

Add:

```go
func TestQuotaResetApproveRequiresNodeAndCommentForV2(t *testing.T)
func TestQuotaResetWorkflowAdvancedReturnsLatestSummaryDetails(t *testing.T)
func TestQuotaResetApproverCandidatesAcceptSearchAndPagination(t *testing.T)
func TestQuotaResetApprovalChainRoutesListOptionsAndSave(t *testing.T)
func TestQuotaResetNotificationSettingsUseExplicitChannelAndRedactedURL(t *testing.T)
func TestQuotaResetNotificationTestReturnsCoverageWarning(t *testing.T)
func TestQuotaResetDirectoryUnavailableMapsToServiceUnavailable(t *testing.T)
```

The approve request body is:

```json
{"request_node_id":456,"decision_reason":"Approved for the release investigation."}
```

The stale response must be HTTP 409 with:

```json
{
  "message": "workflow_advanced",
  "details": {
    "request": {
      "id": 123,
      "workflow": {"current_node": {"id": 457}}
    }
  }
}
```

- [ ] **Step 2: Run handler tests and verify failure**

Run:

```bash
cd backend && go test ./internal/handler -run TestQuotaReset -count=1
```

Expected: FAIL on missing routes and payload fields.

- [ ] **Step 3: Extend service interface and request payloads**

Add to `quotaResetService`:

```go
ListApprovalChains(context.Context) (*quotareset.ApprovalChainListResponse, error)
SaveApprovalChains(context.Context, quotareset.SaveApprovalChainsInput) (*quotareset.ApprovalChainListResponse, error)
ListApprovalChainOptions(context.Context) (*quotareset.ApprovalChainOptionsResponse, error)
GetRequestSummary(context.Context, int, int, bool) (*quotareset.RequestSummary, error)
TestNotificationSettings(context.Context, int) (*quotareset.NotificationTestResult, error)
```

Change candidate signature:

```go
ListApproverCandidates(context.Context, quotareset.ApproverCandidateParams) (*quotareset.ApproverCandidateListResponse, error)
```

Extend payloads:

```go
type quotaResetDecisionRequest struct {
	RequestNodeID int `json:"request_node_id"`
	Reason string `json:"reason"`
	DecisionReason string `json:"decision_reason"`
}

type quotaResetSaveApprovalChainsRequest struct {
	Items []quotareset.ApprovalChainInput `json:"items"`
}

type quotaResetNotificationSettingsRequest struct {
	Enabled bool `json:"enabled"`
	ChannelType string `json:"channel_type"`
	URL *string `json:"url"`
	AuthType string `json:"auth_type"`
	CredentialID *int `json:"credential_id"`
}
```

Pass `RequestNodeID` into `DecisionInput`.

- [ ] **Step 4: Add candidate and chain handlers**

Candidate query:

```go
resp, err := h.service.ListApproverCandidates(c.Request.Context(), quotareset.ApproverCandidateParams{
	SourceID: parseOptionalInt(c.Query("source_id")),
	Query: strings.TrimSpace(c.Query("q")),
	Page: parseOptionalInt(c.Query("page")),
	PageSize: parseOptionalInt(c.Query("page_size")),
})
```

Reuse the package-level `parseOptionalInt` helper already defined in
`backend/internal/handler/events.go`; do not add another query parser.

Add handlers `ListApprovalChains`, `SaveApprovalChains`, and
`ListApprovalChainOptions`. `SaveApprovalChains` obtains the authenticated admin
id and passes full-list replace input.

Register:

```go
adminQuotaResetGroup.GET("/approval-chains", quotaResetHandler.ListApprovalChains)
adminQuotaResetGroup.PUT("/approval-chains", quotaResetHandler.SaveApprovalChains)
adminQuotaResetGroup.GET("/approval-chain-options", quotaResetHandler.ListApprovalChainOptions)
```

- [ ] **Step 5: Return viewer-aware summaries and typed stale details**

Implement the exact service method below using Task 5's summary loader:

```go
func (s *Service) GetRequestSummary(ctx context.Context, requestID, viewerUserID int, admin bool) (*RequestSummary, error)
```

After successful create/cancel/approve/reject/retry, handlers pass the
authenticated viewer id and route admin flag, then return this summary instead
of `quotaResetEntResponse`.

Extend `WorkflowAdvancedError`:

```go
type WorkflowAdvancedError struct {
	RequestID int
	Latest *RequestSummary
}
```

When a stale transition is detected, load the latest summary after the
transaction closes and attach it. In `writeQuotaResetError`:

```go
var advanced *quotareset.WorkflowAdvancedError
if errors.As(err, &advanced) {
	pkg.ErrorWithDetails(c, http.StatusConflict, quotareset.ErrWorkflowAdvanced.Error(), gin.H{
		"request": advanced.Latest,
	})
	return
}
```

Map `ErrDirectoryUnavailable` to HTTP 503, not 422. Map
`ErrApproverConfigReferenced` to HTTP 409.

Return `NotificationTestResult` directly from the notification test handler so
the admin UI can distinguish delivered-without-mention from a fully mentioned
Enterprise WeChat test.

- [ ] **Step 6: Run handler and router tests**

Run:

```bash
cd backend && go test ./internal/handler -run TestQuotaReset -count=1
cd backend && go test ./internal/handler ./internal/quotareset -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit HTTP contracts**

```bash
git add backend/internal/handler backend/internal/quotareset
git commit -m "feat(backend): expose quota reset workflow APIs"
```

---

### Task 9: Add Frontend Workflow Types and API Clients

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/api/quotaReset.ts`
- Modify: `frontend/src/__tests__/quota-reset-api.test.ts`

- [ ] **Step 1: Write failing API call-shape tests**

Add assertions:

```ts
listQuotaResetApproverCandidates({ source_id: 1, q: 'alice', page: 2, page_size: 20 })
expect(client.get).toHaveBeenCalledWith('/admin/quota-reset/approver-candidates', {
  params: { source_id: 1, q: 'alice', page: 2, page_size: 20 },
})

approveQuotaResetRequest(123, {
  request_node_id: 456,
  decision_reason: 'Approved for the release investigation.',
})
expect(client.post).toHaveBeenCalledWith('/user/quota-reset/approvals/123/approve', {
  request_node_id: 456,
  decision_reason: 'Approved for the release investigation.',
})

approveQuotaResetRequest(124)
expect(client.post).toHaveBeenCalledWith('/user/quota-reset/approvals/124/approve', {})

getQuotaResetApprovalChains()
getQuotaResetApprovalChainOptions()
saveQuotaResetApprovalChains([])
expect(client.get).toHaveBeenCalledWith('/admin/quota-reset/approval-chains')
expect(client.get).toHaveBeenCalledWith('/admin/quota-reset/approval-chain-options')
expect(client.put).toHaveBeenCalledWith('/admin/quota-reset/approval-chains', { items: [] })

testQuotaResetNotificationSettings()
expect(client.post).toHaveBeenCalledWith('/admin/quota-reset/notification-settings/test')
```

- [ ] **Step 2: Run API tests and verify failure**

Run:

```bash
cd frontend && npm test -- quota-reset-api
```

Expected: FAIL because v2 types and clients do not exist.

- [ ] **Step 3: Add exact TypeScript contracts**

Add:

```ts
export type QuotaResetNodeStatus =
  | 'queued'
  | 'active'
  | 'approved'
  | 'satisfied_by_prior_approval'
  | 'skipped_no_approver'
  | 'rejected'

export interface QuotaResetDepartmentSnapshot {
  external_id: string
  display_path: string
  resolution: 'configured' | 'directory_representative'
}

export interface QuotaResetNodeApprover {
  user_id: number
  display_name: string
  email: string
  source: 'configured' | 'directory_representative'
}

export interface QuotaResetDecision {
  id: number
  node_id: number
  actor_user_id: number
  actor_display_name: string
  decision: 'approve' | 'reject'
  comment: string
  admin_override: boolean
  created_at: string
}

export interface QuotaResetWorkflowNode {
  id: number
  position: number
  node_type: 'requester_departments' | 'configured_department'
  label: string
  departments: QuotaResetDepartmentSnapshot[]
  status: QuotaResetNodeStatus
  admin_fallback_required: boolean
  approvers: QuotaResetNodeApprover[]
  satisfied_by_decision_id?: number | null
}

export interface QuotaResetWorkflow {
  version: number
  current_node?: QuotaResetWorkflowNode | null
  nodes: QuotaResetWorkflowNode[]
  decisions: QuotaResetDecision[]
  can_approve: boolean
  can_reject: boolean
  can_cancel: boolean
  can_retry: boolean
}
```

Add the following to `QuotaResetRequestSummary`:

```ts
requester_department_paths: string[]
workflow?: QuotaResetWorkflow
```

Add chain, option, and candidate pagination types exactly matching Task 8. Change
notification settings:

```ts
export type QuotaResetNotificationChannel = 'wecom_group_robot' | 'generic_webhook'

export interface QuotaResetNotificationSettings {
  enabled: boolean
  channel_type: QuotaResetNotificationChannel
  template_version: number
  url_configured: boolean
  url_preview: string
  auth_type: 'none' | 'bearer_token'
  credential_id?: number | null
  updated_at?: string
}

export interface QuotaResetNotificationSettingsInput {
  enabled: boolean
  channel_type: QuotaResetNotificationChannel
  url?: string | null
  auth_type: 'none' | 'bearer_token'
  credential_id?: number | null
}

export interface QuotaResetNotificationTestResult {
  delivered: boolean
  recipient_count: number
  missing_recipient_count: number
  warning?: 'wecom_recipient_unavailable' | string
}
```

- [ ] **Step 4: Implement API clients**

Add strict v2 input plus legacy-compatible approve/reject inputs:

```ts
export interface QuotaResetWorkflowDecisionInput {
  request_node_id: number
  decision_reason: string
}

export type QuotaResetApproveInput =
  | QuotaResetWorkflowDecisionInput
  | { request_node_id?: never; decision_reason?: string }

export type QuotaResetRejectInput =
  | QuotaResetWorkflowDecisionInput
  | { request_node_id?: never; decision_reason: string }
```

Use `QuotaResetApproveInput` for user/admin approve with an empty-object default,
and `QuotaResetRejectInput` for user/admin reject. New v2 callers always send
both required fields; existing v1 requests can still approve with `{}` and
reject with the existing comment-only body. Candidate params become:

```ts
export interface QuotaResetApproverCandidateParams {
  source_id?: number
  q?: string
  page?: number
  page_size?: number
}
```

Add:

```ts
export function getQuotaResetApprovalChains() {
  return client.get<ApiResponse<QuotaResetApprovalChainListResponse>>('/admin/quota-reset/approval-chains')
}

export function getQuotaResetApprovalChainOptions() {
  return client.get<ApiResponse<QuotaResetApprovalChainOptionsResponse>>('/admin/quota-reset/approval-chain-options')
}

export function saveQuotaResetApprovalChains(items: QuotaResetApprovalChainInput[]) {
  return client.put<ApiResponse<QuotaResetApprovalChainListResponse>>('/admin/quota-reset/approval-chains', { items })
}
```

Use `QuotaResetNotificationSettingsInput` for update.
Change `testQuotaResetNotificationSettings` to return
`ApiResponse<QuotaResetNotificationTestResult>` and pass the result through to
the settings component; do not collapse a coverage warning into a generic
success message.

- [ ] **Step 5: Run API tests and typecheck**

Run:

```bash
cd frontend && npm test -- quota-reset-api
cd frontend && npx vue-tsc -b
```

Expected: PASS.

- [ ] **Step 6: Commit frontend contracts**

```bash
git add frontend/src/types/index.ts frontend/src/api/quotaReset.ts frontend/src/__tests__/quota-reset-api.test.ts
git commit -m "feat(frontend): add quota reset workflow contracts"
```

---

### Task 10: Split and Upgrade Admin Approval Settings

**Files:**
- Modify: `frontend/src/components/settings/QuotaResetApprovalSettings.vue`
- Create: `frontend/src/components/settings/DepartmentApproverSettings.vue`
- Create: `frontend/src/components/settings/SubscriptionGroupApprovalChains.vue`
- Create: `frontend/src/components/settings/QuotaResetNotificationSettings.vue`
- Modify: `frontend/src/__tests__/quota-reset-approval-settings.test.ts`
- Modify: `frontend/src/__tests__/settings-view.test.ts`
- Modify: `frontend/src/i18n.ts`

- [ ] **Step 1: Write failing settings interaction tests**

Add tests that assert:

```ts
it('searches all matched users and shows WeCom mention coverage')
it('adds and reorders configured department nodes for one subscription group')
it('prevents duplicate departments in one chain')
it('saves every subscription group chain atomically')
it('selects an explicit notification channel and never displays the saved robot key')
it('shows a preset WeCom preview instead of a raw template editor')
it('shows a warning when a WeCom test is delivered without an at-mention')
it('preserves an existing URL when the admin does not replace it')
```

The candidate test must select Department Alpha, search `alice`, and assert the
API call no longer contains `department_external_id`:

```ts
expect(api.listQuotaResetApproverCandidates).toHaveBeenCalledWith({
  source_id: 1,
  q: 'alice',
  page: 1,
  page_size: 20,
})
expect(wrapper.text()).toContain('Alice')
expect(wrapper.text()).toContain('Can mention in WeCom')
```

The chain reorder test starts with Alpha then Beta, clicks the Beta up button,
saves, and expects `[Beta, Alpha]`.

- [ ] **Step 2: Run settings tests and verify failure**

Run:

```bash
cd frontend && npm test -- quota-reset-approval-settings settings-view
```

Expected: FAIL on missing components and contracts.

- [ ] **Step 3: Reduce the parent to orchestration**

Replace the 552-line parent with:

```vue
<script setup lang="ts">
import { ref } from 'vue'
import DepartmentApproverSettings from './DepartmentApproverSettings.vue'
import SubscriptionGroupApprovalChains from './SubscriptionGroupApprovalChains.vue'
import QuotaResetNotificationSettings from './QuotaResetNotificationSettings.vue'
import { useI18n } from '@/i18n'
import type { Credential } from '@/types'

defineProps<{ credentials: Credential[] }>()
const { t } = useI18n()
const approverRevision = ref(0)
</script>

<template>
  <section class="space-y-4" data-testid="quota-reset-approval-settings">
    <div>
      <h3 class="text-lg font-semibold text-gray-900">{{ t('quotaResetSettings.title') }}</h3>
      <p class="mt-1 text-sm text-gray-500">{{ t('quotaResetSettings.subtitle') }}</p>
    </div>
    <DepartmentApproverSettings @saved="approverRevision += 1" />
    <SubscriptionGroupApprovalChains :approver-revision="approverRevision" />
    <QuotaResetNotificationSettings :credentials="credentials" />
  </section>
</template>
```

Do not wrap child sections in another card. Each child owns one bordered
settings surface.

- [ ] **Step 4: Implement searchable department approvers**

Move the current department selector, latest-request-wins search sequence,
config table, and full-list save into `DepartmentApproverSettings.vue`.

Changes:

1. Department selection still uses current Directory Sync.
2. After department selection, the approver control is a searchable dropdown
   over all matched users.
3. Search starts when the dropdown opens and filters inside the dropdown.
4. Candidate rows show name, email, department paths, and mention coverage.
5. Save errors surface backend chain-reference details.

Use a separate sequence for user searches:

```ts
let candidateRequestSequence = 0

async function searchCandidates() {
  const sequence = ++candidateRequestSequence
  candidateLoading.value = true
  try {
    const response = await listQuotaResetApproverCandidates({
      source_id: selectedDirectorySourceID.value ?? undefined,
      q: candidateSearch.value.trim(),
      page: 1,
      page_size: 20,
    })
    if (sequence !== candidateRequestSequence) return
    candidates.value = response.data.data?.items ?? []
  } finally {
    if (sequence === candidateRequestSequence) candidateLoading.value = false
  }
}
```

Use backend names rather than raw ids in existing rows. Emit `saved` after a
successful full replacement.

- [ ] **Step 5: Implement the subscription-group chain editor**

Create `SubscriptionGroupApprovalChains.vue` with:

```ts
const groups = ref<QuotaResetApprovalChainGroupOption[]>([])
const departments = ref<QuotaResetApprovalChainDepartmentOption[]>([])
const chains = ref<QuotaResetApprovalChainInput[]>([])
const selectedGroupKey = ref('')

const selectedChain = computed(() => {
  const [providerID, groupID] = selectedGroupKey.value.split(':')
  return chains.value.find(
    chain => chain.provider_id === Number(providerID) && chain.group_id === groupID,
  )
})

function moveNode(index: number, offset: -1 | 1) {
  const nodes = selectedChain.value?.nodes
  if (!nodes) return
  const target = index + offset
  if (target < 0 || target >= nodes.length) return
  const next = [...nodes]
  ;[next[index], next[target]] = [next[target], next[index]]
  selectedChain.value!.nodes = next
}
```

The UI must:

1. Load options and saved chains together.
2. Create an empty local chain when an unconfigured group is selected.
3. Add a department only when it is not already in that chain.
4. Use fixed-size icon buttons for up, down, and delete with accessible labels.
5. Save the complete `chains` array through one API call.
6. Reload when `approverRevision` changes and show stale reference errors.

Do not use drag-and-drop; explicit controls are keyboard accessible and stable
on mobile.

- [ ] **Step 6: Implement explicit notification channel settings**

Create `QuotaResetNotificationSettings.vue`:

```ts
const form = ref<QuotaResetNotificationSettingsInput>({
  enabled: false,
  channel_type: 'wecom_group_robot',
  auth_type: 'none',
  credential_id: null,
})
const existingURLPreview = ref('')
const replacementURL = ref('')

function payload(): QuotaResetNotificationSettingsInput {
  return {
    enabled: form.value.enabled,
    channel_type: form.value.channel_type,
    auth_type: form.value.channel_type === 'wecom_group_robot' ? 'none' : form.value.auth_type,
    credential_id: form.value.channel_type === 'generic_webhook' && form.value.auth_type === 'bearer_token'
      ? form.value.credential_id
      : null,
    ...(replacementURL.value.trim() ? { url: replacementURL.value.trim() } : {}),
  }
}
```

Render:

1. A channel `<select>` for WeCom and Generic.
2. Existing redacted endpoint preview.
3. A replacement URL input that never receives the saved secret.
4. Bearer controls only for Generic.
5. A read-only synthetic preview. WeCom preview includes requester, team,
   reason, node, progress, and a visible `@Bob` illustration.
6. Test/save buttons and backend warning/error detail. A delivered test with
   `warning="wecom_recipient_unavailable"` uses warning styling and explicitly
   says the message arrived without an `@` mention.
7. No raw JSON or template editor.

- [ ] **Step 7: Add bilingual settings copy**

Add exact English and Chinese keys for:

```ts
'quotaResetSettings.chains'
'quotaResetSettings.chainGroup'
'quotaResetSettings.chainEmpty'
'quotaResetSettings.addNode'
'quotaResetSettings.moveUp'
'quotaResetSettings.moveDown'
'quotaResetSettings.removeNode'
'quotaResetSettings.saveChains'
'quotaResetSettings.channelType'
'quotaResetSettings.channelWeCom'
'quotaResetSettings.channelGeneric'
'quotaResetSettings.endpointConfigured'
'quotaResetSettings.replaceEndpoint'
'quotaResetSettings.presetPreview'
'quotaResetSettings.weComMentionAvailable'
'quotaResetSettings.weComMentionUnavailable'
'quotaResetSettings.weComTestMentionUnavailable'
```

Use concise Chinese product copy, not implementation guidance.

- [ ] **Step 8: Run settings tests and production typecheck**

Run:

```bash
cd frontend && npm test -- quota-reset-approval-settings settings-view
cd frontend && npx vue-tsc -b
```

Expected: PASS.

- [ ] **Step 9: Commit admin settings**

```bash
git add frontend/src/components/settings frontend/src/__tests__/quota-reset-approval-settings.test.ts frontend/src/__tests__/settings-view.test.ts frontend/src/i18n.ts
git commit -m "feat(frontend): configure quota reset approval workflows"
```

---

### Task 11: Add Node Timeline and Mandatory Decision Comments

**Files:**
- Create: `frontend/src/components/quota-reset/QuotaResetWorkflowTimeline.vue`
- Create: `frontend/src/components/quota-reset/QuotaResetDecisionDialog.vue`
- Modify: `frontend/src/components/quota-reset/QuotaResetRequestList.vue`
- Modify: `frontend/src/views/QuotaResetView.vue`
- Modify: `frontend/src/__tests__/quota-reset-view.test.ts`
- Modify: `frontend/src/i18n.ts`

- [ ] **Step 1: Write failing workflow UI tests**

Add:

```ts
it('renders ordered node status and prior-approval attribution')
it('uses backend can_approve instead of queue mode')
it('requires a comment for approve and reject')
it('submits the current request_node_id')
it('refreshes from workflow_advanced details')
it('does not render queued future work as actionable')
it('shows requester display name and every direct team path')
it('keeps legacy v1 approve and reject actions usable without request_node_id')
```

The approve test:

```ts
await wrapper.get('[data-testid="quota-reset-approve-1"]').trigger('click')
const dialog = wrapper.get('[data-testid="quota-reset-decision-dialog"]')
await dialog.get('button[data-testid="quota-reset-decision-submit"]').trigger('click')
expect(dialog.text()).toContain('Comment is required')
await dialog.get('textarea').setValue('Approved for the release investigation.')
await dialog.get('button[data-testid="quota-reset-decision-submit"]').trigger('click')
expect(api.approveQuotaResetRequest).toHaveBeenCalledWith(1, {
  request_node_id: 456,
  decision_reason: 'Approved for the release investigation.',
})
```

- [ ] **Step 2: Run workflow view tests and verify failure**

Run:

```bash
cd frontend && npm test -- quota-reset-view
```

Expected: FAIL on missing timeline and dialog.

- [ ] **Step 3: Implement the workflow timeline**

Create `QuotaResetWorkflowTimeline.vue` with one ordered list, not nested cards:

```vue
<ol class="divide-y divide-slate-200" data-testid="quota-reset-workflow-timeline">
  <li
    v-for="node in workflow.nodes"
    :key="node.id"
    class="grid gap-2 py-3 sm:grid-cols-[2rem_minmax(0,1fr)]"
  >
    <span class="flex h-7 w-7 items-center justify-center rounded-full text-xs font-semibold" :class="nodeDotClass(node.status)">
      {{ node.position + 1 }}
    </span>
    <div class="min-w-0">
      <div class="flex flex-wrap items-center gap-2">
        <p class="text-sm font-semibold text-slate-900">{{ node.label }}</p>
        <span class="text-xs text-slate-500">{{ nodeStatusLabel(node.status) }}</span>
      </div>
      <p class="mt-1 break-words text-xs text-slate-500">{{ approverNames(node) }}</p>
      <p v-if="node.status === 'satisfied_by_prior_approval'" class="mt-2 text-sm text-emerald-700">
        {{ reusedDecisionLabel(node) }}
      </p>
      <p v-else-if="decisionFor(node)" class="mt-2 whitespace-pre-wrap break-words text-sm text-slate-700">
        {{ decisionFor(node)?.actor_display_name }}: {{ decisionFor(node)?.comment }}
      </p>
    </div>
  </li>
</ol>
```

Use stable dimensions and wrapping. Do not expose notification ids.

- [ ] **Step 4: Implement one mandatory-comment decision dialog**

Create `QuotaResetDecisionDialog.vue`:

```ts
const props = defineProps<{
  open: boolean
  mode: 'approve' | 'reject'
  request: QuotaResetRequestSummary | null
  busy?: boolean
}>()

const emit = defineEmits<{
  close: []
  submit: [{ request_node_id?: number; decision_reason: string }]
}>()

const comment = ref('')
const error = ref('')

function submit() {
  const value = comment.value.trim()
  const isV2 = (props.request?.workflow?.version ?? 1) >= 2
  const nodeID = props.request?.workflow?.current_node?.id
  if ((isV2 || props.mode === 'reject') && !value) {
    error.value = t('quotaReset.commentRequired')
    return
  }
  if (isV2 && !nodeID) {
    error.value = t('quotaReset.workflowAdvanced')
    return
  }
  emit('submit', {
    ...(nodeID ? { request_node_id: nodeID } : {}),
    decision_reason: value,
  })
}
```

Render a normal modal with title, current-node label, textarea, cancel, and
mode-specific command button. Reset local state whenever it opens for a new
request.

- [ ] **Step 5: Make backend permissions authoritative**

In `QuotaResetRequestList.vue`:

```ts
function canCancel(item: QuotaResetRequestSummary) {
  return item.workflow?.can_cancel ?? (props.mode === 'mine' && item.status === 'pending')
}

function canDecide(item: QuotaResetRequestSummary) {
  if (item.workflow) {
    return Boolean(item.workflow.can_approve || item.workflow.can_reject)
  }
  return (props.mode === 'approvals' || props.mode === 'admin') && item.status === 'pending'
}

function canRetry(item: QuotaResetRequestSummary) {
  return item.workflow?.can_retry ?? (
    (props.mode === 'approvals' || props.mode === 'admin') &&
    item.status === 'approved_reset_failed'
  )
}
```

The fallback is only for legacy v1 summaries. Emit `select` to open a request
detail dialog containing requester, teams, reason, reset result, and timeline.

- [ ] **Step 6: Replace prompt actions and handle stale workflow**

Remove `window.prompt`. Store selected request and decision mode. Submit through
the user or admin API according to the active queue. V2 approve and reject open
the mandatory-comment dialog and send the current node id. Legacy v1 approve
keeps the existing immediate empty-body action; legacy v1 reject uses the same
dialog for its already-required comment but omits `request_node_id`.

On HTTP 409 `workflow_advanced`, replace the matching local row with
`error.response.data.details.request`, close the stale dialog, refresh counts,
and show a neutral “workflow advanced” toast instead of a generic failure.

All other successful actions reload queues with forced Work Items counts, using
the existing queued refresh behavior.

- [ ] **Step 7: Add bilingual workflow copy**

Add keys for:

```ts
'quotaReset.comment'
'quotaReset.commentRequired'
'quotaReset.approveCommentPlaceholder'
'quotaReset.rejectCommentPlaceholder'
'quotaReset.workflowAdvanced'
'quotaReset.node.queued'
'quotaReset.node.active'
'quotaReset.node.approved'
'quotaReset.node.satisfied_by_prior_approval'
'quotaReset.node.skipped_no_approver'
'quotaReset.node.rejected'
'quotaReset.adminOverride'
'quotaReset.adminFallback'
'quotaReset.requesterTeams'
```

- [ ] **Step 8: Run focused tests and build**

Run:

```bash
cd frontend && npm test -- quota-reset-view quota-reset-api work-items-store
cd frontend && npm run build
```

Expected: PASS.

- [ ] **Step 9: Commit workflow UI**

```bash
git add frontend/src/components/quota-reset frontend/src/views/QuotaResetView.vue frontend/src/__tests__/quota-reset-view.test.ts frontend/src/i18n.ts
git commit -m "feat(frontend): show multi-stage quota reset approvals"
```

---

### Task 12: Synchronize Docs and Complete Full-Stack Verification

**Files:**
- Create: `frontend/e2e_quota_reset_workflow.py`
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-07-10-multi-stage-quota-reset-approval-design.md`
- Modify: `docs/superpowers/plans/2026-07-10-multi-stage-quota-reset-approval.md`

- [ ] **Step 1: Add deterministic browser workflow coverage**

Create `frontend/e2e_quota_reset_workflow.py` using the existing
`e2e_role_test.py` Playwright style. It must:

1. Seed authenticated requester, approver, and admin sessions through mocked
   auth endpoints.
2. Mock `/work-items/counts`, quota request lists, chain options/config, and
   decision transitions with synthetic data only.
3. Verify an active approver sees one badge and one enabled action.
4. Verify a future queued approver sees no actionable request.
5. Verify approve dialog blocks empty comment and submits `request_node_id`.
6. Return a response where the same actor satisfies two later nodes and verify
   both render as prior-approval satisfied.
7. Verify admin settings reorder a chain and select `wecom_group_robot`.
8. Capture screenshots at 1280x800 and 390x844.
9. Assert no horizontal document overflow and no control overlap.

Use:

```python
def assert_no_horizontal_overflow(page):
    overflow = page.evaluate(
        "() => document.documentElement.scrollWidth > document.documentElement.clientWidth"
    )
    assert not overflow, "page has horizontal overflow"

def decision_payload(route):
    body = json.loads(route.request.post_data or "{}")
    assert body == {
        "request_node_id": 456,
        "decision_reason": "Approved for the release investigation.",
    }
    route.fulfill(
        status=200,
        content_type="application/json",
        body=json.dumps({"code": 200, "data": APPROVED_REQUEST}),
    )
```

Exit non-zero on any assertion and write screenshots only under
`/tmp/ae-e2e-quota-reset`.

- [ ] **Step 2: Run all focused backend suites**

Run:

```bash
cd backend && go test ./internal/quotareset ./internal/workitems ./internal/handler -count=1
cd backend && go test -race ./internal/quotareset -run 'TestWorkflowDecisionRejectsStaleNode' -count=1
```

Expected: PASS.

- [ ] **Step 3: Run the full backend suite**

Run:

```bash
cd backend && go test ./...
```

Expected: PASS.

- [ ] **Step 4: Run focused and full frontend suites**

Run:

```bash
cd frontend && npm test -- quota-reset-api quota-reset-approval-settings quota-reset-view work-items-store work-items-view settings-view
cd frontend && npm test
cd frontend && npm run build
```

Expected: all Vitest files pass and the production build succeeds.

- [ ] **Step 5: Run role and quota-reset browser checks**

Start Vite in a persistent terminal:

```bash
cd frontend && npm run dev -- --host 127.0.0.1
```

Then run:

```bash
cd frontend && npm run test:e2e:role
cd frontend && python3 e2e_quota_reset_workflow.py
```

Expected: both scripts exit 0. Inspect desktop and mobile screenshots for
clipped text, incoherent overlap, empty dialogs, and unstable control sizing.
Stop the Vite session after verification.

- [ ] **Step 6: Rebuild the source-based development stack**

Run from the repository root:

```bash
docker compose -p ai-efficiency \
  --env-file /Users/admin/ai-efficiency/deploy/.env \
  -f deploy/docker-compose.dev.yml \
  up -d --build --force-recreate --remove-orphans
```

Expected: PostgreSQL, Redis, and backend become healthy. Verify:

```bash
curl -fsS http://localhost:18081/api/v1/health/ready
docker compose -p ai-efficiency --env-file /Users/admin/ai-efficiency/deploy/.env -f deploy/docker-compose.dev.yml ps
```

Report environment or proxy failures separately; do not mark this step complete
unless the stack was actually rebuilt and readiness succeeded.

- [ ] **Step 7: Update current architecture and spec status**

Update `docs/architecture.md`:

1. Replace the single-node quota reset description with exact-department
   initial-node resolution, subscription-group chains, approval reuse, and
   current-node Work Items counts.
2. Document normalized chain/node/approver/decision ownership under
   `backend/internal/quotareset`.
3. Document explicit notification channel adapters and Enterprise WeChat
   Markdown mentions.
4. Keep relay integration through `relay.UserSubscriptionQuotaResetter`.

Change the new spec header to:

```markdown
**Status:** Current implemented contract
```

Add an implementation note with the final commit and verification date. Do not
rewrite the 2026-07-07 historical spec.

- [ ] **Step 8: Record verification progress in this live plan**

Check only steps actually completed. Set:

```markdown
**Status:** Verification in progress
```

Keep browser and compose boxes unchecked until they have real evidence. List any
remaining gap in English at the top.

- [ ] **Step 9: Run final hygiene checks**

Run:

```bash
git diff --check
rg -n 'T[B]D|T[O]DO|F[I]XME|b1d[a]b|qyapi[.]weixin[.]qq[.]com/cgi-bin/webhook/send[?]key=' \
  docs/superpowers/specs/2026-07-10-multi-stage-quota-reset-approval-design.md \
  docs/superpowers/plans/2026-07-10-multi-stage-quota-reset-approval.md \
  backend/internal/quotareset frontend/src frontend/e2e_quota_reset_workflow.py
git status --short
```

Expected: no whitespace errors, placeholders, real webhook key, or unrelated
files.

- [ ] **Step 10: Commit browser coverage and current docs**

```bash
git add frontend/e2e_quota_reset_workflow.py
git commit -m "test(frontend): cover quota reset workflow in browser"

git add docs/architecture.md \
  docs/superpowers/specs/2026-07-10-multi-stage-quota-reset-approval-design.md \
  docs/superpowers/plans/2026-07-10-multi-stage-quota-reset-approval.md
git commit -m "docs(architecture): document multi-stage quota reset approvals"
```

- [ ] **Step 11: Mark the plan complete only after every step has evidence**

Record final test counts, browser screenshots, compose health result, and commit
ids in the plan's completion note. Check every completed step, set:

```markdown
**Status:** Complete
```

Then commit only the live ledger update:

```bash
git add docs/superpowers/plans/2026-07-10-multi-stage-quota-reset-approval.md
git commit -m "docs(plan): complete quota reset workflow implementation"
```
