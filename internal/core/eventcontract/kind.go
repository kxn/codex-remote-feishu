package eventcontract

type Kind string

const (
	KindUnknown             Kind = ""
	KindSnapshot            Kind = "snapshot.updated"
	KindSelection           Kind = "selection.prompt"
	KindPage                Kind = "page.view"
	KindRequest             Kind = "request.prompt"
	KindPathPicker          Kind = "path.picker"
	KindTargetPicker        Kind = "target.picker"
	KindThreadHistory       Kind = "thread.history"
	KindPendingInput        Kind = "pending.input.state"
	KindNotice              Kind = "notice"
	KindPlanUpdate          Kind = "plan.updated"
	KindBlockCommitted      Kind = "block.committed"
	KindTimelineText        Kind = "timeline.text"
	KindImageOutput         Kind = "image.output"
	KindExecCommandProgress Kind = "exec_command.progress"
	KindAgentCommand        Kind = "agent.command"
	KindDaemonCommand       Kind = "daemon.command"
)

var allKinds = []Kind{
	KindSnapshot,
	KindSelection,
	KindPage,
	KindRequest,
	KindPathPicker,
	KindTargetPicker,
	KindThreadHistory,
	KindPendingInput,
	KindNotice,
	KindPlanUpdate,
	KindBlockCommitted,
	KindTimelineText,
	KindImageOutput,
	KindExecCommandProgress,
	KindAgentCommand,
	KindDaemonCommand,
}

func IsKnownKind(kind Kind) bool {
	for _, candidate := range allKinds {
		if candidate == kind {
			return true
		}
	}
	return false
}
