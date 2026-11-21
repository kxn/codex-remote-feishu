package workprovision

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// ResolveUnknown is only for a trusted human/admin ingress; there is deliberately
// no Agent CLI command or caller-controlled approval boolean. The selected ID is
// read from the platform before an old write can be considered confirmed.
func (s *Service) ResolveUnknown(ctx context.Context, recovery Recovery) (Report, error) {
	state, err := s.load(ctx, recovery.OperationID)
	if err != nil {
		return Report{}, err
	}
	var step workstore.ProvisionStep
	replayed := false
	err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		if recovery.StepID == "home.index" {
			if state.home.Revision != recovery.ExpectedRevision || recovery.ExternalID != state.home.DocumentID {
				return workstore.ErrConflict
			}
		} else {
			step, err = tx.GetProvisionStep(recovery.OperationID, recovery.StepID)
			if err != nil {
				return err
			}
			if step.State == work.OperationConfirmed {
				sourceKey := recovery.SourceMessageID
				if recovery.InteractionID != "" {
					sourceKey = recovery.InteractionID
				}
				audit, e := tx.GetProvisionStep(recovery.OperationID, "recovery."+recovery.StepID+"."+sourceKey)
				if e != nil {
					return workstore.ErrConflict
				}
				var original struct {
					ExternalID   string `json:"external_id"`
					SecondaryID  string `json:"secondary_id"`
					CandidateURL string `json:"candidate_url"`
					Revision     int64  `json:"request_revision"`
				}
				if e = json.Unmarshal(audit.Receipt, &original); e != nil {
					return e
				}
				if original.Revision != recovery.ExpectedRevision || original.SecondaryID != recovery.SecondaryID || original.CandidateURL != recovery.CandidateURL || (recovery.CandidateURL == "" && original.ExternalID != recovery.ExternalID) {
					return workstore.ErrIdempotencyConflict
				}
				if recovery.InteractionID != "" {
					if e = s.validateRecoveryInteraction(tx, state, recovery, time.Time{}); e != nil {
						return e
					}
				} else {
					message, e := tx.GetMessage(recovery.SourceMessageID)
					if e != nil {
						return e
					}
					if message.Source != state.operation.Actor {
						return workstore.ErrScopeDenied
					}
				}
				replayed = true
				return nil
			}
			if step.Revision != recovery.ExpectedRevision || step.State != work.OperationUnknown {
				return workstore.ErrConflict
			}
		}
		if recovery.InteractionID != "" {
			after := step.UpdatedAt
			if recovery.StepID == "home.index" {
				after = state.home.UpdatedAt
			}
			return s.validateRecoveryInteraction(tx, state, recovery, after)
		}
		message, err := tx.GetMessage(recovery.SourceMessageID)
		if err != nil {
			return err
		}
		if message.Source != state.operation.Actor || message.Source.Kind != work.ActorPrincipal || message.ID == state.request.SourceMessageID || message.DerivedFrom != nil {
			return workstore.ErrScopeDenied
		}
		if message.PlatformKey == nil || message.PlatformKey.Tenant != state.request.Tenant || message.PlatformKey.ChatID != state.request.ChatID {
			return workstore.ErrScopeDenied
		}
		after := step.UpdatedAt
		if recovery.StepID == "home.index" {
			after = state.home.UpdatedAt
		}
		if message.CreatedAt.Before(after) {
			return fmt.Errorf("recovery requires a new human instruction after the uncertain result")
		}
		return nil
	})
	if err != nil {
		return Report{}, err
	}
	if replayed {
		return s.report(ctx, state, nil)
	}
	platform, err := s.platform(ctx, state.home.Identity)
	if err != nil {
		return Report{}, err
	}
	if recovery.CandidateURL != "" {
		switch recovery.StepID {
		case "materials.document":
			resolver, ok := platform.(RecoveryDocumentResolver)
			if !ok {
				return Report{}, fmt.Errorf("document URL resolver unavailable")
			}
			document, err := resolver.ResolveRecoveryDocument(ctx, recovery.CandidateURL)
			if err != nil {
				return Report{}, err
			}
			recovery.ExternalID = document.ID
		case "materials.folder":
			link, err := url.Parse(recovery.CandidateURL)
			if err != nil || link.Scheme != "https" {
				return Report{}, fmt.Errorf("需要飞书文件夹链接")
			}
			host := strings.ToLower(link.Hostname())
			parts := strings.Split(strings.Trim(link.Path, "/"), "/")
			if (!strings.HasSuffix(host, ".feishu.cn") && !strings.HasSuffix(host, ".larksuite.com")) || len(parts) != 3 || parts[0] != "drive" || parts[1] != "folder" {
				return Report{}, fmt.Errorf("需要实际飞书文件夹链接")
			}
			recovery.ExternalID = parts[2]
		default:
			return Report{}, fmt.Errorf("此恢复需要明确选择实际存在的对象")
		}
	}

	if recovery.StepID == "home.index" {
		blocks, err := platform.ReadHomeBlocks(ctx, state.home.DocumentID, state.home.IndexBlockID, state.home.StatusBlockID)
		if err != nil {
			return Report{}, err
		}
		proof, _ := json.Marshal(map[string]any{"source_message_id": recovery.SourceMessageID, "source_interaction_id": recovery.InteractionID, "source_view_id": recovery.ViewID, "resolution": "human_authorized_managed_index_replace", "original_home": state.home, "observed_blocks": blocks})
		sourceKey := recovery.SourceMessageID
		if recovery.InteractionID != "" {
			sourceKey = recovery.InteractionID
		}
		if _, err = s.step(ctx, state, "recovery.home.index."+sourceKey, state.home.Identity, json.RawMessage(proof), false, func(context.Context, json.RawMessage) (effect, error) {
			return effect{ID: state.home.DocumentID, Data: json.RawMessage(proof)}, nil
		}); err != nil {
			return Report{}, err
		}
		if err = s.homeUpdate(ctx, &state, func(home *workstore.ProjectHome) {
			home.IndexBaseline = blocks.Index.Fingerprint
			home.StatusBaseline = blocks.Status.Fingerprint
			home.SyncState = "pending"
		}); err != nil {
			return Report{}, err
		}
		if err = s.RefreshHome(ctx, state.project.ID); err != nil {
			return Report{}, err
		}
		return s.report(ctx, state, nil)
	}
	if recovery.ExternalID == "" {
		return Report{}, fmt.Errorf("recovery requires an exact external ID")
	}
	var receipt effect
	stepKind := recovery.StepID
	if IsNavigationTabRestoreStep(stepKind) {
		stepKind = "navigation.tab"
	}
	switch stepKind {
	case "materials.folder":
		if err = platform.CheckFolder(ctx, recovery.ExternalID); err != nil {
			return Report{}, err
		}
		receipt = effect{ID: recovery.ExternalID, Data: Folder{ID: recovery.ExternalID}}
	case "materials.document":
		doc, err := platform.ReadDocument(ctx, recovery.ExternalID)
		if err != nil {
			return Report{}, err
		}
		receipt = effect{ID: doc.ID, Data: doc}
	case "materials.index":
		if recovery.SecondaryID == "" {
			return Report{}, fmt.Errorf("index recovery also requires the actual status block ID")
		}
		blocks, err := platform.ReadHomeBlocks(ctx, state.home.DocumentID, recovery.ExternalID, recovery.SecondaryID)
		if err != nil {
			return Report{}, err
		}
		var desired HomeIndex
		if err = json.Unmarshal(step.Intent, &desired); err != nil {
			return Report{}, err
		}
		text, status := renderIndex(desired)
		if blocks.Index.Text != text || blocks.Status.Text != status {
			return Report{}, fmt.Errorf("candidate blocks differ from the exact recorded index intent")
		}
		receipt = effect{ID: recovery.ExternalID, Data: HomeBlockIDs{IndexID: recovery.ExternalID, StatusID: recovery.SecondaryID, Revision: blocks.Revision}}
	case "navigation.tab":
		tabs, err := platform.ListTabs(ctx, state.request.ChatID)
		if err != nil {
			return Report{}, err
		}
		found := false
		for _, tab := range tabs {
			if tab.ID == recovery.ExternalID && (tab.Type == "doc" || tab.Type == "url") && tab.URL == state.home.URL {
				receipt = effect{ID: tab.ID, Data: tab}
				found = true
				break
			}
		}
		if !found {
			return Report{}, fmt.Errorf("candidate tab ID does not point to the exact selected home")
		}
	case "navigation.message":
		message, err := platform.ReadNavigation(ctx, recovery.ExternalID)
		if err != nil {
			return Report{}, err
		}
		var desired Navigation
		if err = json.Unmarshal(step.Intent, &desired); err != nil {
			return Report{}, err
		}
		if message.ChatID != state.request.ChatID || message.Text != renderNavigation(desired) {
			return Report{}, fmt.Errorf("candidate message does not match the recorded navigation")
		}
		receipt = effect{ID: message.MessageID, Data: message}
	default:
		return Report{}, fmt.Errorf("this effect requires its owning component's recovery evidence")
	}
	sourceKey := recovery.SourceMessageID
	if recovery.InteractionID != "" {
		sourceKey = recovery.InteractionID
	}
	approval, _ := json.Marshal(map[string]any{"source_interaction_id": recovery.InteractionID, "source_view_id": recovery.ViewID, "candidate_url": recovery.CandidateURL, "resolution": "human_selected_verified_existing", "original_call_state": "unknown", "original_blocker": step.Blocker, "original_receipt": step.Receipt, "source_message_id": recovery.SourceMessageID, "external_id": recovery.ExternalID, "secondary_id": recovery.SecondaryID, "request_revision": recovery.ExpectedRevision})
	_, err = s.step(ctx, state, "recovery."+step.StepID+"."+sourceKey, state.home.Identity, json.RawMessage(approval), false, func(context.Context, json.RawMessage) (effect, error) {
		return effect{ID: recovery.ExternalID, Data: json.RawMessage(approval)}, nil
	})
	if err != nil {
		return Report{}, err
	}
	if _, err = s.confirmKnown(ctx, state, step, receipt); err != nil {
		return Report{}, err
	}
	if IsNavigationTabRestoreStep(recovery.StepID) {
		generation := state.home.NavigationGeneration
		if recovery.StepID != navigationTabRestoreStep(state.home.TabID) {
			return Report{}, workstore.ErrConflict
		}
		if err = s.RestoreFixedTab(ctx, state.project.ID, generation); err != nil {
			return Report{}, err
		}
		return s.report(ctx, state, nil)
	}
	return s.Run(ctx, recovery.OperationID)
}
