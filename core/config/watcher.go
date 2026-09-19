package config

// WatcherInfo mirrors ConfigWatcher.Info.
type WatcherInfo struct {
	Ignore []string `json:"ignore,omitempty"`
}
