package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// PrivateModelWait stores only causal identity; the card and catalog remain in
// their existing publication and profile catalog stores.
type PrivateModelWait struct {
	TrackingKey, QueryID, InstanceID, ConfigurationHash string
	MessageID, ViewToken                                string
	SourceSequence                                      int64
	CatalogRevision                                     int64
	ObservationHash                                     string
	ProfileID, SettingsFingerprint, CatalogNamespace    string
}

// ProjectPrivateModelCatalog renders an already verified shared observation;
// only its identity and last applied revision are retained in the checkpoint.
func (s *Service) ProjectPrivateModelCatalog(surface, query, tracking, observationHash, profile, fingerprint, namespace string, revision int64, catalog *agentproto.ModelCatalogSnapshot, problem string) ([]eventcontract.Event, bool) {
	v := s.root.Surfaces[surface]
	r := s.surfaceUIRuntimeState(v)
	if r == nil || r.PrivateModelWait == nil || observationHash == "" {
		return nil, false
	}
	w := r.PrivateModelWait
	if w.QueryID != query || w.TrackingKey != tracking || w.ConfigurationHash != privateModelConfiguration(v) || namespace != "" && revision < w.CatalogRevision || w.ObservationHash == observationHash {
		return nil, false
	}
	if w.ProfileID != "" && (w.ProfileID != profile || w.SettingsFingerprint != fingerprint || w.CatalogNamespace != "" && namespace != "" && w.CatalogNamespace != namespace) {
		return nil, false
	}
	flow, ok := control.FeishuConfigFlowDefinitionByCommandID(control.FeishuCommandModel)
	if !ok {
		return nil, false
	}
	if catalog == nil {
		catalog = &agentproto.ModelCatalogSnapshot{}
	}
	card := control.FeishuCatalogConfigView{}
	if problem != "" {
		card.StatusKind, card.StatusText = "error", problem
	}
	view := s.buildConfigCommandViewStateWithCatalog(v, flow, card, catalog)
	page := s.configPageEventFromCatalogView(v, view)
	if page.PageView == nil {
		return nil, false
	}
	page.PageView.TrackingKey, page.PageView.MessageID = w.TrackingKey, w.MessageID
	page.PageView.PrivateModelResult, page.PageView.PrivateExpectedViewToken = true, w.ViewToken
	page.PageView.PrivateCatalogNamespace = namespace
	addPrivateModelRefresh(page.PageView)
	page.Payload = nil
	page = page.Normalized()
	w.ObservationHash = observationHash
	if namespace != "" {
		w.CatalogRevision = revision
	}
	w.ProfileID, w.SettingsFingerprint = profile, fingerprint
	if namespace != "" {
		w.CatalogNamespace = namespace
	}
	return []eventcontract.Event{page}, true
}

func privateModelConfiguration(v *state.SurfaceConsoleRecord) string {
	raw, _ := json.Marshal(state.SurfaceDesiredBackendContract(v))
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s *Service) beginPrivateModelWait(v *state.SurfaceConsoleRecord, action control.Action, page *eventcontract.Event) {
	if !s.config.PrivateApplication || v == nil || page == nil || page.PageView == nil {
		return
	}
	w := &PrivateModelWait{TrackingKey: s.pickers.nextLauncherFlowToken(), InstanceID: v.AttachedInstanceID, ConfigurationHash: privateModelConfiguration(v)}
	if action.Inbound != nil && action.Inbound.CardCallback {
		w.MessageID = action.Inbound.OpenMessageID
	}
	s.ensureSurfaceUIRuntime(v).PrivateModelWait = w
	page.PageView.TrackingKey = w.TrackingKey
	addPrivateModelRefresh(page.PageView)
	page.Payload = nil
	*page = page.Normalized()
}

func (s *Service) BindPrivateModelQuery(surface, command string) {
	v := s.root.Surfaces[surface]
	r := s.surfaceUIRuntimeState(v)
	if r != nil && r.PrivateModelWait != nil && r.PrivateModelWait.QueryID == "" {
		r.PrivateModelWait.QueryID = command
	}
}

func (s *Service) RecordPrivateModelCardResponse(surface string, seq int64, message, token string) {
	v := s.root.Surfaces[surface]
	r := s.surfaceUIRuntimeState(v)
	if r != nil && r.PrivateModelWait != nil && r.PrivateModelWait.MessageID == message && message != "" {
		r.PrivateModelWait.SourceSequence, r.PrivateModelWait.ViewToken = seq, token
	}
}

func (s *Service) RecordPrivateModelPublishedView(surface, tracking, message, token string) {
	v := s.root.Surfaces[surface]
	r := s.surfaceUIRuntimeState(v)
	if r != nil && r.PrivateModelWait != nil && r.PrivateModelWait.TrackingKey == tracking && message != "" && token != "" {
		r.PrivateModelWait.MessageID, r.PrivateModelWait.ViewToken = message, token
	}
}

func (s *Service) PrivateModelWaiting(surface, tracking string) (PrivateModelWait, bool) {
	v := s.root.Surfaces[surface]
	r := s.surfaceUIRuntimeState(v)
	if r == nil || r.PrivateModelWait == nil || r.PrivateModelWait.TrackingKey != tracking || r.PrivateModelWait.ConfigurationHash != privateModelConfiguration(v) {
		return PrivateModelWait{}, false
	}
	return *r.PrivateModelWait, true
}

func (s *Service) privateModelCatalogResult(instance string, event agentproto.Event) []eventcontract.Event {
	if !s.config.PrivateApplication || event.CommandID == "" {
		return nil
	}
	var result []eventcontract.Event
	for _, v := range s.root.Surfaces {
		r := s.surfaceUIRuntimeState(v)
		if r == nil || r.PrivateModelWait == nil {
			continue
		}
		w := r.PrivateModelWait
		if w.QueryID != event.CommandID || w.InstanceID != instance || v.AttachedInstanceID != instance || w.ConfigurationHash != privateModelConfiguration(v) || !s.privateModelInstanceMatches(v, s.root.Instances[instance]) {
			continue
		}
		view := s.buildConfigCommandView(v, control.FeishuCommandModel)
		if view.Config == nil {
			continue
		}
		page := s.configPageEventFromCatalogView(v, view)
		if page.PageView == nil {
			continue
		}
		page.PageView.TrackingKey, page.PageView.MessageID = w.TrackingKey, w.MessageID
		page.PageView.PrivateModelResult = true
		page.PageView.PrivateExpectedViewToken = w.ViewToken
		addPrivateModelRefresh(page.PageView)
		page.Payload = nil
		page = page.Normalized()
		result = append(result, page)
	}
	return result
}

func addPrivateModelRefresh(page *control.FeishuPageView) {
	page.RelatedButtons = append(page.RelatedButtons, control.CommandCatalogButton{Label: "刷新模型列表", Kind: control.CommandCatalogButtonAction, CommandText: "/model refresh"})
}
