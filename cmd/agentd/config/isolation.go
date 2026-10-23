package config

import "fmt"

// Validate checks a per-server isolation configuration against the server name
// and working directory. Nil is valid (shares the working directory).
func (i *IsolationConfig) Validate(name, workingDirectory string) error {
	if i == nil {
		return nil
	}
	switch i.Mode {
	case "", IsolationNone:
		return nil
	case IsolationCopy, IsolationWorktree:
		if i.BaseDir == "" {
			return fmt.Errorf("server %q: isolation.baseDir is required for mode %s", name, i.Mode)
		}
	case IsolationOverlay:
		if err := i.validateOverlay(name); err != nil {
			return err
		}
	case IsolationContainer, IsolationRemote:
		return fmt.Errorf(
			"server %q: isolation.mode %s is external; "+
				"such an instance runs its own agentd and is provisioned by the host",
			name, i.Mode)
	default:
		return fmt.Errorf("server %q: isolation.mode must be none, copy, worktree or overlay, got %q", name, i.Mode)
	}
	if workingDirectory == "" {
		return fmt.Errorf("server %q: isolation mode %s requires workingDirectory", name, i.Mode)
	}
	return nil
}

// validateOverlay checks the fields overlay mode requires.
func (i *IsolationConfig) validateOverlay(name string) error {
	if i.BaseDir == "" {
		return fmt.Errorf("server %q: isolation.baseDir is required for mode %s", name, IsolationOverlay)
	}
	if !i.BaseReadOnly {
		return fmt.Errorf("server %q: isolation.baseReadOnly is required for mode %s", name, IsolationOverlay)
	}
	if i.Cow != nil && !i.Cow.Mode.Valid() {
		return fmt.Errorf("server %q: isolation.cow.mode must be auto, reflink or clonefile, got %q", name, i.Cow.Mode)
	}
	return nil
}
