package config

// ControlConfig configures the optional control API: a project-instance
// lifecycle API that creates and destroys isolated instances at runtime.
type ControlConfig struct {
	// Listen is the control API bind address (required to enable it).
	Listen string `yaml:"listen"`
	// ProjectListen is the host:port dynamic project instances bind to; the
	// default (127.0.0.1:0) picks a free loopback port per instance.
	ProjectListen string `yaml:"projectListen"`
	// MaxLeases caps the number of live project instances; 0 (the default) is
	// unlimited. It bounds a control-API caller from exhausting resources.
	MaxLeases int `yaml:"maxLeases"`
	// Auth, when set, guards the control API.
	Auth *AuthConfig `yaml:"auth"`
}
