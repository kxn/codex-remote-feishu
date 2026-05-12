package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type pickerRuntimeBuild func() (eventcontract.Event, error)
type pickerRuntimeBuildFailure func(error) []eventcontract.Event

func pickerEventSlice(build pickerRuntimeBuild, onBuildError pickerRuntimeBuildFailure) []eventcontract.Event {
	if build == nil {
		return nil
	}
	event, err := build()
	if err != nil {
		if onBuildError == nil {
			return nil
		}
		return onBuildError(err)
	}
	return []eventcontract.Event{event}
}

func (s *Service) openPickerRuntime(
	surface *state.SurfaceConsoleRecord,
	open func() error,
	rollback func(),
	onOpenError func(error) []eventcontract.Event,
	build pickerRuntimeBuild,
	onBuildError pickerRuntimeBuildFailure,
) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	if err := open(); err != nil {
		if onOpenError == nil {
			return nil
		}
		return onOpenError(err)
	}
	return pickerEventSlice(build, func(err error) []eventcontract.Event {
		if rollback != nil {
			rollback()
		}
		if onBuildError == nil {
			return nil
		}
		return onBuildError(err)
	})
}

func mutatePickerAndRebuild(
	mutate func(),
	build pickerRuntimeBuild,
	onBuildError pickerRuntimeBuildFailure,
) []eventcontract.Event {
	if mutate != nil {
		mutate()
	}
	return pickerEventSlice(build, onBuildError)
}
