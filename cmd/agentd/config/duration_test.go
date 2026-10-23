package config

import (
	"testing"
	"time"
)

func TestDurationHelpers(t *testing.T) {
	if got, err := ParseOptionalDuration(""); err != nil || got != 0 {
		t.Fatalf("ParseOptionalDuration empty = %v, %v", got, err)
	}
	if got, err := ParseOptionalDuration("5s"); err != nil || got != 5*time.Second {
		t.Fatalf("ParseOptionalDuration 5s = %v, %v", got, err)
	}
	if _, err := ParseOptionalDuration("-1s"); err == nil {
		t.Fatal("expected a negative duration to be rejected")
	}
	if got, err := KeepAliveDuration(""); err != nil || got != 15*time.Second {
		t.Fatalf("KeepAliveDuration empty = %v, %v", got, err)
	}
	if _, err := KeepAliveDuration("-1s"); err == nil {
		t.Fatal("expected a negative keepAlive to be rejected")
	}
	if got, err := InteractionTimeout(""); err != nil || got != 0 {
		t.Fatalf("InteractionTimeout empty = %v, %v", got, err)
	}
	if _, err := InteractionTimeout("-1s"); err == nil {
		t.Fatal("expected a negative interactionTimeout to be rejected")
	}
}
