package main

import (
	"os"
	"strings"

	"github.com/user/go-agent-sdk"
)

// interactivePermission prompts for each "ask" permission request. It follows
// the three-stage flow: choose Allow once / Allow always / Reject, confirm an
// "always" choice against the patterns it would remember, or collect a message
// explaining a rejection.
func interactivePermission(console *console) agent.PermissionHandlerFunc {
	return func(request agent.PermissionRequest) (agent.PermissionDecision, error) {
		return askPermission(console, request)
	}
}

// permissionChoice is the decision requested at the first stage.
type permissionChoice int

const (
	permissionInvalid permissionChoice = iota
	permissionOnce
	permissionAlways
	permissionReject
)

func askPermission(console *console, request agent.PermissionRequest) (agent.PermissionDecision, error) {
	for {
		writePermissionRequest(console, request)
		// Allow always is the default, so a demo only has to confirm it.
		answer, err := console.prompt("Choice [2]: ")
		if err != nil {
			// EOF: fail safe rather than allow.
			return denied(""), nil
		}
		switch parsePermissionChoice(answer) {
		case permissionOnce:
			return agent.PermissionDecision{Allow: true}, nil
		case permissionAlways:
			if len(request.Save) == 0 {
				console.write("This action cannot be remembered; allowing once.\n")
				return agent.PermissionDecision{Allow: true}, nil
			}
			if confirmAlways(console, request) {
				return agent.PermissionDecision{Allow: true, Always: true}, nil
			}
		case permissionReject:
			return rejectPermission(console), nil
		}
		console.write("\n")
	}
}

// confirmAlways shows the patterns an "always" choice would remember and asks
// for confirmation, returning false when the user cancels.
func confirmAlways(console *console, request agent.PermissionRequest) bool {
	console.write("\n" + yellow + "△ Always allow" + reset + "\n")
	for _, line := range alwaysLines(request) {
		console.write("  " + line + "\n")
	}
	console.write("\n  1) Confirm\n  2) Cancel\n")
	for {
		answer, err := console.prompt("Choice [1]: ")
		if err != nil {
			return false
		}
		switch strings.TrimSpace(answer) {
		case "", "1", "confirm", "c":
			return true
		case "2", "cancel":
			return false
		}
	}
}

// rejectPermission collects an optional message explaining the rejection.
func rejectPermission(console *console) agent.PermissionDecision {
	console.write("\n" + yellow + "△ Reject permission" + reset + "\n")
	console.write("  Tell the agent what to do differently\n")
	message, err := console.prompt("Message: ")
	if err != nil {
		return denied("")
	}
	return denied(message)
}

// writePermissionRequest renders the request: a title, the patterns it covers,
// and the three choices.
func writePermissionRequest(console *console, request agent.PermissionRequest) {
	icon, title := permissionTitle(request)
	console.write("\n" + yellow + "△ Permission required" + reset + "\n")
	console.write("  " + icon + " " + title + "\n")
	if len(request.Resources) > 0 {
		console.write("\nPatterns\n")
		for _, resource := range request.Resources {
			console.write("  - " + resource + "\n")
		}
	}
	console.write("\n  1) Allow once\n  2) Allow always\n  3) Reject\n")
}

// permissionTitle renders the request's icon and title.
func permissionTitle(request agent.PermissionRequest) (icon, title string) {
	if request.Action == "external_directory" {
		return "←", "Access external directory " + externalDirectory(request)
	}
	return "⚙", "Call tool " + request.Action
}

// externalDirectory renders the directory from the request's first pattern, with
// the home directory abbreviated.
func externalDirectory(request agent.PermissionRequest) string {
	raw := ""
	if len(request.Resources) > 0 {
		raw = request.Resources[0]
	}
	if i := strings.IndexByte(raw, '*'); i >= 0 {
		raw = raw[:i]
	}
	raw = strings.TrimRight(raw, "/\\")
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if raw == home {
			return "~"
		}
		if strings.HasPrefix(raw, home+"/") {
			return "~" + raw[len(home):]
		}
	}
	return raw
}

// alwaysLines is the confirmation body: the patterns the SDK will remember. The
// SDK writes them to the project's permission table, so they persist until
// removed; the wording says so instead of implying they expire on restart.
func alwaysLines(request agent.PermissionRequest) []string {
	if len(request.Save) == 1 && request.Save[0] == "*" {
		return []string{"This will allow " + request.Action + " until the rule is removed (saved for this project)."}
	}
	lines := []string{"This will allow the following patterns until the rule is removed (saved for this project)"}
	for _, pattern := range request.Save {
		lines = append(lines, "- "+pattern)
	}
	return lines
}

// parsePermissionChoice maps an answer to a choice; an empty answer keeps the
// default (Allow always).
func parsePermissionChoice(answer string) permissionChoice {
	switch strings.TrimSpace(answer) {
	case "", "2", "always", "a":
		return permissionAlways
	case "1", "once", "o":
		return permissionOnce
	case "3", "reject", "r":
		return permissionReject
	}
	return permissionInvalid
}

// denied builds a rejection, falling back to a generic message.
func denied(message string) agent.PermissionDecision {
	if strings.TrimSpace(message) == "" {
		message = "User denied this action."
	}
	return agent.PermissionDecision{Allow: false, Message: message}
}
