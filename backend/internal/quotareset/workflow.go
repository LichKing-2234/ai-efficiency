package quotareset

import (
	"encoding/json"
	"fmt"
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
	DepartmentPaths []string          `json:"department_paths"`
	NotificationIDs map[string]string `json:"notification_ids"`
}

type WorkflowApprover struct {
	UserID          int               `json:"user_id"`
	DisplayName     string            `json:"display_name"`
	Email           string            `json:"email"`
	Source          string            `json:"source"`
	NotificationIDs map[string]string `json:"notification_ids"`
}

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

type WorkflowDecisionInput struct {
	RequesterUserID  int
	ActorUserID      int
	ActorDisplayName string
	Comment          string
	Approve          bool
	Admin            bool
	DecidedAt        time.Time
}

type WorkflowTransition struct {
	ActivatedStep    *int
	SatisfiedSteps   []int
	TerminalApproved bool
	TerminalRejected bool
}

func EncodeWorkflow(workflow *Workflow) (map[string]any, error) {
	if err := workflow.validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(workflow)
	if err != nil {
		return nil, fmt.Errorf("%w: encode: %v", ErrInvalidWorkflow, err)
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("%w: normalize: %v", ErrInvalidWorkflow, err)
	}
	return result, nil
}

func DecodeWorkflow(raw map[string]any) (*Workflow, error) {
	if raw == nil {
		return nil, fmt.Errorf("%w: document is missing", ErrInvalidWorkflow)
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: encode stored document: %v", ErrInvalidWorkflow, err)
	}
	var workflow Workflow
	if err := json.Unmarshal(encoded, &workflow); err != nil {
		return nil, fmt.Errorf("%w: decode stored document: %v", ErrInvalidWorkflow, err)
	}
	if err := workflow.validate(); err != nil {
		return nil, err
	}
	return &workflow, nil
}

func (w *Workflow) ActiveApproverUserIDs() []int {
	if w == nil || w.CurrentStep < 0 || w.CurrentStep >= len(w.Steps) {
		return []int{}
	}
	step := w.Steps[w.CurrentStep]
	if step.Status != WorkflowStepActive {
		return []int{}
	}
	seen := make(map[int]struct{}, len(step.Approvers))
	ids := make([]int, 0, len(step.Approvers))
	for _, approver := range step.Approvers {
		if approver.UserID <= 0 {
			continue
		}
		if _, exists := seen[approver.UserID]; exists {
			continue
		}
		seen[approver.UserID] = struct{}{}
		ids = append(ids, approver.UserID)
	}
	sort.Ints(ids)
	return ids
}

func (w *Workflow) Decide(input WorkflowDecisionInput) (WorkflowTransition, error) {
	if err := w.validate(); err != nil {
		return WorkflowTransition{}, err
	}
	if w.CurrentStep >= len(w.Steps) {
		return WorkflowTransition{}, ErrInvalidStatus
	}
	comment := strings.TrimSpace(input.Comment)
	if comment == "" {
		return WorkflowTransition{}, ErrDecisionRequired
	}
	if input.ActorUserID <= 0 {
		return WorkflowTransition{}, ErrNotApprover
	}
	if !input.Admin && input.ActorUserID == input.RequesterUserID {
		return WorkflowTransition{}, ErrSelfApprovalForbidden
	}
	stepIndex := w.CurrentStep
	step := &w.Steps[stepIndex]
	if !input.Admin && !workflowStepContainsApprover(*step, input.ActorUserID) {
		return WorkflowTransition{}, ErrNotApprover
	}
	decidedAt := input.DecidedAt
	if decidedAt.IsZero() {
		decidedAt = time.Now().UTC()
	}
	step.Decision = &WorkflowDecision{
		ActorUserID:      input.ActorUserID,
		ActorDisplayName: strings.TrimSpace(input.ActorDisplayName),
		Comment:          comment,
		Approve:          input.Approve,
		Admin:            input.Admin,
		DecidedAt:        decidedAt,
	}
	if !input.Approve {
		step.Status = WorkflowStepRejected
		w.CurrentStep = len(w.Steps)
		return WorkflowTransition{TerminalRejected: true, SatisfiedSteps: []int{}}, nil
	}

	step.Status = WorkflowStepApproved
	transition := WorkflowTransition{SatisfiedSteps: []int{}}
	for next := stepIndex + 1; next < len(w.Steps); next++ {
		source := w.priorApprovingStepFor(next)
		if source >= 0 {
			w.Steps[next].Status = WorkflowStepSatisfied
			w.Steps[next].SatisfiedByStep = intPointer(source)
			transition.SatisfiedSteps = append(transition.SatisfiedSteps, next)
			continue
		}
		w.Steps[next].Status = WorkflowStepActive
		w.CurrentStep = next
		transition.ActivatedStep = intPointer(next)
		return transition, nil
	}
	w.CurrentStep = len(w.Steps)
	transition.TerminalApproved = true
	return transition, nil
}

func (w *Workflow) priorApprovingStepFor(stepIndex int) int {
	if w == nil || stepIndex < 0 || stepIndex >= len(w.Steps) {
		return -1
	}
	for prior := 0; prior < stepIndex; prior++ {
		decision := w.Steps[prior].Decision
		if decision == nil || !decision.Approve {
			continue
		}
		if workflowStepContainsApprover(w.Steps[stepIndex], decision.ActorUserID) {
			return prior
		}
	}
	return -1
}

func workflowStepContainsApprover(step WorkflowStep, userID int) bool {
	for _, approver := range step.Approvers {
		if approver.UserID == userID {
			return true
		}
	}
	return false
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

func intPointer(value int) *int {
	return &value
}
