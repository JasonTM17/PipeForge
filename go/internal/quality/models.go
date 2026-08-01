package quality

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	MaxRulesPerDataset = 256
	MaxRuleNameLength  = 120
)

var (
	ErrInvalidInput  = errors.New("invalid quality rule input")
	ErrRuleNotFound  = errors.New("quality rule not found")
	ErrRuleNameUsed  = errors.New("quality rule name already exists")
	ErrRuleState     = errors.New("quality rule is not mutable")
	ErrDatasetAccess = errors.New("quality rule dataset access denied")
)

type RuleType string

const (
	RuleNotNull          RuleType = "NOT_NULL"
	RuleUnique           RuleType = "UNIQUE"
	RuleBetween          RuleType = "BETWEEN"
	RuleMinLength        RuleType = "MIN_LENGTH"
	RuleMaxLength        RuleType = "MAX_LENGTH"
	RuleRegex            RuleType = "REGEX"
	RuleEmailFormat      RuleType = "EMAIL_FORMAT"
	RuleDateFormat       RuleType = "DATE_FORMAT"
	RuleAllowedValues    RuleType = "ALLOWED_VALUES"
	RuleColumnType       RuleType = "COLUMN_TYPE"
	RuleRowCountBetween  RuleType = "ROW_COUNT_BETWEEN"
	RuleReferentialSet   RuleType = "REFERENTIAL_SET"
	RuleCustomExpression RuleType = "CUSTOM_EXPRESSION"
)

type Severity string

const (
	SeverityInfo     Severity = "INFO"
	SeverityWarning  Severity = "WARNING"
	SeverityError    Severity = "ERROR"
	SeverityCritical Severity = "CRITICAL"
)

type Status string

const (
	StatusPassed  Status = "PASSED"
	StatusFailed  Status = "FAILED"
	StatusError   Status = "ERROR"
	StatusSkipped Status = "SKIPPED"
)

type Rule struct {
	ID            uuid.UUID      `json:"id"`
	OwnerUserID   uuid.UUID      `json:"ownerUserId"`
	DatasetID     uuid.UUID      `json:"datasetId"`
	Name          string         `json:"name"`
	ColumnScope   []string       `json:"columnScope"`
	RuleType      RuleType       `json:"ruleType"`
	Configuration map[string]any `json:"configuration"`
	Severity      Severity       `json:"severity"`
	Enabled       bool           `json:"enabled"`
	CreatedBy     uuid.UUID      `json:"createdBy"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
	DeletedAt     *time.Time     `json:"deletedAt,omitempty"`
}

type Page struct {
	Items    []Rule
	Page     int
	PageSize int
	Total    int64
}

type CreateRequest struct {
	DatasetID     uuid.UUID
	Name          string
	ColumnScope   []string
	RuleType      RuleType
	Configuration map[string]any
	Severity      Severity
	Enabled       *bool
}

type UpdateRequest struct {
	Name          *string
	ColumnScope   *[]string
	RuleType      *RuleType
	Configuration *map[string]any
	Severity      *Severity
	Enabled       *bool
}

type ListQuery struct {
	OwnerUserID *uuid.UUID
	DatasetID   *uuid.UUID
	Page        int
	PageSize    int
}

type Store interface {
	Create(context.Context, uuid.UUID, uuid.UUID, CreateRequest) (Rule, error)
	List(context.Context, ListQuery) (Page, error)
	Get(context.Context, uuid.UUID) (Rule, error)
	Update(context.Context, uuid.UUID, uuid.UUID, UpdateRequest) (Rule, error)
	Delete(context.Context, uuid.UUID, uuid.UUID) error
}
