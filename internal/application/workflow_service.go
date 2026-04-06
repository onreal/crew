package application

import (
	"context"
	"fmt"

	"crew/internal/domain"
)

type WorkflowService struct {
	workflows WorkflowRepository
	outbox    EventOutbox
	events    EventBus
	tx        UnitOfWork
	clock     Clock
}

func NewWorkflowService(
	workflows WorkflowRepository,
	outbox EventOutbox,
	events EventBus,
	tx UnitOfWork,
	clock Clock,
) *WorkflowService {
	return &WorkflowService{
		workflows: workflows,
		outbox:    outbox,
		events:    events,
		tx:        tx,
		clock:     clock,
	}
}

func (s *WorkflowService) Register(ctx context.Context, cmd RegisterWorkflowCommand) (domain.Workflow, error) {
	if err := cmd.Validate(); err != nil {
		return domain.Workflow{}, err
	}

	workflow, err := domain.NewWorkflow(cmd.Workflow)
	if err != nil {
		return domain.Workflow{}, err
	}

	if err := s.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.workflows.Save(txCtx, workflow); err != nil {
			return err
		}

		return s.outbox.Add(txCtx, RecordedEvent{
			Topic:      TopicWorkflowRegistered,
			Payload:    WorkflowRegisteredEvent{Workflow: workflow},
			OccurredAt: s.clock.Now(),
		})
	}); err != nil {
		return domain.Workflow{}, err
	}

	return workflow, nil
}

func (s *WorkflowService) Get(ctx context.Context, query GetWorkflowQuery) (domain.Workflow, error) {
	if err := query.Validate(); err != nil {
		return domain.Workflow{}, err
	}

	return s.workflows.GetByID(ctx, query.WorkflowID)
}

func (s *WorkflowService) Advance(ctx context.Context, cmd AdvanceWorkflowCommand) (WorkflowProgression, error) {
	if err := cmd.Validate(); err != nil {
		return WorkflowProgression{}, err
	}

	workflow, err := s.workflows.GetByID(ctx, cmd.WorkflowID)
	if err != nil {
		return WorkflowProgression{}, err
	}

	stepIndex := buildWorkflowStepIndex(workflow)
	previousByStep := buildWorkflowPredecessorIndex(workflow)
	currentStep, err := resolveCurrentStep(workflow, cmd.CurrentStepID, stepIndex)
	if err != nil {
		return WorkflowProgression{}, err
	}

	if err := validateCurrentStepReadiness(workflow.EntryStepID, currentStep.ID, cmd.CompletedStepIDs, previousByStep); err != nil {
		return WorkflowProgression{}, err
	}

	progression := WorkflowProgression{
		Workflow:    workflow,
		CurrentStep: currentStep,
		Terminal:    currentStep.Kind == domain.WorkflowStepKindStop,
	}

	if progression.Terminal {
		if err := s.events.Publish(ctx, TopicWorkflowProgressed, WorkflowProgressedEvent{
			WorkflowID:       workflow.ID,
			CurrentStep:      currentStep,
			ReadyNextSteps:   nil,
			BlockedNextSteps: nil,
			Terminal:         true,
		}); err != nil {
			return WorkflowProgression{}, err
		}

		return progression, nil
	}

	completed := make(map[domain.WorkflowStepID]struct{}, len(cmd.CompletedStepIDs)+1)
	for _, stepID := range cmd.CompletedStepIDs {
		completed[stepID] = struct{}{}
	}
	completed[currentStep.ID] = struct{}{}

	for _, nextID := range currentStep.NextStepIDs {
		nextStep := stepIndex[nextID]
		if nextStep.Kind == domain.WorkflowStepKindFanIn {
			if readyForFanIn(nextStep.ID, completed, previousByStep) {
				progression.ReadyNextSteps = append(progression.ReadyNextSteps, nextStep)
			} else {
				progression.BlockedNextSteps = append(progression.BlockedNextSteps, nextStep)
			}
			continue
		}

		progression.ReadyNextSteps = append(progression.ReadyNextSteps, nextStep)
	}

	if err := s.events.Publish(ctx, TopicWorkflowProgressed, WorkflowProgressedEvent{
		WorkflowID:       workflow.ID,
		CurrentStep:      currentStep,
		ReadyNextSteps:   progression.ReadyNextSteps,
		BlockedNextSteps: progression.BlockedNextSteps,
		Terminal:         progression.Terminal,
	}); err != nil {
		return WorkflowProgression{}, err
	}

	return progression, nil
}

func resolveCurrentStep(
	workflow domain.Workflow,
	currentStepID domain.WorkflowStepID,
	stepIndex map[domain.WorkflowStepID]domain.WorkflowStep,
) (domain.WorkflowStep, error) {
	if currentStepID == "" {
		return stepIndex[workflow.EntryStepID], nil
	}

	step, exists := stepIndex[currentStepID]
	if !exists {
		return domain.WorkflowStep{}, NotFoundError{Entity: "workflow step", ID: string(currentStepID)}
	}

	return step, nil
}

func buildWorkflowStepIndex(workflow domain.Workflow) map[domain.WorkflowStepID]domain.WorkflowStep {
	index := make(map[domain.WorkflowStepID]domain.WorkflowStep, len(workflow.Steps))
	for _, step := range workflow.Steps {
		index[step.ID] = step
	}

	return index
}

func buildWorkflowPredecessorIndex(workflow domain.Workflow) map[domain.WorkflowStepID][]domain.WorkflowStepID {
	index := make(map[domain.WorkflowStepID][]domain.WorkflowStepID, len(workflow.Steps))
	for _, step := range workflow.Steps {
		for _, nextID := range step.NextStepIDs {
			index[nextID] = append(index[nextID], step.ID)
		}
	}

	return index
}

func readyForFanIn(
	stepID domain.WorkflowStepID,
	completed map[domain.WorkflowStepID]struct{},
	predecessors map[domain.WorkflowStepID][]domain.WorkflowStepID,
) bool {
	for _, predecessorID := range predecessors[stepID] {
		if _, exists := completed[predecessorID]; !exists {
			return false
		}
	}

	return true
}

func validateCurrentStepReadiness(
	entryStepID domain.WorkflowStepID,
	currentStepID domain.WorkflowStepID,
	completedStepIDs []domain.WorkflowStepID,
	predecessors map[domain.WorkflowStepID][]domain.WorkflowStepID,
) error {
	if currentStepID == entryStepID {
		return nil
	}

	completed := make(map[domain.WorkflowStepID]struct{}, len(completedStepIDs))
	for _, stepID := range completedStepIDs {
		completed[stepID] = struct{}{}
	}

	for _, predecessorID := range predecessors[currentStepID] {
		if _, exists := completed[predecessorID]; !exists {
			return fmt.Errorf("%w: workflow step %q is not ready because predecessor %q is incomplete", ErrPrecondition, currentStepID, predecessorID)
		}
	}

	return nil
}
