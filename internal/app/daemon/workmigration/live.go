package workmigration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (c *Coordinator) ObserveLegacyHello(ctx context.Context, hello agentproto.Hello, host HostAttribution) error {
	if hello.Instance.InstanceID == "" {
		return fmt.Errorf("legacy Hello requires its exact instance")
	}
	if host.HostID != "" && !validEvidence(host.Evidence) {
		return fmt.Errorf("legacy host attribution requires trusted evidence")
	}
	raw, err := json.Marshal(hello)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	key := "live-hello:" + hello.Instance.InstanceID + ":" + hex.EncodeToString(sum[:20])
	return c.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := tx.ObserveLegacyRuntime(hello.Instance.InstanceID, "hello", raw, c.options.Clock()); err != nil {
			return err
		}
		authority, err := tx.GetMigrationAuthority()
		if err != nil {
			return err
		}
		cwd := hello.Instance.WorkspaceRoot
		if cwd == "" {
			cwd = hello.Instance.WorkspaceKey
		}
		native := LegacyNative{Backend: work.Backend(hello.Instance.Backend), Namespace: string(hello.Instance.Backend), InstanceID: hello.Instance.InstanceID, HostID: host.HostID, CWD: cwd, Facts: raw}
		evidence, _ := json.Marshal(map[string]any{"native": native, "classification": "live_legacy_unknown", "host_attribution": host, "source": "legacy_hello"})
		return putNativeFences(tx, authority.MigrationID, key, native, "legacy_live_native_unknown", evidence)
	})
}

func putNativeFences(tx *workstore.Tx, migrationID, key string, native LegacyNative, reason string, evidence json.RawMessage) error {
	if err := tx.ObserveNativeUpgradeLegacyConflict(native.Backend, native.HostID); err != nil {
		return err
	}
	var resources []string
	host := native.HostID
	if host == "" {
		host = "unverified-host"
	}
	resources = append(resources, "native-installation:"+string(native.Backend)+":"+host)
	if native.Namespace != "" && native.SessionID != "" {
		resources = append(resources, nativeResourceKey(native))
	}
	if strings.TrimSpace(native.CWD) != "" {
		if native.HostID == "" {
			resources = append(resources, workstore.UnverifiedHostCWDKey(native.CWD), workstore.UnverifiedHostCWDKey(workstore.CanonicalPotentialLocalCWD(native.CWD)))
		} else {
			resources = append(resources, "cwd:"+native.HostID+":"+native.CWD)
		}
	}
	for _, resource := range resources {
		if err := tx.PutMigrationResourceFence(workstore.MigrationResourceFence{ResourceKey: resource, MigrationID: migrationID, RecordKey: key, Reason: reason, Evidence: evidence}); err != nil {
			return err
		}
	}
	return nil
}

// AttributeLegacyHost narrows a conservative possible-host fence. Native work
// on the now-proven host remains fenced until separate quiescence recovery.
func (c *Coordinator) AttributeLegacyHost(ctx context.Context, id, key string, host HostAttribution) error {
	if host.HostID == "" || !validEvidence(host.Evidence) {
		return fmt.Errorf("actual host evidence required")
	}
	return c.store.Within(ctx, func(tx *workstore.Tx) error {
		fences, err := tx.ListMigrationResourceFences(id, key, 256)
		if err != nil {
			return err
		}
		if len(fences) == 0 {
			return workstore.ErrNotFound
		}
		var native LegacyNative
		for _, fence := range fences {
			var proof struct {
				Native LegacyNative `json:"native"`
			}
			if json.Unmarshal(fence.Evidence, &proof) == nil && proof.Native.CWD != "" {
				native = proof.Native
				break
			}
		}
		if native.CWD == "" {
			return fmt.Errorf("resource has no exact original cwd observation")
		}
		native.HostID = host.HostID
		proof, _ := json.Marshal(map[string]any{"native": native, "classification": "host_attributed_native_unknown", "host_attribution": host})
		if err = putNativeFences(tx, id, key, native, "legacy_native_requires_quiescence", proof); err != nil {
			return err
		}
		return tx.NarrowMigrationHostFences(id, key, host.HostID, host.Evidence)
	})
}
