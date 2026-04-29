package daemon

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func newRestoreHintTestApp(stateDir string) *App {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		IdleTTL:    time.Hour,
		KillGrace:  time.Second,
		Paths:      relayruntime.Paths{StateDir: stateDir},
		BinaryPath: "codex",
	})
	return app
}

func seedHeadlessInstance(app *App, instanceID, threadID string) {
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:              instanceID,
		DisplayName:             "headless",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "headless",
		Source:                  "headless",
		Managed:                 true,
		Online:                  true,
		ObservedFocusedThreadID: threadID,
		Threads: map[string]*state.ThreadRecord{
			threadID: {ThreadID: threadID, Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
}
