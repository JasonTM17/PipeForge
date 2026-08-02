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
	ErrProgressNotFound    = errors.New("progress snapshot not found")
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
	LeaseID        uuid.UUID `json:"-"`
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

type ProgressSnapshot struct {
	OwnerUserID        uuid.UUID `json:"-"`
	JobID              uuid.UUID `json:"jobId"`
	AttemptID          uuid.UUID `json:"attemptId"`
	LeaseID            uuid.UUID `json:"-"`
	Stage              string    `json:"stage"`
	ProcessedRows      int64     `json:"processedRows"`
	EstimatedTotalRows *int64    `json:"estimatedTotalRows,omitempty"`
	ProgressPercent    float64   `json:"progressPercent"`
	Throughput         float64   `json:"throughput"`
	UpdatedAt          time.Time `json:"updatedAt"`
	ReceivedAt         time.Time `json:"receivedAt"`
}

type ProgressPage struct {
	Items    []ProgressSnapshot `json:"items"`
	Page     int                `json:"page"`
	PageSize int                `json:"pageSize"`
	Total    int64              `json:"total"`
}

type ProgressQuery struct {
	OwnerUserID *uuid.UUID
	JobID       uuid.UUID
	Page        int
	PageSize    int
}

type Store interface {
	Process(context.Context, queue.Envelope) (Outcome, error)
	ListArtifacts(context.Context, ArtifactListQuery) (ArtifactPage, error)
	GetArtifact(context.Context, uuid.UUID) (Artifact, error)
}

type ProgressStore interface {
	GetProgress(context.Context, ProgressQuery) (ProgressSnapshot, error)
	ListProgress(context.Context, ProgressQuery) (ProgressPage, error)
}

type Processor interface {
	Process(context.Context, queue.Envelope) (Outcome, error)
}

type ObjectReader interface {
	Get(context.Context, string) (io.ReadCloser, error)
}
