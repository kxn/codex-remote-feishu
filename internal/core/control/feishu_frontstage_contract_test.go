package control

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

func TestFrontstageOwnerCardNormalizeMatrix(t *testing.T) {
	tests := []struct {
		name       string
		gotPhase   frontstagecontract.Phase
		gotPolicy  frontstagecontract.ActionPolicy
		gotSealed  bool
		wantPhase  frontstagecontract.Phase
		wantPolicy frontstagecontract.ActionPolicy
		wantSealed bool
	}{
		{
			name: "approval waiting dispatch seals",
			gotPhase: func() frontstagecontract.Phase {
				view := NormalizeFeishuRequestView(FeishuRequestView{
					Backend:    agentproto.BackendClaude,
					Phase:      frontstagecontract.PhaseWaitingDispatch,
					StatusText: "已提交当前请求，等待 Claude 继续。",
				})
				if view.ActionPolicy != frontstagecontract.ActionPolicyReadOnly || !view.Sealed {
					t.Fatalf("unexpected normalized waiting request: %#v", view)
				}
				return view.Phase
			}(),
			gotPolicy:  NormalizeFeishuRequestView(FeishuRequestView{Phase: frontstagecontract.PhaseWaitingDispatch}).ActionPolicy,
			gotSealed:  NormalizeFeishuRequestView(FeishuRequestView{Phase: frontstagecontract.PhaseWaitingDispatch}).Sealed,
			wantPhase:  frontstagecontract.PhaseWaitingDispatch,
			wantPolicy: frontstagecontract.ActionPolicyReadOnly,
			wantSealed: true,
		},
		{
			name: "request editing stays interactive",
			gotPhase: func() frontstagecontract.Phase {
				return NormalizeFeishuRequestView(FeishuRequestView{}).Phase
			}(),
			gotPolicy:  NormalizeFeishuRequestView(FeishuRequestView{}).ActionPolicy,
			gotSealed:  NormalizeFeishuRequestView(FeishuRequestView{}).Sealed,
			wantPhase:  frontstagecontract.PhaseEditing,
			wantPolicy: frontstagecontract.ActionPolicyInteractive,
			wantSealed: false,
		},
		{
			name: "page sealed becomes readonly",
			gotPhase: func() frontstagecontract.Phase {
				return NormalizeFeishuPageView(FeishuPageView{CommandID: "compact", Sealed: true}).Phase
			}(),
			gotPolicy:  NormalizeFeishuPageView(FeishuPageView{CommandID: "compact", Sealed: true}).ActionPolicy,
			gotSealed:  NormalizeFeishuPageView(FeishuPageView{CommandID: "compact", Sealed: true}).Sealed,
			wantPhase:  frontstagecontract.PhaseSucceeded,
			wantPolicy: frontstagecontract.ActionPolicyReadOnly,
			wantSealed: true,
		},
		{
			name: "target picker processing keeps cancel-only",
			gotPhase: func() frontstagecontract.Phase {
				return NormalizeFeishuTargetPickerView(FeishuTargetPickerView{
					Stage:               FeishuTargetPickerStageProcessing,
					CanCancelProcessing: true,
				}).Phase
			}(),
			gotPolicy: NormalizeFeishuTargetPickerView(FeishuTargetPickerView{
				Stage:               FeishuTargetPickerStageProcessing,
				CanCancelProcessing: true,
			}).ActionPolicy,
			gotSealed: NormalizeFeishuTargetPickerView(FeishuTargetPickerView{
				Stage:               FeishuTargetPickerStageProcessing,
				CanCancelProcessing: true,
			}).Sealed,
			wantPhase:  frontstagecontract.PhaseProcessing,
			wantPolicy: frontstagecontract.ActionPolicyCancelOnly,
			wantSealed: false,
		},
		{
			name: "path picker terminal seals",
			gotPhase: func() frontstagecontract.Phase {
				return NormalizeFeishuPathPickerView(FeishuPathPickerView{Phase: frontstagecontract.PhaseCancelled}).Phase
			}(),
			gotPolicy:  NormalizeFeishuPathPickerView(FeishuPathPickerView{Phase: frontstagecontract.PhaseCancelled}).ActionPolicy,
			gotSealed:  NormalizeFeishuPathPickerView(FeishuPathPickerView{Phase: frontstagecontract.PhaseCancelled}).Sealed,
			wantPhase:  frontstagecontract.PhaseCancelled,
			wantPolicy: frontstagecontract.ActionPolicyReadOnly,
			wantSealed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.gotPhase != tt.wantPhase {
				t.Fatalf("phase = %q, want %q", tt.gotPhase, tt.wantPhase)
			}
			if tt.gotPolicy != tt.wantPolicy {
				t.Fatalf("action policy = %q, want %q", tt.gotPolicy, tt.wantPolicy)
			}
			if tt.gotSealed != tt.wantSealed {
				t.Fatalf("sealed = %v, want %v", tt.gotSealed, tt.wantSealed)
			}
		})
	}
}
