package result

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/google/uuid"
)

const (
	OutcomeApplied                 = "APPLIED"
	OutcomeDuplicate               = "DUPLICATE"
	OutcomeIgnored                 = "IGNORED"
	OutcomeRejected                = "REJECTED"
	MaxArtifactsPerResult          = 32
	MaxArtifactDownloadBytes int64 = 512 * 1024 * 1024
)

var (
	ErrInvalidEvent        = errors.New("invalid result event")
	ErrInvalidInput        = errors.New("invalid artifact request")
	ErrArtifactNotFound    = errors.New("artifact not found")
	ErrArtifactUnavailable = errors.New("artifact is unavailable")
	ErrArtifactTooLarge    = errors.New("artifact exceeds download limit")
)

type Outcome struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type Artifact struct {
	ID             uuid.UUID `json:"id"`
	OwnerUserID    uuid.UUID `json:"-"`
	JobID          uuid.UUID `json:"jobId"`
	AttemptID      uuid.UUID `json:"attemptId"`
	LeaseID        uuid.UUID `json:"leaseId"`
	Kind           string    `json:"kind"`
	ObjectKey      string    `json:"-"`
	SizeBytes      int64     `json:"sizeBytes"`
	ContentType    string    `json:"contentType"`
	ChecksumSHA256 string    `json:"checksumSha256"`
	State          string    `json:"state"`
	CreatedAt      time.Time `json:"createdAt"`
}

type ArtifactPage struct {
	Items    []Artifact
	Page     int
	PageSize int
	Total    int64
}

type ArtifactListQuery struct {
	OwnerUserID *uuid.UUID
	JobID       *uuid.UUID
	Kind        string
	Page        int
	PageSize    int
}

type Store interface {
	Process(context.Context, queue.Envelope) (Outcome, error)
	ListArtifacts(context.Context, ArtifactListQuery) (ArtifactPage, error)
	GetArtifact(context.Context, uuid.UUID) (Artifact, error)
}

type Processor interface {
	Process(context.Context, queue.Envelope) (Outcome, error)
}

type ObjectReader interface {
	Get(context.Context, string) (io.ReadCloser, error)
}
