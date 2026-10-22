package config

// AuthConfig enables bearer authentication on the served endpoints, the
// control API, and dynamically created project instances.
type AuthConfig struct {
	// Type is the authentication scheme; "bearer" is the only supported value
	// and must be set explicitly.
	Type string `yaml:"type" json:"type"`
	// Token is the bearer token clients must present.
	Token string `yaml:"token" json:"token"`
}
