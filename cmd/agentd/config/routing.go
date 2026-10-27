package config

import (
	"net"
	"strings"
)

// NormalizePathPrefix ensures one leading slash and no trailing slash.
func NormalizePathPrefix(pathPrefix string) string {
	if pathPrefix == "" {
		return ""
	}
	return "/" + strings.Trim(pathPrefix, "/")
}

// NormalizeHost lowercases a host and drops a valid port, so configured and
// request hosts compare consistently. Bracketed IPv6 literals are unwrapped.
func NormalizeHost(host string) string {
	host = strings.TrimSpace(host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}

	return strings.ToLower(host)
}
