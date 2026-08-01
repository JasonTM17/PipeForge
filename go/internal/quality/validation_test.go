package quality

import (
	"errors"
	"testing"
)

func TestValidateDefinitionSupportsBoundedRuleFamilies(t *testing.T) {
	cases := []Definition{
		{Name: "required", ColumnScope: []string{"email"}, RuleType: RuleNotNull, Severity: SeverityError},
		{Name: "unique", ColumnScope: []string{"tenant", "id"}, RuleType: RuleUnique, Severity: SeverityCritical, Configuration: map[string]any{"maxTrackedValues": 1000}},
		{Name: "range", ColumnScope: []string{"score"}, RuleType: RuleBetween, Severity: SeverityWarning, Configuration: map[string]any{"min": 0, "max": 10}},
		{Name: "min length", ColumnScope: []string{"name"}, RuleType: RuleMinLength, Severity: SeverityInfo, Configuration: map[string]any{"value": 2}},
		{Name: "max length", ColumnScope: []string{"name"}, RuleType: RuleMaxLength, Severity: SeverityInfo, Configuration: map[string]any{"value": 100}},
		{Name: "regex", ColumnScope: []string{"code"}, RuleType: RuleRegex, Severity: SeverityError, Configuration: map[string]any{"pattern": "^[A-Z]+$"}},
		{Name: "email", ColumnScope: []string{"email"}, RuleType: RuleEmailFormat, Severity: SeverityError},
		{Name: "date", ColumnScope: []string{"created"}, RuleType: RuleDateFormat, Severity: SeverityError, Configuration: map[string]any{"format": "2006-01-02"}},
		{Name: "allowed", ColumnScope: []string{"state"}, RuleType: RuleAllowedValues, Severity: SeverityWarning, Configuration: map[string]any{"values": []any{"READY", "FAILED"}}},
		{Name: "type", ColumnScope: []string{"amount"}, RuleType: RuleColumnType, Severity: SeverityError, Configuration: map[string]any{"type": "number"}},
		{Name: "row count", RuleType: RuleRowCountBetween, Severity: SeverityInfo, Configuration: map[string]any{"min": 1, "max": 10}},
		{Name: "reference", ColumnScope: []string{"country"}, RuleType: RuleReferentialSet, Severity: SeverityWarning, Configuration: map[string]any{"values": []string{"TH", "VN"}}},
	}
	for _, definition := range cases {
		if _, err := ValidateDefinition(definition); err != nil {
			t.Errorf("%s rejected: %v", definition.Name, err)
		}
	}
}

func TestValidateDefinitionRejectsUnsafeOrAmbiguousRules(t *testing.T) {
	cases := []Definition{
		{Name: "custom", ColumnScope: []string{"value"}, RuleType: RuleCustomExpression, Severity: SeverityError, Configuration: map[string]any{"expression": "__import__('os')"}},
		{Name: "unsafe column", ColumnScope: []string{"value;DROP"}, RuleType: RuleNotNull, Severity: SeverityError},
		{Name: "unknown config", ColumnScope: []string{"value"}, RuleType: RuleNotNull, Severity: SeverityError, Configuration: map[string]any{"expression": "x"}},
		{Name: "invalid range", ColumnScope: []string{"value"}, RuleType: RuleBetween, Severity: SeverityError, Configuration: map[string]any{"min": 10, "max": 1}},
		{Name: "unsafe regex", ColumnScope: []string{"value"}, RuleType: RuleRegex, Severity: SeverityError, Configuration: map[string]any{"pattern": "(a+)+$"}},
		{Name: "empty reference", ColumnScope: []string{"value"}, RuleType: RuleReferentialSet, Severity: SeverityError, Configuration: map[string]any{"values": []any{}}},
		{Name: "column on row count", ColumnScope: []string{"value"}, RuleType: RuleRowCountBetween, Severity: SeverityError, Configuration: map[string]any{"min": 0, "max": 1}},
	}
	for _, definition := range cases {
		if _, err := ValidateDefinition(definition); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s expected invalid input, got %v", definition.Name, err)
		}
	}
}

func TestValidateSnapshotRulesRejectsCodeAndAcceptsImmutableShape(t *testing.T) {
	valid := []any{
		map[string]any{
			"id": "rule-1", "name": "required", "columnScope": []any{"email"},
			"ruleType": string(RuleNotNull), "configuration": map[string]any{}, "severity": string(SeverityError), "enabled": true,
		},
	}
	if err := ValidateSnapshotRules(valid); err != nil {
		t.Fatalf("valid snapshot rejected: %v", err)
	}
	unsafe := []any{
		map[string]any{
			"id": "rule-1", "name": "custom", "columnScope": []any{"value"},
			"ruleType": string(RuleCustomExpression), "configuration": map[string]any{"expression": "__import__('os')"}, "severity": string(SeverityError), "enabled": true,
		},
	}
	if err := ValidateSnapshotRules(unsafe); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unsafe snapshot was accepted: %v", err)
	}
}
