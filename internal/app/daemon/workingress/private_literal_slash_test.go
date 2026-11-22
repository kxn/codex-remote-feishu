package workingress

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
)

func TestQuotedPrivateLiteralSlashKeepsExactArgumentsWithoutExpansionDiagnostics(t *testing.T) {
	for _, text := range []string{"/menu send_settings", "/goal edit --budget 1200 修复登录", "/mcp status --cursor next-page"} {
		t.Run(text, func(t *testing.T) {
			i, s, _ := ingressFixture(t, false)
			_, nav := privateCommandSource(t, i, s, "setup")
			receive(t, i, rawMessage("leader", "original", "dm", "p2p", "open-person", "person", "original authorized work"))
			process(t, i)
			envelope := rawMessage("leader", "quoted-slash", "dm", "p2p", "open-person", "person", text)
			envelope.ParentID = "original"
			receipt := receive(t, i, envelope)
			process(t, i)
			txRun(t, s, func(tx *workstore.Tx) error {
				commands, err := tx.ListPendingPrivateCommands()
				if err != nil {
					return err
				}
				for _, command := range commands {
					if command.IngressID == receipt.IngressID {
						if command.Text != text || command.ContextID != nav.FrontContextID {
							t.Fatalf("literal command polluted by expansion: %+v", command)
						}
						return nil
					}
				}
				t.Fatal("quoted literal slash lost command owner")
				return nil
			})
		})
	}
}

func TestQuotedOrdinaryTextRetainsExpansionWarningAndDoesNotBecomeSlash(t *testing.T) {
	i, s, _ := ingressFixture(t, false)
	privateCommandSource(t, i, s, "setup")
	receive(t, i, rawMessage("leader", "original", "dm", "p2p", "open-person", "person", "original authorized work"))
	process(t, i)
	const text = "请解释 /goal 的用法，先不要执行"
	envelope := rawMessage("leader", "ordinary-quoted", "dm", "p2p", "open-person", "person", text)
	envelope.ParentID = "original"
	receipt := receive(t, i, envelope)
	process(t, i)
	txRun(t, s, func(tx *workstore.Tx) error {
		commands, err := tx.ListPendingPrivateCommands()
		if err != nil {
			return err
		}
		for _, command := range commands {
			if command.IngressID == receipt.IngressID {
				t.Fatal("ordinary command explanation became an executable slash")
			}
		}
		raw, err := tx.GetIngress(receipt.IngressID)
		if err != nil {
			return err
		}
		message, err := tx.GetMessage(raw.MessageID)
		if err != nil {
			return err
		}
		body, err := tx.GetContent(message.ScopeID, message.BodyRef)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(string(body.Data), text) || !strings.Contains(string(body.Data), "部分内容未获取") || !strings.Contains(string(body.Data), "引用消息") {
			t.Fatalf("ordinary quoted content lost partial expansion warning: %s", body.Data)
		}
		return nil
	})
}
