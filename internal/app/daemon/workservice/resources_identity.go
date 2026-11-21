package workservice

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func resourceConnectorAllowed(scope work.WorkScope, resource work.Resource, connector ResourceConnector) bool {
	if resource.ScopeID != scope.ID {
		return false
	}
	if resource.ConnectorIdentity == connector.Identity() {
		return true
	}
	_, feishu := connector.(*FeishuResourceConnector)
	return scope.Kind == work.ScopeProject && feishu && work.IsFeishuResource(resource)
}

// Reuse an existing ID so historical versions and deliveries remain reachable.
// New Feishu project resources use the logical source, regardless of which Bot
// first presents it. Credential-bound non-Feishu resources retain their IDs.
func findRegisteredResource(tx *workstore.Tx, candidate work.Resource, sharedFeishu bool) (work.Resource, bool, error) {
	resources, err := tx.ListResources(candidate.ScopeID)
	if err != nil {
		return work.Resource{}, false, err
	}
	var selected work.Resource
	for _, resource := range resources {
		if resource.SourceIdentity != candidate.SourceIdentity {
			continue
		}
		if sharedFeishu && work.IsFeishuResource(resource) || !sharedFeishu && resource.ConnectorIdentity == candidate.ConnectorIdentity {
			if selected.ID == "" || resource.ID < selected.ID {
				selected = resource
			}
		}
	}
	return selected, selected.ID != "", nil
}

func newRegisteredResourceID(resource work.Resource, sharedFeishu bool) string {
	connector := resource.ConnectorIdentity
	if sharedFeishu {
		connector = "feishu"
	}
	return stableID("resource", resource.ScopeID, connector, resource.SourceIdentity)
}
