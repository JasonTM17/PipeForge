package job

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/JasonTM17/PipeForge/go/internal/quality"
	"github.com/google/uuid"
)

const (
	MaxOperations       = 32
	MaxConfigProperties = 32
	MaxOperationTypeLen = 64
)

var columnNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

func ValidateCreateCommand(command CreateCommand) (string, error) {
	if command.OwnerUserID == [16]byte{} || command.ActorUserID == [16]byte{} || command.DatasetVersionID == [16]byte{} {
		return "", fmt.Errorf("%w: owner, actor, and dataset version are required", ErrInvalidInput)
	}
	if err := ValidateOperations(command.Operations); err != nil {
		return "", err
	}
	if command.Priority < 0 || command.Priority > MaxPriority {
		return "", fmt.Errorf("%w: priority must be between 0 and %d", ErrInvalidInput, MaxPriority)
	}
	if command.MaxAttempts < 1 || command.MaxAttempts > MaxAttempts {
		return "", fmt.Errorf("%w: maxAttempts must be between 1 and %d", ErrInvalidInput, MaxAttempts)
	}
	if len(command.IdempotencyKey) > MaxIdempotencyKeySize {
		return "", fmt.Errorf("%w: idempotency key is too long", ErrInvalidInput)
	}
	if command.TraceID == "" {
		return "", fmt.Errorf("%w: trace ID is required", ErrInvalidInput)
	}
	return RequestFingerprint(command)
}

func ValidateOperations(operations []Operation) error {
	if len(operations) == 0 || len(operations) > MaxOperations {
		return fmt.Errorf("%w: operations must contain between 1 and %d items", ErrInvalidInput, MaxOperations)
	}
	for index, operation := range operations {
		operationType := strings.TrimSpace(operation.Type)
		if operationType == "" || operationType != operation.Type || len(operationType) > MaxOperationTypeLen {
			return fmt.Errorf("%w: operation %d has an invalid type", ErrInvalidInput, index)
		}
		if operation.Config == nil || len(operation.Config) > MaxConfigProperties {
			return fmt.Errorf("%w: operation %d config must be an object with at most %d properties", ErrInvalidInput, index, MaxConfigProperties)
		}
		switch operationType {
		case "PROFILE_DATASET":
			if err := validateProfileConfig(operation.Config); err != nil {
				return fmt.Errorf("%w: operation %d profile config: %v", ErrInvalidInput, index, err)
			}
		case "CHECK_MISSING_VALUES":
			if err := validateConfigKeys(operation.Config, "columns"); err != nil {
				return fmt.Errorf("%w: operation %d config: %v", ErrInvalidInput, index, err)
			}
			if _, err := requiredColumns(operation.Config, "columns"); err != nil {
				return fmt.Errorf("%w: operation %d columns: %v", ErrInvalidInput, index, err)
			}
		case "CHECK_DUPLICATES":
			if err := validateConfigKeys(operation.Config, "keyColumns"); err != nil {
				return fmt.Errorf("%w: operation %d config: %v", ErrInvalidInput, index, err)
			}
			if _, err := requiredColumns(operation.Config, "keyColumns"); err != nil {
				return fmt.Errorf("%w: operation %d keyColumns: %v", ErrInvalidInput, index, err)
			}
		case "DETECT_OUTLIERS":
			if err := validateOutlierConfig(operation.Config); err != nil {
				return fmt.Errorf("%w: operation %d outlier config: %v", ErrInvalidInput, index, err)
			}
		case "VALIDATE_QUALITY":
			if err := validateConfigKeys(operation.Config, "rules"); err != nil {
				return fmt.Errorf("%w: operation %d config: %v", ErrInvalidInput, index, err)
			}
			if err := quality.ValidateSnapshotRules(operation.Config["rules"]); err != nil {
				return fmt.Errorf("%w: operation %d quality rules: %v", ErrInvalidInput, index, err)
			}
		default:
			return fmt.Errorf("%w: operation %d type %q is not supported", ErrInvalidInput, index, operationType)
		}
	}
	return nil
}

func Fingerprint(operations []Operation) (string, error) {
	canonical, err := CanonicalJSON(operations)
	if err != nil {
		return "", fmt.Errorf("%w: canonicalize operations: %v", ErrInvalidInput, err)
	}
	hash := sha256.Sum256(canonical)
	return hex.EncodeToString(hash[:]), nil
}

func RequestFingerprint(command CreateCommand) (string, error) {
	return fingerprintValue(struct {
		DatasetVersionID uuid.UUID   `json:"datasetVersionId"`
		Operations       []Operation `json:"operations"`
		Priority         int16       `json:"priority"`
		MaxAttempts      int16       `json:"maxAttempts"`
	}{
		DatasetVersionID: command.DatasetVersionID,
		Operations:       command.Operations,
		Priority:         command.Priority,
		MaxAttempts:      command.MaxAttempts,
	})
}

func fingerprintValue(value any) (string, error) {
	canonical, err := CanonicalJSON(value)
	if err != nil {
		return "", fmt.Errorf("%w: canonicalize request: %v", ErrInvalidInput, err)
	}
	hash := sha256.Sum256(canonical)
	return hex.EncodeToString(hash[:]), nil
}

func CanonicalJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var normalized any
	if err := decoder.Decode(&normalized); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return nil, fmt.Errorf("multiple JSON values")
	}
	return canonicalValue(normalized)
}

func canonicalValue(value any) ([]byte, error) {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var buffer bytes.Buffer
		buffer.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				buffer.WriteByte(',')
			}
			keyBytes, err := json.Marshal(key)
			if err != nil {
				return nil, err
			}
			valueBytes, err := canonicalValue(typed[key])
			if err != nil {
				return nil, err
			}
			buffer.Write(keyBytes)
			buffer.WriteByte(':')
			buffer.Write(valueBytes)
		}
		buffer.WriteByte('}')
		return buffer.Bytes(), nil
	case []any:
		var buffer bytes.Buffer
		buffer.WriteByte('[')
		for index, item := range typed {
			if index > 0 {
				buffer.WriteByte(',')
			}
			itemBytes, err := canonicalValue(item)
			if err != nil {
				return nil, err
			}
			buffer.Write(itemBytes)
		}
		buffer.WriteByte(']')
		return buffer.Bytes(), nil
	default:
		return json.Marshal(value)
	}
}

func requiredColumns(config map[string]any, key string) ([]string, error) {
	var values []any
	switch typed := config[key].(type) {
	case []any:
		values = typed
	case []string:
		values = make([]any, len(typed))
		for index := range typed {
			values[index] = typed[index]
		}
	default:
		return nil, fmt.Errorf("%s must be a non-empty array", key)
	}
	if len(values) == 0 || len(values) > 256 {
		return nil, fmt.Errorf("%s must contain between 1 and 256 columns", key)
	}
	result := make([]string, len(values))
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		column, ok := value.(string)
		if !ok || !validColumnName(column) {
			return nil, fmt.Errorf("%s contains an invalid column", key)
		}
		if _, duplicate := seen[column]; duplicate {
			return nil, fmt.Errorf("%s contains duplicate column %q", key, column)
		}
		seen[column] = struct{}{}
		result[index] = column
	}
	return result, nil
}

func validateConfigKeys(config map[string]any, allowed ...string) error {
	allowedKeys := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedKeys[key] = struct{}{}
	}
	for key := range config {
		if _, ok := allowedKeys[key]; !ok {
			return fmt.Errorf("unsupported config field %q", key)
		}
	}
	return nil
}

func validateOutlierConfig(config map[string]any) error {
	if err := validateConfigKeys(config, "column", "method", "threshold", "nullPolicy", "minimumSampleSize", "sampleOutputLimit"); err != nil {
		return err
	}
	column, ok := config["column"].(string)
	if !ok || !validColumnName(column) {
		return fmt.Errorf("column is invalid")
	}
	method, ok := config["method"].(string)
	if !ok || (method != "IQR" && method != "Z_SCORE" && method != "MODIFIED_Z_SCORE") {
		return fmt.Errorf("method is invalid")
	}
	if value, present := config["threshold"]; present {
		threshold, ok := finiteNumber(value)
		if !ok || threshold < 0.0001 || threshold > 1_000 {
			return fmt.Errorf("threshold is outside the supported bound")
		}
	}
	if value, present := config["nullPolicy"]; present {
		policy, ok := value.(string)
		if !ok || (policy != "SKIP" && policy != "FAIL") {
			return fmt.Errorf("nullPolicy is invalid")
		}
	}
	if value, present := config["minimumSampleSize"]; present {
		minimum, ok := boundedInteger(value)
		if !ok || minimum < 3 || minimum > 1_000_000 {
			return fmt.Errorf("minimumSampleSize is outside the supported bound")
		}
	}
	if value, present := config["sampleOutputLimit"]; present {
		limit, ok := boundedInteger(value)
		if !ok || limit < 0 || limit > 128 {
			return fmt.Errorf("sampleOutputLimit is outside the supported bound")
		}
	}
	return nil
}

func finiteNumber(value any) (float64, bool) {
	var number float64
	switch typed := value.(type) {
	case int:
		number = float64(typed)
	case int8:
		number = float64(typed)
	case int16:
		number = float64(typed)
	case int32:
		number = float64(typed)
	case int64:
		number = float64(typed)
	case uint:
		number = float64(typed)
	case uint8:
		number = float64(typed)
	case uint16:
		number = float64(typed)
	case uint32:
		number = float64(typed)
	case uint64:
		number = float64(typed)
	case float32:
		number = float64(typed)
	case float64:
		number = typed
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		number = parsed
	default:
		return 0, false
	}
	return number, !math.IsNaN(number) && !math.IsInf(number, 0)
}

func boundedInteger(value any) (int64, bool) {
	number, ok := finiteNumber(value)
	if !ok || number != math.Trunc(number) || number < math.MinInt64 || number > math.MaxInt64 {
		return 0, false
	}
	return int64(number), true
}

func validColumnName(value string) bool {
	return columnNamePattern.MatchString(value)
}
