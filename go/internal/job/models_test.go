package job

import (
	"errors"
	"testing"
)

func TestValidateTransitionTable(t *testing.T) {
	valid := [][2]string{
		{StateCreated, StateQueued},
		{StateQueued, StateLeased},
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
