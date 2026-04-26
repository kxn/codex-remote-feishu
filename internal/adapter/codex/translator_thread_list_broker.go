package codex

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type threadListQuery struct {
	Limit          int
	Cursor         string
	SortKey        string
	Archived       bool
	ModelProviders []string
	SourceKinds    []string
}

type threadListInflight struct {
	Key             string
	OwnerRequestID  string
	OwnerVisible    bool
	AliasRequestIDs []string
}

type threadListBroker struct {
	inflightByKey     map[string]*threadListInflight
	ownerKeyByRequest map[string]string
}

type threadListBrokerClientObservation struct {
	Query          threadListQuery
	Key            string
	OwnerRequestID string
	OwnerVisible   bool
	AliasCount     int
	Suppress       bool
	NewOwner       bool
}

type threadListBrokerOwner struct {
	RequestID string
	Visible   bool
}

type threadListBrokerResponseResolution struct {
	Key               string
	OwnerRequestID    string
	OwnerVisible      bool
	AliasRequestCount int
	AliasResponses    [][]byte
}

func newThreadListBroker() *threadListBroker {
	return &threadListBroker{
		inflightByKey:     map[string]*threadListInflight{},
		ownerKeyByRequest: map[string]string{},
	}
}

func defaultThreadListQuery() threadListQuery {
	return threadListQuery{
		Limit:   50,
		SortKey: "created_at",
	}
}

func normalizeThreadListQuery(params map[string]any) threadListQuery {
	query := defaultThreadListQuery()
	if params == nil {
		return query
	}
	if limit := lookupIntFromAny(params["limit"]); limit > 0 {
		query.Limit = limit
	}
	if cursor := strings.TrimSpace(lookupStringFromAny(firstNonNil(params["cursor"], params["pageToken"], params["page_token"]))); cursor != "" {
		query.Cursor = cursor
	}
	if sortKey := firstNonEmptyString(
		lookupStringFromAny(params["sortKey"]),
		lookupStringFromAny(params["sort_key"]),
	); sortKey != "" {
		query.SortKey = sortKey
	}
	if archived, ok := params["archived"]; ok {
		query.Archived = lookupBoolFromAny(archived)
	}
	query.ModelProviders = normalizeThreadListStringSlice(firstNonNil(params["modelProviders"], params["model_providers"]))
	query.SourceKinds = normalizeThreadListStringSlice(firstNonNil(params["sourceKinds"], params["source_kinds"]))
	return query
}

func (q threadListQuery) key() string {
	return fmt.Sprintf(
		"limit=%d|cursor=%s|sort=%s|archived=%t|models=%s|sources=%s",
		q.Limit,
		q.Cursor,
		q.SortKey,
		q.Archived,
		strings.Join(q.ModelProviders, ","),
		strings.Join(q.SourceKinds, ","),
	)
}

func normalizeThreadListStringSlice(source any) []string {
	raw := contentArrayValues(source)
	if len(raw) == 0 {
		return nil
	}
	values := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, current := range raw {
		value := strings.TrimSpace(lookupStringFromAny(current))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		values = append(values, value)
	}
	if len(values) == 0 {
		return nil
	}
	sort.Strings(values)
	return values
}

func (b *threadListBroker) ObserveClientRequest(requestID string, params map[string]any) threadListBrokerClientObservation {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return threadListBrokerClientObservation{}
	}
	query := normalizeThreadListQuery(params)
	key := query.key()
	observation := threadListBrokerClientObservation{
		Query: query,
		Key:   key,
	}
	if inflight := b.inflightByKey[key]; inflight != nil {
		observation.OwnerRequestID = inflight.OwnerRequestID
		observation.OwnerVisible = inflight.OwnerVisible
		if requestID == inflight.OwnerRequestID {
			return observation
		}
		inflight.AliasRequestIDs = append(inflight.AliasRequestIDs, requestID)
		observation.Suppress = true
		observation.AliasCount = len(inflight.AliasRequestIDs)
		return observation
	}
	b.inflightByKey[key] = &threadListInflight{
		Key:            key,
		OwnerRequestID: requestID,
		OwnerVisible:   true,
	}
	b.ownerKeyByRequest[requestID] = key
	observation.OwnerRequestID = requestID
	observation.OwnerVisible = true
	observation.NewOwner = true
	return observation
}

func (b *threadListBroker) LookupOwner(query threadListQuery) (threadListBrokerOwner, bool) {
	inflight := b.inflightByKey[query.key()]
	if inflight == nil {
		return threadListBrokerOwner{}, false
	}
	return threadListBrokerOwner{
		RequestID: inflight.OwnerRequestID,
		Visible:   inflight.OwnerVisible,
	}, true
}

func (b *threadListBroker) RegisterNativeOwner(requestID string, query threadListQuery) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return
	}
	key := query.key()
	b.inflightByKey[key] = &threadListInflight{
		Key:            key,
		OwnerRequestID: requestID,
		OwnerVisible:   false,
	}
	b.ownerKeyByRequest[requestID] = key
}

func (b *threadListBroker) ResolveResponse(message map[string]any) (threadListBrokerResponseResolution, bool, error) {
	requestID, ok := message["id"]
	if !ok {
		return threadListBrokerResponseResolution{}, false, nil
	}
	inflight, ok := b.takeInflightByOwner(fmt.Sprint(requestID))
	if !ok {
		return threadListBrokerResponseResolution{}, false, nil
	}
	aliasResponses, err := buildAliasedJSONRPCResponses(message, inflight.AliasRequestIDs)
	if err != nil {
		return threadListBrokerResponseResolution{}, false, err
	}
	return threadListBrokerResponseResolution{
		Key:               inflight.Key,
		OwnerRequestID:    inflight.OwnerRequestID,
		OwnerVisible:      inflight.OwnerVisible,
		AliasRequestCount: len(inflight.AliasRequestIDs),
		AliasResponses:    aliasResponses,
	}, true, nil
}

func (b *threadListBroker) InflightCount() int {
	if b == nil {
		return 0
	}
	return len(b.inflightByKey)
}

func (b *threadListBroker) takeInflightByOwner(requestID string) (*threadListInflight, bool) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil, false
	}
	key := b.ownerKeyByRequest[requestID]
	if strings.TrimSpace(key) == "" {
		return nil, false
	}
	delete(b.ownerKeyByRequest, requestID)
	inflight := b.inflightByKey[key]
	if inflight == nil || inflight.OwnerRequestID != requestID {
		return nil, false
	}
	delete(b.inflightByKey, key)
	return inflight, true
}

func buildAliasedJSONRPCResponses(message map[string]any, aliasRequestIDs []string) ([][]byte, error) {
	if len(aliasRequestIDs) == 0 {
		return nil, nil
	}
	out := make([][]byte, 0, len(aliasRequestIDs))
	for _, requestID := range aliasRequestIDs {
		requestID = strings.TrimSpace(requestID)
		if requestID == "" {
			continue
		}
		response := map[string]any{
			"id": requestID,
		}
		if version, ok := message["jsonrpc"]; ok {
			response["jsonrpc"] = version
		}
		if result, ok := message["result"]; ok {
			response["result"] = result
		}
		if errPayload, ok := message["error"]; ok {
			response["error"] = errPayload
		}
		bytes, err := json.Marshal(response)
		if err != nil {
			return nil, err
		}
		out = append(out, append(bytes, '\n'))
	}
	return out, nil
}
