package harness

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontractcompat"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/renderer"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/testkit/mockcodex"
	"github.com/kxn/codex-remote-feishu/testkit/mockfeishu"
)

type Harness struct {
	Now        time.Time
	Service    *orchestrator.Service
	Translator *codex.Translator
	Codex      *mockcodex.MockCodex
	Feishu     *mockfeishu.Recorder
	InstanceID string
}

func New() *Harness {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	h := &Harness{
		Now:        now,
		Translator: codex.NewTranslator("inst-1"),
		Codex:      mockcodex.New(),
		Feishu:     mockfeishu.NewRecorder(),
		InstanceID: "inst-1",
	}
	service := orchestrator.NewService(func() time.Time { return h.Now }, orchestrator.Config{TurnHandoffWait: 800 * time.Millisecond}, renderer.NewPlanner())
	codexMock := mockcodex.New()
	codexMock.SeedThread("thread-1", "/data/dl/droid", "修复登录流程")
	service.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	h.Service = service
	h.Codex = codexMock
	return h
}

func (h *Harness) ApplyAction(action control.Action) error {
	events := h.Service.ApplySurfaceAction(action)
	return h.consumeUIEvents(events)
}

func (h *Harness) Tick(d time.Duration) error {
	h.Now = h.Now.Add(d)
	events := h.Service.Tick(h.Now)
	return h.consumeUIEvents(events)
}

func (h *Harness) LocalClient(raw []byte) error {
	result, err := h.Translator.ObserveClient(raw)
	if err != nil {
		return err
	}
	if err := h.applyAgentEvents(result.Events); err != nil {
		return err
	}
	outputs, err := h.Codex.HandleLocalClientMessage(raw)
	if err != nil {
		return err
	}
	for _, output := range outputs {
		if err := h.processServerOutput(output); err != nil {
			return err
		}
	}
	return nil
}

func (h *Harness) consumeUIEvents(events []control.UIEvent) error {
	return h.consumeEvents(eventcontractcompat.FromLegacyUIEvents(events))
}

func (h *Harness) consumeEvents(events []eventcontract.Event) error {
	h.Feishu.ApplyEvents(events)
	for _, event := range events {
		payload, ok := event.Payload.(eventcontract.AgentCommandPayload)
		if !ok {
			continue
		}
		nativeCommands, err := h.Translator.TranslateCommand(payload.Command)
		if err != nil {
			return err
		}
		for _, native := range nativeCommands {
			outputs, err := h.Codex.HandleRemoteCommand(native)
			if err != nil {
				return err
			}
			for _, output := range outputs {
				if err := h.processServerOutput(output); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (h *Harness) processServerOutput(raw []byte) error {
	result, err := h.Translator.ObserveServer(raw)
	if err != nil {
		return err
	}
	if err := h.applyAgentEvents(result.Events); err != nil {
		return err
	}
	for _, followup := range result.OutboundToCodex {
		outputs, err := h.Codex.HandleRemoteCommand(followup)
		if err != nil {
			return err
		}
		for _, output := range outputs {
			if err := h.processServerOutput(output); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *Harness) applyAgentEvents(events []agentproto.Event) error {
	for _, event := range events {
		ui := h.Service.ApplyAgentEvent(h.InstanceID, event)
		if err := h.consumeUIEvents(ui); err != nil {
			return err
		}
	}
	return nil
}
