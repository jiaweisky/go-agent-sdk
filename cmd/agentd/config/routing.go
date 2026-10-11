package config

import (
	"net"
	"strings"
)

// NormalizeMount trims a trailing slash, keeping a single leading one.
func NormalizeMount(mount string) string {
	if mount == "" {
		return ""
	}
	return "/" + strings.Trim(mount, "/")
}

// NormalizeHost lowercases a host and drops any port, so configured and request
// hosts compare consistently.
func NormalizeHost(host string) string {
	host = strings.TrimSpace(host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(host)
}
