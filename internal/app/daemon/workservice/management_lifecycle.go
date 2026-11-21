package workservice

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// ManagementAuthorizer is a trusted management-policy port. Models supply the
// desired change; the current human principal/source comes from admitted input.
// Calls happen outside SQL and are revalidated against the current run afterward.
type ManagementAuthorizer interface {
	AuthorizeManagement(context.Context, string, string, string, work.SourceRef) error
}

// WithManagementAuthorizer returns an immutable configuration copy. Invoke only
// during construction, before exposing either service instance to callers.
func (s *Service) WithManagementAuthorizer(authorizer ManagementAuthorizer) *Service {
	configured := *s
	configured.managementAuthorizer = authorizer
	return &configured
}
