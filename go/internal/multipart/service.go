package multipart

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/auth"
	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/JasonTM17/PipeForge/go/internal/dataset"
	"github.com/JasonTM17/PipeForge/go/internal/storage"
	"github.com/JasonTM17/PipeForge/go/internal/upload"
	"github.com/google/uuid"
)

const (
	minimumPartSize = int64(5 * 1024 * 1024)
	maximumPartSize = int64(5 * 1024 * 1024 * 1024)
	maximumURLTTL   = 7 * 24 * time.Hour
)

type Config struct {
	PartSize        int64
	MaxParts        int
	MaxBytes        int64
	SessionTTL      time.Duration
	PartURLTTL      time.Duration
	CompletionGrace time.Duration
}

type Service struct {
	Datasets  dataset.Store
	Sessions  Store
	Objects   storage.ObjectStore
	Multipart storage.MultipartStore
	Config    Config
	Now       func() time.Time
}

func NewService(datasets dataset.Store, sessions Store, objects storage.ObjectStore, config Config) (*Service, error) {
	if datasets == nil || sessions == nil || objects == nil {
		return nil, errors.New("multipart service dependencies are not configured")
	}
	multipartObjects, ok := objects.(storage.MultipartStore)
	if !ok || multipartObjects == nil {
		return nil, errors.New("object store does not support multipart uploads")
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	return &Service{Datasets: datasets, Sessions: sessions, Objects: objects, Multipart: multipartObjects, Config: config, Now: time.Now}, nil
}

func (s Config) validate() error {
	if s.PartSize < minimumPartSize || s.PartSize > maximumPartSize {
		return fmt.Errorf("multipart part size must be between %d and %d bytes", minimumPartSize, maximumPartSize)
	}
	if s.MaxParts < 1 || s.MaxParts > 10000 {
		return errors.New("multipart max parts must be between 1 and 10000")
	}
	if s.MaxBytes < s.PartSize || s.MaxBytes > s.PartSize*int64(s.MaxParts) {
		return errors.New("multipart max bytes must fit within configured part bounds")
	}
	if s.SessionTTL <= 0 || s.PartURLTTL <= 0 || s.PartURLTTL > maximumURLTTL || s.CompletionGrace <= 0 {
		return errors.New("multipart session, URL TTL, and completion grace are invalid")
	}
	return nil
}

func (s *Service) Initiate(ctx context.Context, principal auth.Principal, datasetID uuid.UUID, request InitiateRequest) (Session, error) {
	if err := s.requireConfigured(); err != nil {
		return Session{}, err
	}
	if err := authz.RequireScope(principal, authz.ScopeDatasetsWrite); err != nil {
		return Session{}, err
	}
	if principal.UserID == uuid.Nil {
		return Session{}, authz.ErrUnauthenticated
	}
	item, err := s.Datasets.FindDataset(ctx, datasetID)
	if err != nil {
		return Session{}, err
	}
	if item.DeletedAt != nil || !authz.CanAccessOwner(principal, item.OwnerUserID, authz.ScopeDatasetsWrite) {
		return Session{}, authz.ErrForbidden
	}
	filename, contentType, format, checksum, idempotencyKey, err := validateInitiateRequest(request)
	if err != nil {
		return Session{}, err
	}
	if request.ExpectedSize <= 0 {
		return Session{}, fmt.Errorf("%w: expected size must be positive", ErrInvalidInput)
	}
	if request.ExpectedSize > s.Config.MaxBytes {
		return Session{}, ErrUploadTooLarge
	}
	partCount := int((request.ExpectedSize-1)/s.Config.PartSize + 1)
	if partCount < 1 || partCount > s.Config.MaxParts {
		return Session{}, fmt.Errorf("%w: expected size requires too many parts", ErrInvalidInput)
	}
	if idempotencyKey != "" {
		existing, lookupErr := s.Sessions.FindActiveByIdempotency(ctx, principal.UserID, datasetID, idempotencyKey)
		if lookupErr == nil {
			if sameInitiateRequest(existing, filename, contentType, format, request.ExpectedSize, checksum) {
				return existing, nil
			}
			return Session{}, ErrIdempotencyConflict
		}
		if !errors.Is(lookupErr, ErrSessionNotFound) {
			return Session{}, lookupErr
		}
	}

	now := s.now()
	sessionID := uuid.New()
	versionID := uuid.New()
	objectKey := dataset.ObjectKey(datasetID, versionID)
	version, err := s.Datasets.ReserveVersion(ctx, dataset.VersionReservation{
		ID: versionID, DatasetID: datasetID, CreatedBy: principal.UserID,
		OriginalFilename: filename, ContentType: contentType, Format: format,
		ObjectKey: objectKey, ArtifactPrefix: dataset.ArtifactPrefix(datasetID, versionID),
		SourceInfo: map[string]any{"uploadMode": "multipart", "sessionId": sessionID.String()},
	})
	if err != nil {
		return Session{}, err
	}
	uploadID, err := s.Multipart.InitiateMultipart(ctx, objectKey, contentType)
	if err != nil {
		return Session{}, s.cleanupReserved(ctx, datasetID, versionID, principal.UserID, fmt.Errorf("%w: initiate remote upload: %v", ErrRemoteStorage, err))
	}
	expectedChecksum := checksum
	session := Session{
		ID: sessionID, OwnerUserID: principal.UserID, DatasetID: datasetID, VersionID: version.ID,
		UploadID: uploadID, ObjectKey: objectKey, State: StateInitiated,
		OriginalFilename: filename, ContentType: contentType, Format: format,
		ExpectedSize: request.ExpectedSize, PartSize: s.Config.PartSize, PartCount: partCount,
		IdempotencyKey: idempotencyKey, ExpiresAt: now.Add(s.Config.SessionTTL), CreatedAt: now, UpdatedAt: now,
	}
	if expectedChecksum != "" {
		session.ExpectedChecksum = &expectedChecksum
	}
	if err := s.Sessions.CreateSession(ctx, session); err != nil {
		cleanupErr := s.cleanupReservedRemote(ctx, objectKey, uploadID, datasetID, versionID, principal.UserID)
		if cleanupErr != nil {
			orphanErr := s.persistOrphanSession(ctx, session, now, "initiation_compensation_failed")
			return Session{}, errors.Join(err, cleanupErr, orphanErr)
		}
		if errors.Is(err, ErrIdempotencyConflict) && idempotencyKey != "" {
			existing, lookupErr := s.Sessions.FindActiveByIdempotency(ctx, principal.UserID, datasetID, idempotencyKey)
			if lookupErr == nil && sameInitiateRequest(existing, filename, contentType, format, request.ExpectedSize, checksum) {
				return existing, nil
			}
			if lookupErr != nil {
				return Session{}, errors.Join(err, lookupErr)
			}
		}
		return Session{}, err
	}
	return session, nil
}

func (s *Service) Get(ctx context.Context, principal auth.Principal, sessionID uuid.UUID) (SessionDetail, error) {
	if err := s.requireConfigured(); err != nil {
		return SessionDetail{}, err
	}
	if err := authz.RequireScope(principal, authz.ScopeDatasetsRead); err != nil {
		return SessionDetail{}, err
	}
	session, err := s.Sessions.FindSession(ctx, sessionID)
	if err != nil {
		return SessionDetail{}, err
	}
	if !authz.CanAccessOwner(principal, session.OwnerUserID, authz.ScopeDatasetsRead) {
		return SessionDetail{}, authz.ErrForbidden
	}
	parts, err := s.Sessions.ListParts(ctx, sessionID)
	if err != nil {
		return SessionDetail{}, err
	}
	return SessionDetail{Session: session, Parts: parts}, nil
}

func (s *Service) ListParts(ctx context.Context, principal auth.Principal, sessionID uuid.UUID) ([]Part, error) {
	detail, err := s.Get(ctx, principal, sessionID)
	if err != nil {
		return nil, err
	}
	return detail.Parts, nil
}

func (s *Service) PresignPart(ctx context.Context, principal auth.Principal, sessionID uuid.UUID, partNumber int) (PartURL, error) {
	if err := s.requireConfigured(); err != nil {
		return PartURL{}, err
	}
	if err := authz.RequireScope(principal, authz.ScopeDatasetsWrite); err != nil {
		return PartURL{}, err
	}
	session, err := s.authorizedSession(ctx, principal, sessionID, authz.ScopeDatasetsWrite)
	if err != nil {
		return PartURL{}, err
	}
	now := s.now()
	if !now.Before(session.ExpiresAt) {
		return PartURL{}, ErrSessionExpired
	}
	if session.State != StateInitiated {
		if session.State == StateCompleted {
			return PartURL{}, ErrSessionCompleted
		}
		return PartURL{}, ErrSessionState
	}
	if partNumber < 1 || partNumber > session.PartCount {
		return PartURL{}, fmt.Errorf("%w: part number is outside the session bounds", ErrInvalidInput)
	}
	expiresAt := now.Add(s.Config.PartURLTTL)
	if expiresAt.After(session.ExpiresAt) {
		expiresAt = session.ExpiresAt
	}
	urlValue, err := s.Multipart.PresignPart(ctx, session.ObjectKey, session.UploadID, partNumber, expiresAt.Sub(now))
	if err != nil {
		return PartURL{}, fmt.Errorf("%w: presign part: %v", ErrRemoteStorage, err)
	}
	return PartURL{PartNumber: partNumber, URL: urlValue, Method: http.MethodPut, ExpiresAt: expiresAt}, nil
}

func (s *Service) RegisterPart(ctx context.Context, principal auth.Principal, sessionID uuid.UUID, partNumber int, etag string, size int64) (Part, error) {
	if err := s.requireConfigured(); err != nil {
		return Part{}, err
	}
	if err := authz.RequireScope(principal, authz.ScopeDatasetsWrite); err != nil {
		return Part{}, err
	}
	session, err := s.authorizedSession(ctx, principal, sessionID, authz.ScopeDatasetsWrite)
	if err != nil {
		return Part{}, err
	}
	if err := validateMutablePart(session, partNumber, size, s.now()); err != nil {
		return Part{}, err
	}
	normalizedETag, err := normalizeETag(etag)
	if err != nil {
		return Part{}, err
	}
	if existing, lookupErr := s.Sessions.FindPart(ctx, sessionID, partNumber); lookupErr == nil {
		if existing.ETag == normalizedETag && existing.SizeBytes == size {
			return existing, nil
		}
		return Part{}, ErrPartConflict
	} else if !errors.Is(lookupErr, ErrPartNotFound) {
		return Part{}, lookupErr
	}
	remotePart, err := s.Multipart.GetMultipartPart(ctx, session.ObjectKey, session.UploadID, partNumber)
	if err != nil {
		if errors.Is(err, storage.ErrMultipartPartNotFound) {
			return Part{}, ErrPartNotFound
		}
		return Part{}, fmt.Errorf("%w: list remote parts: %v", ErrRemoteStorage, err)
	}
	if strings.Trim(remotePart.ETag, `"`) != normalizedETag || remotePart.Size != size {
		return Part{}, ErrPartConflict
	}
	return s.Sessions.RegisterPart(ctx, sessionID, partNumber, normalizedETag, size)
}

func (s *Service) Abort(ctx context.Context, principal auth.Principal, sessionID uuid.UUID) error {
	if err := s.requireConfigured(); err != nil {
		return err
	}
	if err := authz.RequireScope(principal, authz.ScopeDatasetsWrite); err != nil {
		return err
	}
	session, err := s.authorizedSession(ctx, principal, sessionID, authz.ScopeDatasetsWrite)
	if err != nil {
		return err
	}
	now := s.now()
	session, err = s.Sessions.BeginAbort(ctx, sessionID, session.OwnerUserID, now)
	if err != nil {
		return err
	}
	if session.State == StateAborted || session.State == StateExpired {
		return nil
	}
	if session.State != StateAborting {
		return ErrSessionState
	}
	if err := s.Multipart.AbortMultipart(ctx, session.ObjectKey, session.UploadID); err != nil {
		return fmt.Errorf("%w: abort remote upload: %v", ErrRemoteStorage, err)
	}
	versionErr := s.Datasets.FailVersion(ctx, session.DatasetID, session.VersionID, principal.UserID, "multipart_upload_aborted")
	markErr := s.Sessions.MarkAborted(ctx, sessionID, now)
	if versionErr != nil || markErr != nil {
		return errors.Join(versionErr, markErr)
	}
	return nil
}

type CleanupReport struct {
	Claimed    int
	Expired    int
	Reconciled int
	Failed     int
	Failures   []error
}

func (s *Service) CleanupExpired(ctx context.Context, limit int) (CleanupReport, error) {
	if err := s.requireConfigured(); err != nil {
		return CleanupReport{}, err
	}
	if limit < 1 || limit > 1000 {
		return CleanupReport{}, fmt.Errorf("%w: cleanup limit must be between 1 and 1000", ErrInvalidInput)
	}
	now := s.now()
	sessions, err := s.Sessions.ClaimExpired(ctx, now, now.Add(-s.Config.CompletionGrace), limit)
	if err != nil {
		return CleanupReport{}, err
	}
	report := CleanupReport{Claimed: len(sessions), Failures: make([]error, 0)}
	for _, session := range sessions {
		if _, headErr := s.Objects.Head(ctx, session.ObjectKey); headErr == nil {
			if reconcileErr := s.reconcileCompletedObject(ctx, session, now); reconcileErr != nil {
				report.Failed++
				report.Failures = append(report.Failures, fmt.Errorf("session %s reconciliation failed: %w", session.ID, reconcileErr))
			} else {
				report.Reconciled++
			}
			continue
		} else if !errors.Is(headErr, storage.ErrObjectNotFound) {
			report.Failed++
			report.Failures = append(report.Failures, fmt.Errorf("session %s object existence check failed: %w", session.ID, headErr))
			continue
		}
		remoteErr := s.Multipart.AbortMultipart(ctx, session.ObjectKey, session.UploadID)
		versionErr := s.Datasets.FailVersion(ctx, session.DatasetID, session.VersionID, uuid.Nil, "multipart_session_expired")
		if errors.Is(versionErr, dataset.ErrVersionNotFound) {
			versionErr = nil
		}
		if remoteErr != nil || versionErr != nil {
			report.Failed++
			report.Failures = append(report.Failures, errors.Join(remoteErr, versionErr))
			continue
		}
		if markErr := s.Sessions.MarkExpired(ctx, session.ID, "session_expired", now); markErr != nil {
			report.Failed++
			report.Failures = append(report.Failures, markErr)
			continue
		}
		report.Expired++
	}
	return report, nil
}

func (s *Service) authorizedSession(ctx context.Context, principal auth.Principal, sessionID uuid.UUID, scope string) (Session, error) {
	session, err := s.Sessions.FindSession(ctx, sessionID)
	if err != nil {
		return Session{}, err
	}
	if !authz.CanAccessOwner(principal, session.OwnerUserID, scope) {
		return Session{}, authz.ErrForbidden
	}
	return session, nil
}

func (s *Service) cleanupReserved(ctx context.Context, datasetID, versionID, actorID uuid.UUID, primary error) error {
	return errors.Join(primary, s.Datasets.AbortVersion(ctx, datasetID, versionID, actorID))
}

func (s *Service) cleanupReservedRemote(ctx context.Context, objectKey, uploadID string, datasetID, versionID, actorID uuid.UUID) error {
	return errors.Join(s.Multipart.AbortMultipart(ctx, objectKey, uploadID), s.Datasets.AbortVersion(ctx, datasetID, versionID, actorID))
}

func (s *Service) persistOrphanSession(ctx context.Context, session Session, now time.Time, reason string) error {
	session.State = StateAborting
	session.IdempotencyKey = ""
	session.ExpiresAt = now
	session.UpdatedAt = now
	session.LastError = &reason
	return s.Sessions.CreateSession(ctx, session)
}

func (s *Service) requireConfigured() error {
	if s == nil || s.Datasets == nil || s.Sessions == nil || s.Objects == nil || s.Multipart == nil {
		return errors.New("multipart service is not configured")
	}
	return nil
}

func (s *Service) now() time.Time {
	if s.Now == nil {
		return time.Now().UTC()
	}
	return s.Now().UTC()
}

func validateInitiateRequest(request InitiateRequest) (string, string, upload.Format, string, string, error) {
	filename, err := upload.NormalizeFilename(request.Filename)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("%w: filename: %v", ErrInvalidInput, err)
	}
	contentType := strings.TrimSpace(request.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	format, err := upload.DetectDeclaredFormat(filename, contentType)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("%w: format: %v", ErrInvalidInput, err)
	}
	checksum := ""
	if strings.TrimSpace(request.ExpectedChecksum) != "" {
		checksum, err = upload.NormalizeChecksum(request.ExpectedChecksum)
		if err != nil {
			return "", "", "", "", "", fmt.Errorf("%w: checksum: %v", ErrInvalidInput, err)
		}
	}
	idempotencyKey := strings.TrimSpace(request.IdempotencyKey)
	if len(idempotencyKey) > 128 || strings.ContainsAny(idempotencyKey, "\r\n") {
		return "", "", "", "", "", fmt.Errorf("%w: idempotency key is invalid", ErrInvalidInput)
	}
	return filename, contentType, format, checksum, idempotencyKey, nil
}

func sameInitiateRequest(session Session, filename, contentType string, format upload.Format, expectedSize int64, checksum string) bool {
	if session.OriginalFilename != filename || session.ContentType != contentType || session.Format != format || session.ExpectedSize != expectedSize {
		return false
	}
	if session.ExpectedChecksum == nil {
		return checksum == ""
	}
	return *session.ExpectedChecksum == checksum
}

func validateMutablePart(session Session, partNumber int, size int64, now time.Time) error {
	if session.State != StateInitiated {
		if session.State == StateCompleted {
			return ErrSessionCompleted
		}
		return ErrSessionState
	}
	if !now.Before(session.ExpiresAt) {
		return ErrSessionExpired
	}
	if partNumber < 1 || partNumber > session.PartCount || size <= 0 {
		return fmt.Errorf("%w: part number or size is invalid", ErrInvalidInput)
	}
	expected := session.PartSize
	if partNumber == session.PartCount {
		expected = session.ExpectedSize - session.PartSize*int64(session.PartCount-1)
	}
	if size != expected {
		return ErrSizeMismatch
	}
	return nil
}

func normalizeETag(value string) (string, error) {
	etag := strings.Trim(strings.TrimSpace(value), `"`)
	if etag == "" || len(etag) > 256 || strings.ContainsAny(etag, "\r\n") {
		return "", fmt.Errorf("%w: ETag is invalid", ErrInvalidInput)
	}
	return etag, nil
}
