package codex

type ProtocolDirection string

const (
	ProtocolDirectionClientRequest      ProtocolDirection = "clientRequest"
	ProtocolDirectionServerRequest      ProtocolDirection = "serverRequest"
	ProtocolDirectionServerNotification ProtocolDirection = "serverNotification"
)

type ProtocolTargetLayer string

const (
	ProtocolTargetPassThroughOnly       ProtocolTargetLayer = "pass-through-only"
	ProtocolTargetCanonicalized         ProtocolTargetLayer = "canonicalized"
	ProtocolTargetStateOnly             ProtocolTargetLayer = "state-only"
	ProtocolTargetProductVisible        ProtocolTargetLayer = "product-visible"
	ProtocolTargetHeadlessDriven        ProtocolTargetLayer = "headless-driven"
	ProtocolTargetUnsupportedFailClosed ProtocolTargetLayer = "unsupported-fail-closed"
)

type ProtocolSupportStatus string

const (
	ProtocolStatusSupported   ProtocolSupportStatus = "supported"
	ProtocolStatusPartial     ProtocolSupportStatus = "partial"
	ProtocolStatusPlanned     ProtocolSupportStatus = "planned"
	ProtocolStatusUnsupported ProtocolSupportStatus = "unsupported"
	ProtocolStatusNativeOnly  ProtocolSupportStatus = "native-only"
	ProtocolStatusDeprecated  ProtocolSupportStatus = "deprecated"
)

type ProtocolCadence string

const (
	ProtocolCadenceEvent         ProtocolCadence = "event"
	ProtocolCadenceSnapshot      ProtocolCadence = "snapshot"
	ProtocolCadenceStreamDelta   ProtocolCadence = "stream-delta"
	ProtocolCadenceHighFrequency ProtocolCadence = "high-frequency"
	ProtocolCadenceRequest       ProtocolCadence = "request"
	ProtocolCadenceResponse      ProtocolCadence = "response"
)

type FeishuProjectionPolicy string

const (
	FeishuProjectionIgnore                FeishuProjectionPolicy = "ignore"
	FeishuProjectionFinalOnly             FeishuProjectionPolicy = "final-only"
	FeishuProjectionLatestOnly            FeishuProjectionPolicy = "latest-only"
	FeishuProjectionCoalesced             FeishuProjectionPolicy = "coalesced"
	FeishuProjectionStateOnly             FeishuProjectionPolicy = "state-only"
	FeishuProjectionPassThroughOnly       FeishuProjectionPolicy = "pass-through-only"
	FeishuProjectionProductVisible        FeishuProjectionPolicy = "product-visible"
	FeishuProjectionUnsupportedFailClosed FeishuProjectionPolicy = "unsupported-fail-closed"
)

type ProtocolCoverageEntry struct {
	Method                    string
	Direction                 ProtocolDirection
	TargetLayer               ProtocolTargetLayer
	Status                    ProtocolSupportStatus
	Owner                     string
	Notes                     string
	Cadence                   ProtocolCadence
	AuthoritativeFinal        string
	FeishuProjectionPolicy    FeishuProjectionPolicy
	HeadlessOptOutCandidate   bool
	OptOutBlockedBy           string
	NativePassThroughRequired bool
}

type protocolCoverageGroup struct {
	Methods                   []string
	Direction                 ProtocolDirection
	TargetLayer               ProtocolTargetLayer
	Status                    ProtocolSupportStatus
	Owner                     string
	Notes                     string
	Cadence                   ProtocolCadence
	AuthoritativeFinal        string
	FeishuProjectionPolicy    FeishuProjectionPolicy
	HeadlessOptOutCandidate   bool
	OptOutBlockedBy           string
	NativePassThroughRequired bool
}

func ProtocolCoverageManifest() []ProtocolCoverageEntry {
	groups := []protocolCoverageGroup{
		{
			Direction:              ProtocolDirectionServerNotification,
			TargetLayer:            ProtocolTargetCanonicalized,
			Status:                 ProtocolStatusSupported,
			Owner:                  "codex-translator",
			Cadence:                ProtocolCadenceEvent,
			FeishuProjectionPolicy: FeishuProjectionProductVisible,
			Notes:                  "Core turn and item lifecycle already translated into agentproto/orchestrator events.",
			Methods: []string{
				"error",
				"thread/started",
				"thread/status/changed",
				"thread/name/updated",
				"turn/started",
				"hook/started",
				"turn/completed",
				"hook/completed",
				"item/started",
				"item/autoApprovalReview/started",
				"item/autoApprovalReview/completed",
				"item/completed",
				"serverRequest/resolved",
				"thread/compacted",
			},
		},
		{
			Direction:              ProtocolDirectionServerNotification,
			TargetLayer:            ProtocolTargetStateOnly,
			Status:                 ProtocolStatusSupported,
			Owner:                  "codex-translator",
			Cadence:                ProtocolCadenceSnapshot,
			FeishuProjectionPolicy: FeishuProjectionLatestOnly,
			Notes:                  "Authoritative snapshot/state notifications currently consumed without typewriter projection.",
			Methods: []string{
				"thread/tokenUsage/updated",
				"turn/diff/updated",
				"turn/plan/updated",
				"model/rerouted",
			},
		},
		{
			Direction:              ProtocolDirectionServerNotification,
			TargetLayer:            ProtocolTargetCanonicalized,
			Status:                 ProtocolStatusSupported,
			Owner:                  "codex-translator",
			Cadence:                ProtocolCadenceEvent,
			FeishuProjectionPolicy: FeishuProjectionCoalesced,
			Notes:                  "Tool progress is canonicalized, with product projection bounded by orchestrator policy.",
			Methods: []string{
				"item/mcpToolCall/progress",
			},
		},
		{
			Direction:               ProtocolDirectionServerNotification,
			TargetLayer:             ProtocolTargetCanonicalized,
			Status:                  ProtocolStatusSupported,
			Owner:                   "codex-translator",
			Cadence:                 ProtocolCadenceStreamDelta,
			AuthoritativeFinal:      "item/completed",
			FeishuProjectionPolicy:  FeishuProjectionFinalOnly,
			HeadlessOptOutCandidate: true,
			Notes:                   "Delta is parsed for native compatibility; Feishu Remote should prefer final item text.",
			Methods: []string{
				"item/agentMessage/delta",
			},
		},
		{
			Direction:               ProtocolDirectionServerNotification,
			TargetLayer:             ProtocolTargetCanonicalized,
			Status:                  ProtocolStatusSupported,
			Owner:                   "codex-translator",
			Cadence:                 ProtocolCadenceStreamDelta,
			AuthoritativeFinal:      "item/completed",
			FeishuProjectionPolicy:  FeishuProjectionFinalOnly,
			HeadlessOptOutCandidate: true,
			Notes:                   "Delta carriers are parsed but Feishu projection is final-only or coalesced; completed item text is authoritative for plan proposals.",
			Methods: []string{
				"item/plan/delta",
				"item/reasoning/textDelta",
				"item/reasoning/summaryTextDelta",
				"item/commandExecution/outputDelta",
				"item/fileChange/outputDelta",
			},
		},
		{
			Direction:              ProtocolDirectionServerNotification,
			TargetLayer:            ProtocolTargetStateOnly,
			Status:                 ProtocolStatusSupported,
			Owner:                  "codex-translator",
			Cadence:                ProtocolCadenceSnapshot,
			AuthoritativeFinal:     "item/completed or latest snapshot",
			FeishuProjectionPolicy: FeishuProjectionLatestOnly,
			Notes:                  "Structural notifications are carried as state-only/no-op inputs; patchUpdated uses latest-only file-change snapshot state.",
			Methods: []string{
				"item/commandExecution/terminalInteraction",
				"item/fileChange/patchUpdated",
				"item/reasoning/summaryPartAdded",
			},
		},
		{
			Direction:              ProtocolDirectionServerNotification,
			TargetLayer:            ProtocolTargetStateOnly,
			Status:                 ProtocolStatusSupported,
			Owner:                  "#691",
			Cadence:                ProtocolCadenceEvent,
			FeishuProjectionPolicy: FeishuProjectionStateOnly,
			Notes:                  "Notice carrier is canonicalized into protocol.notice and retained state-only; product projection is intentionally deferred.",
			Methods: []string{
				"warning",
				"guardianWarning",
				"deprecationNotice",
				"configWarning",
			},
		},
		{
			Direction:              ProtocolDirectionServerNotification,
			TargetLayer:            ProtocolTargetStateOnly,
			Status:                 ProtocolStatusPlanned,
			Owner:                  "#691-follow-up",
			Cadence:                ProtocolCadenceEvent,
			FeishuProjectionPolicy: FeishuProjectionStateOnly,
			Notes:                  "Windows-specific warning remains planned until the platform payload is confirmed.",
			Methods: []string{
				"windows/worldWritableWarning",
			},
		},
		{
			Direction:              ProtocolDirectionServerNotification,
			TargetLayer:            ProtocolTargetStateOnly,
			Status:                 ProtocolStatusSupported,
			Owner:                  "#692",
			Cadence:                ProtocolCadenceEvent,
			FeishuProjectionPolicy: FeishuProjectionStateOnly,
			Notes:                  "Model adjunct notifications are stored as turn-scoped state-only carriers and are not model/rerouted.",
			Methods: []string{
				"model/verification",
				"model/safetyBuffering/updated",
			},
		},
		{
			Direction:              ProtocolDirectionServerNotification,
			TargetLayer:            ProtocolTargetStateOnly,
			Status:                 ProtocolStatusPlanned,
			Owner:                  "#689-follow-up",
			Cadence:                ProtocolCadenceEvent,
			FeishuProjectionPolicy: FeishuProjectionStateOnly,
			Notes:                  "Moderation metadata remains planned until the product-safe state shape is confirmed.",
			Methods: []string{
				"turn/moderationMetadata",
			},
		},
		{
			Direction:              ProtocolDirectionServerNotification,
			TargetLayer:            ProtocolTargetStateOnly,
			Status:                 ProtocolStatusSupported,
			Owner:                  "#694",
			Cadence:                ProtocolCadenceEvent,
			FeishuProjectionPolicy: FeishuProjectionStateOnly,
			Notes:                  "Thread lifecycle/goal/settings are stored as state-only carriers; thread/closed is not detach.",
			Methods: []string{
				"thread/archived",
				"thread/deleted",
				"thread/unarchived",
				"thread/closed",
				"thread/goal/updated",
				"thread/goal/cleared",
				"thread/settings/updated",
			},
		},
		{
			Direction:              ProtocolDirectionServerNotification,
			TargetLayer:            ProtocolTargetStateOnly,
			Status:                 ProtocolStatusPlanned,
			Owner:                  "#695",
			Cadence:                ProtocolCadenceEvent,
			FeishuProjectionPolicy: FeishuProjectionStateOnly,
			Notes:                  "Capability/account/app/MCP notifications are passive state inputs for later status surfaces.",
			Methods: []string{
				"skills/changed",
				"mcpServer/oauthLogin/completed",
				"mcpServer/startupStatus/updated",
				"account/updated",
				"account/rateLimits/updated",
				"app/list/updated",
				"account/login/completed",
				"accountLoginCompleted",
				"remoteControl/status/changed",
				"windowsSandbox/setupCompleted",
			},
		},
		{
			Direction:                 ProtocolDirectionServerNotification,
			TargetLayer:               ProtocolTargetPassThroughOnly,
			Status:                    ProtocolStatusNativeOnly,
			Owner:                     "native-client",
			Cadence:                   ProtocolCadenceEvent,
			FeishuProjectionPolicy:    FeishuProjectionPassThroughOnly,
			Notes:                     "Native-heavy app-server surfaces are not claimed by Feishu Remote.",
			NativePassThroughRequired: true,
			Methods: []string{
				"rawResponseItem/completed",
				"rawResponse/completed",
				"process/exited",
				"externalAgentConfig/import/progress",
				"externalAgentConfig/import/completed",
				"fs/changed",
				"fuzzyFileSearch/sessionUpdated",
				"fuzzyFileSearch/sessionCompleted",
				"thread/environment/connected",
				"thread/environment/disconnected",
				"thread/realtime/started",
				"thread/realtime/itemAdded",
				"thread/realtime/transcript/done",
				"thread/realtime/sdp",
				"thread/realtime/error",
				"thread/realtime/closed",
			},
		},
		{
			Direction:                 ProtocolDirectionServerNotification,
			TargetLayer:               ProtocolTargetPassThroughOnly,
			Status:                    ProtocolStatusNativeOnly,
			Owner:                     "native-client",
			Cadence:                   ProtocolCadenceHighFrequency,
			FeishuProjectionPolicy:    FeishuProjectionPassThroughOnly,
			HeadlessOptOutCandidate:   true,
			NativePassThroughRequired: true,
			Notes:                     "High-frequency native streams must not become Feishu typewriter/card patch traffic.",
			Methods: []string{
				"command/exec/outputDelta",
				"process/outputDelta",
				"thread/realtime/transcript/delta",
				"thread/realtime/outputAudio/delta",
			},
		},
		{
			Direction:              ProtocolDirectionClientRequest,
			TargetLayer:            ProtocolTargetHeadlessDriven,
			Status:                 ProtocolStatusSupported,
			Owner:                  "codex-translator",
			Cadence:                ProtocolCadenceRequest,
			FeishuProjectionPolicy: FeishuProjectionProductVisible,
			Notes:                  "Core client requests used by Feishu Remote/headless command flow.",
			Methods: []string{
				"initialize",
				"thread/start",
				"thread/resume",
				"thread/fork",
				"thread/name/set",
				"thread/compact/start",
				"thread/list",
				"thread/read",
				"turn/start",
				"turn/steer",
				"turn/interrupt",
				"review/start",
				"model/list",
				"mcpServer/oauth/login",
			},
		},
		{
			Direction:              ProtocolDirectionClientRequest,
			TargetLayer:            ProtocolTargetStateOnly,
			Status:                 ProtocolStatusPartial,
			Owner:                  "codex-translator",
			Cadence:                ProtocolCadenceRequest,
			FeishuProjectionPolicy: FeishuProjectionStateOnly,
			Notes:                  "Readable state requests are available for future status/diagnostic surfaces, not chat mainline.",
			Methods: []string{
				"mcpServerStatus/list",
				"account/read",
				"account/rateLimits/read",
				"config/read",
			},
		},
		{
			Direction:                 ProtocolDirectionClientRequest,
			TargetLayer:               ProtocolTargetPassThroughOnly,
			Status:                    ProtocolStatusNativeOnly,
			Owner:                     "native-client",
			Cadence:                   ProtocolCadenceRequest,
			FeishuProjectionPolicy:    FeishuProjectionPassThroughOnly,
			NativePassThroughRequired: true,
			Notes:                     "Client request exists upstream but is not a current Feishu/headless product command.",
			Methods: []string{
				"thread/archive", "thread/delete", "thread/unsubscribe", "thread/increment_elicitation",
				"thread/decrement_elicitation", "thread/goal/set", "thread/goal/get", "thread/goal/clear",
				"thread/metadata/update", "thread/settings/update", "thread/memoryMode/set", "memory/reset",
				"thread/unarchive", "thread/shellCommand", "thread/approveGuardianDeniedAction",
				"thread/backgroundTerminals/clean", "thread/backgroundTerminals/list", "thread/backgroundTerminals/terminate",
				"thread/rollback", "thread/search", "thread/loaded/list", "thread/turns/list", "thread/items/list",
				"thread/inject_items", "skills/list", "skills/extraRoots/set", "hooks/list", "marketplace/add",
				"marketplace/remove", "marketplace/upgrade", "plugin/list", "plugin/installed", "plugin/read",
				"plugin/skill/read", "plugin/share/save", "plugin/share/updateTargets", "plugin/share/list",
				"plugin/share/checkout", "plugin/share/delete", "app/read", "app/list", "fs/readFile",
				"fs/writeFile", "fs/createDirectory", "fs/getMetadata", "fs/readDirectory", "fs/remove",
				"fs/copy", "fs/watch", "fs/unwatch", "skills/config/write", "plugin/install",
				"plugin/uninstall", "thread/realtime/start", "thread/realtime/appendAudio", "thread/realtime/appendText",
				"thread/realtime/appendSpeech", "thread/realtime/stop", "thread/realtime/listVoices",
				"modelProvider/capabilities/read", "experimentalFeature/list", "permissionProfile/list",
				"experimentalFeature/enablement/set", "remoteControl/enable", "remoteControl/disable",
				"remoteControl/status/read", "remoteControl/pairing/start", "remoteControl/pairing/status",
				"remoteControl/client/list", "remoteControl/client/revoke", "collaborationMode/list",
				"mock/experimentalMethod", "environment/add", "environment/info", "environment/status",
				"config/mcpServer/reload", "mcpServer/resource/read", "mcpServer/tool/call",
				"windowsSandbox/setupStart", "windowsSandbox/readiness", "account/login/start",
				"account/login/cancel", "account/logout", "account/rateLimitResetCredit/consume",
				"account/usage/read", "account/workspaceMessages/read", "account/sendAddCreditsNudgeEmail",
				"feedback/upload", "command/exec", "command/exec/write", "command/exec/terminate",
				"command/exec/resize", "process/spawn", "process/writeStdin", "process/kill",
				"process/resizePty", "externalAgentConfig/detect", "externalAgentConfig/import",
				"externalAgentConfig/import/readHistories", "config/value/write", "config/batchWrite",
				"configRequirements/read", "getConversationSummary", "gitDiffToRemote", "getAuthStatus",
				"fuzzyFileSearch", "fuzzyFileSearch/sessionStart", "fuzzyFileSearch/sessionUpdate",
				"fuzzyFileSearch/sessionStop",
			},
		},
		{
			Direction:              ProtocolDirectionServerRequest,
			TargetLayer:            ProtocolTargetCanonicalized,
			Status:                 ProtocolStatusSupported,
			Owner:                  "codex-translator",
			Cadence:                ProtocolCadenceRequest,
			FeishuProjectionPolicy: FeishuProjectionProductVisible,
			Notes:                  "Interactive request families already map into request cards or explicit unsupported tool callbacks.",
			Methods: []string{
				"item/commandExecution/requestApproval",
				"item/fileChange/requestApproval",
				"item/tool/requestUserInput",
				"mcpServer/elicitation/request",
				"item/permissions/requestApproval",
				"item/tool/call",
			},
		},
		{
			Direction:              ProtocolDirectionServerRequest,
			TargetLayer:            ProtocolTargetUnsupportedFailClosed,
			Status:                 ProtocolStatusUnsupported,
			Owner:                  "#696",
			Cadence:                ProtocolCadenceRequest,
			FeishuProjectionPolicy: FeishuProjectionUnsupportedFailClosed,
			Notes:                  "Sensitive requests must not be faked by Feishu Remote.",
			Methods: []string{
				"account/chatgptAuthTokens/refresh",
				"attestation/generate",
				"currentTime/read",
			},
		},
		{
			Direction:              ProtocolDirectionServerRequest,
			TargetLayer:            ProtocolTargetCanonicalized,
			Status:                 ProtocolStatusDeprecated,
			Owner:                  "#696",
			Cadence:                ProtocolCadenceRequest,
			FeishuProjectionPolicy: FeishuProjectionProductVisible,
			Notes:                  "Legacy approvals should remain explicit compat if upstream still emits them.",
			Methods: []string{
				"applyPatchApproval",
				"execCommandApproval",
			},
		},
	}
	entries := make([]ProtocolCoverageEntry, 0, 256)
	for _, group := range groups {
		for _, method := range group.Methods {
			entries = append(entries, ProtocolCoverageEntry{
				Method:                    method,
				Direction:                 group.Direction,
				TargetLayer:               group.TargetLayer,
				Status:                    group.Status,
				Owner:                     group.Owner,
				Notes:                     group.Notes,
				Cadence:                   group.Cadence,
				AuthoritativeFinal:        group.AuthoritativeFinal,
				FeishuProjectionPolicy:    group.FeishuProjectionPolicy,
				HeadlessOptOutCandidate:   group.HeadlessOptOutCandidate,
				OptOutBlockedBy:           group.OptOutBlockedBy,
				NativePassThroughRequired: group.NativePassThroughRequired,
			})
		}
	}
	return entries
}
