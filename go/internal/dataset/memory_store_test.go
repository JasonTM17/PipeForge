package dataset

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/storage"
	"github.com/google/uuid"
)

type memoryDatasetStore struct {
	mu       sync.Mutex
	datasets map[uuid.UUID]Dataset
	versions map[uuid.UUID]DatasetVersion
	aborted  []uuid.UUID
}

func newMemoryDatasetStore(ownerID uuid.UUID) (*memoryDatasetStore, Dataset) {
	now := time.Now().UTC()
	dataset := Dataset{ID: uuid.New(), OwnerUserID: ownerID, Name: "events", State: "REGISTERED", CreatedAt: now, UpdatedAt: now}
	return &memoryDatasetStore{
		datasets: map[uuid.UUID]Dataset{dataset.ID: dataset},
		versions: make(map[uuid.UUID]DatasetVersion),
	}, dataset
}

func (s *memoryDatasetStore) CreateDataset(_ context.Context, ownerID uuid.UUID, name, description string) (Dataset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	dataset := Dataset{ID: uuid.New(), OwnerUserID: ownerID, Name: name, Description: description, State: "REGISTERED", CreatedAt: now, UpdatedAt: now}
	s.datasets[dataset.ID] = dataset
	return dataset, nil
}

func (s *memoryDatasetStore) FindDataset(_ context.Context, datasetID uuid.UUID) (Dataset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dataset, ok := s.datasets[datasetID]
	if !ok {
		return Dataset{}, ErrDatasetNotFound
	}
	return dataset, nil
}

func (s *memoryDatasetStore) ListDatasets(_ context.Context, ownerID *uuid.UUID, page, pageSize int, state, name string) (DatasetPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]Dataset, 0)
	for _, dataset := range s.datasets {
		if dataset.DeletedAt != nil || (ownerID != nil && dataset.OwnerUserID != *ownerID) || (state != "" && dataset.State != state) {
			continue
		}
		items = append(items, dataset)
	}
	return DatasetPage{Items: items, Total: int64(len(items))}, nil
}

func (s *memoryDatasetStore) DeleteDataset(_ context.Context, datasetID, _ uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dataset, ok := s.datasets[datasetID]
	if !ok {
		return ErrDatasetNotFound
	}
	now := time.Now().UTC()
	dataset.State = "DELETED"
	dataset.DeletedAt = &now
	dataset.UpdatedAt = now
	s.datasets[datasetID] = dataset
	return nil
}

func (s *memoryDatasetStore) ReserveVersion(_ context.Context, reservation VersionReservation) (DatasetVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dataset, ok := s.datasets[reservation.DatasetID]
	if !ok {
		return DatasetVersion{}, ErrDatasetNotFound
	}
	if dataset.DeletedAt != nil {
		return DatasetVersion{}, ErrDatasetDeleted
	}
	versionNumber := int32(1)
	for _, version := range s.versions {
		if version.DatasetID == reservation.DatasetID && version.VersionNumber >= versionNumber {
			versionNumber = version.VersionNumber + 1
		}
	}
	now := time.Now().UTC()
	version := DatasetVersion{
		ID:               reservation.ID,
		DatasetID:        reservation.DatasetID,
		VersionNumber:    versionNumber,
		State:            "UPLOADING",
		OriginalFilename: reservation.OriginalFilename,
		ContentType:      reservation.ContentType,
		Format:           reservation.Format,
		ObjectKey:        reservation.ObjectKey,
		SourceInfo:       reservation.SourceInfo,
		ArtifactPrefix:   reservation.ArtifactPrefix,
		CreatedBy:        reservation.CreatedBy,
		CreatedAt:        now,
	}
	s.versions[version.ID] = version
	dataset.State = "UPLOADING"
	dataset.UpdatedAt = now
	s.datasets[dataset.ID] = dataset
	return version, nil
}

func (s *memoryDatasetStore) FinalizeVersion(_ context.Context, datasetID, versionID uuid.UUID, size int64, checksum string) (DatasetVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok := s.versions[versionID]
	if !ok || version.DatasetID != datasetID {
		return DatasetVersion{}, ErrVersionNotFound
	}
	if version.State != "UPLOADING" {
		return DatasetVersion{}, ErrVersionState
	}
	now := time.Now().UTC()
	version.State = "AVAILABLE"
	version.SizeBytes = &size
	version.ChecksumSHA256 = &checksum
	version.AvailableAt = &now
	s.versions[versionID] = version
	dataset := s.datasets[datasetID]
	dataset.State = "AVAILABLE"
	dataset.UpdatedAt = now
	s.datasets[datasetID] = dataset
	return version, nil
}

func (s *memoryDatasetStore) AbortVersion(_ context.Context, datasetID, versionID, _ uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.versions[versionID]; !ok {
		return ErrVersionNotFound
	}
	delete(s.versions, versionID)
	s.aborted = append(s.aborted, versionID)
	dataset := s.datasets[datasetID]
	dataset.State = "REGISTERED"
	dataset.UpdatedAt = time.Now().UTC()
	s.datasets[datasetID] = dataset
	return nil
}

func (s *memoryDatasetStore) ListVersions(_ context.Context, datasetID uuid.UUID) ([]DatasetVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	versions := make([]DatasetVersion, 0)
	for _, version := range s.versions {
		if version.DatasetID == datasetID {
			versions = append(versions, version)
		}
	}
	return versions, nil
}

var _ Store = (*memoryDatasetStore)(nil)

type memoryObjectStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	deleted []string
	putErr  error
}

func newMemoryObjectStore() *memoryObjectStore {
	return &memoryObjectStore{objects: make(map[string][]byte)}
}

func (s *memoryObjectStore) Put(_ context.Context, key string, reader io.Reader, _ int64, contentType string) (storage.ObjectInfo, error) {
	if s.putErr != nil {
		return storage.ObjectInfo{}, s.putErr
	}
	value, err := io.ReadAll(reader)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	s.mu.Lock()
	s.objects[key] = append([]byte(nil), value...)
	s.mu.Unlock()
	return storage.ObjectInfo{Key: key, Size: int64(len(value)), ContentType: contentType}, nil
}

func (s *memoryObjectStore) Head(_ context.Context, key string) (storage.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.objects[key]
	if !ok {
		return storage.ObjectInfo{}, errors.New("object not found")
	}
	return storage.ObjectInfo{Key: key, Size: int64(len(value))}, nil
}

func (s *memoryObjectStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.objects[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(value)), nil
}

func (s *memoryObjectStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	s.deleted = append(s.deleted, key)
	return nil
}

var _ storage.ObjectStore = (*memoryObjectStore)(nil)
