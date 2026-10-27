package sandbox

import (
	"context"
	"fmt"
	"iter"
	"regexp"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

// reportingProcess adds command-side violation detection to any sandbox wrapper.
// It runs outside the wrapper so seatbelt, bwrap and AppContainer all get the
// same behavior.
//
// precise parses syscall-level denials (Linux strace output) and is enabled only
// where the platform supports it; the default heuristic mode scans the command's
// stderr for OS denial messages instead.
type reportingProcess struct {
	inner         core.AppProcess
	precise       bool
	reportToModel bool
}

func (p reportingProcess) Run(
	ctx context.Context, command string, args []string, options ...core.RunOptions,
) (*core.RunResult, error) {
	result, err := p.inner.Run(ctx, command, args, options...)
	if err != nil || result == nil || !hasViolationSink(options) {
		return result, err
	}

	// Precise mode reads the syscall trace, which can show a denial even when the
	// command recovers and exits 0. Heuristic mode only scans a failed command.
	if !p.precise && result.ExitCode == 0 {
		return result, err
	}

	if violation, ok := p.detectViolation(result); ok {
		options[0].OnViolation(violation)
		if p.reportToModel {
			appendDenialNote(result, options[0], violation)
		}
	}

	return result, err
}

func (p reportingProcess) RunStream(
	ctx context.Context, command string, args []string, options ...core.RunStreamOptions,
) iter.Seq2[string, error] {
	stream := p.inner.RunStream(ctx, command, args, options...)
	if len(options) == 0 || options[0].OnViolation == nil {
		return stream
	}

	return func(yield func(string, error) bool) {
		reported := false
		for line, err := range stream {
			if p.precise {
				if !isStraceLine(line) {
					if !yield(line, err) {
						return
					}
					continue
				}

				// Never leak strace output; report it when the syscall failed.
				if violation, ok := preciseDenial(line); ok && !reported {
					options[0].OnViolation(violation)
					reported = true
				}

				continue
			}

			if !reported {
				if violation, ok := detectCommandViolation(line); ok {
					options[0].OnViolation(violation)
					reported = true
				}
			}

			if !yield(line, err) {
				return
			}
		}
	}
}

func hasViolationSink(options []core.RunOptions) bool {
	return len(options) > 0 && options[0].OnViolation != nil
}

// detect reports the first violation and, in precise mode, removes the strace
// lines from the result so the tool output stays clean. Precise mode does not
// fall back to the heuristic scan (strace is required at startup).
func (p reportingProcess) detectViolation(result *core.RunResult) (core.PolicyViolation, bool) {
	if p.precise {
		var found *core.PolicyViolation

		result.Output, found = cleanPrecise(result.Output, found)
		result.Stderr, found = cleanPrecise(result.Stderr, found)
		if found != nil {
			return *found, true
		}

		return core.PolicyViolation{}, false
	}

	output := string(result.Output) + "\n" + string(result.Stderr)

	return detectCommandViolation(output)
}

// appendDenialNote appends a compact note the model can act on rather than
// retrying the same command.
func appendDenialNote(result *core.RunResult, option core.RunOptions, violation core.PolicyViolation) {
	detail := violation.Action
	if violation.Path != "" {
		detail += " " + violation.Path
	}

	note := fmt.Sprintf("\n[sandbox] blocked %s", detail)

	// Only the combined-output caller (bash) reads Output as the command text.
	// Tools that parse stdout (ripgrep) must not receive the note there.
	if option.CombineOutput {
		result.Output = append(result.Output, []byte(note)...)
		return
	}

	result.Stderr = append(result.Stderr, []byte(note)...)
}

// straceLine matches a traced syscall line, including successful ones. Every
// such line must be stripped from the tool output; only failures become events.
var straceLine = regexp.MustCompile(`^\s*(?:\d+\s+)?(openat|connect)\(`)

// syscallDenial matches the failure tail of a traced syscall.
var syscallDenial = regexp.MustCompile(`=\s*-1\s+E(ACCES|PERM|ROFS|NETUNREACH|HOSTUNREACH)\b`)

var quotedPath = regexp.MustCompile(`"([^"]+)"`)

// inetAddr extracts an IPv4/IPv6 literal from a connect() sockaddr.
var inetAddr = regexp.MustCompile(`inet_(?:addr|pton)\((?:AF_INET6,\s*)?"([^"]+)"`)

// isStraceLine reports whether a line is strace output.
func isStraceLine(line string) bool {
	return straceLine.MatchString(line)
}

// preciseDenial parses a failed traced syscall into a violation.
func preciseDenial(line string) (core.PolicyViolation, bool) {
	match := straceLine.FindStringSubmatch(line)
	if match == nil || !syscallDenial.MatchString(line) {
		return core.PolicyViolation{}, false
	}

	violation := core.PolicyViolation{
		Action:    "file",
		Rule:      "os-denied",
		Surface:   "command",
		Certainty: "enforced",
	}

	if match[1] == "connect" {
		violation.Action = "network"
		if addr := inetAddr.FindStringSubmatch(line); addr != nil {
			violation.Host = addr[1]
		}

		return violation, true
	}

	if quoted := quotedPath.FindStringSubmatch(line); quoted != nil {
		violation.Path = quoted[1]
	}

	if strings.Contains(line, "O_WRONLY") || strings.Contains(line, "O_RDWR") || strings.Contains(line, "O_CREAT") {
		violation.Action = "write"
	}

	return violation, true
}

// cleanPrecise drops every strace line and records the first failed syscall.
func cleanPrecise(field []byte, found *core.PolicyViolation) ([]byte, *core.PolicyViolation) {
	lines := strings.Split(string(field), "\n")
	kept := make([]string, 0, len(lines))

	for _, line := range lines {
		if isStraceLine(line) {
			if violation, ok := preciseDenial(line); ok && found == nil {
				found = &violation
			}
			continue
		}

		kept = append(kept, line)
	}

	return []byte(strings.Join(kept, "\n")), found
}

// denialLine extracts a path from common OS denial messages.
var denialLine = regexp.MustCompile(`(?m)(/\S+?): (?:Operation not permitted|Permission denied|Read-only file system)`)

// commandDenials maps an OS denial message to the violation action.
var commandDenials = []struct {
	needle string
	action string
}{
	{"Operation not permitted", "file"},
	{"Permission denied", "file"},
	{"Read-only file system", "write"},
	{"Network is unreachable", "network"},
	{"No route to host", "network"},
	{"Access is denied", "file"},
}

// detectCommandViolation reports the first denial pattern found in output. It is
// intentionally best-effort: a denial the OS surfaced as text, never an
// authoritative enforcement record (Linux allow-only views return ENOENT, which
// is not distinguishable from a missing path and is not reported).
func detectCommandViolation(output string) (core.PolicyViolation, bool) {
	for _, denial := range commandDenials {
		if !strings.Contains(output, denial.needle) {
			continue
		}

		violation := core.PolicyViolation{
			Action:    denial.action,
			Rule:      "os-denied",
			Surface:   "command",
			Certainty: "suspected",
		}

		if match := denialLine.FindStringSubmatch(output); match != nil {
			violation.Path = match[1]
		}

		return violation, true
	}

	return core.PolicyViolation{}, false
}
