package workservice

import (
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestDecisionMineReturnsOnlyCurrentPersonalPrincipalDecisions(t *testing.T) {
	s, token, run := runningService(t)
	owner := work.Actor{Kind: work.ActorPrincipal, ID: "human"}
	if err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		if err := tx.PutPrincipal(work.Principal{ID: "other-person", DisplayName: "other", Revision: 1}, 0); err != nil {
			return err
		}
		prompt := work.NewContent("mine-question", "scope", "text/plain", []byte("Can we proceed?"), s.now())
		if err := tx.PutContent(prompt); err != nil {
			return err
		}
		for _, actor := range []work.Actor{owner, {Kind: work.ActorPrincipal, ID: "other-person"}, {Kind: work.ActorRole, ID: "leader"}} {
			request := work.DecisionRequest{ID: "decision-" + actor.ID, ScopeID: "scope", Owner: actor, AllowedActors: []work.Actor{actor}, Kind: work.DecisionBusiness, State: work.DecisionOpen, RequestGeneration: 1, Revision: 1, Presentation: &work.DecisionPresentation{Title: "Proceed", PromptRef: prompt.ID, Choices: []work.DecisionChoice{{Value: "yes", Label: "Yes"}, {Value: "no", Label: "No"}}}}
			if err := tx.PutDecisionRequest(request, 0); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	result, err := s.DecisionMine(t.Context(), token, run, KnowledgeListRequest{})
	if err != nil || len(result.Items) != 1 || result.Items[0].RequestID != "decision-human" || result.Items[0].Question != "Can we proceed?" {
		t.Fatalf("pending personal decisions %+v %v", result, err)
	}
	group, groupToken, groupRun := lifecycleProjectService(t, false)
	if _, err = group.DecisionMine(t.Context(), groupToken, groupRun, KnowledgeListRequest{}); !errors.Is(err, workauth.ErrUnauthorized) {
		t.Fatalf("private decisions visible from group: %v", err)
	}
}
