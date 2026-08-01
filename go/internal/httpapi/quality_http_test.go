package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/dataset"
	"github.com/JasonTM17/PipeForge/go/internal/quality"
	"github.com/google/uuid"
)

func TestQualityRuleHTTPLifecycleAndOwnership(t *testing.T) {
	identityStore := newHTTPMemoryStore()
	identityService := newHTTPIdentityService(t, identityStore)
	datasetStore := newHTTPDatasetStore()
	qualityStore := newHTTPQualityStore()
	qualityService, err := quality.NewService(qualityStore, datasetStore)
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	router := NewRouter(Dependencies{Identity: identityService, Quality: qualityService})

	ownerTokens := registerHTTPUser(t, identityService, "quality-owner@example.com")
	datasetID := uuid.New()
	datasetStore.datasets[datasetID] = dataset.Dataset{ID: datasetID, OwnerUserID: ownerTokens.User.ID, Name: "quality-fixture", State: "AVAILABLE"}

	created := performJSONRequest(router, http.MethodPost, "/api/v1/quality-rules", `{"datasetId":"`+datasetID.String()+`","name":"Email required","columnScope":["email"],"ruleType":"NOT_NULL","configuration":{},"severity":"ERROR"}`, ownerTokens.AccessToken)
	if created.Code != http.StatusCreated {
		t.Fatalf("quality rule creation failed: %d %s", created.Code, created.Body.String())
	}
	var rule quality.Rule
	if err := json.Unmarshal(created.Body.Bytes(), &rule); err != nil || rule.DatasetID != datasetID || rule.Enabled != true {
		t.Fatalf("unexpected created quality rule: err=%v rule=%+v", err, rule)
	}

	listed := performJSONRequest(router, http.MethodGet, "/v1/datasets/"+datasetID.String()+"/quality-rules?page=1&pageSize=10", "", ownerTokens.AccessToken)
	if listed.Code != http.StatusOK || !containsJSON(listed.Body.String(), `"total":1`) {
		t.Fatalf("quality rule list failed: %d %s", listed.Code, listed.Body.String())
	}

	updated := performJSONRequest(router, http.MethodPatch, "/v1/quality-rules/"+rule.ID.String(), `{"severity":"WARNING","enabled":false}`, ownerTokens.AccessToken)
	if updated.Code != http.StatusOK || !containsJSON(updated.Body.String(), `"severity":"WARNING"`) || !containsJSON(updated.Body.String(), `"enabled":false`) {
		t.Fatalf("quality rule update failed: %d %s", updated.Code, updated.Body.String())
	}
	got := performJSONRequest(router, http.MethodGet, "/v1/quality-rules/"+rule.ID.String(), "", ownerTokens.AccessToken)
	if got.Code != http.StatusOK || !containsJSON(got.Body.String(), `"name":"Email required"`) {
		t.Fatalf("quality rule get failed: %d %s", got.Code, got.Body.String())
	}

	otherTokens := registerHTTPUser(t, identityService, "quality-other@example.com")
	crossOwnerList := performJSONRequest(router, http.MethodGet, "/v1/quality-rules?datasetId="+datasetID.String(), "", otherTokens.AccessToken)
	if crossOwnerList.Code != http.StatusForbidden {
		t.Fatalf("cross-owner list was not denied: %d %s", crossOwnerList.Code, crossOwnerList.Body.String())
	}
	crossOwnerGet := performJSONRequest(router, http.MethodGet, "/v1/quality-rules/"+rule.ID.String(), "", otherTokens.AccessToken)
	if crossOwnerGet.Code != http.StatusForbidden {
		t.Fatalf("cross-owner get was not denied: %d %s", crossOwnerGet.Code, crossOwnerGet.Body.String())
	}

	deleted := performJSONRequest(router, http.MethodDelete, "/api/v1/quality-rules/"+rule.ID.String(), "", ownerTokens.AccessToken)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("quality rule delete failed: %d %s", deleted.Code, deleted.Body.String())
	}
	notFound := performJSONRequest(router, http.MethodGet, "/api/v1/quality-rules/"+rule.ID.String(), "", ownerTokens.AccessToken)
	if notFound.Code != http.StatusNotFound {
		t.Fatalf("deleted quality rule should not be readable: %d %s", notFound.Code, notFound.Body.String())
	}
}

func TestQualityRuleHTTPRejectsUnsafeAndUnknownInput(t *testing.T) {
	identityStore := newHTTPMemoryStore()
	identityService := newHTTPIdentityService(t, identityStore)
	datasetStore := newHTTPDatasetStore()
	qualityStore := newHTTPQualityStore()
	qualityService, err := quality.NewService(qualityStore, datasetStore)
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	router := NewRouter(Dependencies{Identity: identityService, Quality: qualityService})
	tokens := registerHTTPUser(t, identityService, "quality-validation@example.com")
	datasetID := uuid.New()
	datasetStore.datasets[datasetID] = dataset.Dataset{ID: datasetID, OwnerUserID: tokens.User.ID, Name: "validation-fixture", State: "AVAILABLE"}

	unsafe := performJSONRequest(router, http.MethodPost, "/v1/quality-rules", `{"datasetId":"`+datasetID.String()+`","name":"Unsafe","columnScope":["value"],"ruleType":"CUSTOM_EXPRESSION","configuration":{"expression":"__import__('os')"},"severity":"ERROR"}`, tokens.AccessToken)
	if unsafe.Code != http.StatusBadRequest {
		t.Fatalf("custom expression was accepted: %d %s", unsafe.Code, unsafe.Body.String())
	}
	unknown := performJSONRequest(router, http.MethodPost, "/v1/quality-rules", `{"datasetId":"`+datasetID.String()+`","name":"Unknown","columnScope":["value"],"ruleType":"NOT_NULL","configuration":{},"severity":"ERROR","unexpected":true}`, tokens.AccessToken)
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown field was accepted: %d %s", unknown.Code, unknown.Body.String())
	}
}

type httpQualityStore struct {
	mu    sync.Mutex
	rules map[uuid.UUID]quality.Rule
}

func newHTTPQualityStore() *httpQualityStore {
	return &httpQualityStore{rules: make(map[uuid.UUID]quality.Rule)}
}

func (s *httpQualityStore) Create(_ context.Context, ownerID, actorID uuid.UUID, request quality.CreateRequest) (quality.Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.rules {
		if item.DeletedAt == nil && item.DatasetID == request.DatasetID && item.Name == request.Name {
			return quality.Rule{}, quality.ErrRuleNameUsed
		}
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	now := time.Now().UTC()
	item := quality.Rule{ID: uuid.New(), OwnerUserID: ownerID, DatasetID: request.DatasetID, Name: request.Name, ColumnScope: append([]string(nil), request.ColumnScope...), RuleType: request.RuleType, Configuration: cloneQualityConfig(request.Configuration), Severity: request.Severity, Enabled: enabled, CreatedBy: actorID, CreatedAt: now, UpdatedAt: now}
	s.rules[item.ID] = item
	return item, nil
}

func (s *httpQualityStore) List(_ context.Context, query quality.ListQuery) (quality.Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]quality.Rule, 0)
	for _, item := range s.rules {
		if item.DeletedAt != nil || (query.OwnerUserID != nil && item.OwnerUserID != *query.OwnerUserID) || (query.DatasetID != nil && item.DatasetID != *query.DatasetID) {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID.String() < items[j].ID.String() })
	start := (query.Page - 1) * query.PageSize
	if start >= len(items) {
		return quality.Page{Items: []quality.Rule{}, Page: query.Page, PageSize: query.PageSize, Total: int64(len(items))}, nil
	}
	end := start + query.PageSize
	if end > len(items) {
		end = len(items)
	}
	return quality.Page{Items: items[start:end], Page: query.Page, PageSize: query.PageSize, Total: int64(len(items))}, nil
}

func (s *httpQualityStore) Get(_ context.Context, ruleID uuid.UUID) (quality.Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.rules[ruleID]
	if !ok || item.DeletedAt != nil {
		return quality.Rule{}, quality.ErrRuleNotFound
	}
	return item, nil
}

func (s *httpQualityStore) Update(_ context.Context, ruleID, _ uuid.UUID, request quality.UpdateRequest) (quality.Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.rules[ruleID]
	if !ok || item.DeletedAt != nil {
		return quality.Rule{}, quality.ErrRuleNotFound
	}
	if request.Name != nil {
		item.Name = *request.Name
	}
	if request.ColumnScope != nil {
		item.ColumnScope = append([]string(nil), (*request.ColumnScope)...)
	}
	if request.RuleType != nil {
		item.RuleType = *request.RuleType
	}
	if request.Configuration != nil {
		item.Configuration = cloneQualityConfig(*request.Configuration)
	}
	if request.Severity != nil {
		item.Severity = *request.Severity
	}
	if request.Enabled != nil {
		item.Enabled = *request.Enabled
	}
	item.UpdatedAt = time.Now().UTC()
	s.rules[ruleID] = item
	return item, nil
}

func (s *httpQualityStore) Delete(_ context.Context, ruleID, _ uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.rules[ruleID]
	if !ok || item.DeletedAt != nil {
		return quality.ErrRuleNotFound
	}
	now := time.Now().UTC()
	item.DeletedAt = &now
	item.UpdatedAt = now
	s.rules[ruleID] = item
	return nil
}

func cloneQualityConfig(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func containsJSON(value, fragment string) bool {
	return strings.Contains(value, fragment)
}
