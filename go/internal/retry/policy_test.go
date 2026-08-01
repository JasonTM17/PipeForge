package retry

import (
	"errors"
	"testing"
	"time"
)

type fixedSource struct{ value int64 }

func (s fixedSource) Int63n(int64) int64 { return s.value }

func TestDefaultPolicyUsesBoundedExponentialSchedule(t *testing.T) {
	policy := DefaultPolicy()
	for attempt, expected := range map[int]time.Duration{2: 10 * time.Second, 3: 30 * time.Second, 4: 2 * time.Minute, 5: 10 * time.Minute} {
		delay, err := policy.Delay(attempt, nil)
		if err != nil || delay != expected {
			t.Fatalf("attempt %d delay=%s err=%v, expected %s", attempt, delay, err, expected)
		}
	}
	if next, ok, err := policy.Next(5); err != nil || ok || next != 6 {
		t.Fatalf("retry budget was not exhausted: next=%d ok=%v err=%v", next, ok, err)
	}
}

func TestDelayJitterIsDeterministicAndBounded(t *testing.T) {
	policy := DefaultPolicy()
	delay, err := policy.Delay(3, fixedSource{value: 5 * int64(time.Second)})
	if err != nil {
		t.Fatalf("Delay returned error: %v", err)
	}
	if delay != 35*time.Second {
		t.Fatalf("unexpected deterministic jitter: %s", delay)
	}
	if delay > 36*time.Second {
		t.Fatalf("jitter exceeded policy bound: %s", delay)
	}
}

func TestPolicyRejectsInvalidAttemptsAndClassifiesFailures(t *testing.T) {
	policy := DefaultPolicy()
	if _, err := policy.Delay(1, nil); !errors.Is(err, ErrInvalidAttempt) {
		t.Fatalf("expected invalid next attempt error, got %v", err)
	}
	if !IsRetryable("temporary_minio_failure") || !IsRetryable("NETWORK_TIMEOUT") || IsRetryable("UNSUPPORTED_FORMAT") {
		t.Fatal("failure classification is incorrect")
	}
}
