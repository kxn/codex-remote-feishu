package work

import "fmt"

type TaskConversation struct {
	ScopeID             string `json:"scope_id"`
	TaskID              string `json:"task_id"`
	Tenant              string `json:"tenant"`
	ChatID              string `json:"chat_id"`
	RootMessageID       string `json:"root_message_id"`
	ChannelGeneration   int64  `json:"channel_generation"`
	InheritedFromTaskID string `json:"inherited_from_task_id,omitempty"`
	Revision            int64  `json:"revision"`
}

func (v TaskConversation) Validate() error {
	if err := required(v.ScopeID, v.TaskID, v.Tenant, v.ChatID, v.RootMessageID); err != nil {
		return err
	}
	if v.ChannelGeneration < 0 || v.InheritedFromTaskID == v.TaskID {
		return fmt.Errorf("invalid task conversation generation or inheritance")
	}
	return positive("task conversation revision", v.Revision)
}

type PlatformMessageMapping struct {
	Tenant            string `json:"tenant"`
	ChatID            string `json:"chat_id"`
	MessageID         string `json:"message_id"`
	ScopeID           string `json:"scope_id"`
	TaskID            string `json:"task_id,omitempty"`
	RootMessageID     string `json:"root_message_id"`
	ChannelGeneration int64  `json:"channel_generation"`
	OperationID       string `json:"operation_id,omitempty"`
	OutboxID          string `json:"outbox_id,omitempty"`
	RoleID            string `json:"role_id,omitempty"`
	BotGeneration     int64  `json:"bot_generation"`
	IngressID         string `json:"ingress_id,omitempty"`
}

func (v PlatformMessageMapping) Validate() error {
	if err := required(v.Tenant, v.ChatID, v.MessageID, v.ScopeID, v.RootMessageID); err != nil {
		return err
	}
	if v.ChannelGeneration < 0 || v.BotGeneration < 0 {
		return fmt.Errorf("negative message mapping generation")
	}
	if (v.IngressID == "") == (v.OutboxID == "") {
		return fmt.Errorf("mapping needs exactly one ingress or publication receipt")
	}
	if v.OutboxID != "" {
		if err := required(v.OperationID, v.RoleID); err != nil {
			return err
		}
		return positive("publication bot generation", v.BotGeneration)
	}
	return nil
}
