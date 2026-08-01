package retry

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	DefaultMaxAttempts   = 5
	MaxSupportedAttempts = 20
)

var (
	ErrInvalidPolicy  = errors.New("invalid retry policy")
	ErrInvalidAttempt = errors.New("invalid retry attempt")
)

// Source supplies bounded randomness so retry tests can be deterministic.
type Source interface {
	Int63n(int64) int64
}

type Policy struct {
	MaxAttempts    int
	Delays         []time.Duration
	JitterFraction float64
}

func DefaultPolicy() Policy {
	return Policy{
		MaxAttempts:    DefaultMaxAttempts,
		Delays:         []time.Duration{0, 10 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute},
		JitterFraction: 0.2,
	}
}

func (p Policy) Validate() error {
	if p.MaxAttempts < 1 || p.MaxAttempts > MaxSupportedAttempts || len(p.Delays) == 0 || len(p.Delays) > p.MaxAttempts || p.JitterFraction < 0 || p.JitterFraction > 0.5 {
		return ErrInvalidPolicy
	}
	previous := time.Duration(-1)
	for _, delay := range p.Delays {
		if delay < 0 || delay < previous {
			return ErrInvalidPolicy
		}
		previous = delay
	}
	return nil
}

// Next returns the bounded next attempt number and whether the retry budget remains.
func (p Policy) Next(currentAttempt int) (int, bool, error) {
	if err := p.Validate(); err != nil {
		return 0, false, err
	}
	if currentAttempt < 1 || currentAttempt > p.MaxAttempts {
		return 0, false, fmt.Errorf("%w: %d", ErrInvalidAttempt, currentAttempt)
	}
	next := currentAttempt + 1
	return next, next <= p.MaxAttempts, nil
}

// Delay returns the delay before nextAttempt. The last configured delay is used
// for a larger bounded policy so adding attempts never creates an unbounded wait.
func (p Policy) Delay(nextAttempt int, source Source) (time.Duration, error) {
	if err := p.Validate(); err != nil {
		return 0, err
	}
	if nextAttempt < 2 || nextAttempt > p.MaxAttempts {
		return 0, fmt.Errorf("%w: %d", ErrInvalidAttempt, nextAttempt)
	}
	index := nextAttempt - 1
	if index >= len(p.Delays) {
		index = len(p.Delays) - 1
	}
	base := p.Delays[index]
	if base == 0 || p.JitterFraction == 0 || source == nil {
		return base, nil
	}
	maximum := int64(float64(base) * p.JitterFraction)
	if maximum < 1 {
		return base, nil
	}
	return base + time.Duration(source.Int63n(maximum+1)), nil
}

var retryableCodes = map[string]struct{}{
	"MINIO_UNAVAILABLE":    {},
	"RABBITMQ_UNAVAILABLE": {},
	"NETWORK_TIMEOUT":      {},
	"WORKER_INTERRUPTED":   {},
	"RESOURCE_EXHAUSTED":   {},
	"LEASE_EXPIRED":        {},
}

func IsRetryable(code string) bool {
	code = strings.ToUpper(strings.TrimSpace(code))
	if _, ok := retryableCodes[code]; ok {
		return true
	}
	return strings.HasPrefix(code, "TEMPORARY_") || strings.HasSuffix(code, "_TIMEOUT")
}
