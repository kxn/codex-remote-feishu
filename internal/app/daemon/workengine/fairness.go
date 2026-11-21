package workengine

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"sort"
)

func rotate(values []string, after string) []string {
	if len(values) == 0 {
		return nil
	}
	index := sort.SearchStrings(values, after)
	for index < len(values) && values[index] <= after {
		index++
	}
	return append(append([]string{}, values[index:]...), values[:index]...)
}

// fairTargets rotates scopes first, then roles within each scope. A scope with
// many roles therefore cannot consume every turn ahead of a smaller scope.
func fairTargets(targets []workstore.SchedulingTarget, cursor workstore.SchedulingCursor) []workstore.SchedulingTarget {
	roles := map[string][]string{}
	for _, target := range targets {
		roles[target.ScopeID] = append(roles[target.ScopeID], target.RoleID)
	}
	scopes := make([]string, 0, len(roles))
	for scope := range roles {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	scopes = rotate(scopes, cursor.LastScopeID)
	for scope, list := range roles {
		sort.Strings(list)
		roles[scope] = rotate(list, cursor.RoleByScope[scope])
	}
	result := []workstore.SchedulingTarget{}
	for round := 0; ; round++ {
		more := false
		for _, scope := range scopes {
			if round < len(roles[scope]) {
				result = append(result, workstore.SchedulingTarget{ScopeID: scope, RoleID: roles[scope][round]})
				more = true
			}
		}
		if !more {
			return result
		}
	}
}
