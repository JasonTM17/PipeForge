package multipart

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strings"

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
	session, err := s.Sessions.BeginComplete(ctx, sessionID, principal.UserID, s.now())
	if err != nil {
		return dataset.DatasetVersion{}, err
	}
	parts, err := normalizeCompleteParts(request, session.PartCount)
	if err != nil {
		return dataset.DatasetVersion{}, s.resetCompletion(ctx, session.ID, err)
	}
	registered, err := s.Sessions.ListParts(ctx, session.ID)
	if err != nil {
		return dataset.DatasetVersion{}, s.resetCompletion(ctx, session.ID, err)
	}
	if err := verifyRegisteredParts(parts, registered, session.ExpectedSize, session.PartSize); err != nil {
		return dataset.DatasetVersion{}, s.resetCompletion(ctx, session.ID, err)
	}

	remoteParts, listErr := s.Multipart.ListMultipartParts(ctx, session.ObjectKey, session.UploadID)
	remoteCompleted := false
	if listErr == nil {
		if err := verifyRemoteParts(parts, remoteParts); err != nil {
			return dataset.DatasetVersion{}, s.resetCompletion(ctx, session.ID, err)
		}
	} else {
		if _, headErr := s.Objects.Head(ctx, session.ObjectKey); headErr != nil {
			return dataset.DatasetVersion{}, s.resetCompletion(ctx, session.ID, fmt.Errorf("%w: list remote parts: %v", ErrRemoteStorage, listErr))
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
				return dataset.DatasetVersion{}, s.resetCompletion(ctx, session.ID, fmt.Errorf("%w: complete remote upload: %v", ErrRemoteStorage, err))
			}
			remoteCompleted = true
		}
	}

	object, err := s.Objects.Head(ctx, session.ObjectKey)
	if err != nil {
		return dataset.DatasetVersion{}, s.resetCompletion(ctx, session.ID, fmt.Errorf("%w: verify completed object: %v", ErrRemoteStorage, err))
	}
	if object.Size != session.ExpectedSize {
		return dataset.DatasetVersion{}, s.failInvalidObject(ctx, session, ErrSizeMismatch)
	}
	actualSize, checksum, prefix, err := s.inspectObject(ctx, session.ObjectKey, session.ExpectedSize)
	if err != nil {
		return dataset.DatasetVersion{}, s.failInvalidObject(ctx, session, fmt.Errorf("%w: inspect completed object: %v", ErrRemoteStorage, err))
	}
	if actualSize != session.ExpectedSize {
		return dataset.DatasetVersion{}, s.failInvalidObject(ctx, session, ErrSizeMismatch)
	}
	if _, err := upload.DetectFormat(session.OriginalFilename, session.ContentType, prefix); err != nil {
		return dataset.DatasetVersion{}, s.failInvalidObject(ctx, session, fmt.Errorf("%w: %v", ErrInvalidInput, err))
	}
	if session.ExpectedChecksum != nil && *session.ExpectedChecksum != checksum {
		return dataset.DatasetVersion{}, s.failInvalidObject(ctx, session, ErrChecksumMismatch)
	}
	version, err := s.Datasets.FinalizeVersion(ctx, session.DatasetID, session.VersionID, principal.UserID, actualSize, checksum)
	if err != nil {
		return dataset.DatasetVersion{}, s.resetCompletion(ctx, session.ID, err)
	}
	if err := s.Sessions.MarkCompleted(ctx, session.ID, s.now()); err != nil {
		return dataset.DatasetVersion{}, err
	}
	return version, nil
}

func (s *Service) resetCompletion(ctx context.Context, sessionID uuid.UUID, primary error) error {
	if resetErr := s.Sessions.ResetCompletion(ctx, sessionID, primary.Error()); resetErr != nil {
		return errors.Join(primary, resetErr)
	}
	return primary
}

func (s *Service) failInvalidObject(ctx context.Context, session Session, primary error) error {
	deleteErr := s.Objects.Delete(ctx, session.ObjectKey)
	versionErr := s.Datasets.FailVersion(ctx, session.DatasetID, session.VersionID, uuid.Nil, "multipart_object_verification_failed")
	markErr := s.Sessions.MarkFailed(ctx, session.ID, primary.Error())
	return errors.Join(primary, deleteErr, versionErr, markErr)
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
