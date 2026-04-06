package domain

import (
	"fmt"
	"slices"
	"strings"
)

type Workflow struct {
	ID          WorkflowID
	Name        string
	EntryStepID WorkflowStepID
	Steps       []WorkflowStep
}

type WorkflowStep struct {
	ID          WorkflowStepID
	Name        string
	Kind        WorkflowStepKind
	ActorID     AgentID
	NextStepIDs []WorkflowStepID
	RetryLimit  int
}

type WorkflowStepKind string

const (
	WorkflowStepKindAgent  WorkflowStepKind = "agent"
	WorkflowStepKindFanOut WorkflowStepKind = "fan_out"
	WorkflowStepKindFanIn  WorkflowStepKind = "fan_in"
	WorkflowStepKindStop   WorkflowStepKind = "stop"
)

func NewWorkflow(workflow Workflow) (Workflow, error) {
	if err := workflow.Validate(); err != nil {
		return Workflow{}, err
	}

	workflow.Steps = cloneWorkflowSteps(workflow.Steps)
	return workflow, nil
}

func (w Workflow) Validate() error {
	if err := w.ID.Validate(); err != nil {
		return err
	}

	if strings.TrimSpace(w.Name) == "" {
		return fmt.Errorf("workflow name must not be empty")
	}

	if err := w.EntryStepID.Validate(); err != nil {
		return err
	}

	if len(w.Steps) == 0 {
		return fmt.Errorf("workflow must contain at least one step")
	}

	stepsByID := make(map[WorkflowStepID]WorkflowStep, len(w.Steps))
	incomingCounts := make(map[WorkflowStepID]int, len(w.Steps))
	for _, step := range w.Steps {
		if err := step.Validate(); err != nil {
			return err
		}

		if _, exists := stepsByID[step.ID]; exists {
			return fmt.Errorf("workflow step IDs must be unique, duplicate %q", step.ID)
		}

		stepsByID[step.ID] = step
	}

	if _, exists := stepsByID[w.EntryStepID]; !exists {
		return fmt.Errorf("workflow entry step %q does not exist", w.EntryStepID)
	}

	for _, step := range w.Steps {
		for _, nextID := range step.NextStepIDs {
			if _, exists := stepsByID[nextID]; !exists {
				return fmt.Errorf("workflow step %q references unknown next step %q", step.ID, nextID)
			}
			incomingCounts[nextID]++
		}
	}

	for _, step := range w.Steps {
		if step.Kind == WorkflowStepKindFanIn && incomingCounts[step.ID] < 2 {
			return fmt.Errorf("fan-in workflow step %q must have at least two incoming edges", step.ID)
		}
	}

	return nil
}

func (s WorkflowStep) Validate() error {
	if err := s.ID.Validate(); err != nil {
		return err
	}

	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("workflow step name must not be empty")
	}

	if err := s.Kind.Validate(); err != nil {
		return err
	}

	if s.RetryLimit < 0 {
		return fmt.Errorf("workflow retry limit must be >= 0, got %d", s.RetryLimit)
	}

	seenNext := make(map[WorkflowStepID]struct{}, len(s.NextStepIDs))
	for _, nextID := range s.NextStepIDs {
		if err := nextID.Validate(); err != nil {
			return err
		}

		if _, exists := seenNext[nextID]; exists {
			return fmt.Errorf("workflow next step IDs must be unique, duplicate %q", nextID)
		}

		seenNext[nextID] = struct{}{}
	}

	switch s.Kind {
	case WorkflowStepKindAgent:
		if err := s.ActorID.Validate(); err != nil {
			return fmt.Errorf("agent workflow step requires actor ID: %w", err)
		}
		if len(s.NextStepIDs) > 1 {
			return fmt.Errorf("agent workflow steps may have at most one next step")
		}
	case WorkflowStepKindFanOut:
		if s.ActorID != "" {
			return fmt.Errorf("fan-out workflow steps must not define an actor ID")
		}
		if len(s.NextStepIDs) < 2 {
			return fmt.Errorf("fan-out workflow steps must define at least two next steps")
		}
	case WorkflowStepKindFanIn:
		if s.ActorID != "" {
			return fmt.Errorf("fan-in workflow steps must not define an actor ID")
		}
		if len(s.NextStepIDs) > 1 {
			return fmt.Errorf("fan-in workflow steps may have at most one next step")
		}
	case WorkflowStepKindStop:
		if s.ActorID != "" {
			return fmt.Errorf("stop workflow steps must not define an actor ID")
		}
		if len(s.NextStepIDs) != 0 {
			return fmt.Errorf("stop workflow steps must not define next steps")
		}
	}

	return nil
}

func cloneWorkflowSteps(steps []WorkflowStep) []WorkflowStep {
	cloned := slices.Clone(steps)
	for i := range cloned {
		cloned[i].NextStepIDs = slices.Clone(cloned[i].NextStepIDs)
	}

	return cloned
}

func (k WorkflowStepKind) Validate() error {
	switch k {
	case WorkflowStepKindAgent, WorkflowStepKindFanOut, WorkflowStepKindFanIn, WorkflowStepKindStop:
		return nil
	default:
		return fmt.Errorf("invalid workflow step kind %q", k)
	}
}
