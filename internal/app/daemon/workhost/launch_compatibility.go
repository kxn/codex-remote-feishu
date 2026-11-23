package workhost

import (
	"reflect"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// The launch and dispatch paths must agree on when an existing process can
// serve the next turn. Admission separately checks the live preference revision.
func (r LaunchRecord) reusableFor(settings LaunchSettings, owner work.WorkerSession, role work.Role, bot work.RoleBotBinding, bundle agentproto.InstructionBundle, hostID string, personal bool) bool {
	s := r.Snapshot
	return s.HostID == hostID && s.HostID == settings.Location.HostID &&
		s.Owner.BotGeneration == bot.IdentityGeneration && s.Owner.ControllerGeneration == owner.ControllerGeneration &&
		r.SettingsRevision == role.SettingsRevision && r.RoleDescriptionVersion == role.DescriptionVersion &&
		s.NativeMemoryPolicyVersion == agentproto.NativeMemoryPolicyVersion && s.InstructionBundle.Revision == bundle.Revision &&
		r.ProfileID == settings.ProfileID && r.Backend == settings.Backend &&
		s.CWD == settings.Location.Cwd && owner.LocationID == settings.Location.ID && owner.LocationGeneration == settings.Location.Generation &&
		(personal || r.PreferencesRevision == settings.PreferencesRevision) && sameRuntimePolicy(r, settings)
}

func sameRuntimePolicy(record LaunchRecord, settings LaunchSettings) bool {
	return record.ConnectionIdentity == settings.ConnectionIdentity &&
		record.Snapshot.NativeBinaryPath == settings.NativeBinaryPath &&
		record.Snapshot.NativeBinaryVersion == settings.NativeBinaryVersion &&
		record.Snapshot.ReadOnly == settings.ReadOnly &&
		reflect.DeepEqual(record.Overrides, settings.Overrides) &&
		reflect.DeepEqual(record.CodexResume, settings.CodexResume)
}
