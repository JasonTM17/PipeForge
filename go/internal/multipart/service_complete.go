package multipart

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/auth"
	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/JasonTM17/PipeForge/go/internal/dataset"
	"github.com/JasonTM17/PipeForge/go/internal/storage"
	"github.com/JasonTM17/PipeForge/go/internal/upload"
	"github.com/google/uuid"
)

func (s *Service) Complete(ctx context.Context, principal auth.Principal, sessionID uuid.UUID, request CompleteRequest) (dataset.DatasetVersion, error) {
	if err := s.requireConfigured(); err != nil {
		return dataset.DatasetVersion{}, err
	}
	if err := authz.RequireScope(principal, authz.ScopeDatasetsWrite); err != nil {
		return dataset.DatasetVersion{}, err
	}
	authorized, err := s.authorizedSession(ctx, principal, sessionID, authz.ScopeDatasetsWrite)
	if err != nil {
		return dataset.DatasetVersion{}, err
	}
	now := s.now()
	session, err := s.Sessions.BeginComplete(ctx, sessionID, authorized.OwnerUserID, now, now.Add(-s.Config.CompletionGrace))
	if err != nil {
		return dataset.DatasetVersion{}, err
	}
	operationToken, err := sessionOperationToken(session)
	if err != nil {
		return dataset.DatasetVersion{}, err
	}
	parts, err := normalizeCompleteParts(request, session.PartCount)
	if err != nil {
		return dataset.DatasetVersion{}, s.resetCompletion(ctx, session.ID, operationToken, err)
	}
	registered, err := s.Sessions.ListParts(ctx, session.ID)
	if err != nil {
		return dataset.DatasetVersion{}, s.resetCompletion(ctx, session.ID, operationToken, err)
	}
	if err := verifyRegisteredParts(parts, registered, session.ExpectedSize, session.PartSize); err != nil {
		return dataset.DatasetVersion{}, s.resetCompletion(ctx, session.ID, operationToken, err)
	}

	remoteParts, listErr := s.Multipart.ListMultipartParts(ctx, session.ObjectKey, session.UploadID)
	remoteCompleted := false
	if listErr == nil {
		if err := verifyRemoteParts(parts, remoteParts); err != nil {
			return dataset.DatasetVersion{}, s.resetCompletion(ctx, session.ID, operationToken, err)
		}
	} else {
		if _, headErr := s.Objects.Head(ctx, session.ObjectKey); headErr != nil {
			return dataset.DatasetVersion{}, s.resetCompletion(ctx, session.ID, operationToken, fmt.Errorf("%w: list remote parts: %v", ErrRemoteStorage, listErr))
		}
		remoteCompleted = true
	}

	if !remoteCompleted {
		completeParts := make([]storage.MultipartPart, 0, len(parts))
		for index, part := range parts {
			completeParts = append(completeParts, storage.MultipartPart{PartNumber: part.PartNumber, ETag: part.ETag, Size: registered[index].SizeBytes})
		}
		if _, err := s.Multipart.CompleteMultipart(ctx, session.ObjectKey, session.UploadID, completeParts, session.ContentType); err != nil {
			if _, headErr := s.Objects.Head(ctx, session.ObjectKey); headErr != nil {
				return dataset.DatasetVersion{}, s.resetCompletion(ctx, session.ID, operationToken, fmt.Errorf("%w: complete remote upload: %v", ErrRemoteStorage, err))
			}
			remoteCompleted = true
		}
	}

	object, err := s.Objects.Head(ctx, session.ObjectKey)
	if err != nil {
		return dataset.DatasetVersion{}, s.resetCompletion(ctx, session.ID, operationToken, fmt.Errorf("%w: verify completed object: %v", ErrRemoteStorage, err))
	}
	if object.Size != session.ExpectedSize {
		return dataset.DatasetVersion{}, s.failInvalidObject(ctx, session, operationToken, ErrSizeMismatch)
	}
	actualSize, checksum, prefix, err := s.inspectObject(ctx, session.ObjectKey, session.ExpectedSize)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotFound) {
			return dataset.DatasetVersion{}, s.failInvalidObject(ctx, session, operationToken, fmt.Errorf("%w: inspect completed object: %v", ErrRemoteStorage, err))
		}
		return dataset.DatasetVersion{}, s.resetCompletion(ctx, session.ID, operationToken, fmt.Errorf("%w: inspect completed object: %v", ErrRemoteStorage, err))
	}
	if actualSize != session.ExpectedSize {
		return dataset.DatasetVersion{}, s.failInvalidObject(ctx, session, operationToken, ErrSizeMismatch)
	}
	if _, err := upload.DetectFormat(session.OriginalFilename, session.ContentType, prefix); err != nil {
		return dataset.DatasetVersion{}, s.failInvalidObject(ctx, session, operationToken, fmt.Errorf("%w: %v", ErrInvalidInput, err))
	}
	if session.ExpectedChecksum != nil && *session.ExpectedChecksum != checksum {
		return dataset.DatasetVersion{}, s.failInvalidObject(ctx, session, operationToken, ErrChecksumMismatch)
	}
	version, err := finalizeVersionForUpload(s.Datasets, ctx, session.DatasetID, session.VersionID, principal.UserID, actualSize, checksum, session.ID, operationToken)
	if err != nil {
		return dataset.DatasetVersion{}, s.resetCompletion(ctx, session.ID, operationToken, err)
	}
	if err := s.Sessions.MarkCompleted(ctx, session.ID, operationToken, s.now()); err != nil {
		return dataset.DatasetVersion{}, err
	}
	return version, nil
}

func (s *Service) reconcileCompletedObject(ctx context.Context, session Session, now time.Time) error {
	reconciled, err := s.Sessions.BeginReconciliation(ctx, session.ID, now)
	if err != nil {
		return err
	}
	operationToken, err := sessionOperationToken(reconciled)
	if err != nil {
		return err
	}
	object, err := s.Objects.Head(ctx, reconciled.ObjectKey)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotFound) {
			return s.failInvalidObject(ctx, reconciled, operationToken, fmt.Errorf("%w: completed object disappeared during reconciliation", ErrRemoteStorage))
		}
		return s.resetCompletion(ctx, reconciled.ID, operationToken, fmt.Errorf("%w: reconcile completed object: %v", ErrRemoteStorage, err))
	}
	if object.Size != reconciled.ExpectedSize {
		return s.failInvalidObject(ctx, reconciled, operationToken, ErrSizeMismatch)
	}
	actualSize, checksum, prefix, err := s.inspectObject(ctx, reconciled.ObjectKey, reconciled.ExpectedSize)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotFound) {
			return s.failInvalidObject(ctx, reconciled, operationToken, fmt.Errorf("%w: completed object disappeared during inspection", ErrRemoteStorage))
		}
		return s.resetCompletion(ctx, reconciled.ID, operationToken, fmt.Errorf("%w: inspect completed object during reconciliation: %v", ErrRemoteStorage, err))
	}
	if actualSize != reconciled.ExpectedSize {
		return s.failInvalidObject(ctx, reconciled, operationToken, ErrSizeMismatch)
	}
	if _, err := upload.DetectFormat(reconciled.OriginalFilename, reconciled.ContentType, prefix); err != nil {
		return s.failInvalidObject(ctx, reconciled, operationToken, fmt.Errorf("%w: %v", ErrInvalidInput, err))
	}
	if reconciled.ExpectedChecksum != nil && *reconciled.ExpectedChecksum != checksum {
		return s.failInvalidObject(ctx, reconciled, operationToken, ErrChecksumMismatch)
	}
	if _, err := finalizeVersionForUpload(s.Datasets, ctx, reconciled.DatasetID, reconciled.VersionID, uuid.Nil, actualSize, checksum, reconciled.ID, operationToken); err != nil {
		if errors.Is(err, dataset.ErrDatasetDeleted) {
			return s.failInvalidObject(ctx, reconciled, operationToken, err)
		}
		return s.resetCompletion(ctx, reconciled.ID, operationToken, err)
	}
	return s.Sessions.MarkCompleted(ctx, reconciled.ID, operationToken, now)
}

func sessionOperationToken(session Session) (uuid.UUID, error) {
	if session.OperationToken == nil || *session.OperationToken == uuid.Nil {
		return uuid.Nil, ErrSessionState
	}
	return *session.OperationToken, nil
}

func (s *Service) resetCompletion(ctx context.Context, sessionID, operationToken uuid.UUID, primary error) error {
	if resetErr := s.Sessions.ResetCompletion(ctx, sessionID, operationToken, primary.Error()); resetErr != nil {
		return errors.Join(primary, resetErr)
	}
	return primary
}

func (s *Service) failInvalidObject(ctx context.Context, session Session, operationToken uuid.UUID, primary error) error {
	deleteErr := s.Objects.Delete(ctx, session.ObjectKey)
	versionErr := s.Datasets.FailVersion(ctx, session.DatasetID, session.VersionID, uuid.Nil, "multipart_object_verification_failed")
	markErr := s.Sessions.MarkFailed(ctx, session.ID, operationToken, primary.Error())
	return errors.Join(primary, deleteErr, versionErr, markErr)
}

func finalizeVersionForUpload(store dataset.Store, ctx context.Context, datasetID, versionID, actorID uuid.UUID, size int64, checksum string, sessionID, operationToken uuid.UUID) (dataset.DatasetVersion, error) {
	return store.FinalizeVersionForUpload(ctx, datasetID, versionID, actorID, size, checksum, sessionID, operationToken)
}

func normalizeCompleteParts(request CompleteRequest, expectedCount int) ([]CompletePart, error) {
	if len(request.Parts) != expectedCount {
		return nil, fmt.Errorf("%w: exactly %d ordered parts are required", ErrInvalidInput, expectedCount)
	}
	parts := make([]CompletePart, len(request.Parts))
	for index, requested := range request.Parts {
		if requested.PartNumber != index+1 {
			return nil, fmt.Errorf("%w: parts must be ordered without gaps", ErrInvalidInput)
		}
		etag, err := normalizeETag(requested.ETag)
		if err != nil {
			return nil, err
		}
		parts[index] = CompletePart{PartNumber: requested.PartNumber, ETag: etag}
	}
	return parts, nil
}

func verifyRegisteredParts(requested []CompletePart, registered []Part, expectedSize, partSize int64) error {
	if len(registered) != len(requested) {
		return ErrPartNotFound
	}
	for index, requestedPart := range requested {
		registeredPart := registered[index]
		if registeredPart.PartNumber != requestedPart.PartNumber || registeredPart.ETag != requestedPart.ETag {
			return ErrPartConflict
		}
		expectedPartSize := partSize
		if index == len(requested)-1 {
			expectedPartSize = expectedSize - partSize*int64(len(requested)-1)
		}
		if registeredPart.SizeBytes != expectedPartSize {
			return ErrSizeMismatch
		}
	}
	return nil
}

func verifyRemoteParts(requested []CompletePart, remote []storage.MultipartPart) error {
	if len(remote) != len(requested) {
		return ErrPartNotFound
	}
	byNumber := make(map[int]storage.MultipartPart, len(remote))
	for _, part := range remote {
		if _, exists := byNumber[part.PartNumber]; exists {
			return ErrPartConflict
		}
		byNumber[part.PartNumber] = part
	}
	for _, requestedPart := range requested {
		remotePart, ok := byNumber[requestedPart.PartNumber]
		if !ok || stringsTrimQuotes(remotePart.ETag) != requestedPart.ETag {
			return ErrPartConflict
		}
	}
	return nil
}

func stringsTrimQuotes(value string) string {
	value = strings.TrimSpace(value)
	return strings.Trim(value, `"`)
}

func (s *Service) inspectObject(ctx context.Context, key string, expectedSize int64) (int64, string, []byte, error) {
	reader, err := s.Objects.Get(ctx, key)
	if err != nil {
		return 0, "", nil, err
	}
	defer reader.Close()
	hasher := sha256.New()
	limited := io.LimitReader(reader, expectedSize+1)
	counted := &countedReader{Reader: io.TeeReader(limited, hasher)}
	prefix := make([]byte, upload.SniffBytes)
	n, readErr := io.ReadFull(counted, prefix)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return 0, "", nil, readErr
	}
	prefix = prefix[:n]
	if _, err := io.Copy(io.Discard, counted); err != nil {
		return 0, "", nil, err
	}
	return counted.Count, upload.ChecksumBytes(hasher.Sum(nil)), prefix, nil
}

type countedReader struct {
	Reader io.Reader
	Count  int64
}

func (r *countedReader) Read(buffer []byte) (int, error) {
	n, err := r.Reader.Read(buffer)
	r.Count += int64(n)
	return n, err
}
