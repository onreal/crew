package application

import (
	"context"

	"crew/internal/domain"
)

type TransientProgressEvent struct {
	Provider string
	AgentID  domain.AgentID
	Kind     string
	RawType  string
	Text     string
}

type transientProgressReporterKey struct{}

func WithTransientProgressReporter(ctx context.Context, report func(TransientProgressEvent)) context.Context {
	if report == nil {
		return ctx
	}
	return context.WithValue(ctx, transientProgressReporterKey{}, report)
}

func TransientProgressReporterFromContext(ctx context.Context) func(TransientProgressEvent) {
	report, _ := ctx.Value(transientProgressReporterKey{}).(func(TransientProgressEvent))
	return report
}
