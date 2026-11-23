package workruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	preview "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/preview"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// The store, runtime coordinator and SDK HTTP transport are real; only the
// preview provider is stubbed to make uploads and failures deterministic.
func TestPrivatePreviewDurableLane(t *testing.T) {
	for _, tc := range []struct {
		name                                                 string
		fail, restartStarted, disabled, quiet, noop, partial bool
	}{
		{name: "rewritten multi-card and diff"}, {name: "provider failure", fail: true}, {name: "partially available preview", partial: true}, {name: "noop provider", noop: true},
		{name: "restart after upload started", restartStarted: true}, {name: "preview disabled", disabled: true},
		{name: "quiet still delivers final preview", quiet: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, work.BackendCodex, false, true)
			cwd := t.TempDir()
			seedPrivatePreviewLane(t, f, cwd)
			if tc.quiet {
				setPrivateDisplayMode(f, "quiet")
			}
			var calls atomic.Int32
			var request preview.FinalBlockPreviewRequest
			var binding work.RoleBotBinding
			resultText := strings.Repeat("A long preview paragraph with enough text to split cards.\n\n", 800) + "[preview](https://example.test/temporary-file)"
			if !tc.disabled {
				f.coordinator.options.PreviewPrivateFinal = func(_ context.Context, bot work.RoleBotBinding, req preview.FinalBlockPreviewRequest) (preview.FinalBlockPreviewResult, error) {
					calls.Add(1)
					binding, request = bot, req
					if tc.fail {
						return preview.FinalBlockPreviewResult{}, errors.New("preview offline")
					}
					if tc.noop {
						return preview.FinalBlockPreviewResult{}, nil
					}
					result := preview.FinalBlockPreviewResult{Block: req.Block, TurnDiffPreview: &control.TurnDiffPreview{URL: "https://example.test/temporary-diff"}}
					result.Block.Text = resultText
					if tc.partial {
						return result, errors.New("second file unavailable")
					}
					return result, nil
				}
			}
			f.coordinator.options.HostID = "host"
			f.coordinator.options.EnqueueReconcile = func(string) {}
			sdk := startPrivateSDK(t, false)
			pub, err := workpublish.New(f.store, privateSDKResolver{sdk.api}, func() time.Time { return f.now })
			if err != nil {
				t.Fatal(err)
			}
			f.start()
			diff := "diff --git a/a b/a\n+changed"
			if err = f.handle(agentproto.Event{Kind: agentproto.EventTurnDiffUpdated, ThreadID: "session", TurnID: "turn-1", TurnDiff: diff}); err != nil {
				t.Fatal(err)
			}
			if err = f.handle(agentproto.Event{Kind: agentproto.EventItemCompleted, ThreadID: "session", TurnID: "turn-1", ItemID: "file", ItemKind: "file_change", FileChanges: []agentproto.FileChangeRecord{{Path: "a", Diff: "+changed"}}}); err != nil {
				t.Fatal(err)
			}
			original := "Open [file](./report.md) for the original answer."
			privateFinalEvent(f, original)
			if err = completePrivateTurn(f); err != nil {
				t.Fatal(err)
			}
			if !tc.disabled {
				var id string
				f.tx(func(tx *workstore.Tx) error {
					pending, e := tx.ListPendingPrivatePreviews(8)
					if e != nil {
						return e
					}
					if len(pending) != 1 {
						t.Fatalf("expected queued terminal: %+v", pending)
					}
					id = pending[0]
					if tc.restartStarted {
						v, e := tx.GetPrivateExecutionDisplay(id)
						if e != nil {
							return e
						}
						old := v.Revision
						v.PreviewState = "started"
						v.Revision++
						return tx.PutPrivateExecutionDisplay(v, old)
					}
					return nil
				})
				if err := f.coordinator.Reconcile(context.Background(), "attempt"); err != nil && !errors.Is(err, workstore.ErrConflict) {
					t.Fatal(err)
				}
				f.tx(func(tx *workstore.Tx) error {
					gate, e := tx.GetRuntimeGate("worker")
					if e == nil {
						t.Fatalf("pending preview created gate: %+v", gate)
					}
					if !errors.Is(e, workstore.ErrNotFound) {
						return e
					}
					return nil
				})
				drainPrivatePublisher(t, pub)
				for _, req := range sdk.requests() {
					if strings.Contains(req.body, "original answer") {
						t.Fatal("final published before preview")
					}
				}
				if tc.restartStarted {
					f.coordinator = New(f.store, Options{Driver: f.driver, Clock: func() time.Time { return f.now }, HostID: "host", PreviewPrivateFinal: f.coordinator.options.PreviewPrivateFinal, EnqueueReconcile: func(string) {}})
				}
				if err = f.coordinator.RunPrivatePreviews(context.Background(), 8); err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(5 * time.Second)
				for {
					done := false
					f.tx(func(tx *workstore.Tx) error {
						v, e := tx.GetPrivateExecutionDisplay(id)
						if e != nil {
							return e
						}
						done = v.PreviewTerminal == nil
						return nil
					})
					if done {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("preview lane did not finish")
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			drainPrivatePublisher(t, pub)
			if err = f.coordinator.Reconcile(context.Background(), "attempt"); err != nil {
				t.Fatal(err)
			}
			assertPrivateSucceeded(t, f, 1)
			reqs := sdk.requests()
			var bodies strings.Builder
			for _, r := range reqs {
				bodies.WriteString(r.body)
			}
			visible := bodies.String()
			if tc.disabled || tc.fail || tc.restartStarted || tc.noop {
				if !strings.Contains(visible, "original answer") {
					t.Fatalf("original lost: %+v", reqs)
				}
			} else {
				if len(reqs) < 2 || !strings.Contains(visible, "temporary-file") || !strings.Contains(visible, "temporary-diff") {
					t.Fatalf("preview or split missing: %+v", reqs)
				}
			}
			wantCalls := int32(1)
			if tc.disabled || tc.restartStarted {
				wantCalls = 0
			}
			if calls.Load() != wantCalls {
				t.Fatalf("calls=%d want %d", calls.Load(), wantCalls)
			}
			if wantCalls == 1 {
				if binding.GatewayRef != "gateway" || binding.AppID != "app" || request.ActorUserID != "review-human" || request.WorkspaceRoot != cwd || request.ThreadCWD != cwd || request.Block.Text != original || request.TurnDiffSnapshot == nil || request.TurnDiffSnapshot.Diff != diff || !request.PrivateWorkspaceOnly {
					t.Fatalf("incorrect frozen request: bot=%+v req=%+v", binding, request)
				}
			}
			before := len(reqs)
			if err = f.coordinator.RunPrivatePreviews(context.Background(), 8); err != nil {
				t.Fatal(err)
			}
			drainPrivatePublisher(t, pub)
			if len(sdk.requests()) != before || calls.Load() != wantCalls {
				t.Fatal("completed preview repeated")
			}
		})
	}
}

func seedPrivatePreviewLane(t *testing.T, f *fixture, cwd string) {
	t.Helper()
	f.tx(func(tx *workstore.Tx) error {
		raw, err := tx.GetIngressByPlatformMessage("tenant", "private-chat", "om_user")
		if err != nil {
			return err
		}
		old := raw.Revision
		raw.State = work.IngressProcessed
		raw.MessageID = "private-human-input"
		raw.Revision++
		if err = tx.PutIngress(raw, old); err != nil {
			return err
		}
		launch := agentproto.RuntimeLaunchSnapshot{Version: agentproto.WorkerProtocolVersion, Owner: f.carrier.Owner, HostID: "host", CWD: cwd, JournalPath: "/tmp/journal", CarrierIncarnation: "inc-1", InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, InstructionBundle: agentproto.InstructionBundle{Revision: "v1", Text: "instructions"}}
		body, err := json.Marshal(struct {
			Snapshot agentproto.RuntimeLaunchSnapshot `json:"snapshot"`
		}{launch})
		if err != nil {
			return err
		}
		content := work.NewContent("launch-body", "scope", "application/json", body, f.now)
		if err = tx.PutContent(content); err != nil {
			return err
		}
		_, _, err = tx.PutOperation(work.Operation{ID: "launch", ScopeID: "scope", ContextID: "context", Actor: work.Actor{Kind: work.ActorRole, ID: "role"}, Kind: "worker.launch", TargetID: "worker", TargetGeneration: 1, RequestRef: content.ID, RequestHash: content.SHA256, IdempotencyKey: "launch", State: work.OperationConfirmed, ExternalReceipt: "carrier-confirmed", Revision: 1}, 0)
		return err
	})
}

func TestGroupTerminalDoesNotEnterPrivatePreviewLane(t *testing.T) {
	f := newFixture(t, work.BackendCodex, false, true)
	var calls atomic.Int32
	f.coordinator.options.PreviewPrivateFinal = func(context.Context, work.RoleBotBinding, preview.FinalBlockPreviewRequest) (preview.FinalBlockPreviewResult, error) {
		calls.Add(1)
		return preview.FinalBlockPreviewResult{}, nil
	}
	carrier := f.carrier
	carrier.Owner.ScopeID = "group-scope"
	f.tx(func(tx *workstore.Tx) error {
		if err := tx.PutTeam(work.Team{ID: "group-team", Name: "Team", LeaderRoleID: "role", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutScope(work.WorkScope{ID: "group-scope", Kind: work.ScopeProject, ProjectID: "group-project", TeamID: "group-team", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutProject(work.Project{ID: "group-project", Name: "Group", ScopeID: "group-scope", TeamID: "group-team", LeaderRoleID: "role", State: work.ProjectActive, Revision: 1}, 0); err != nil {
			return err
		}
		return f.coordinator.applyNativeOutput(tx, carrier, agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "group-thread", TurnID: "group-turn", Status: "completed"})
	})
	if err := f.coordinator.RunPrivatePreviews(context.Background(), 8); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		pending, err := tx.ListPendingPrivatePreviews(8)
		if len(pending) != 0 || calls.Load() != 0 {
			t.Fatalf("group terminal entered private preview: %+v", pending)
		}
		return err
	})
}
