package work

import (
	"encoding/json"
	"fmt"
	"time"
)

type SenderIDs struct {
	OpenID  string `json:"open_id,omitempty"`
	UserID  string `json:"user_id,omitempty"`
	UnionID string `json:"union_id,omitempty"`
}
type RawMention struct {
	Key  string    `json:"key"`
	IDs  SenderIDs `json:"ids"`
	Type string    `json:"type"`
	Name string    `json:"name,omitempty"`
}

// RawIngressEnvelope is copied from the verified SDK event plus the receiving
// gateway configuration. It deliberately excludes callback tokens/credentials.
type RawIngressEnvelope struct {
	MessageCreatedAt time.Time    `json:"message_created_at,omitempty"`
	GatewayRef       string       `json:"gateway_ref"`
	AppID            string       `json:"app_id"`
	EventID          string       `json:"event_id"`
	Tenant           string       `json:"tenant"`
	SenderTenant     string       `json:"sender_tenant,omitempty"`
	ChatID           string       `json:"chat_id"`
	ChatType         string       `json:"chat_type"`
	MessageID        string       `json:"message_id"`
	RootID           string       `json:"root_id,omitempty"`
	ParentID         string       `json:"parent_id,omitempty"`
	ThreadID         string       `json:"thread_id,omitempty"`
	SenderType       string       `json:"sender_type"`
	SenderIDs        SenderIDs    `json:"sender_ids"`
	MessageType      string       `json:"message_type"`
	RawContent       string       `json:"raw_content"`
	Mentions         []RawMention `json:"mentions,omitempty"`
	ReceivedAt       time.Time    `json:"received_at"`
}

func (v RawIngressEnvelope) Validate() error {
	if err := required(v.GatewayRef, v.AppID, v.EventID, v.Tenant, v.ChatID, v.MessageID, v.SenderType, v.MessageType); err != nil {
		return err
	}
	if !oneOf(v.ChatType, "p2p", "group") {
		return fmt.Errorf("invalid chat type")
	}
	if v.ReceivedAt.IsZero() {
		return fmt.Errorf("ingress receipt time required")
	}
	return nil
}

type InputContentState string

const (
	InputContentNone    InputContentState = ""
	InputContentPending InputContentState = "pending"
	InputContentReady   InputContentState = "ready"
	InputContentPartial InputContentState = "partial"
)

type IngressState string

const (
	IngressPending   IngressState = "pending"
	IngressProcessed IngressState = "processed"
	IngressIgnored   IngressState = "ignored"
	IngressCommand   IngressState = "command"
)

type IngressRoute struct {
	ReplyRoutingError  string    `json:"reply_routing_error,omitempty"`
	ContextID          string    `json:"context_id,omitempty"`
	NavigationRevision int64     `json:"navigation_revision"`
	ProjectViewScopeID string    `json:"project_view_scope_id,omitempty"`
	ProjectContextID   string    `json:"project_context_id,omitempty"`
	Kind               ScopeKind `json:"kind"`
	ScopeID            string    `json:"scope_id,omitempty"`
	ProjectID          string    `json:"project_id,omitempty"`
	BindingGeneration  int64     `json:"binding_generation"`
}
type IngressGateway struct {
	GatewayRef    string `json:"gateway_ref"`
	AppID         string `json:"app_id"`
	RoleID        string `json:"role_id"`
	BotGeneration int64  `json:"bot_generation"`
	BotOpenID     string `json:"bot_open_id,omitempty"`
	UnionDomain   string `json:"union_domain,omitempty"`
}

func (v IngressGateway) Validate() error {
	if err := required(v.GatewayRef, v.AppID, v.RoleID); err != nil {
		return err
	}
	return positive("gateway generation", v.BotGeneration)
}

type IngressObservation struct {
	Envelope RawIngressEnvelope `json:"envelope"`
	Gateway  IngressGateway     `json:"gateway"`
}
type IngressRecord struct {
	ReplyDeliveryError string             `json:"reply_delivery_error,omitempty"`
	NextAttemptAt      *time.Time         `json:"next_attempt_at,omitempty"`
	Gateway            IngressGateway     `json:"gateway"`
	ID                 string             `json:"id"`
	Envelope           RawIngressEnvelope `json:"envelope"`
	Route              IngressRoute       `json:"route"`
	State              IngressState       `json:"state"`
	MessageID          string             `json:"message_id,omitempty"`
	LastError          string             `json:"last_error,omitempty"`
	Revision           int64              `json:"revision"`
}

func (v IngressRecord) Validate() error {
	if err := required(v.ID); err != nil {
		return err
	}
	if err := v.Gateway.Validate(); err != nil {
		return err
	}
	if err := v.Envelope.Validate(); err != nil {
		return err
	}
	if !oneOf(v.Route.Kind, ScopePersonal, ScopeProject, ScopeBootstrap) || v.Route.BindingGeneration < 0 {
		return fmt.Errorf("invalid frozen ingress route")
	}
	if v.Route.Kind == ScopeProject && (v.Route.ProjectID == "" || v.Route.ScopeID == "") {
		return fmt.Errorf("project route requires project and scope")
	}
	if !oneOf(v.State, IngressPending, IngressProcessed, IngressIgnored, IngressCommand) {
		return fmt.Errorf("invalid ingress state")
	}
	return positive("ingress revision", v.Revision)
}

type IdentityEvidence struct {
	Tenant         string    `json:"tenant"`
	AppID          string    `json:"app_id"`
	Generation     int64     `json:"generation"`
	UnionDomain    string    `json:"union_domain,omitempty"`
	IDs            SenderIDs `json:"ids"`
	ObservationRef string    `json:"observation_ref"`
}

func (v IdentityEvidence) Validate() error {
	if err := required(v.Tenant, v.AppID, v.ObservationRef); err != nil {
		return err
	}
	if v.IDs.OpenID == "" && v.IDs.UserID == "" && v.IDs.UnionID == "" {
		return fmt.Errorf("sender has no stable platform identity")
	}
	return positive("identity evidence generation", v.Generation)
}

type TypedCommandEnvelope struct {
	ContextID          string          `json:"context_id,omitempty"`
	NavigationRevision int64           `json:"navigation_revision"`
	ProjectViewScopeID string          `json:"project_view_scope_id,omitempty"`
	ProjectContextID   string          `json:"project_context_id,omitempty"`
	Receipt            string          `json:"receipt,omitempty"`
	LastError          string          `json:"last_error,omitempty"`
	ID                 string          `json:"id"`
	IngressID          string          `json:"ingress_id"`
	ScopeID            string          `json:"scope_id"`
	PrincipalID        string          `json:"principal_id"`
	RoleID             string          `json:"role_id"`
	GatewayRef         string          `json:"gateway_ref"`
	Text               string          `json:"text"`
	Raw                json.RawMessage `json:"raw"`
	State              string          `json:"state"`
	Revision           int64           `json:"revision"`
}

func (v TypedCommandEnvelope) Validate() error {
	if err := required(v.ID, v.IngressID, v.ScopeID, v.PrincipalID, v.RoleID, v.GatewayRef, v.Text); err != nil {
		return err
	}
	if !oneOf(v.State, "pending", "sending", "unknown", "handled") {
		return fmt.Errorf("invalid private command state")
	}
	return positive("command revision", v.Revision)
}

// MembershipObservedAt dates the platform membership fact, not the delayed
// callback delivery. Replays cannot refresh a later revocation.
func (v RawIngressEnvelope) MembershipObservedAt() time.Time {
	if !v.MessageCreatedAt.IsZero() {
		return v.MessageCreatedAt
	}
	return v.ReceivedAt
}
