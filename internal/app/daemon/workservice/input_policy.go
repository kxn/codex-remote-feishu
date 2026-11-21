package workservice

import (
	"errors"
	"sort"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type workInputPolicy struct {
	ReportingDetail string                        `json:"conversation_reporting_detail"`
	Agreements      []work.ParticipationAgreement `json:"participation_agreements"`
	Practices       []work.PracticeAdoption       `json:"adopted_practices"`
	Resources       []work.ExecutionLocation      `json:"human_selected_code_resources"`
}

func (s *Service) inputPolicy(tx *workstore.Tx, wc work.WorkContext) (workInputPolicy, error) {
	result := workInputPolicy{ReportingDetail: "normal"}
	preferences, err := tx.GetContextPreferences(wc.ID)
	if err == nil && preferences.Verbose != nil {
		result.ReportingDetail = *preferences.Verbose
	}
	if err != nil && !errors.Is(err, workstore.ErrNotFound) {
		return result, err
	}
	scope, err := tx.GetScope(wc.ScopeID)
	if err != nil {
		return result, err
	}
	if scope.Kind != work.ScopeBootstrap {
		agreements, err := tx.ListAgreements(scope.ID)
		if err != nil {
			return result, err
		}
		superseded := map[string]bool{}
		for _, agreement := range agreements {
			if !agreement.ValidFrom.After(s.now()) && (agreement.ValidUntil == nil || agreement.ValidUntil.After(s.now())) && agreement.SupersedesID != "" {
				superseded[agreement.SupersedesID] = true
			}
		}
		for _, agreement := range agreements {
			if superseded[agreement.ID] || agreement.ValidFrom.After(s.now()) || agreement.ValidUntil != nil && !agreement.ValidUntil.After(s.now()) {
				continue
			}
			result.Agreements = append(result.Agreements, agreement)
		}
		adoptions, err := tx.ListPracticeAdoptions(scope.ID)
		if err != nil {
			return result, err
		}
		read := work.ReadScope{ScopeID: scope.ID, ContextID: wc.ID, TaskID: wc.TaskID, RoleID: wc.RoleID}
		for _, adoption := range adoptions {
			if adoption.TaskID != "" && adoption.TaskID != wc.TaskID {
				continue
			}
			if _, _, err = tx.GetPracticeFor(read, adoption.Owner, adoption.Practice); errors.Is(err, workstore.ErrScopeDenied) {
				continue
			} else if err != nil {
				return result, err
			}
			result.Practices = append(result.Practices, adoption)
		}
	}
	preference, err := tx.GetExecutionPreference(scope.ID)
	if err == nil {
		for _, id := range preference.SelectedLocationIDs {
			location, e := tx.GetLocation(id)
			if e != nil {
				return result, e
			}
			result.Resources = append(result.Resources, location)
		}
	} else if !errors.Is(err, workstore.ErrNotFound) {
		return result, err
	}
	sort.Slice(result.Agreements, func(i, j int) bool { return result.Agreements[i].ID < result.Agreements[j].ID })
	sort.Slice(result.Practices, func(i, j int) bool { return result.Practices[i].ID < result.Practices[j].ID })
	return result, nil
}
