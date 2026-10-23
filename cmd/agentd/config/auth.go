package config

import (
	"errors"
	"fmt"
)

// AuthType is the authentication scheme.
type AuthType string

const (
	// AuthBearer requires a bearer token on every request except public routes.
	AuthBearer AuthType = "bearer"
)

// Valid reports whether t is a supported authentication scheme.
func (t AuthType) Valid() bool {
	return t == AuthBearer
}

// AuthConfig enables bearer authentication on the served endpoints, the
// control API, and dynamically created project instances.
type AuthConfig struct {
	// Type is the authentication scheme; AuthBearer is the only supported value
	// and must be set explicitly.
	Type AuthType `yaml:"type" json:"type"`
	// Token is the bearer token clients must present.
	Token string `yaml:"token" json:"token"`
}

// Validate rejects a malformed auth block. Nil is valid.
func (a *AuthConfig) Validate() error {
	if a == nil {
		return nil
	}
	if !a.Type.Valid() {
		return fmt.Errorf("auth.type must be %q, got %q", AuthBearer, a.Type)
	}
	if a.Token == "" {
		return errors.New("auth.token is required for bearer")
	}
	return nil
}
