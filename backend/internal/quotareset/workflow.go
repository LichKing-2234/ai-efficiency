package quotareset

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	workflowVersionV2    = 2
	maxWorkflowSteps     = 21
	maxWorkflowApprovers = 100

	WorkflowStepRequesterDepartments = "requester_departments"
	WorkflowStepConfiguredDepartment = "configured_department"

	WorkflowStepQueued    = "queued"
	WorkflowStepActive    = "active"
	WorkflowStepApproved  = "approved"
	WorkflowStepSatisfied = "satisfied_by_prior_approval"
	WorkflowStepRejected  = "rejected"
)

type Workflow struct {
	Version     int            `json:"version"`
	CurrentStep int            `json:"current_step"`
	Requester   WorkflowPerson `json:"requester"`
	Steps       []WorkflowStep `json:"steps"`
}

type WorkflowPerson struct {
	UserID          int               `json:"user_id"`
	DisplayName     string            `json:"display_name"`
	Email           string            `json:"email"`
	DepartmentPaths []string          `json:"department_paths,omitempty"`
	NotificationIDs map[string]string `json:"notification_ids,omitempty"`
}

type WorkflowApprover = WorkflowPerson

type WorkflowStep struct {
	Kind                  string             `json:"kind"`
	Label                 string             `json:"label"`
	DepartmentExternalIDs []string           `json:"department_external_ids"`
	Approvers             []WorkflowApprover `json:"approvers"`
	AdminFallback         bool               `json:"admin_fallback"`
	Status                string             `json:"status"`
	Decision              *WorkflowDecision  `json:"decision,omitempty"`
	SatisfiedByStep       *int               `json:"satisfied_by_step,omitempty"`
}

type WorkflowDecision struct {
	ActorUserID      int       `json:"actor_user_id"`
	ActorDisplayName string    `json:"actor_display_name"`
	Comment          string    `json:"comment"`
	Approve          bool      `json:"approve"`
	Admin            bool      `json:"admin"`
	DecidedAt        time.Time `json:"decided_at"`
}

type WorkflowDecisionInput WorkflowDecision

func EncodeWorkflow(workflow *Workflow) (map[string]any, error) {
	if err := workflow.validate(); err != nil {
		return nil, err
	}
	result, err := convertJSON[map[string]any](workflow)
	if err != nil {
		return nil, fmt.Errorf("%w: normalize: %v", ErrInvalidWorkflow, err)
	}
	return result, nil
}

func DecodeWorkflow(raw map[string]any) (*Workflow, error) {
	workflow, err := convertJSON[Workflow](raw)
	if err != nil {
		return nil, fmt.Errorf("%w: decode stored document: %v", ErrInvalidWorkflow, err)
	}
	if err := workflow.validate(); err != nil {
		return nil, err
	}
	return &workflow, nil
}

func (w *Workflow) ActiveApproverUserIDs() []int {
	if w == nil || w.CurrentStep < 0 || w.CurrentStep >= len(w.Steps) || w.Steps[w.CurrentStep].Status != WorkflowStepActive {
		return []int{}
	}
	return workflowApproverUserIDs(w.Steps[w.CurrentStep].Approvers)
}

func (w *Workflow) Decide(input WorkflowDecisionInput) ([]int, error) {
	if err := w.validate(); err != nil {
		return nil, err
	}
	if w.CurrentStep >= len(w.Steps) {
		return nil, ErrInvalidStatus
	}
	comment := strings.TrimSpace(input.Comment)
	if comment == "" {
		return nil, ErrDecisionRequired
	}
	if input.ActorUserID <= 0 {
		return nil, ErrNotApprover
	}
	if !input.Admin && input.ActorUserID == w.Requester.UserID {
		return nil, ErrSelfApprovalForbidden
	}
	stepIndex := w.CurrentStep
	step := &w.Steps[stepIndex]
	if !input.Admin && !workflowStepContainsApprover(*step, input.ActorUserID) {
		return nil, ErrNotApprover
	}
	decision := WorkflowDecision(input)
	decision.ActorDisplayName = strings.TrimSpace(decision.ActorDisplayName)
	decision.Comment = comment
	step.Decision = &decision
	if !input.Approve {
		step.Status = WorkflowStepRejected
		w.CurrentStep = len(w.Steps)
		return nil, nil
	}

	step.Status = WorkflowStepApproved
	var satisfiedSteps []int
	for next := stepIndex + 1; next < len(w.Steps); next++ {
		source := w.priorApprovingStepFor(next)
		if source >= 0 {
			w.Steps[next].Status = WorkflowStepSatisfied
			w.Steps[next].SatisfiedByStep = &source
			satisfiedSteps = append(satisfiedSteps, next)
			continue
		}
		w.Steps[next].Status = WorkflowStepActive
		w.CurrentStep = next
		return satisfiedSteps, nil
	}
	w.CurrentStep = len(w.Steps)
	return satisfiedSteps, nil
}

func (w *Workflow) priorApprovingStepFor(stepIndex int) int {
	return slices.IndexFunc(w.Steps[:stepIndex], func(step WorkflowStep) bool {
		decision := step.Decision
		return decision != nil && decision.Approve && workflowStepContainsApprover(w.Steps[stepIndex], decision.ActorUserID)
	})
}

func workflowStepContainsApprover(step WorkflowStep, userID int) bool {
	return slices.ContainsFunc(step.Approvers, func(approver WorkflowApprover) bool {
		return approver.UserID == userID
	})
}

func (w *Workflow) validate() error {
	if w == nil {
		return fmt.Errorf("%w: document is nil", ErrInvalidWorkflow)
	}
	if w.Version != workflowVersionV2 {
		return fmt.Errorf("%w: unsupported version %d", ErrInvalidWorkflow, w.Version)
	}
	if w.Requester.UserID <= 0 {
		return fmt.Errorf("%w: requester is missing", ErrInvalidWorkflow)
	}
	if len(w.Steps) == 0 || len(w.Steps) > maxWorkflowSteps {
		return fmt.Errorf("%w: step count %d", ErrInvalidWorkflow, len(w.Steps))
	}
	if w.CurrentStep < 0 || w.CurrentStep > len(w.Steps) {
		return fmt.Errorf("%w: current step %d", ErrInvalidWorkflow, w.CurrentStep)
	}
	uniqueApprovers := make(map[int]struct{})
	activeSteps := 0
	for index := range w.Steps {
		step := &w.Steps[index]
		if step.Kind != WorkflowStepRequesterDepartments && step.Kind != WorkflowStepConfiguredDepartment {
			return fmt.Errorf("%w: step %d kind %q", ErrInvalidWorkflow, index, step.Kind)
		}
		switch step.Status {
		case WorkflowStepQueued, WorkflowStepActive, WorkflowStepApproved, WorkflowStepSatisfied, WorkflowStepRejected:
		default:
			return fmt.Errorf("%w: step %d status %q", ErrInvalidWorkflow, index, step.Status)
		}
		if step.Status == WorkflowStepActive {
			activeSteps++
		}
		seenInStep := make(map[int]struct{}, len(step.Approvers))
		for _, approver := range step.Approvers {
			if approver.UserID <= 0 {
				return fmt.Errorf("%w: step %d has invalid approver", ErrInvalidWorkflow, index)
			}
			if _, exists := seenInStep[approver.UserID]; exists {
				return fmt.Errorf("%w: step %d repeats approver %d", ErrInvalidWorkflow, index, approver.UserID)
			}
			seenInStep[approver.UserID] = struct{}{}
			uniqueApprovers[approver.UserID] = struct{}{}
		}
		if len(step.Approvers) == 0 && !step.AdminFallback {
			return fmt.Errorf("%w: step %d has no approver", ErrInvalidWorkflow, index)
		}
		if step.SatisfiedByStep != nil && (*step.SatisfiedByStep < 0 || *step.SatisfiedByStep >= index) {
			return fmt.Errorf("%w: step %d has invalid satisfying step", ErrInvalidWorkflow, index)
		}
	}
	if len(uniqueApprovers) > maxWorkflowApprovers {
		return fmt.Errorf("%w: approver count %d", ErrInvalidWorkflow, len(uniqueApprovers))
	}
	if w.CurrentStep < len(w.Steps) {
		if activeSteps != 1 || w.Steps[w.CurrentStep].Status != WorkflowStepActive {
			return fmt.Errorf("%w: active step mismatch", ErrInvalidWorkflow)
		}
	} else if activeSteps != 0 {
		return fmt.Errorf("%w: terminal workflow has active step", ErrInvalidWorkflow)
	}
	return nil
}

func workflowApproverUserIDs(approvers []WorkflowApprover) []int {
	ids := make([]int, len(approvers))
	for index, approver := range approvers {
		ids[index] = approver.UserID
	}
	return uniqueSortedWorkflowIDs(ids)
}

func uniqueSortedWorkflowIDs(ids []int) []int {
	sort.Ints(ids)
	return slices.Compact(ids)
}

func convertJSON[T any](value any) (result T, err error) {
	raw, err := json.Marshal(value)
	if err == nil {
		err = json.Unmarshal(raw, &result)
	}
	return result, err
}
