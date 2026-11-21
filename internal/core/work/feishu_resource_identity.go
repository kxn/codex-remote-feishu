package work

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func feishuResourceURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return nil, fmt.Errorf("an HTTPS cloud document URL is required")
	}
	host := strings.ToLower(parsed.Hostname())
	if !strings.HasSuffix(host, ".feishu.cn") && !strings.HasSuffix(host, ".larksuite.com") && !strings.HasSuffix(host, ".larkoffice.com") {
		return nil, fmt.Errorf("resource link must use a recognized Feishu or Lark document host")
	}
	return parsed, nil
}

func ParseFeishuDocumentLink(value string) (kind, token string, err error) {
	parsed, err := feishuResourceURL(value)
	if err != nil {
		return "", "", err
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 || parts[1] == "" || strings.ContainsAny(parts[1], "/\\?# !:") {
		return "", "", fmt.Errorf("cloud document URL must identify one existing object")
	}
	switch parts[0] {
	case "docx", "wiki", "sheets", "base":
		return parts[0], parts[1], nil
	}
	return "", "", fmt.Errorf("cloud document URL type is not docx, wiki, sheets or base")
}

// IsFeishuResource recognizes persisted Feishu provenance and its canonical
// platform locator. A durable Wiki link may be an alias for the underlying
// document token verified at registration.
func IsFeishuResource(resource Resource) bool {
	identity, feishu := strings.CutPrefix(resource.ConnectorIdentity, "feishu:")
	lastColon := strings.LastIndexByte(identity, ':')
	if !feishu || lastColon < 1 {
		return false
	}
	gateway, app, ok := strings.Cut(identity[:lastColon], ":")
	if !ok || gateway == "" || app == "" {
		return false
	}
	generation, err := strconv.ParseInt(identity[lastColon+1:], 10, 64)
	if err != nil || generation < 1 {
		return false
	}
	sourceType, token, ok := strings.Cut(resource.SourceIdentity, ":")
	if !ok || token == "" {
		return false
	}
	if sourceType == "docx" || sourceType == "sheets" || sourceType == "base" {
		if strings.ContainsAny(token, "/\\?# !:") {
			return false
		}
		kind, locator, err := ParseFeishuDocumentLink(resource.CanonicalURI)
		return err == nil && (kind == "wiki" || kind == sourceType && locator == token)
	}
	parsed, err := feishuResourceURL(resource.CanonicalURI)
	if err != nil {
		return false
	}
	path := strings.Trim(parsed.Path, "/")
	if sourceType == "folder" {
		return !strings.ContainsAny(token, "/\\?# !:") && path == "drive/folder/"+token
	}
	space, node, wikiParent := strings.Cut(token, ":")
	if sourceType != "wiki_parent" || !wikiParent || space == "" || strings.ContainsAny(space+node, "/\\?# !:") {
		return false
	}
	return node == "" && path == "wiki/space/"+space || node != "" && path == "wiki/"+node
}
