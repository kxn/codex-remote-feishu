package owneridentity

import (
	"testing"
	"time"
)

func TestVerifyOwnerCard(t *testing.T) {
	now := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	claims := func() OwnerCardClaims {
		return OwnerCardClaims{
			FlowID:           "flow-1",
			SurfaceSessionID: "surface-1",
			OwnerUserID:      "user-1",
			ExpiresAt:        now.Add(time.Hour),
		}
	}
	cases := []struct {
		name         string
		mutate       func(*OwnerCardClaims)
		surfaceID    string
		flowID       string
		actorUserID  string
		surfaceActor string
		now          time.Time
		want         OwnerCardVerdict
	}{
		{"ok", nil, "surface-1", "flow-1", "user-1", "", now, OwnerCardOK},
		{"ok actor via surface", nil, "surface-1", "flow-1", "", "user-1", now, OwnerCardOK},
		{"flow id mismatch", nil, "surface-1", "flow-2", "user-1", "", now, OwnerCardExpired},
		{"empty flow id", nil, "surface-1", "", "user-1", "", now, OwnerCardExpired},
		{"expired", nil, "surface-1", "flow-1", "user-1", "", now.Add(2 * time.Hour), OwnerCardExpired},
		{"wrong surface", nil, "surface-2", "flow-1", "user-1", "", now, OwnerCardWrongSurface},
		{"unauthorized", nil, "surface-1", "flow-1", "user-2", "", now, OwnerCardUnauthorized},
		{"surface not bound ignores surface", func(c *OwnerCardClaims) { c.SurfaceSessionID = "" }, "anything", "flow-1", "user-1", "", now, OwnerCardOK},
		{"zero expiry not expired", func(c *OwnerCardClaims) { c.ExpiresAt = time.Time{} }, "surface-1", "flow-1", "user-1", "", now, OwnerCardOK},
		{"padded ids match", func(c *OwnerCardClaims) {
			c.FlowID = "  flow-1 "
			c.SurfaceSessionID = " surface-1 "
			c.OwnerUserID = " user-1 "
		}, " surface-1 ", " flow-1 ", " user-1 ", "", now, OwnerCardOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := claims()
			if tc.mutate != nil {
				tc.mutate(&c)
			}
			got := VerifyOwnerCard(c, tc.surfaceID, tc.flowID, tc.actorUserID, tc.surfaceActor, tc.now)
			if got != tc.want {
				t.Fatalf("VerifyOwnerCard() = %q, want %q", got, tc.want)
			}
		})
	}
}
