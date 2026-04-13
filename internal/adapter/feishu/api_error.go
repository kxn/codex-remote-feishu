package feishu

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

type APIErrorDetail struct {
	Key   string
	Value string
}

type APIErrorPermissionViolation struct {
	Type        string
	Subject     string
	Description string
}

type APIErrorHelp struct {
	URL         string
	Description string
}

type APIError struct {
	API                  string
	Code                 int
	Msg                  string
	RequestID            string
	LogID                string
	Troubleshooter       string
	Details              []APIErrorDetail
	PermissionViolations []APIErrorPermissionViolation
	Helps                []APIErrorHelp
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	api := strings.TrimSpace(e.API)
	if api == "" {
		api = "unknown"
	}
	msg := strings.TrimSpace(e.Msg)
	if msg == "" {
		return fmt.Sprintf("feishu api %s failed: code=%d", api, e.Code)
	}
	return fmt.Sprintf("feishu api %s failed: code=%d msg=%s", api, e.Code, msg)
}

type PermissionGapEvidence struct {
	Scope        string
	ScopeType    string
	ApplyURL     string
	ErrorCode    int
	ErrorMessage string
	SourceAPI    string
	RequestID    string
}

var permissionScopePattern = regexp.MustCompile(`([a-z][a-z0-9_.-]*:[a-z0-9_.-]+)`)

func newAPIError(api string, resp *larkcore.ApiResp, codeErr larkcore.CodeError) *APIError {
	err := &APIError{
		API:  strings.TrimSpace(api),
		Code: codeErr.Code,
		Msg:  strings.TrimSpace(codeErr.Msg),
	}
	if resp != nil {
		err.RequestID = strings.TrimSpace(resp.RequestId())
		err.LogID = strings.TrimSpace(resp.LogId())
	}
	if codeErr.Err == nil {
		return err
	}
	if strings.TrimSpace(codeErr.Err.LogID) != "" {
		err.LogID = strings.TrimSpace(codeErr.Err.LogID)
	}
	err.Troubleshooter = strings.TrimSpace(codeErr.Err.Troubleshooter)
	for _, item := range codeErr.Err.Details {
		if item == nil {
			continue
		}
		err.Details = append(err.Details, APIErrorDetail{
			Key:   strings.TrimSpace(item.Key),
			Value: strings.TrimSpace(item.Value),
		})
	}
	for _, item := range codeErr.Err.PermissionViolations {
		if item == nil {
			continue
		}
		err.PermissionViolations = append(err.PermissionViolations, APIErrorPermissionViolation{
			Type:        strings.TrimSpace(item.Type),
			Subject:     strings.TrimSpace(item.Subject),
			Description: strings.TrimSpace(item.Description),
		})
	}
	for _, item := range codeErr.Err.Helps {
		if item == nil {
			continue
		}
		err.Helps = append(err.Helps, APIErrorHelp{
			URL:         strings.TrimSpace(item.URL),
			Description: strings.TrimSpace(item.Description),
		})
	}
	return err
}

func ExtractPermissionGap(err error) (PermissionGapEvidence, bool) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		if gap, ok := permissionGapFromAPIError(apiErr); ok {
			return gap, true
		}
	}
	var driveErr *driveAPIError
	if errors.As(err, &driveErr) {
		if gap, ok := permissionGapFromDriveAPIError(driveErr); ok {
			return gap, true
		}
	}
	return PermissionGapEvidence{}, false
}

func permissionGapFromAPIError(err *APIError) (PermissionGapEvidence, bool) {
	if err == nil {
		return PermissionGapEvidence{}, false
	}
	gap := PermissionGapEvidence{
		ErrorCode:    err.Code,
		ErrorMessage: strings.TrimSpace(err.Msg),
		SourceAPI:    strings.TrimSpace(err.API),
		RequestID:    firstNonEmpty(strings.TrimSpace(err.RequestID), strings.TrimSpace(err.LogID)),
	}
	for _, item := range err.PermissionViolations {
		if scope := normalizePermissionScope(item.Subject); scope != "" {
			gap.Scope = scope
			gap.ScopeType = normalizePermissionScopeType(item.Type)
			break
		}
	}
	for _, item := range err.Details {
		key := strings.ToLower(strings.TrimSpace(item.Key))
		switch key {
		case "scope", "scope_name", "permission", "permission_scope":
			if gap.Scope == "" {
				gap.Scope = normalizePermissionScope(item.Value)
			}
		case "scope_type", "permission_type":
			if gap.ScopeType == "" {
				gap.ScopeType = normalizePermissionScopeType(item.Value)
			}
		}
	}
	if gap.Scope == "" {
		gap.Scope = firstPermissionScopeInText(
			err.Msg,
			permissionViolationDescriptions(err.PermissionViolations),
			detailValues(err.Details),
		)
	}
	gap.ApplyURL = firstPermissionURL(err)
	if gap.Scope == "" {
		return PermissionGapEvidence{}, false
	}
	return gap, true
}

func permissionGapFromDriveAPIError(err *driveAPIError) (PermissionGapEvidence, bool) {
	if err == nil {
		return PermissionGapEvidence{}, false
	}
	if !isPreviewDriveAccessDeniedError(err) {
		return PermissionGapEvidence{}, false
	}
	return PermissionGapEvidence{
		Scope:        "drive:drive",
		ScopeType:    "tenant",
		ApplyURL:     "",
		ErrorCode:    err.Code,
		ErrorMessage: strings.TrimSpace(err.Msg),
		SourceAPI:    firstNonEmpty(strings.TrimSpace(err.API), "drive.v1"),
		RequestID:    firstNonEmpty(strings.TrimSpace(err.RequestID), strings.TrimSpace(err.LogID)),
	}, true
}

func firstPermissionURL(err *APIError) string {
	if err == nil {
		return ""
	}
	for _, item := range err.Helps {
		if strings.TrimSpace(item.URL) != "" {
			return strings.TrimSpace(item.URL)
		}
	}
	if strings.TrimSpace(err.Troubleshooter) != "" {
		return strings.TrimSpace(err.Troubleshooter)
	}
	return ""
}

func normalizePermissionScope(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if permissionScopePattern.MatchString(value) {
		return permissionScopePattern.FindString(value)
	}
	return ""
}

func normalizePermissionScopeType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "tenant", "app", "tenant_access_token":
		return "tenant"
	case "user", "user_access_token":
		return "user"
	default:
		return value
	}
}

func firstPermissionScopeInText(values ...string) string {
	for _, value := range values {
		if scope := normalizePermissionScope(value); scope != "" {
			return scope
		}
	}
	return ""
}

func permissionViolationDescriptions(values []APIErrorPermissionViolation) string {
	parts := make([]string, 0, len(values))
	for _, item := range values {
		if strings.TrimSpace(item.Description) != "" {
			parts = append(parts, strings.TrimSpace(item.Description))
		}
	}
	return strings.Join(parts, "\n")
}

func detailValues(values []APIErrorDetail) string {
	parts := make([]string, 0, len(values))
	for _, item := range values {
		if strings.TrimSpace(item.Value) != "" {
			parts = append(parts, strings.TrimSpace(item.Value))
		}
	}
	return strings.Join(parts, "\n")
}
