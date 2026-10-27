package config

import (
	"errors"
	"fmt"
	"time"
)

// parseDuration parses an optional duration. An empty value uses emptyDefault;
// field prefixes parse and negative-value errors when set.
func parseDuration(raw string, emptyDefault time.Duration, field string) (time.Duration, error) {
	if raw == "" {
		return emptyDefault, nil
	}

	duration, err := time.ParseDuration(raw)
	if err != nil {
		if field != "" {
			return 0, fmt.Errorf("%s: %w", field, err)
		}
		return 0, err
	}

	if duration < 0 {
		if field != "" {
			return 0, fmt.Errorf("%s must not be negative", field)
		}
		return 0, errors.New("must not be negative")
	}

	return duration, nil
}

// ParseOptionalDuration parses a duration string; an empty string means zero.
// Negative durations are rejected.
func ParseOptionalDuration(raw string) (time.Duration, error) {
	return parseDuration(raw, 0, "")
}

// KeepAliveDuration parses a keepAlive value; empty means the 15s default
// (0 disables).
func KeepAliveDuration(keepAlive string) (time.Duration, error) {
	return parseDuration(keepAlive, 15*time.Second, "keepAlive")
}

// InteractionTimeout parses an interactionTimeout value; empty means no timeout.
func InteractionTimeout(interactionTimeout string) (time.Duration, error) {
	return parseDuration(interactionTimeout, 0, "interactionTimeout")
}
