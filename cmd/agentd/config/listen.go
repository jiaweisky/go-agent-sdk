package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// IsLoopback reports whether listen binds only the loopback interface.
func IsLoopback(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}

	if host == "" {
		return false // empty host binds all interfaces
	}

	if strings.EqualFold(host, "localhost") {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}

// NormalizeListenAddress removes case, IP-literal and numeric-port formatting
// that do not change the bound address. It does not unify wildcard hosts
// (":8080" vs "0.0.0.0:8080") or resolve DNS aliases.
func NormalizeListenAddress(listen string) string { return normalizeListenAddress(listen) }

// normalizeListenAddress removes case, IP-literal and numeric-port formatting
// that do not change the bound address. It does not resolve DNS aliases.
func normalizeListenAddress(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return listen
	}

	if ip := net.ParseIP(host); ip != nil {
		host = ip.String()
	} else {
		host = strings.ToLower(host)
	}

	if number, err := strconv.Atoi(port); err == nil {
		port = strconv.Itoa(number)
	}

	return net.JoinHostPort(host, port)
}

// validateControlListenOverlap rejects a control API or its fixed project
// listener binding the same address as a server or as the control API itself.
func validateControlListenOverlap(control *ControlConfig, servers []Server) error {
	if control == nil {
		return nil
	}

	if err := validateListenAgainstServers(control.Listen, "control.listen", servers); err != nil {
		return err
	}

	if control.ProjectListen == "" {
		return nil
	}

	if err := validateListenAgainstServers(control.ProjectListen, "control.projectListen", servers); err != nil {
		return err
	}

	if listenAddressesConflict(control.ProjectListen, control.Listen) {
		return fmt.Errorf(
			"control.projectListen %q conflicts with control.listen %q",
			control.ProjectListen, control.Listen)
	}

	return nil
}

// validateListenAgainstServers rejects a listen address that overlaps a server.
func validateListenAgainstServers(listen, field string, servers []Server) error {
	for _, server := range servers {
		if listenAddressesConflict(listen, server.Listen) {
			return fmt.Errorf(
				"%s %q conflicts with server %q listen %q",
				field, listen, server.Name, server.Listen)
		}
	}

	return nil
}

// listenAddressesConflict reports whether two listen addresses would compete
// for the same TCP port. Port 0 is ephemeral, so it never conflicts.
func listenAddressesConflict(a, b string) bool {
	ah, ap, err := net.SplitHostPort(normalizeListenAddress(a))
	if err != nil {
		return false
	}

	bh, bp, err := net.SplitHostPort(normalizeListenAddress(b))
	if err != nil {
		return false
	}

	if ap != bp || ap == "0" {
		return false
	}

	if wildcardListenHost(ah) || wildcardListenHost(bh) {
		return true
	}

	return strings.EqualFold(ah, bh)
}

// wildcardListenHost reports whether a listen host binds every interface.
func wildcardListenHost(host string) bool {
	if host == "" {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsUnspecified()
}
