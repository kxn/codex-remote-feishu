package feishuidentity

import "testing"

func TestSurfaceRefRoundTrip(t *testing.T) {
	ref := SurfaceRef{
		Platform:  " feishu ",
		GatewayID: " app-1 ",
		ScopeKind: " chat ",
		ScopeID:   " oc_room ",
	}
	if got := ref.SurfaceID(); got != "feishu:app-1:chat:oc_room" {
		t.Fatalf("SurfaceID() = %q, want canonical identity", got)
	}

	parsed, ok := ParseSurfaceRef(" feishu:app-1:chat:oc_room ")
	if !ok {
		t.Fatal("expected canonical identity to parse")
	}
	if parsed.GatewayID != "app-1" || !parsed.IsChat() || parsed.IsUser() {
		t.Fatalf("parsed ref = %#v, want chat identity", parsed)
	}
}

func TestParseSurfaceRefRejectsUnknownShapes(t *testing.T) {
	for _, surfaceID := range []string{
		"",
		"feishu:user:ou_user",
		"feishu:app-1:unknown:scope",
		"feishu:app-1:unknown:chat:oc_room",
		"other:app-1:chat:oc_room",
		"feishu::chat:oc_room",
		"feishu:app-1:chat:",
		"feishu:app-1:chat:@om_topic",
		"feishu:app-1:chat:oc_room@",
		"feishu:app-1:chat:oc_room@om_topic@extra",
	} {
		if ref, ok := ParseSurfaceRef(surfaceID); ok {
			t.Fatalf("ParseSurfaceRef(%q) = %#v, want rejection", surfaceID, ref)
		}
	}
}

func TestSurfaceRefRejectsUnknownScope(t *testing.T) {
	ref := SurfaceRef{
		Platform:  PlatformFeishu,
		GatewayID: "app-1",
		ScopeKind: "channel",
		ScopeID:   "oc_room",
	}
	if got := ref.SurfaceID(); got != "" {
		t.Fatalf("SurfaceID() = %q, want empty for unknown scope", got)
	}
}

func TestTopicSurfaceKeepsRoomIdentity(t *testing.T) {
	for _, surfaceID := range []string{"feishu:app-1:chat:oc_room", "feishu:app-1:chat:oc_room@om_a", "feishu:app-1:chat:oc_room@om_b"} {
		ref, ok := ParseSurfaceRef(surfaceID)
		if !ok || ref.ChatID() != "oc_room" || ref.SurfaceID() != surfaceID {
			t.Fatalf("topic identity = %#v, valid=%v", ref, ok)
		}
	}
	ref, _ := ParseSurfaceRef("feishu:app-1:user:ou_user")
	if ref.ChatID() != "" {
		t.Fatal("user surface must not become a room")
	}
}
