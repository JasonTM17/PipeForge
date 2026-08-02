package job

import (
	"errors"
	"testing"
)

func TestValidateTransitionTable(t *testing.T) {
	valid := [][2]string{
		{StateCreated, StateQueued},
		{StateQueued, StateLeased},
		{StateQueued, StateCancelled},
		{StateRunning, StateSucceeded},
		{StateRunning, StateFailedRetryable},
		{StateFailedRetryable, StateQueued},
		{StateCancelRequested, StateCancelled},
		{StateDeadLettered, StateQueued},
	}
	for _, pair := range valid {
		if err := ValidateTransition(pair[0], pair[1]); err != nil {
			t.Errorf("expected %s -> %s to be valid: %v", pair[0], pair[1], err)
		}
	}
	invalid := [][2]string{
		{StateSucceeded, StateQueued},
		{StateCancelled, StateRunning},
		{StateQueued, StateSucceeded},
		{StateRunning, StateCreated},
		{StateCreated, StateCreated},
	}
	for _, pair := range invalid {
		if !errors.Is(ValidateTransition(pair[0], pair[1]), ErrInvalidTransition) {
			t.Errorf("expected %s -> %s to be invalid", pair[0], pair[1])
		}
	}
}

func TestFingerprintCanonicalizesObjectKeysButPreservesOperationOrder(t *testing.T) {
	first := []Operation{
		{Type: "CHECK_MISSING_VALUES", Config: map[string]any{"columns": []string{"email"}}},
		{Type: "PROFILE_DATASET", Config: map[string]any{}},
	}
	second := []Operation{
		{Type: "CHECK_MISSING_VALUES", Config: map[string]any{"columns": []string{"email"}}},
		{Type: "PROFILE_DATASET", Config: map[string]any{}},
	}
	firstFingerprint, err := Fingerprint(first)
	if err != nil {
		t.Fatalf("fingerprint first: %v", err)
	}
	secondFingerprint, err := Fingerprint(second)
	if err != nil {
		t.Fatalf("fingerprint second: %v", err)
	}
	if firstFingerprint != secondFingerprint {
		t.Fatalf("same operations produced different fingerprints: %s != %s", firstFingerprint, secondFingerprint)
	}
	second[0].Config["columns"] = []string{"phone"}
	changedFingerprint, err := Fingerprint(second)
	if err != nil {
		t.Fatalf("fingerprint changed request: %v", err)
	}
	if changedFingerprint == firstFingerprint {
		t.Fatal("changed operations produced the same fingerprint")
	}
}

func TestValidateOperationsRejectsUnsafeOrUnknownContracts(t *testing.T) {
	cases := []struct {
		name       string
		operations []Operation
	}{
		{name: "empty", operations: nil},
		{name: "unknown type", operations: []Operation{{Type: "DROP_TABLE", Config: map[string]any{}}}},
		{name: "unsafe column", operations: []Operation{{Type: "CHECK_MISSING_VALUES", Config: map[string]any{"columns": []string{"email;DROP"}}}}},
		{name: "outlier method", operations: []Operation{{Type: "DETECT_OUTLIERS", Config: map[string]any{"column": "amount", "method": "MAD"}}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := ValidateOperations(testCase.operations); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("expected invalid input, got %v", err)
			}
		})
	}
}

func TestValidateOperationsAcceptsBoundedProfileConfig(t *testing.T) {
	valid := []Operation{{
		Type: "PROFILE_DATASET",
		Config: map[string]any{
			"sampling":            map[string]any{"strategy": "RESERVOIR", "maxRows": 128, "seed": 7},
			"includeCommonValues": true,
			"maxCommonValues":     10,
			"quantiles":           []float64{0.5, 0.95},
			"distinctStrategy":    "APPROXIMATE",
			"maxDistinctValues":   1024,
			"maxMemoryBytes":      1 << 20,
			"sensitiveColumns":    []string{"email"},
		},
	}}
	if err := ValidateOperations(valid); err != nil {
		t.Fatalf("expected bounded profile config to pass: %v", err)
	}
	invalid := []Operation{{
		Type:   "PROFILE_DATASET",
		Config: map[string]any{"unknown": true},
	}}
	if err := ValidateOperations(invalid); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected unknown profile field to fail, got %v", err)
	}
}

func TestValidateOperationsAcceptsQualitySnapshotAndRejectsCustomExpression(t *testing.T) {
	valid := []Operation{{
		Type: "VALIDATE_QUALITY",
		Config: map[string]any{
			"rules": []any{map[string]any{
				"id": "email-required", "name": "Email is required", "columnScope": []any{"email"},
				"ruleType": "NOT_NULL", "configuration": map[string]any{}, "severity": "ERROR", "enabled": true,
			}},
		},
	}}
	if err := ValidateOperations(valid); err != nil {
		t.Fatalf("expected quality snapshot to pass: %v", err)
	}
	unsafe := []Operation{{
		Type: "VALIDATE_QUALITY",
		Config: map[string]any{
			"rules": []any{map[string]any{
				"id": "custom", "name": "unsafe", "columnScope": []any{"value"},
				"ruleType": "CUSTOM_EXPRESSION", "configuration": map[string]any{"expression": "__import__('os')"}, "severity": "ERROR", "enabled": true,
			}},
		},
	}}
	if err := ValidateOperations(unsafe); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected custom expression to fail closed, got %v", err)
	}
}

func TestValidateOperationsAcceptsBoundedAnomalyConfig(t *testing.T) {
	valid := []Operation{{
		Type: "DETECT_OUTLIERS",
		Config: map[string]any{
			"column": "amount", "method": "MODIFIED_Z_SCORE", "threshold": 3.5,
			"nullPolicy": "FAIL", "minimumSampleSize": 5, "sampleOutputLimit": 64,
		},
	}}
	if err := ValidateOperations(valid); err != nil {
		t.Fatalf("expected bounded anomaly config to pass: %v", err)
	}
	unknown := []Operation{{Type: "DETECT_OUTLIERS", Config: map[string]any{"column": "amount", "method": "IQR", "expression": "x"}}}
	if err := ValidateOperations(unknown); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected unknown anomaly field to fail: %v", err)
	}
}
