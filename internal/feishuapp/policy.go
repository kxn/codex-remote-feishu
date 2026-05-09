package feishuapp

const (
	FeishuEventSubscriptionTypeWebsocket = "websocket"
	FeishuCallbackTypeWebsocket          = "websocket"
	FeishuDefaultAbilityBot              = "bot"
)

type FixedPolicy struct {
	EventSubscriptionType     string `json:"eventSubscriptionType"`
	EventRequestURL           string `json:"eventRequestUrl,omitempty"`
	CallbackType              string `json:"callbackType"`
	CallbackRequestURL        string `json:"callbackRequestUrl,omitempty"`
	BotEnabled                bool   `json:"botEnabled"`
	MobileDefaultAbility      string `json:"mobileDefaultAbility"`
	PcDefaultAbility          string `json:"pcDefaultAbility"`
	PreserveExistingEncryptKV bool   `json:"preserveExistingEncryptKV"`
}

func DefaultFixedPolicy() FixedPolicy {
	return FixedPolicy{
		EventSubscriptionType:     FeishuEventSubscriptionTypeWebsocket,
		EventRequestURL:           "",
		CallbackType:              FeishuCallbackTypeWebsocket,
		CallbackRequestURL:        "",
		BotEnabled:                true,
		MobileDefaultAbility:      FeishuDefaultAbilityBot,
		PcDefaultAbility:          FeishuDefaultAbilityBot,
		PreserveExistingEncryptKV: true,
	}
}
