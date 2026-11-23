package orchestrator

import (
	"strings"
	"testing"
	"time"
)

func TestPredictionDesktopDraftHasNoLocalLengthOrAgePolicy(t *testing.T) {
	svc, sid, iid := predictionTestSetup(t)
	request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
	suggestion := strings.Repeat("next ", 50) + "next"
	result := svc.ApplyAgentEvent(iid, predictionTestResult(t, request.CommandID, "turn-1", suggestion))
	if findPageEvent(result) == nil {
		t.Fatal("desktop suggestions do not have a 200-character rejection policy")
	}
	now := svc.now().Add(20 * time.Minute)
	svc.now = func() time.Time { return now }
	svc.expirePredictions()
	if svc.predictions[sid].Canceled {
		t.Fatal("unchanged conversation must not expire a completed suggestion after a custom TTL")
	}
	accept := predictionTestAction(t, predictionAcceptKind, sid, request.CommandID, "user-1", "")
	page := findPageEvent(svc.ApplySurfaceAction(accept))
	if page == nil || page.Sections[0].Entries[0].Form.Field.DefaultValue != suggestion {
		t.Fatal("native suggestion should remain editable while its source context is current")
	}
}

func TestPredictionPreservesAdapterNormalizedSuggestion(t *testing.T) {
	svc, sid, iid := predictionTestSetup(t)
	request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
	suggestion := "\u0085next\u0085"
	svc.ApplyAgentEvent(iid, predictionTestResult(t, request.CommandID, "turn-1", suggestion))
	if svc.predictions[sid].Suggestion != suggestion {
		t.Fatal("core must preserve the adapter's ECMAScript-normalized suggestion")
	}
	page := findPageEvent(svc.ApplySurfaceAction(predictionTestAction(t, predictionAcceptKind, sid, request.CommandID, "user-1", "")))
	if page == nil || page.Sections[0].Entries[0].Form.Field.DefaultValue != suggestion {
		t.Fatal("editable draft must retain the adapter's normalized text")
	}
}

func TestPredictionReplyAgeDoesNotShortenNativeSampling(t *testing.T) {
	for _, tick := range []bool{false, true} {
		t.Run(map[bool]string{false: "result", true: "tick-before-result"}[tick], func(t *testing.T) {
			svc, sid, iid := predictionTestSetup(t)
			request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
			now := svc.now().Add(35 * time.Second)
			svc.now = func() time.Time { return now }
			if tick && findAgentCommand(svc.Tick(now), predictionCancelKind) != nil {
				t.Fatal("reply age must not cancel sampling before the native response timeout")
			}
			result := svc.ApplyAgentEvent(iid, predictionTestResult(t, request.CommandID, "turn-1", "准备之后生成的建议"))
			if findPageEvent(result) == nil {
				t.Fatal("a current suggestion must remain eligible after preparation plus sampling exceeds 30 seconds")
			}
			if findPageEvent(svc.ApplyAgentEvent(iid, predictionTestResult(t, request.CommandID, "turn-1", "重复结果"))) != nil {
				t.Fatal("the sampling result must only be consumed once")
			}
		})
	}
}
