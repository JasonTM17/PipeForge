package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/dataset"
	"github.com/JasonTM17/PipeForge/go/internal/storage"
	"github.com/JasonTM17/PipeForge/go/internal/upload"
	"github.com/google/uuid"
)

func TestDatasetHTTPLifecycleAndOwnership(t *testing.T) {
	identityStore := newHTTPMemoryStore()
	identityService := newHTTPIdentityService(t, identityStore)
	datasetStore := newHTTPDatasetStore()
	objects := newHTTPObjectStore()
	datasetService, err := dataset.NewService(datasetStore, objects, 1024)
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	router := NewRouter(Dependencies{Identity: identityService, Dataset: datasetService})

	registerResponse := performJSONRequest(router, http.MethodPost, "/v1/auth/register", `{"email":"owner@example.com","password":"correct horse battery staple"}`, "")
	if registerResponse.Code != http.StatusCreated {
		t.Fatalf("registration failed: %d %s", registerResponse.Code, registerResponse.Body.String())
	}
	var ownerTokens tokenResponse
	if err := json.Unmarshal(registerResponse.Body.Bytes(), &ownerTokens); err != nil {
		t.Fatalf("decode owner tokens: %v", err)
	}

	createdResponse := performJSONRequest(router, http.MethodPost, "/v1/datasets", `{"name":"sales","description":"daily sales"}`, ownerTokens.AccessToken)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("dataset creation failed: %d %s", createdResponse.Code, createdResponse.Body.String())
	}
	var created dataset.Dataset
	if err := json.Unmarshal(createdResponse.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode dataset: %v", err)
	}

	body := []byte("{\"id\":1}\n")
	uploadResponse := performDatasetUpload(router, created.ID, body, ownerTokens.AccessToken, "sales.jsonl", "application/jsonl")
	if uploadResponse.Code != http.StatusCreated {
		t.Fatalf("dataset upload failed: %d %s", uploadResponse.Code, uploadResponse.Body.String())
	}
	var uploaded dataset.DatasetVersion
	if err := json.Unmarshal(uploadResponse.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("decode uploaded version: %v", err)
	}
	if uploaded.State != "AVAILABLE" || uploaded.Format != upload.FormatJSONL || uploaded.ObjectKey != "" {
		t.Fatalf("unexpected uploaded version response: %+v", uploaded)
	}
	if len(objects.objects) != 1 {
		t.Fatalf("expected one stored object, got %d", len(objects.objects))
	}

	detailResponse := performJSONRequest(router, http.MethodGet, "/v1/datasets/"+created.ID.String(), "", ownerTokens.AccessToken)
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("dataset get failed: %d %s", detailResponse.Code, detailResponse.Body.String())
	}
	var detail datasetDetailResponse
	if err := json.Unmarshal(detailResponse.Body.Bytes(), &detail); err != nil || len(detail.Versions) != 1 || detail.Versions[0].ObjectKey != "" {
		t.Fatalf("dataset detail exposed or lost object metadata: err=%v detail=%+v", err, detail)
	}

	listResponse := performJSONRequest(router, http.MethodGet, "/v1/datasets?page=1&pageSize=10&name=sales", "", ownerTokens.AccessToken)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"total":1`) {
		t.Fatalf("dataset list failed: %d %s", listResponse.Code, listResponse.Body.String())
	}

	secondRegister := performJSONRequest(router, http.MethodPost, "/v1/auth/register", `{"email":"other@example.com","password":"correct horse battery staple"}`, "")
	var otherTokens tokenResponse
	if secondRegister.Code != http.StatusCreated || json.Unmarshal(secondRegister.Body.Bytes(), &otherTokens) != nil {
		t.Fatalf("second registration failed: %d %s", secondRegister.Code, secondRegister.Body.String())
	}
	crossOwnerGet := performJSONRequest(router, http.MethodGet, "/v1/datasets/"+created.ID.String(), "", otherTokens.AccessToken)
	if crossOwnerGet.Code != http.StatusForbidden {
		t.Fatalf("cross-owner get was not denied: %d %s", crossOwnerGet.Code, crossOwnerGet.Body.String())
	}
	crossOwnerUpload := performDatasetUpload(router, created.ID, body, otherTokens.AccessToken, "other.jsonl", "application/jsonl")
	if crossOwnerUpload.Code != http.StatusForbidden {
		t.Fatalf("cross-owner upload was not denied: %d %s", crossOwnerUpload.Code, crossOwnerUpload.Body.String())
	}

	deleted := performJSONRequest(router, http.MethodDelete, "/v1/datasets/"+created.ID.String(), "", ownerTokens.AccessToken)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("dataset delete failed: %d %s", deleted.Code, deleted.Body.String())
	}
	deletedGet := performJSONRequest(router, http.MethodGet, "/v1/datasets/"+created.ID.String(), "", ownerTokens.AccessToken)
	if deletedGet.Code != http.StatusForbidden {
		t.Fatalf("deleted dataset should not be readable: %d %s", deletedGet.Code, deletedGet.Body.String())
	}
}

func TestDatasetHTTPRejectsInvalidUploadMetadata(t *testing.T) {
	identityStore := newHTTPMemoryStore()
	identityService := newHTTPIdentityService(t, identityStore)
	datasetStore := newHTTPDatasetStore()
	datasetService, err := dataset.NewService(datasetStore, newHTTPObjectStore(), 1024)
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	router := NewRouter(Dependencies{Identity: identityService, Dataset: datasetService})
	registerResponse := performJSONRequest(router, http.MethodPost, "/v1/auth/register", `{"email":"fixture@example.com","password":"correct horse battery staple"}`, "")
	var tokens tokenResponse
	if registerResponse.Code != http.StatusCreated || json.Unmarshal(registerResponse.Body.Bytes(), &tokens) != nil {
		t.Fatalf("registration failed: %d %s", registerResponse.Code, registerResponse.Body.String())
	}
	ownerID := tokens.User.ID
	datasetStore.datasets[ownerID] = dataset.Dataset{ID: ownerID, OwnerUserID: ownerID, Name: "fixture", State: "REGISTERED"}
	invalidName := performDatasetUpload(router, ownerID, []byte("a,b\n1,2\n"), tokens.AccessToken, "../escape.csv", "text/csv")
	if invalidName.Code != http.StatusBadRequest {
		t.Fatalf("unsafe filename was not rejected: %d %s", invalidName.Code, invalidName.Body.String())
	}
	missingName := performDatasetUpload(router, ownerID, []byte("a,b\n1,2\n"), tokens.AccessToken, "", "text/csv")
	if missingName.Code != http.StatusBadRequest {
		t.Fatalf("missing filename was not rejected: %d %s", missingName.Code, missingName.Body.String())
	}
}

func performDatasetUpload(router http.Handler, datasetID uuid.UUID, body []byte, bearer, filename, contentType string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/datasets/"+datasetID.String()+"/versions", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+bearer)
	request.Header.Set("Content-Type", contentType)
	if filename != "" {
		request.Header.Set("X-Filename", filename)
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

type httpDatasetStore struct {
	datasets map[uuid.UUID]dataset.Dataset
	versions map[uuid.UUID]dataset.DatasetVersion
}

func newHTTPDatasetStore() *httpDatasetStore {
	return &httpDatasetStore{datasets: make(map[uuid.UUID]dataset.Dataset), versions: make(map[uuid.UUID]dataset.DatasetVersion)}
}

func (s *httpDatasetStore) CreateDataset(_ context.Context, ownerID uuid.UUID, name, description string) (dataset.Dataset, error) {
	now := time.Now().UTC()
	item := dataset.Dataset{ID: uuid.New(), OwnerUserID: ownerID, Name: name, Description: description, State: "REGISTERED", CreatedAt: now, UpdatedAt: now}
	s.datasets[item.ID] = item
	return item, nil
}

func (s *httpDatasetStore) FindDataset(_ context.Context, datasetID uuid.UUID) (dataset.Dataset, error) {
	item, ok := s.datasets[datasetID]
	if !ok {
		return dataset.Dataset{}, dataset.ErrDatasetNotFound
	}
	return item, nil
}

func (s *httpDatasetStore) ListDatasets(_ context.Context, ownerID *uuid.UUID, page, pageSize int, state, name string) (dataset.DatasetPage, error) {
	items := make([]dataset.Dataset, 0)
	for _, item := range s.datasets {
		if item.DeletedAt != nil || (ownerID != nil && item.OwnerUserID != *ownerID) || (state != "" && item.State != state) || (name != "" && !strings.Contains(strings.ToLower(item.Name), strings.ToLower(name))) {
			continue
		}
		items = append(items, item)
	}
	start := (page - 1) * pageSize
	if start >= len(items) {
		return dataset.DatasetPage{Items: []dataset.Dataset{}, Total: int64(len(items))}, nil
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return dataset.DatasetPage{Items: items[start:end], Total: int64(len(items))}, nil
}

func (s *httpDatasetStore) DeleteDataset(_ context.Context, datasetID, _ uuid.UUID) error {
	item, ok := s.datasets[datasetID]
	if !ok {
		return dataset.ErrDatasetNotFound
	}
	now := time.Now().UTC()
	item.State = "DELETED"
	item.DeletedAt = &now
	s.datasets[datasetID] = item
	return nil
}

func (s *httpDatasetStore) ReserveVersion(_ context.Context, reservation dataset.VersionReservation) (dataset.DatasetVersion, error) {
	item, ok := s.datasets[reservation.DatasetID]
	if !ok {
		return dataset.DatasetVersion{}, dataset.ErrDatasetNotFound
	}
	if item.DeletedAt != nil {
		return dataset.DatasetVersion{}, dataset.ErrDatasetDeleted
	}
	number := int32(1)
	for _, version := range s.versions {
		if version.DatasetID == reservation.DatasetID && version.VersionNumber >= number {
			number = version.VersionNumber + 1
		}
	}
	version := dataset.DatasetVersion{ID: reservation.ID, DatasetID: reservation.DatasetID, VersionNumber: number, State: "UPLOADING", OriginalFilename: reservation.OriginalFilename, ContentType: reservation.ContentType, Format: reservation.Format, ObjectKey: reservation.ObjectKey, SourceInfo: reservation.SourceInfo, ArtifactPrefix: reservation.ArtifactPrefix, CreatedBy: reservation.CreatedBy, CreatedAt: time.Now().UTC()}
	s.versions[version.ID] = version
	item.State = "UPLOADING"
	s.datasets[item.ID] = item
	return version, nil
}

func (s *httpDatasetStore) FinalizeVersion(_ context.Context, datasetID, versionID, _ uuid.UUID, size int64, checksum string) (dataset.DatasetVersion, error) {
	version, ok := s.versions[versionID]
	if !ok || version.DatasetID != datasetID {
		return dataset.DatasetVersion{}, dataset.ErrVersionNotFound
	}
	if version.State == "AVAILABLE" {
		if version.SizeBytes == nil || *version.SizeBytes != size || version.ChecksumSHA256 == nil || *version.ChecksumSHA256 != checksum {
			return dataset.DatasetVersion{}, dataset.ErrVersionState
		}
		return version, nil
	}
	now := time.Now().UTC()
	version.State = "AVAILABLE"
	version.SizeBytes = &size
	version.ChecksumSHA256 = &checksum
	version.AvailableAt = &now
	s.versions[versionID] = version
	item := s.datasets[datasetID]
	item.State = "AVAILABLE"
	s.datasets[datasetID] = item
	return version, nil
}

func (s *httpDatasetStore) FinalizeVersionForUpload(ctx context.Context, datasetID, versionID, actorID uuid.UUID, size int64, checksum string, _, _ uuid.UUID) (dataset.DatasetVersion, error) {
	return s.FinalizeVersion(ctx, datasetID, versionID, actorID, size, checksum)
}

func (s *httpDatasetStore) FindVersion(_ context.Context, datasetID, versionID uuid.UUID) (dataset.DatasetVersion, error) {
	version, ok := s.versions[versionID]
	if !ok || version.DatasetID != datasetID {
		return dataset.DatasetVersion{}, dataset.ErrVersionNotFound
	}
	return version, nil
}

func (s *httpDatasetStore) FindVersionByID(_ context.Context, versionID uuid.UUID) (dataset.DatasetVersion, error) {
	version, ok := s.versions[versionID]
	if !ok {
		return dataset.DatasetVersion{}, dataset.ErrVersionNotFound
	}
	return version, nil
}

func (s *httpDatasetStore) FailVersion(_ context.Context, datasetID, versionID, _ uuid.UUID, _ string) error {
	version, ok := s.versions[versionID]
	if !ok || version.DatasetID != datasetID {
		return dataset.ErrVersionNotFound
	}
	version.State = "FAILED"
	s.versions[versionID] = version
	item := s.datasets[datasetID]
	item.State = "REGISTERED"
	s.datasets[datasetID] = item
	return nil
}

func (s *httpDatasetStore) AbortVersion(_ context.Context, _, versionID, _ uuid.UUID) error {
	if _, ok := s.versions[versionID]; !ok {
		return dataset.ErrVersionNotFound
	}
	delete(s.versions, versionID)
	return nil
}

func (s *httpDatasetStore) ListVersions(_ context.Context, datasetID uuid.UUID) ([]dataset.DatasetVersion, error) {
	versions := make([]dataset.DatasetVersion, 0)
	for _, version := range s.versions {
		if version.DatasetID == datasetID {
			versions = append(versions, version)
		}
	}
	return versions, nil
}

var _ dataset.Store = (*httpDatasetStore)(nil)

type httpObjectStore struct {
	objects map[string][]byte
}

func newHTTPObjectStore() *httpObjectStore {
	return &httpObjectStore{objects: make(map[string][]byte)}
}

func (s *httpObjectStore) Put(_ context.Context, key string, reader io.Reader, _ int64, contentType string) (storage.ObjectInfo, error) {
	value, err := io.ReadAll(reader)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	s.objects[key] = value
	return storage.ObjectInfo{Key: key, Size: int64(len(value)), ContentType: contentType}, nil
}

func (s *httpObjectStore) Head(_ context.Context, key string) (storage.ObjectInfo, error) {
	value, ok := s.objects[key]
	if !ok {
		return storage.ObjectInfo{}, errors.New("object not found")
	}
	return storage.ObjectInfo{Key: key, Size: int64(len(value))}, nil
}

func (s *httpObjectStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	value, ok := s.objects[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(value)), nil
}

func (s *httpObjectStore) Delete(_ context.Context, key string) error {
	delete(s.objects, key)
	return nil
}

var _ storage.ObjectStore = (*httpObjectStore)(nil)
