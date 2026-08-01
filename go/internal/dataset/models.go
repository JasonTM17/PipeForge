package dataset

import (
	"context"
	"errors"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/upload"
	"github.com/google/uuid"
)

var (
	ErrDatasetNotFound = errors.New("dataset not found")
	ErrDatasetNameUsed = errors.New("dataset name already exists")
	ErrDatasetDeleted  = errors.New("dataset is deleted")
	ErrVersionNotFound = errors.New("dataset version not found")
	ErrVersionState    = errors.New("dataset version is not mutable")
)

type Dataset struct {
	ID          uuid.UUID  `json:"id"`
	OwnerUserID uuid.UUID  `json:"ownerUserId"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	State       string     `json:"state"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	DeletedAt   *time.Time `json:"deletedAt,omitempty"`
}

type DatasetVersion struct {
	ID               uuid.UUID      `json:"id"`
	DatasetID        uuid.UUID      `json:"datasetId"`
	VersionNumber    int32          `json:"versionNumber"`
	State            string         `json:"state"`
	OriginalFilename string         `json:"originalFilename"`
	ContentType      string         `json:"contentType"`
	Format           upload.Format  `json:"format"`
	SizeBytes        *int64         `json:"sizeBytes,omitempty"`
	ObjectKey        string         `json:"-"`
	ChecksumSHA256   *string        `json:"checksumSha256,omitempty"`
	RowCount         *int64         `json:"rowCount,omitempty"`
	ColumnCount      *int32         `json:"columnCount,omitempty"`
	Encoding         *string        `json:"encoding,omitempty"`
	CompressionType  *string        `json:"compressionType,omitempty"`
	SourceInfo       map[string]any `json:"sourceInfo"`
	ArtifactPrefix   string         `json:"artifactPrefix"`
	CreatedBy        uuid.UUID      `json:"createdBy"`
	CreatedAt        time.Time      `json:"createdAt"`
	AvailableAt      *time.Time     `json:"availableAt,omitempty"`
}

type DatasetPage struct {
	Items []Dataset
	Total int64
}

type VersionReservation struct {
	ID               uuid.UUID
	DatasetID        uuid.UUID
	CreatedBy        uuid.UUID
	OriginalFilename string
	ContentType      string
	Format           upload.Format
	ObjectKey        string
	ArtifactPrefix   string
	SourceInfo       map[string]any
}

type Store interface {
	CreateDataset(context.Context, uuid.UUID, string, string) (Dataset, error)
	FindDataset(context.Context, uuid.UUID) (Dataset, error)
	ListDatasets(context.Context, *uuid.UUID, int, int, string, string) (DatasetPage, error)
	DeleteDataset(context.Context, uuid.UUID, uuid.UUID) error
	ReserveVersion(context.Context, VersionReservation) (DatasetVersion, error)
	FinalizeVersion(context.Context, uuid.UUID, uuid.UUID, int64, string) (DatasetVersion, error)
	AbortVersion(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error
	ListVersions(context.Context, uuid.UUID) ([]DatasetVersion, error)
}
