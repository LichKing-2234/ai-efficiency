package quotareset

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestWorkflowApproveAdvancesAndReusesPriorActor(t *testing.T) {
	workflow := workflowFixture()
	when := time.Date(2026, 7, 15, 9, 30, 0, 0, time.UTC)

	satisfiedSteps, err := workflow.Decide(WorkflowDecisionInput{
		ActorUserID:      2,
		ActorDisplayName: "bob",
		Comment:          "额度异常，确认重置",
		Approve:          true,
		DecidedAt:        when,
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if len(satisfiedSteps) != 1 || satisfiedSteps[0] != 1 {
		t.Fatalf("satisfied steps = %v, want [1]", satisfiedSteps)
	}
	if got := workflow.Steps[0].Decision; got == nil || got.ActorUserID != 2 || got.Comment != "额度异常，确认重置" || !got.DecidedAt.Equal(when) {
		t.Fatalf("first decision = %+v", got)
	}
	if workflow.Steps[1].Status != WorkflowStepSatisfied || workflow.Steps[1].SatisfiedByStep == nil || *workflow.Steps[1].SatisfiedByStep != 0 {
		t.Fatalf("reused step = %+v", workflow.Steps[1])
	}
	if workflow.Steps[2].Status != WorkflowStepActive || workflow.CurrentStep != 2 {
		t.Fatalf("active state = current %d step %+v", workflow.CurrentStep, workflow.Steps[2])
	}
	if got := workflow.ActiveApproverUserIDs(); len(got) != 1 || got[0] != 3 {
		t.Fatalf("ActiveApproverUserIDs() = %v, want [3]", got)
	}

	_, err = workflow.Decide(WorkflowDecisionInput{
		ActorUserID:      3,
		ActorDisplayName: "carol",
		Comment:          "同意最终重置",
		Approve:          true,
		DecidedAt:        when.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("final Decide() error = %v", err)
	}
	if workflow.CurrentStep != len(workflow.Steps) || workflow.Steps[2].Status != WorkflowStepApproved {
		t.Fatalf("final workflow = current %d step %+v, want terminal approval", workflow.CurrentStep, workflow.Steps[2])
	}
}

func TestWorkflowRejectIsTerminal(t *testing.T) {
	workflow := workflowFixture()
	_, err := workflow.Decide(WorkflowDecisionInput{
		ActorUserID:      2,
		ActorDisplayName: "bob",
		Comment:          "信息不足",
		DecidedAt:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if workflow.CurrentStep != len(workflow.Steps) || workflow.Steps[0].Status != WorkflowStepRejected {
		t.Fatalf("step status = %q, want rejected", workflow.Steps[0].Status)
	}
}

func TestWorkflowTerminalStateCannotBeDecidedAgain(t *testing.T) {
	workflow := workflowFixture()
	workflow.CurrentStep = len(workflow.Steps)
	for index := range workflow.Steps {
		workflow.Steps[index].Status = WorkflowStepApproved
		workflow.Steps[index].Decision = &WorkflowDecision{
			ActorUserID: 2,
			Comment:     "already approved",
			Approve:     true,
			DecidedAt:   time.Now().UTC(),
		}
	}

	if _, err := workflow.Decide(WorkflowDecisionInput{ActorUserID: 2, Comment: "again", Approve: true}); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("Decide() error = %v, want ErrInvalidStatus", err)
	}
}

func TestWorkflowDecisionAuthorizationFailsClosed(t *testing.T) {
	tests := []struct {
		name      string
		input     WorkflowDecisionInput
		wantError error
	}{
		{
			name:      "comment required",
			input:     WorkflowDecisionInput{ActorUserID: 2, Approve: true},
			wantError: ErrDecisionRequired,
		},
		{
			name:      "requester cannot decide",
			input:     WorkflowDecisionInput{ActorUserID: 1, Comment: "self", Approve: true},
			wantError: ErrSelfApprovalForbidden,
		},
		{
			name:      "non candidate cannot decide",
			input:     WorkflowDecisionInput{ActorUserID: 99, Comment: "approve", Approve: true},
			wantError: ErrNotApprover,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflow := workflowFixture()
			_, err := workflow.Decide(tt.input)
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("Decide() error = %v, want %v", err, tt.wantError)
			}
		})
	}
}

func TestWorkflowAdminCanDecideOnlyActiveStep(t *testing.T) {
	workflow := workflowFixture()
	_, err := workflow.Decide(WorkflowDecisionInput{
		ActorUserID:      50,
		ActorDisplayName: "admin",
		Comment:          "admin fallback",
		Approve:          true,
		Admin:            true,
		DecidedAt:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if workflow.CurrentStep != 1 || workflow.Steps[1].Status != WorkflowStepActive {
		t.Fatalf("workflow = current %d step %+v, want active step 1", workflow.CurrentStep, workflow.Steps[1])
	}
	if workflow.Steps[0].Decision == nil || !workflow.Steps[0].Decision.Admin {
		t.Fatalf("decision = %+v, want admin decision", workflow.Steps[0].Decision)
	}
}

func TestWorkflowDecodeRejectsMalformedAndOversizedDocuments(t *testing.T) {
	if _, err := DecodeWorkflow(map[string]any{"version": 1}); !errors.Is(err, ErrInvalidWorkflow) {
		t.Fatalf("DecodeWorkflow(version 1) error = %v, want ErrInvalidWorkflow", err)
	}

	workflow := workflowFixture()
	workflow.Steps = make([]WorkflowStep, maxWorkflowSteps+1)
	encoded, err := json.Marshal(workflow)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if _, err := DecodeWorkflow(raw); !errors.Is(err, ErrInvalidWorkflow) {
		t.Fatalf("DecodeWorkflow(oversized) error = %v, want ErrInvalidWorkflow", err)
	}
}

func workflowFixture() *Workflow {
	return &Workflow{
		Version:     workflowVersionV2,
		CurrentStep: 0,
		Requester: WorkflowPerson{
			UserID:          1,
			DisplayName:     "alice",
			Email:           "alice@example.com",
			DepartmentPaths: []string{"Company / Group Alpha"},
		},
		Steps: []WorkflowStep{
			{
				Kind:                  WorkflowStepRequesterDepartments,
				Label:                 "Company / Group Alpha",
				DepartmentExternalIDs: []string{"dept-alpha"},
				Approvers: []WorkflowApprover{
					{UserID: 2, DisplayName: "bob", Email: "bob@example.org", NotificationIDs: map[string]string{"wecom": "bob"}},
				},
				Status: WorkflowStepActive,
			},
			{
				Kind:                  WorkflowStepConfiguredDepartment,
				Label:                 "Company / Group Beta",
				DepartmentExternalIDs: []string{"dept-beta"},
				Approvers: []WorkflowApprover{
					{UserID: 2, DisplayName: "bob", Email: "bob@example.org"},
				},
				Status: WorkflowStepQueued,
			},
			{
				Kind:                  WorkflowStepConfiguredDepartment,
				Label:                 "Company / Group Gamma",
				DepartmentExternalIDs: []string{"dept-gamma"},
				Approvers: []WorkflowApprover{
					{UserID: 3, DisplayName: "carol", Email: "carol@example.org"},
				},
				Status: WorkflowStepQueued,
			},
		},
	}
}
