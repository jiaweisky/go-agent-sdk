package config

import "fmt"

// Validate checks a per-server isolation configuration against its working
// directory. Nil is valid (shares the working directory).
func (i *IsolationConfig) Validate(workingDirectory string) error {
	if i == nil {
		return nil
	}
	switch i.Mode {
	case "", IsolationNone:
		return nil
	case IsolationCopy, IsolationWorktree:
		if i.BaseDir == "" {
			return fmt.Errorf("isolation.baseDir is required for mode %s", i.Mode)
		}
	case IsolationOverlay:
		if err := i.validateOverlay(); err != nil {
			return err
		}
	case IsolationContainer, IsolationRemote:
		return fmt.Errorf(
			"isolation.mode %s is external; "+
				"such an instance runs its own agentd and is provisioned by the host",
			i.Mode)
	default:
		return fmt.Errorf("isolation.mode must be none, copy, worktree or overlay, got %q", i.Mode)
	}
	if workingDirectory == "" {
		return fmt.Errorf("isolation mode %s requires workingDirectory", i.Mode)
	}
	return nil
}

// validateOverlay checks the fields overlay mode requires.
func (i *IsolationConfig) validateOverlay() error {
	if i.BaseDir == "" {
		return fmt.Errorf("isolation.baseDir is required for mode %s", IsolationOverlay)
	}
	if !i.BaseReadOnly {
		return fmt.Errorf("isolation.baseReadOnly is required for mode %s", IsolationOverlay)
	}
	if i.Cow != nil && !i.Cow.Mode.Valid() {
		return fmt.Errorf("isolation.cow.mode must be auto, reflink or clonefile, got %q", i.Cow.Mode)
	}
	return nil
}
