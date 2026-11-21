package workstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestRuntimeReceiptAtomicReplayAndConflictingEvidence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "work.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	event := agentproto.Event{Seq: 1, Kind: agentproto.EventTurnStarted, ThreadID: "native", TurnID: "turn"}
	hash, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil {
		t.Fatal(err)
	}
	event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: "incarnation", Sequence: 1, PayloadHash: hash}
	sentinel := errors.New("business apply failed")
	err = s.Within(ctx, func(tx *Tx) error {
		_, _, err := tx.AcceptRuntimeEvent("instance", event, time.Now(), func(*Tx) error { return sentinel })
		if !errors.Is(err, sentinel) {
			t.Fatalf("want apply failure got %v", err)
		}
		_, err = tx.GetRuntimeEvent("incarnation", 1)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("failed apply persisted receipt: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	applications := 0
	for i := 0; i < 2; i++ {
		err = s.Within(ctx, func(tx *Tx) error {
			receipt, replayed, err := tx.AcceptRuntimeEvent("instance", event, time.Now(), func(*Tx) error { applications++; return nil })
			if err == nil && (replayed != (i == 1) || receipt.PayloadHash != hash) {
				t.Fatalf("receipt=%+v replay=%v", receipt, replayed)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if applications != 1 {
		t.Fatalf("effects applied %d times", applications)
	}
	err = s.Within(ctx, func(tx *Tx) error {
		_, _, err := tx.AcceptRuntimeEvent("other-instance", event, time.Now(), nil)
		return err
	})
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("instance spoof got %v", err)
	}
	event.Status = "changed"
	event.Journal.PayloadHash, _ = agentproto.RuntimeEventPayloadHash(event)
	err = s.Within(ctx, func(tx *Tx) error { _, _, err := tx.AcceptRuntimeEvent("instance", event, time.Now(), nil); return err })
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("same sequence changed payload got %v", err)
	}
}
