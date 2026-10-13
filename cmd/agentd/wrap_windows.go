//go:build windows

package main

import "errors"

// execReplace is unsupported on Windows: whole-process wrapping has no single
// command equivalent (AppContainer + Job Object have to be assembled), so agentd
// reports the misconfiguration instead of running unwrapped.
func execReplace(name string, args, env []string) error {
	return errors.New("whole-process wrapping is not supported on Windows")
}
