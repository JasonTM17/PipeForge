package dataset

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/JasonTM17/PipeForge/go/internal/auth"
	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/JasonTM17/PipeForge/go/internal/storage"
	"github.com/JasonTM17/PipeForge/go/internal/upload"
	"github.com/google/uuid"
)

var (
	ErrInvalidInput       = errors.New("invalid dataset input")
	ErrUploadTooLarge     = errors.New("upload exceeds configured size limit")
	ErrUploadSizeMismatch = errors.New("upload size does not match content length")
	ErrChecksumMismatch   = errors.New("upload checksum does not match")
	ErrObjectStorage      = errors.New("object storage operation failed")
)

type Service struct {
	Store          Store
	Objects        storage.ObjectStore
	MaxUploadBytes int64
}

type UploadRequest struct {
	Filename         string
	ContentType      string
	ExpectedChecksum string
	ContentLength    int64
	Body             io.Reader
}

func NewService(store Store, objects storage.ObjectStore, maxUploadBytes int64) (*Service, error) {
	if store == nil || objects == nil || maxUploadBytes <= 0 {
		return nil, errors.New("dataset service is not configured")
	}
	return &Service{Store: store, Objects: objects, MaxUploadBytes: maxUploadBytes}, nil
}

func (s *Service) Create(ctx context.Context, principal auth.Principal, name, description string) (Dataset, error) {
	if err := s.requireConfigured(); err != nil {
		return Dataset{}, err
	}
	if err := authz.RequireScope(principal, authz.ScopeDatasetsWrite); err != nil {
		return Dataset{}, err
	}
	if principal.UserID == uuid.Nil {
		return Dataset{}, authz.ErrUnauthenticated
	}
	validatedName, err := validateDatasetName(name)
	if err != nil {
		return Dataset{}, err
	}
	validatedDescription, err := validateDescription(description)
	if err != nil {
		return Dataset{}, err
	}
	return s.Store.CreateDataset(ctx, principal.UserID, validatedName, validatedDescription)
}

func (s *Service) List(ctx context.Context, principal auth.Principal, page, pageSize int, state, name string) (DatasetPage, error) {
	if err := s.requireConfigured(); err != nil {
		return DatasetPage{}, err
	}
	if err := authz.RequireScope(principal, authz.ScopeDatasetsRead); err != nil {
		return DatasetPage{}, err
	}
	page, pageSize, err := normalizePagination(page, pageSize)
	if err != nil {
		return DatasetPage{}, err
	}
	if state != "" && !isDatasetState(state) {
		return DatasetPage{}, fmt.Errorf("%w: invalid dataset state", ErrInvalidInput)
	}
	var ownerID *uuid.UUID
	if principal.Role != auth.RoleAdmin {
		ownerID = &principal.UserID
	}
	return s.Store.ListDatasets(ctx, ownerID, page, pageSize, state, name)
}

func (s *Service) Get(ctx context.Context, principal auth.Principal, datasetID uuid.UUID) (Dataset, []DatasetVersion, error) {
	if err := s.requireConfigured(); err != nil {
		return Dataset{}, nil, err
	}
	if err := authz.RequireScope(principal, authz.ScopeDatasetsRead); err != nil {
		return Dataset{}, nil, err
	}
	dataset, err := s.Store.FindDataset(ctx, datasetID)
	if err != nil {
		return Dataset{}, nil, err
	}
	if dataset.DeletedAt != nil || !authz.CanAccessOwner(principal, dataset.OwnerUserID, authz.ScopeDatasetsRead) {
		return Dataset{}, nil, authz.ErrForbidden
	}
	versions, err := s.Store.ListVersions(ctx, datasetID)
	if err != nil {
		return Dataset{}, nil, err
	}
	return dataset, versions, nil
}

func (s *Service) Delete(ctx context.Context, principal auth.Principal, datasetID uuid.UUID) error {
	if err := s.requireConfigured(); err != nil {
		return err
	}
	if err := authz.RequireScope(principal, authz.ScopeDatasetsWrite); err != nil {
		return err
	}
	dataset, err := s.Store.FindDataset(ctx, datasetID)
	if err != nil {
		return err
	}
	if dataset.DeletedAt != nil || !authz.CanAccessOwner(principal, dataset.OwnerUserID, authz.ScopeDatasetsWrite) {
		return authz.ErrForbidden
	}
	return s.Store.DeleteDataset(ctx, datasetID, principal.UserID)
}

func (s *Service) UploadVersion(ctx context.Context, principal auth.Principal, datasetID uuid.UUID, request UploadRequest) (DatasetVersion, error) {
	if err := s.requireConfigured(); err != nil {
		return DatasetVersion{}, err
	}
	if err := authz.RequireScope(principal, authz.ScopeDatasetsWrite); err != nil {
		return DatasetVersion{}, err
	}
	dataset, err := s.Store.FindDataset(ctx, datasetID)
	if err != nil {
		return DatasetVersion{}, err
	}
	if dataset.DeletedAt != nil || !authz.CanAccessOwner(principal, dataset.OwnerUserID, authz.ScopeDatasetsWrite) {
		return DatasetVersion{}, authz.ErrForbidden
	}
	if request.Body == nil {
		return DatasetVersion{}, fmt.Errorf("%w: upload body is required", ErrInvalidInput)
	}
	if request.ContentLength > s.MaxUploadBytes {
		return DatasetVersion{}, ErrUploadTooLarge
	}
	filename, err := upload.NormalizeFilename(request.Filename)
	if err != nil {
		return DatasetVersion{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	contentType := normalizeContentType(request.ContentType)
	prefix, remaining, err := readPrefix(request.Body)
	if err != nil {
		return DatasetVersion{}, fmt.Errorf("%w: read upload prefix: %v", ErrInvalidInput, err)
	}
	format, err := upload.DetectFormat(filename, contentType, prefix)
	if err != nil {
		return DatasetVersion{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	checksum, err := normalizeExpectedChecksum(request.ExpectedChecksum)
	if err != nil {
		return DatasetVersion{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	versionID := uuid.New()
	objectKey := ObjectKey(datasetID, versionID)
	reservation, err := s.Store.ReserveVersion(ctx, VersionReservation{
		ID: versionID, DatasetID: datasetID, CreatedBy: principal.UserID,
		OriginalFilename: filename, ContentType: contentType, Format: format,
		ObjectKey: objectKey, ArtifactPrefix: ArtifactPrefix(datasetID, versionID),
		SourceInfo: map[string]any{},
	})
	if err != nil {
		return DatasetVersion{}, err
	}
	counter := &countingReader{Reader: io.MultiReader(bytes.NewReader(prefix), remaining)}
	hasher := sha256.New()
	limited := io.LimitReader(counter, s.MaxUploadBytes+1)
	reader := io.TeeReader(limited, hasher)
	object, err := s.Objects.Put(ctx, reservation.ObjectKey, reader, -1, contentType)
	if err != nil {
		return DatasetVersion{}, s.cleanupUpload(ctx, datasetID, versionID, reservation.ObjectKey, fmt.Errorf("%w: %v", ErrObjectStorage, err))
	}
	actualSize := counter.Count
	if actualSize > s.MaxUploadBytes {
		return DatasetVersion{}, s.cleanupUpload(ctx, datasetID, versionID, reservation.ObjectKey, ErrUploadTooLarge)
	}
	if request.ContentLength >= 0 && actualSize != request.ContentLength {
		return DatasetVersion{}, s.cleanupUpload(ctx, datasetID, versionID, reservation.ObjectKey, ErrUploadSizeMismatch)
	}
	if object.Size >= 0 && object.Size != actualSize {
		return DatasetVersion{}, s.cleanupUpload(ctx, datasetID, versionID, reservation.ObjectKey, ErrUploadSizeMismatch)
	}
	actualChecksum := upload.ChecksumBytes(hasher.Sum(nil))
	if checksum != "" && checksum != actualChecksum {
		return DatasetVersion{}, s.cleanupUpload(ctx, datasetID, versionID, reservation.ObjectKey, ErrChecksumMismatch)
	}
	if _, err := s.Objects.Head(ctx, reservation.ObjectKey); err != nil {
		return DatasetVersion{}, s.cleanupUpload(ctx, datasetID, versionID, reservation.ObjectKey, fmt.Errorf("%w: verify stored object: %v", ErrObjectStorage, err))
	}
	version, err := s.Store.FinalizeVersion(ctx, datasetID, versionID, actualSize, actualChecksum)
	if err != nil {
		return DatasetVersion{}, s.cleanupUpload(ctx, datasetID, versionID, reservation.ObjectKey, err)
	}
	return version, nil
}

func (s *Service) cleanupUpload(ctx context.Context, datasetID, versionID uuid.UUID, objectKey string, primary error) error {
	deleteErr := s.Objects.Delete(ctx, objectKey)
	abortErr := s.Store.AbortVersion(ctx, datasetID, versionID, uuid.Nil)
	if deleteErr != nil || abortErr != nil {
		return errors.Join(primary, deleteErr, abortErr)
	}
	return primary
}

func (s *Service) requireConfigured() error {
	if s == nil || s.Store == nil || s.Objects == nil || s.MaxUploadBytes <= 0 {
		return errors.New("dataset service is not configured")
	}
	return nil
}

func ObjectKey(datasetID, versionID uuid.UUID) string {
	return fmt.Sprintf("datasets/%s/versions/%s/raw", datasetID, versionID)
}

func ArtifactPrefix(datasetID, versionID uuid.UUID) string {
	return fmt.Sprintf("artifacts/datasets/%s/versions/%s", datasetID, versionID)
}

func readPrefix(reader io.Reader) ([]byte, io.Reader, error) {
	prefix := make([]byte, upload.SniffBytes)
	n, err := io.ReadFull(reader, prefix)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, nil, err
	}
	return prefix[:n], reader, nil
}

func normalizeContentType(contentType string) string {
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		return "application/octet-stream"
	}
	return contentType
}

func normalizeExpectedChecksum(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	return upload.NormalizeChecksum(value)
}

func validateDatasetName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" || len(name) > 120 || strings.ContainsAny(name, "\r\n") {
		return "", fmt.Errorf("%w: dataset name must be between 1 and 120 bytes", ErrInvalidInput)
	}
	return name, nil
}

func validateDescription(value string) (string, error) {
	description := strings.TrimSpace(value)
	if len(description) > 2000 || strings.ContainsAny(description, "\r\n") {
		return "", fmt.Errorf("%w: dataset description is too long or contains a newline", ErrInvalidInput)
	}
	return description, nil
}

func normalizePagination(page, pageSize int) (int, int, error) {
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}
	if page < 1 || pageSize < 1 || pageSize > 100 {
		return 0, 0, fmt.Errorf("%w: page must be positive and pageSize must be between 1 and 100", ErrInvalidInput)
	}
	return page, pageSize, nil
}

func isDatasetState(state string) bool {
	switch state {
	case "REGISTERED", "UPLOADING", "AVAILABLE", "PROCESSING", "READY", "FAILED", "DELETED":
		return true
	default:
		return false
	}
}

type countingReader struct {
	Reader io.Reader
	Count  int64
}

func (r *countingReader) Read(buffer []byte) (int, error) {
	n, err := r.Reader.Read(buffer)
	r.Count += int64(n)
	return n, err
}
