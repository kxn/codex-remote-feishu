package workstore

import "github.com/kxn/codex-remote-feishu/internal/core/work"

// GetIngressByPlatformMessage uses the canonical platform source identity. It
// never chooses a recent event or another bot's last user/interaction.
func (t *Tx) GetIngressByPlatformMessage(tenant, chatID, messageID string) (work.IngressRecord, error) {
	key := work.MessageKey{Tenant: tenant, ChatID: chatID, MessageID: messageID}
	if err := key.Validate(); err != nil {
		return work.IngressRecord{}, err
	}
	return t.GetIngress(ingressID(work.RawIngressEnvelope{Tenant: tenant, ChatID: chatID, MessageID: messageID}))
}
