package daemon

import (
	"context"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
)

type teamEditAPI struct {
	feishu.BitableAPI
	member           map[string]feishu.BitablePermissionMember
	created, updated int
}

func (p *teamEditAPI) ListPermissionMembers(context.Context, string, string) (map[string]feishu.BitablePermissionMember, error) {
	return p.member, nil
}
func (p *teamEditAPI) GrantPermission(_ context.Context, id, kind, memberType, memberID, principal, perm string) error {
	if id != "doc" || kind != "docx" || memberType != "openchat" || memberID != "group" || principal != "chat" || perm != "edit" {
		panic("unexpected audience")
	}
	p.created++
	return nil
}
func (p *teamEditAPI) UpdatePermission(_ context.Context, id, kind, memberType, memberID, principal, perm, permType string) error {
	if id != "doc" || kind != "docx" || memberType != "openchat" || memberID != "group" || principal != "chat" || perm != "edit" || permType != "container" {
		panic("unexpected permission change")
	}
	p.updated++
	return nil
}
func TestTeamMaterialsEditAccessPreservesStrongerPermission(t *testing.T) {
	for _, perm := range []string{"", "view", "edit", "full_access"} {
		t.Run(perm, func(t *testing.T) {
			api := &teamEditAPI{member: map[string]feishu.BitablePermissionMember{}}
			if perm != "" {
				api.member["openchat:group"] = feishu.BitablePermissionMember{Perm: perm, PermType: "container"}
			}
			if err := ensureTeamMaterialsEdit(t.Context(), api, "doc", "group"); err != nil {
				t.Fatal(err)
			}
			if (api.created == 1) != (perm == "") || (api.updated == 1) != (perm == "view") {
				t.Fatalf("permission %s changed incorrectly: %+v", perm, api)
			}
		})
	}
}
