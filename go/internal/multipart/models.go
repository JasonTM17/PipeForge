package multipart

import (
	"context"
	"errors"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/upload"
	"github.com/google/uuid"
)

const (
	StateInitiated  = "INITIATED"
	StateCompleting = "COMPLETING"
	StateAborting   = "ABORTING"
	StateCompleted  = "COMPLETED"
	StateAborted    = "ABORTED"
	StateExpired    = "EXPIRED"
	StateFailed     = "FAILED"
)

var (
	ErrInvalidInput        = errors.New("invalid multipart upload input")
	ErrSessionNotFound     = errors.New("multipart upload session not found")
	ErrSessionExpired      = errors.New("multipart upload session expired")
	ErrSessionCompleted    = errors.New("multipart upload session already completed")
	ErrSessionState        = errors.New("multipart upload session is not mutable")
	ErrIdempotencyConflict = errors.New("multipart upload idempotency key conflicts with another request")
	ErrPartNotFound        = errors.New("multipart upload part not found")
	ErrPartConflict        = errors.New("multipart upload part conflicts with registered metadata")
	ErrRemoteStorage       = errors.New("multipart object storage operation failed")
	ErrChecksumMismatch    = errors.New("multipart upload checksum does not match")
	ErrSizeMismatch        = errors.New("multipart upload size does not match")
	ErrUploadTooLarge      = errors.New("multipart upload exceeds configured size limit")
)

type Session struct {
	ID               uuid.UUID     `json:"id"`
	OwnerUserID      uuid.UUID     `json:"ownerUserId"`
	DatasetID        uuid.UUID     `json:"datasetId"`
	VersionID        uuid.UUID     `json:"versionId"`
	UploadID         string        `json:"-"`
	ObjectKey        string        `json:"-"`
	State            string        `json:"state"`
	OriginalFilename string        `json:"originalFilename"`
	ContentType      string        `json:"contentType"`
	Format           upload.Format `json:"format"`
	ExpectedSize     int64         `json:"expectedSize"`
	ExpectedChecksum *string       `json:"expectedChecksumSha256,omitempty"`
	PartSize         int64         `json:"partSize"`
	PartCount        int           `json:"partCount"`
	IdempotencyKey   string        `json:"-"`
	ExpiresAt        time.Time     `json:"expiresAt"`
	CreatedAt        time.Time     `json:"createdAt"`
	UpdatedAt        time.Time     `json:"updatedAt"`
	CompletedAt      *time.Time    `json:"completedAt,omitempty"`
	AbortedAt        *time.Time    `json:"abortedAt,omitempty"`
	LastError        *string       `json:"lastError,omitempty"`
}

type Part struct {
	SessionID  uuid.UUID `json:"sessionId"`
	PartNumber int       `json:"partNumber"`
	ETag       string    `json:"etag"`
	SizeBytes  int64     `json:"sizeBytes"`
	CreatedAt  time.Time `json:"createdAt"`
}

type InitiateRequest struct {
	Filename         string
	ContentType      string
	ExpectedSize     int64
	ExpectedChecksum string
	IdempotencyKey   string
}

type CompletePart struct {
	PartNumber int    `json:"partNumber"`
	ETag       string `json:"etag"`
}

type CompleteRequest struct {
	Parts []CompletePart `json:"parts"`
}

type PartURL struct {
	PartNumber int       `json:"partNumber"`
	URL        string    `json:"url"`
	Method     string    `json:"method"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

type SessionDetail struct {
	Session Session `json:"session"`
	Parts   []Part  `json:"parts"`
}

type Store interface {
	CreateSession(context.Context, Session) error
	FindSession(context.Context, uuid.UUID) (Session, error)
	FindActiveByIdempotency(context.Context, uuid.UUID, uuid.UUID, string) (Session, error)
	FindPart(context.Context, uuid.UUID, int) (Part, error)
	ListParts(context.Context, uuid.UUID) ([]Part, error)
	RegisterPart(context.Context, uuid.UUID, int, string, int64) (Part, error)
	BeginComplete(context.Context, uuid.UUID, uuid.UUID, time.Time) (Session, error)
	BeginAbort(context.Context, uuid.UUID, uuid.UUID, time.Time) (Session, error)
	ResetCompletion(context.Context, uuid.UUID, string) error
	MarkCompleted(context.Context, uuid.UUID, time.Time) error
	MarkAborted(context.Context, uuid.UUID, time.Time) error
	MarkFailed(context.Context, uuid.UUID, string) error
	ClaimExpired(context.Context, time.Time, time.Time, int) ([]Session, error)
	MarkExpired(context.Context, uuid.UUID, string, time.Time) error
}
