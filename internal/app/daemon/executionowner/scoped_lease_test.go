package executionowner

import (
	"errors"
	"testing"
)

func TestScopedLeaseExpiresBeforeTransportAndRejectsForeignOwner(t *testing.T) {
	_, b, reg := outboundFixture(t)
	var captured *Lease
	commits := 0
	err := b.SendLeased(t.Context(), reg, func(l *Lease) error {
		captured = l
		wrong := reg
		wrong.Owner.ID = "foreign"
		if err := l.Run(wrong, func() error { t.Fatal("foreign commit ran"); return nil }); !errors.Is(err, ErrConflict) {
			t.Fatalf("foreign owner: %v", err)
		}
		for i := 0; i < 2; i++ {
			if err := l.Run(reg, func() error { commits++; return nil }); err != nil {
				return err
			}
		}
		return nil
	}, func() error {
		if err := captured.Run(reg, func() error { t.Fatal("lease survived into socket write"); return nil }); !errors.Is(err, ErrConflict) {
			t.Fatalf("expired: %v", err)
		}
		return nil
	})
	if err != nil || commits != 2 {
		t.Fatalf("scoped preparation: %d %v", commits, err)
	}
	if err = captured.Run(reg, func() error { return nil }); !errors.Is(err, ErrConflict) {
		t.Fatalf("captured capability reusable: %v", err)
	}
}

func TestScopedLeasePreparationFailureExpiresAndDoesNotSend(t *testing.T) {
	_, b, reg := outboundFixture(t)
	want := errors.New("local transaction failed")
	var captured *Lease
	err := b.SendLeased(t.Context(), reg, func(l *Lease) error { captured = l; return l.Run(reg, func() error { return want }) }, func() error { t.Fatal("sent on failed prepare"); return nil })
	var before *BeforeSendError
	if !errors.Is(err, want) || !errors.As(err, &before) {
		t.Fatalf("lost pre-send result: %v", err)
	}
	if err = captured.Run(reg, func() error { return nil }); !errors.Is(err, ErrConflict) {
		t.Fatal("failed lease stayed open")
	}
}
