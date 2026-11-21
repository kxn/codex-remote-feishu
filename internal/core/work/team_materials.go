package work

import (
	"fmt"
	"net/url"
	"strings"
)

// TeamMaterials owns a stable shared original independently of every project.
// ScopeID is the original authorization/audit scope, never a copied project body.
type TeamMaterials struct {
	ProjectGrants map[string]TeamMaterialsGrant `json:"project_grants,omitempty"`
	ScopeID       string                        `json:"scope_id"`
	Generation    int64                         `json:"generation"`
	Selection     TeamMaterialsSelection        `json:"selection"`
	Identity      RoleBotBinding                `json:"identity"`
	Source        SourceRef                     `json:"source"`
	State         string                        `json:"state"`
	FolderID      string                        `json:"folder_id,omitempty"`
	DocumentID    string                        `json:"document_id,omitempty"`
	URL           string                        `json:"url,omitempty"`
	LastError     string                        `json:"last_error,omitempty"`
}

type TeamMaterialsGrant struct {
	Tenant            string    `json:"tenant"`
	ChatID            string    `json:"chat_id"`
	ChannelGeneration int64     `json:"channel_generation"`
	Source            SourceRef `json:"source"`
}

type TeamMaterialsSelection struct {
	// managed creates a Drive folder and Docx; drive/wiki use a selected parent;
	// existing adopts a real existing Docx/Wiki document without editing its body.
	Mode string `json:"mode"`
	URL  string `json:"url,omitempty"`
}

func (v TeamMaterialsSelection) Validate() error {
	if v.Mode == "managed" {
		if v.URL != "" {
			return fmt.Errorf("managed materials must not guess an existing directory")
		}
		return nil
	}
	if v.Mode != "drive" && v.Mode != "wiki" && v.Mode != "existing" {
		return fmt.Errorf("materials mode must be managed, drive, wiki, or existing")
	}
	u, err := url.Parse(v.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" {
		return fmt.Errorf("select an existing Feishu HTTPS materials URL")
	}
	host := strings.ToLower(u.Hostname())
	if !strings.HasSuffix(host, ".feishu.cn") && !strings.HasSuffix(host, ".larksuite.com") && !strings.HasSuffix(host, ".larkoffice.com") {
		return fmt.Errorf("materials URL must belong to Feishu or Lark")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	valid := v.Mode == "drive" && len(parts) == 3 && parts[0] == "drive" && parts[1] == "folder" && parts[2] != ""
	valid = valid || v.Mode == "wiki" && ((len(parts) == 2 && parts[0] == "wiki" && parts[1] != "") || (len(parts) == 3 && parts[0] == "wiki" && parts[1] == "space" && parts[2] != ""))
	valid = valid || v.Mode == "existing" && len(parts) == 2 && (parts[0] == "docx" || parts[0] == "wiki") && parts[1] != ""
	if !valid {
		return fmt.Errorf("materials URL does not match the selected mode")
	}
	return nil
}
func (v TeamMaterials) Validate() error {
	if err := required(v.ScopeID); err != nil {
		return err
	}
	if err := positive("materials generation", v.Generation); err != nil {
		return err
	}
	if err := v.Selection.Validate(); err != nil {
		return err
	}
	if err := v.Identity.Validate(); err != nil {
		return err
	}
	if err := v.Source.Validate(); err != nil {
		return err
	}
	switch v.State {
	case "pending", "ready", "unavailable", "unknown":
	default:
		return fmt.Errorf("invalid team materials state")
	}
	if v.State == "ready" && (v.DocumentID == "" || v.URL == "") {
		return fmt.Errorf("ready team materials require verified document and URL")
	}
	return nil
}
