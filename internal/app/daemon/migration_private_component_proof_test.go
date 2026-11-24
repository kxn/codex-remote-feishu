package daemon

import (
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"testing"
)

func TestPersonalMigrationComponentDrainIdentity(t *testing.T) {
	p := ingressowner.DrainProof{Owner: ingressowner.Owner{ID: "owner", Epoch: 1, Application: ingressowner.Team}, MigrationID: "migration", Revision: 2, AcceptedWatermark: 5, BindingDigest: "binding", CoverageDigest: "old"}
	q := p
	q.CoverageDigest = "new"
	if !samePersonalMigrationDrain(p, q) {
		t.Fatal("coverage lineage should be checked by router, not treated as changed drain")
	}
	for name, alter := range map[string]func(*ingressowner.DrainProof){"owner": func(v *ingressowner.DrainProof) { v.Owner.ID = "other" }, "epoch": func(v *ingressowner.DrainProof) { v.Owner.Epoch++ }, "migration": func(v *ingressowner.DrainProof) { v.MigrationID = "other" }, "revision": func(v *ingressowner.DrainProof) { v.Revision++ }, "watermark": func(v *ingressowner.DrainProof) { v.AcceptedWatermark++ }, "binding": func(v *ingressowner.DrainProof) { v.BindingDigest = "other" }} {
		t.Run(name, func(t *testing.T) {
			q := p
			alter(&q)
			if samePersonalMigrationDrain(p, q) {
				t.Fatal("different drain accepted")
			}
		})
	}
}
