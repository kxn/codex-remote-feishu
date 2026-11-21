package workservice

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

// ResourceConnector is bound to one configured credential identity. The map in
// Service is keyed by RoleID, so a model cannot choose a more privileged bot.
// Resolve returns platform canonical identity, not title-based ownership guesses.
type ResourceConnector interface {
	Identity() string
	Resolve(context.Context, string) (ResolvedResource, error)
	Capture(context.Context, work.Resource, CaptureResourceRequest) (CapturedResource, error)
}

type ResolvedResource struct {
	SourceIdentity string
	CanonicalURI   string
	Source         work.SourceRef
	Evidence       json.RawMessage
}

type CapturedResource struct {
	Kind                  work.VersionKind
	Source                work.SourceRef
	Content               []byte
	MediaType             string
	ObservationRange      string
	Consistency           work.Consistency
	Complete              bool
	StartedAt, CapturedAt time.Time
}

type CaptureResourceRequest struct {
	Key               string   `json:"key"`
	ResourceID        string   `json:"resource_id"`
	RevisionID        *int     `json:"revision_id,omitempty"`
	SheetID           string   `json:"sheet_id,omitempty"`
	Range             string   `json:"range,omitempty"`
	TableID           string   `json:"table_id,omitempty"`
	FieldNames        []string `json:"field_names,omitempty"`
	ValueRenderOption string   `json:"value_render_option,omitempty"`
}

func NewWithResourceConnectors(store *workstore.Store, signer *workauth.Signer, now func() time.Time, connectors map[string]ResourceConnector) (*Service, error) {
	service, err := New(store, signer, now)
	if err != nil {
		return nil, err
	}
	service.connectors = make(map[string]ResourceConnector, len(connectors))
	for role, connector := range connectors {
		if role == "" || connector == nil || connector.Identity() == "" {
			return nil, fmt.Errorf("resource connector requires explicit role and identity")
		}
		service.connectors[role] = connector
	}
	return service, nil
}

// ResourceConnectorResolver is a trusted local configuration/registry lookup.
// It must not perform network I/O: resolving broker identity can occur during
// an authorization transaction. Returned connector credentials are never stored.
type ResourceConnectorResolver interface {
	Resolve(context.Context, work.RoleBotBinding) (ResourceConnector, error)
}
type ResourceConnectorResolverFunc func(context.Context, work.RoleBotBinding) (ResourceConnector, error)

func (f ResourceConnectorResolverFunc) Resolve(ctx context.Context, binding work.RoleBotBinding) (ResourceConnector, error) {
	return f(ctx, binding)
}
func NewWithResourceConnectorResolver(store *workstore.Store, signer *workauth.Signer, now func() time.Time, resolver ResourceConnectorResolver) (*Service, error) {
	if resolver == nil {
		return nil, fmt.Errorf("resource connector resolver is required")
	}
	service, err := New(store, signer, now)
	if err != nil {
		return nil, err
	}
	service.connectorResolver = resolver
	return service, nil
}
func (s *Service) resolveResourceConnector(ctx context.Context, binding work.RoleBotBinding) (ResourceConnector, error) {
	var connector ResourceConnector
	var err error
	if s.connectorResolver != nil {
		connector, err = s.connectorResolver.Resolve(ctx, binding)
	} else {
		connector = s.connectors[binding.RoleID]
	}
	if err != nil {
		return nil, err
	}
	if connector == nil || connector.Identity() == "" {
		return nil, &workcmd.Error{Code: "blocked", Message: "this role has no configured resource connector"}
	}
	if _, feishu := connector.(*FeishuResourceConnector); feishu && connector.Identity() != FeishuConnectorIdentity(binding) {
		return nil, workauth.ErrUnauthorized
	}
	return connector, nil
}
