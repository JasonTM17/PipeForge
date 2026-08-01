package quality

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strings"
)

var columnNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

type Definition struct {
	Name          string
	ColumnScope   []string
	RuleType      RuleType
	Configuration map[string]any
	Severity      Severity
	Enabled       bool
}

func ValidateDefinition(definition Definition) (Definition, error) {
	definition.Name = strings.TrimSpace(definition.Name)
	if definition.Name == "" || len(definition.Name) > MaxRuleNameLength || strings.ContainsAny(definition.Name, "\r\n") {
		return Definition{}, fmt.Errorf("%w: name must be between 1 and %d bytes", ErrInvalidInput, MaxRuleNameLength)
	}
	columns, err := normalizeColumns(definition.ColumnScope)
	if err != nil {
		return Definition{}, err
	}
	if !isRuleType(definition.RuleType) || definition.RuleType == RuleCustomExpression {
		return Definition{}, fmt.Errorf("%w: rule type %q is unsupported", ErrInvalidInput, definition.RuleType)
	}
	if !isSeverity(definition.Severity) {
		return Definition{}, fmt.Errorf("%w: severity %q is unsupported", ErrInvalidInput, definition.Severity)
	}
	configuration := cloneMap(definition.Configuration)
	if err := validateConfiguration(definition.RuleType, columns, configuration); err != nil {
		return Definition{}, fmt.Errorf("%w: configuration: %v", ErrInvalidInput, err)
	}
	definition.ColumnScope = columns
	definition.Configuration = configuration
	return definition, nil
}

func ValidateSnapshotRules(value any) error {
	rules, err := sliceValue(value)
	if err != nil || len(rules) < 1 || len(rules) > MaxRulesPerDataset {
		return fmt.Errorf("%w: rules must contain between 1 and %d items", ErrInvalidInput, MaxRulesPerDataset)
	}
	seen := make(map[string]struct{}, len(rules))
	for index, raw := range rules {
		object, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("%w: rule %d must be an object", ErrInvalidInput, index)
		}
		if err := validateSnapshotRule(object); err != nil {
			return fmt.Errorf("%w: rule %d: %v", ErrInvalidInput, index, err)
		}
		id, _ := object["id"].(string)
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("%w: duplicate rule id", ErrInvalidInput)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func validateSnapshotRule(object map[string]any) error {
	allowed := map[string]struct{}{"id": {}, "name": {}, "columnScope": {}, "ruleType": {}, "configuration": {}, "severity": {}, "enabled": {}}
	for key := range object {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("unknown field %q", key)
		}
	}
	id, ok := object["id"].(string)
	if !ok || strings.TrimSpace(id) == "" || len(id) > 128 || strings.ContainsAny(id, "\r\n") {
		return fmt.Errorf("id is invalid")
	}
	name, ok := object["name"].(string)
	if !ok {
		return fmt.Errorf("name is required")
	}
	columns, err := stringsValue(object["columnScope"])
	if err != nil {
		return err
	}
	ruleType, ok := object["ruleType"].(string)
	if !ok {
		return fmt.Errorf("ruleType is required")
	}
	severity, _ := object["severity"].(string)
	enabled, ok := object["enabled"].(bool)
	if !ok {
		return fmt.Errorf("enabled is required")
	}
	configuration, ok := object["configuration"].(map[string]any)
	if !ok {
		return fmt.Errorf("configuration is required")
	}
	_, err = ValidateDefinition(Definition{Name: name, ColumnScope: columns, RuleType: RuleType(ruleType), Configuration: configuration, Severity: Severity(severity), Enabled: enabled})
	return err
}

func validateConfiguration(ruleType RuleType, columns []string, config map[string]any) error {
	if ruleType == RuleRowCountBetween {
		if len(columns) != 0 {
			return fmt.Errorf("ROW_COUNT_BETWEEN does not accept columns")
		}
	} else {
		maximum := 1
		if ruleType == RuleUnique {
			maximum = 16
		}
		if len(columns) < 1 || len(columns) > maximum {
			return fmt.Errorf("%s requires 1 to %d columns", ruleType, maximum)
		}
	}
	allowed := map[RuleType]map[string]struct{}{
		RuleNotNull: {}, RuleUnique: {"maxTrackedValues": {}}, RuleBetween: {"min": {}, "max": {}},
		RuleMinLength: {"value": {}}, RuleMaxLength: {"value": {}}, RuleRegex: {"pattern": {}},
		RuleEmailFormat: {}, RuleDateFormat: {"format": {}}, RuleAllowedValues: {"values": {}},
		RuleColumnType: {"type": {}}, RuleRowCountBetween: {"min": {}, "max": {}},
		RuleReferentialSet: {"values": {}},
	}
	keys, ok := allowed[ruleType]
	if !ok {
		return fmt.Errorf("unsupported rule type")
	}
	for key := range config {
		if _, ok := keys[key]; !ok {
			return fmt.Errorf("unknown field %q", key)
		}
	}
	switch ruleType {
	case RuleBetween, RuleRowCountBetween:
		minimum, err := requiredNumber(config, "min")
		if err != nil {
			return err
		}
		maximum, err := requiredNumber(config, "max")
		if err != nil {
			return err
		}
		if minimum > maximum {
			return fmt.Errorf("min must not exceed max")
		}
	case RuleMinLength, RuleMaxLength:
		if err := boundedInteger(config, "value", 0, 1_000_000); err != nil {
			return err
		}
	case RuleUnique:
		if _, present := config["maxTrackedValues"]; present {
			if err := boundedInteger(config, "maxTrackedValues", 1, 2_000_000); err != nil {
				return err
			}
		}
	case RuleRegex:
		pattern, ok := config["pattern"].(string)
		if !ok || len(pattern) == 0 || len(pattern) > 512 {
			return fmt.Errorf("pattern is outside the supported bound")
		}
		if strings.ContainsAny(pattern, "()|") || strings.Contains(pattern, `\1`) || strings.Contains(pattern, `\2`) {
			return fmt.Errorf("pattern uses an unsafe regex construct")
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("pattern is invalid")
		}
	case RuleDateFormat:
		if err := boundedText(config, "format", 64); err != nil {
			return err
		}
	case RuleAllowedValues, RuleReferentialSet:
		if err := scalarValues(config, "values"); err != nil {
			return err
		}
	case RuleColumnType:
		typeName, ok := config["type"].(string)
		_, knownType := map[string]struct{}{"string": {}, "integer": {}, "number": {}, "boolean": {}, "date": {}, "datetime": {}}[typeName]
		if !ok || !knownType {
			return fmt.Errorf("type is unsupported")
		}
	}
	return nil
}

func normalizeColumns(columns []string) ([]string, error) {
	if len(columns) > 256 {
		return nil, fmt.Errorf("%w: columnScope has too many columns", ErrInvalidInput)
	}
	result := append([]string(nil), columns...)
	seen := make(map[string]struct{}, len(result))
	for _, column := range result {
		if !columnNamePattern.MatchString(column) {
			return nil, fmt.Errorf("%w: columnScope contains an invalid column", ErrInvalidInput)
		}
		if _, duplicate := seen[column]; duplicate {
			return nil, fmt.Errorf("%w: columnScope contains duplicate column %q", ErrInvalidInput, column)
		}
		seen[column] = struct{}{}
	}
	return result, nil
}

func isRuleType(value RuleType) bool {
	switch value {
	case RuleNotNull, RuleUnique, RuleBetween, RuleMinLength, RuleMaxLength, RuleRegex,
		RuleEmailFormat, RuleDateFormat, RuleAllowedValues, RuleColumnType, RuleRowCountBetween,
		RuleReferentialSet, RuleCustomExpression:
		return true
	default:
		return false
	}
}

func isSeverity(value Severity) bool {
	switch value {
	case SeverityInfo, SeverityWarning, SeverityError, SeverityCritical:
		return true
	default:
		return false
	}
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func boundedText(config map[string]any, key string, maximum int) error {
	value, ok := config[key].(string)
	if !ok || value == "" || len(value) > maximum || strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%s is outside the supported bound", key)
	}
	return nil
}

func boundedInteger(config map[string]any, key string, minimum, maximum int64) error {
	value, ok := integerValue(config[key])
	if !ok || value < minimum || value > maximum {
		return fmt.Errorf("%s is outside the supported bound", key)
	}
	return nil
}

func requiredNumber(config map[string]any, key string) (float64, error) {
	value, ok := numberValue(config[key])
	if !ok {
		return 0, fmt.Errorf("%s must be a finite number", key)
	}
	return value, nil
}

func scalarValues(config map[string]any, key string) error {
	values, err := sliceValue(config[key])
	if err != nil || len(values) < 1 || len(values) > 10_000 {
		return fmt.Errorf("%s must contain 1 to 10000 items", key)
	}
	for _, value := range values {
		if !isScalar(value) {
			return fmt.Errorf("%s must contain scalar values", key)
		}
	}
	return nil
}

func sliceValue(value any) ([]any, error) {
	if values, ok := value.([]any); ok {
		return values, nil
	}
	reflected := reflect.ValueOf(value)
	if !reflected.IsValid() || (reflected.Kind() != reflect.Slice && reflected.Kind() != reflect.Array) {
		return nil, fmt.Errorf("value must be an array")
	}
	result := make([]any, reflected.Len())
	for index := range result {
		result[index] = reflected.Index(index).Interface()
	}
	return result, nil
}

func stringsValue(value any) ([]string, error) {
	values, err := sliceValue(value)
	if err != nil {
		return nil, err
	}
	result := make([]string, len(values))
	for index, item := range values {
		value, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("columnScope must contain strings")
		}
		result[index] = value
	}
	return result, nil
}

func isScalar(value any) bool {
	if value == nil {
		return true
	}
	switch value.(type) {
	case string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
		return true
	default:
		return false
	}
}

func numberValue(value any) (float64, bool) {
	var number float64
	switch typed := value.(type) {
	case float32:
		number = float64(typed)
	case float64:
		number = typed
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

func integerValue(value any) (int64, bool) {
	number, ok := numberValue(value)
	if !ok || number != math.Trunc(number) || number < math.MinInt64 || number > math.MaxInt64 {
		return 0, false
	}
	return int64(number), true
}
