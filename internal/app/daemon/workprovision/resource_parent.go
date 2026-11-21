package workprovision

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"net/url"
	"strings"
)

// registerMaterialParent exposes only the actual container established or
// verified by this provisioning saga, under its frozen role credential. It does
// not search titles, select another folder, or create a Wiki space.
func registerMaterialParent(tx *workstore.Tx, home workstore.ProjectHome, connectorIdentity string) error {
	if home.ContainerID == "" {
		return nil
	}
	parsed, err := url.Parse(home.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return fmt.Errorf("provisioned home has no canonical URL")
	}
	identity := ""
	switch home.ContainerKind {
	case "drive":
		identity = "folder:" + home.ContainerID
		parsed.Path = "/drive/folder/" + home.ContainerID
	case "wiki":
		parts := strings.SplitN(home.ContainerID, ":", 2)
		if len(parts) != 2 || parts[0] == "" {
			return fmt.Errorf("invalid actual Wiki parent receipt")
		}
		identity = "wiki_parent:" + home.ContainerID
		if parts[1] == "" {
			parsed.Path = "/wiki/space/" + parts[0]
		} else {
			parsed.Path = "/wiki/" + parts[1]
		}
	default:
		return fmt.Errorf("unknown provisioned container kind")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	resources, err := tx.ListResources(home.ScopeID)
	if err != nil {
		return err
	}
	for _, r := range resources {
		if r.ConnectorIdentity == connectorIdentity && r.SourceIdentity == identity {
			return tx.AddResourceCollection(home.ScopeID, r.ID, "project-materials")
		}
	}
	return tx.PutResource(work.Resource{ID: stableID("project-material-parent", home.ScopeID, connectorIdentity, identity), ScopeID: home.ScopeID, ConnectorIdentity: connectorIdentity, SourceIdentity: identity, CanonicalURI: parsed.String(), Purpose: "项目资料目录（建立项目时确认）", CollectionID: "project-materials", Revision: 1}, 0)
}
