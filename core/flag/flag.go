package flag

import (
	"os"
	"runtime"
	"strings"
)

func Truthy(key string) bool {
	value := strings.ToLower(os.Getenv(key))
	return value == "true" || value == "1"
}

func enabledByExperimental(key string) bool {
	if _, ok := os.LookupEnv(key); !ok {
		return Truthy("OPENCODE_EXPERIMENTAL")
	}
	return Truthy(key)
}

// envBoolean ports effect Config.boolean(name).pipe(Config.withDefault(false)):
// it accepts the case-sensitive literals true/false/yes/no/on/off/1/0/y/n and
// treats any other or missing value as false.
func envBoolean(key string) bool {
	switch os.Getenv(key) {
	case "true", "yes", "on", "1", "y":
		return true
	default:
		return false
	}
}

type flagValues struct {
	OTEL_EXPORTER_OTLP_ENDPOINT string
	OTEL_EXPORTER_OTLP_HEADERS  string

	OPENCODE_AUTO_HEAP_SNAPSHOT     bool
	OPENCODE_GIT_BASH_PATH          string
	OPENCODE_CONFIG                 string
	OPENCODE_CONFIG_CONTENT         string
	OPENCODE_DISABLE_AUTOUPDATE     bool
	OPENCODE_ALWAYS_NOTIFY_UPDATE   bool
	OPENCODE_DISABLE_PRUNE          bool
	OPENCODE_DISABLE_TERMINAL_TITLE bool
	OPENCODE_SHOW_TTFD              bool
	OPENCODE_DISABLE_AUTOCOMPACT    bool
	OPENCODE_DISABLE_MODELS_FETCH   bool
	OPENCODE_DISABLE_MOUSE          bool
	OPENCODE_FAKE_VCS               string
	OPENCODE_SERVER_PASSWORD        string
	OPENCODE_SERVER_USERNAME        string
	OPENCODE_DISABLE_FFF            bool

	OPENCODE_EXPERIMENTAL_DISABLE_COPY_ON_SELECT bool
	OPENCODE_MODELS_URL                          string
	OPENCODE_MODELS_PATH                         string
	OPENCODE_DB                                  string

	OPENCODE_WORKSPACE_ID            string
	OPENCODE_EXPERIMENTAL_WORKSPACES bool
}

var Flag = flagValues{
	OTEL_EXPORTER_OTLP_ENDPOINT: os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
	OTEL_EXPORTER_OTLP_HEADERS:  os.Getenv("OTEL_EXPORTER_OTLP_HEADERS"),

	OPENCODE_AUTO_HEAP_SNAPSHOT:     Truthy("OPENCODE_AUTO_HEAP_SNAPSHOT"),
	OPENCODE_GIT_BASH_PATH:          os.Getenv("OPENCODE_GIT_BASH_PATH"),
	OPENCODE_CONFIG:                 os.Getenv("OPENCODE_CONFIG"),
	OPENCODE_CONFIG_CONTENT:         os.Getenv("OPENCODE_CONFIG_CONTENT"),
	OPENCODE_DISABLE_AUTOUPDATE:     Truthy("OPENCODE_DISABLE_AUTOUPDATE"),
	OPENCODE_ALWAYS_NOTIFY_UPDATE:   Truthy("OPENCODE_ALWAYS_NOTIFY_UPDATE"),
	OPENCODE_DISABLE_PRUNE:          Truthy("OPENCODE_DISABLE_PRUNE"),
	OPENCODE_DISABLE_TERMINAL_TITLE: Truthy("OPENCODE_DISABLE_TERMINAL_TITLE"),
	OPENCODE_SHOW_TTFD:              Truthy("OPENCODE_SHOW_TTFD"),
	OPENCODE_DISABLE_AUTOCOMPACT:    Truthy("OPENCODE_DISABLE_AUTOCOMPACT"),
	OPENCODE_DISABLE_MODELS_FETCH:   Truthy("OPENCODE_DISABLE_MODELS_FETCH"),
	OPENCODE_DISABLE_MOUSE:          Truthy("OPENCODE_DISABLE_MOUSE"),
	OPENCODE_FAKE_VCS:               os.Getenv("OPENCODE_FAKE_VCS"),
	OPENCODE_SERVER_PASSWORD:        os.Getenv("OPENCODE_SERVER_PASSWORD"),
	OPENCODE_SERVER_USERNAME:        os.Getenv("OPENCODE_SERVER_USERNAME"),
	OPENCODE_DISABLE_FFF:            disableFFF(),

	OPENCODE_EXPERIMENTAL_DISABLE_COPY_ON_SELECT: disableCopyOnSelect(),
	OPENCODE_MODELS_URL:                          os.Getenv("OPENCODE_MODELS_URL"),
	OPENCODE_MODELS_PATH:                         os.Getenv("OPENCODE_MODELS_PATH"),
	OPENCODE_DB:                                  os.Getenv("OPENCODE_DB"),

	OPENCODE_WORKSPACE_ID:            os.Getenv("OPENCODE_WORKSPACE_ID"),
	OPENCODE_EXPERIMENTAL_WORKSPACES: enabledByExperimental("OPENCODE_EXPERIMENTAL_WORKSPACES"),
}

func disableFFF() bool {
	if _, ok := os.LookupEnv("OPENCODE_DISABLE_FFF"); !ok {
		return runtime.GOOS == "windows"
	}
	return Truthy("OPENCODE_DISABLE_FFF")
}

func disableCopyOnSelect() bool {
	if _, ok := os.LookupEnv("OPENCODE_EXPERIMENTAL_DISABLE_COPY_ON_SELECT"); !ok {
		return runtime.GOOS == "windows"
	}
	return Truthy("OPENCODE_EXPERIMENTAL_DISABLE_COPY_ON_SELECT")
}

// OPENCODE_EXPERIMENTAL_FILEWATCHER and OPENCODE_EXPERIMENTAL_DISABLE_FILEWATCHER
// are Effect Config values in TS; without DI they resolve on access here.
func (flagValues) OPENCODE_EXPERIMENTAL_FILEWATCHER() bool {
	return envBoolean("OPENCODE_EXPERIMENTAL_FILEWATCHER")
}

func (flagValues) OPENCODE_EXPERIMENTAL_DISABLE_FILEWATCHER() bool {
	return envBoolean("OPENCODE_EXPERIMENTAL_DISABLE_FILEWATCHER")
}

// The remaining accessors port the TS get accessors, which are evaluated at
// access time rather than module load.
func (flagValues) OPENCODE_DISABLE_PROJECT_CONFIG() bool {
	return Truthy("OPENCODE_DISABLE_PROJECT_CONFIG")
}

func (flagValues) OPENCODE_EXPERIMENTAL_REFERENCES() bool {
	return enabledByExperimental("OPENCODE_EXPERIMENTAL_REFERENCES")
}

func (flagValues) OPENCODE_TUI_CONFIG() string {
	return os.Getenv("OPENCODE_TUI_CONFIG")
}

func (flagValues) OPENCODE_CONFIG_DIR() string {
	return os.Getenv("OPENCODE_CONFIG_DIR")
}

func (flagValues) OPENCODE_PURE() bool {
	return Truthy("OPENCODE_PURE")
}

func (flagValues) OPENCODE_PERMISSION() string {
	return os.Getenv("OPENCODE_PERMISSION")
}

func (flagValues) OPENCODE_PLUGIN_META_FILE() string {
	return os.Getenv("OPENCODE_PLUGIN_META_FILE")
}

func (flagValues) OPENCODE_CLIENT() string {
	if value, ok := os.LookupEnv("OPENCODE_CLIENT"); ok {
		return value
	}
	return "cli"
}
