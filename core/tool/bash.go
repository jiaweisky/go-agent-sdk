package tool

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strings"
	"time"
	"unicode"

	"github.com/jiaweisky/go-agent-sdk/core"
	"github.com/jiaweisky/go-agent-sdk/jsonschema"
)

const BashName = "bash"

const (
	BashDefaultTimeoutMS = 2 * 60 * 1_000
	BashMaxTimeoutMS     = 10 * 60 * 1_000
	BashMaxCaptureBytes  = 1024 * 1024
)

var BashInputSchema = jsonschema.Object(
	jsonschema.Required("command", jsonschema.String(jsonschema.Description("Shell command string to execute"))),
	jsonschema.Optional("workdir", jsonschema.String(jsonschema.Description(
		"Working directory. Defaults to the active Location; relative paths resolve from that Location."))),
	jsonschema.Optional("timeout", jsonschema.Integer(
		jsonschema.ExclusiveMinimum(0),
		jsonschema.Maximum(BashMaxTimeoutMS),
		jsonschema.Description(fmt.Sprintf(
			"Timeout in milliseconds. Defaults to %d and may not exceed %d.",
			BashDefaultTimeoutMS,
			BashMaxTimeoutMS,
		)),
	)),
)

var BashStructuredOutputSchema = jsonschema.Object(
	jsonschema.Optional("exit", jsonschema.Number()),
	jsonschema.Required("truncated", jsonschema.Boolean()),
	jsonschema.Optional("timeout", jsonschema.Boolean()),
)

var BashOutputSchema = jsonschema.Object(
	jsonschema.Optional("exit", jsonschema.Number()),
	jsonschema.Required("truncated", jsonschema.Boolean()),
	jsonschema.Optional("timeout", jsonschema.Boolean()),
	jsonschema.Required("output", jsonschema.String()),
	jsonschema.Optional("warnings", jsonschema.Array(jsonschema.String())),
)

type BashInput struct {
	Command string  `json:"command"`
	Workdir *string `json:"workdir,omitempty"`
	Timeout *int    `json:"timeout,omitempty"`
}

type BashOutput struct {
	Exit      *int     `json:"exit,omitempty"`
	Truncated bool     `json:"truncated"`
	Timeout   *bool    `json:"timeout,omitempty"`
	Output    string   `json:"output"`
	Warnings  []string `json:"warnings,omitempty"`
}

type BashStructuredOutput struct {
	Exit      *int  `json:"exit,omitempty"`
	Truncated bool  `json:"truncated"`
	Timeout   *bool `json:"timeout,omitempty"`
}

type BashDeps struct {
	Mutation   core.LocationMutation
	Process    core.AppProcess
	Permission core.Permission
	// Shell overrides the platform default shell. Empty uses the default.
	Shell string
}

func MakeBash(deps BashDeps) *Def[BashInput, BashOutput, BashStructuredOutput] {
	return Make(Config[BashInput, BashOutput, BashStructuredOutput]{
		Description: fmt.Sprintf(
			"Execute one shell command string with the host user's filesystem, process, and network authority. "+
				"The active Location is the default working directory. Relative workdir values resolve from that "+
				"Location. External workdir values require external_directory approval; best-effort command-argument "+
				"path warnings are advisory only. Timeout values are milliseconds (default: %d; maximum: %d). "+
				"Uses the configured shell when set; otherwise uses /bin/sh on POSIX and COMSPEC or cmd.exe on Windows.",
			BashDefaultTimeoutMS,
			BashMaxTimeoutMS,
		),
		Input:      BashInputSchema,
		Output:     BashOutputSchema,
		Structured: BashStructuredOutputSchema,
		ToStructuredOutput: func(input BashInput, output any) (BashStructuredOutput, error) {
			out := DecodeStructured[BashOutput](output)
			return BashStructuredOutput{
				Exit:      out.Exit,
				Truncated: out.Truncated,
				Timeout:   out.Timeout,
			}, nil
		},
		ToModelOutput: func(input BashInput, output any) []Content {
			out := DecodeStructured[BashOutput](output)
			return []Content{
				TextContent{Text: out.Output},
				TextContent{Text: BashModelOutput(out)},
			}
		},
		Execute: func(execCtx context.Context, input BashInput, ctx Context) (BashOutput, error) {
			result, err := executeBash(deps, execCtx, input, ctx)
			if err != nil {
				return BashOutput{}, failure(fmt.Sprintf("Unable to execute command: %s", input.Command), err)
			}
			return result, nil
		},
	})
}

// Deferred V2 bash behavior and UX integrations remain visible at the model-facing seam.
// TODO: Port tree-sitter bash / PowerShell parser-based approval reduction.
// TODO: Port BashArity reusable command-prefix approvals.
// TODO: Replace token-based command-argument external-directory advisories with parser-based detection.
// TODO: Restore PowerShell and cmd-specific invocation/path handling on Windows.
// TODO: Add plugin shell.env environment augmentation once V2 plugin hooks exist.
// TODO: Add durable/live progress metadata streaming for long-running commands
// once V2 tool invocation progress context is wired.
// TODO: Persist background job status and define restart recovery before exposing remote observation.
// TODO: Re-add model-facing background launch only with owner-bound get/wait/cancel tools and completion delivery.
// TODO: Add HTTP background-job observation only after durable status, restart recovery, and authorization are defined.
// TODO: Revisit process-group cleanup and platform coverage with shell-specific
// tests if current AppProcess semantics do not fully cover it.
// TODO: Revisit binary output handling if stdout/stderr decoding is text-only.
// TODO: Stream full shell output into managed storage while retaining only a bounded in-memory preview.

func BashModelOutput(output BashOutput) string {
	warnings := ""
	if len(output.Warnings) > 0 {
		lines := make([]string, len(output.Warnings))
		for i, warning := range output.Warnings {
			lines[i] = "- " + warning
		}
		warnings = "\n\nWarnings:\n" + strings.Join(lines, "\n")
	}
	prefix := strings.TrimLeftFunc(warnings, unicode.IsSpace)
	if warnings != "" {
		prefix += "\n\n"
	}
	if output.Timeout != nil && *output.Timeout {
		return prefix + "Command timed out before completion."
	}
	exit := 0
	if output.Exit != nil {
		exit = *output.Exit
	}
	return prefix + fmt.Sprintf("Command exited with code %d.", exit)
}

func bashDefaultShell() string {
	if goruntime.GOOS == "windows" {
		if comspec, ok := os.LookupEnv("COMSPEC"); ok {
			return comspec
		}
		return "cmd.exe"
	}
	return "/bin/sh"
}

func bashShell(shell string) string {
	if shell != "" {
		return shell
	}
	return bashDefaultShell()
}

func bashShellArgs(command string) []string {
	if goruntime.GOOS == "windows" {
		return []string{"/d", "/s", "/c", command}
	}
	return []string{"-c", command}
}

var (
	bashShellTokenPattern = regexp.MustCompile(`(?:[^\s"']+|"[^"]*"|'[^']*')+`)
	bashTrailingPattern   = regexp.MustCompile(`[;,|&]+$`)
)

func bashShellTokens(command string) []string {
	return bashShellTokenPattern.FindAllString(command, -1)
}

func bashUnquote(value string) string {
	if len(value) >= 2 && (value[0] == '\'' || value[0] == '"') && value[len(value)-1] == value[0] {
		return value[1 : len(value)-1]
	}
	return value
}

// bashResolve canonicalizes a path, falling back to the lexically resolved path
// when it does not exist.
func bashResolve(value string) (string, error) {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		return resolved, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return absolute, nil
	}
	return "", err
}

func bashExternalCommandDirectories(command, cwd string) ([]string, error) {
	var directories []string
	seen := make(map[string]bool)
	for _, token := range bashShellTokens(command) {
		value := bashTrailingPattern.ReplaceAllString(bashUnquote(token), "")
		if !filepath.IsAbs(value) {
			continue
		}
		resolved, err := bashResolve(value)
		if err != nil {
			return nil, err
		}
		if core.Contains(cwd, resolved) {
			continue
		}
		directory, err := bashResolve(filepath.Dir(resolved))
		if err != nil {
			return nil, err
		}
		if !seen[directory] {
			seen[directory] = true
			directories = append(directories, directory)
		}
	}
	return directories, nil
}

func bashWarningPath(directory string) string {
	return strings.ReplaceAll(filepath.Join(directory, "*"), "\\", "/")
}

func executeBash(deps BashDeps, execCtx context.Context, input BashInput, ctx Context) (BashOutput, error) {
	source := &core.PermissionSource{
		Type:      core.PermissionSourceTypeTool,
		MessageID: ctx.AssistantMessageID,
		CallID:    ctx.ToolCallID,
	}

	workdir := "."
	if input.Workdir != nil {
		workdir = *input.Workdir
	}
	kind := core.LocationMutationKindDirectory
	target, err := deps.Mutation.Resolve(core.LocationMutationResolveInput{Path: workdir, Kind: &kind})
	if err != nil {
		return BashOutput{}, err
	}

	if external := target.ExternalDirectory; external != nil {
		extPerm := core.LocationMutationExternalDirectoryPermission(*external)
		if err := deps.Permission.Assert(core.PermissionAssertInput{
			SessionID: ctx.SessionID,
			Action:    extPerm.Action,
			Resources: extPerm.Resources,
			Save:      extPerm.Save,
			Source:    source,
			Agent:     &ctx.Agent,
		}); err != nil {
			return BashOutput{}, err
		}
	}

	directories, err := bashExternalCommandDirectories(input.Command, target.Canonical)
	if err != nil {
		return BashOutput{}, err
	}
	var warnings []string
	for _, directory := range directories {
		warnings = append(warnings, fmt.Sprintf(
			"Command argument references external directory %s. Bash runs with host-user filesystem, process, "+
				"and network authority; this scan is advisory only.",
			bashWarningPath(directory),
		))
	}

	if err := deps.Permission.Assert(core.PermissionAssertInput{
		SessionID: ctx.SessionID,
		Action:    BashName,
		Resources: []string{input.Command},
		Save:      []string{input.Command},
		Source:    source,
		Agent:     &ctx.Agent,
	}); err != nil {
		return BashOutput{}, err
	}

	info, err := core.Stat(target.Canonical)
	if err != nil {
		return BashOutput{}, err
	}
	if info.Type != core.FileKindDirectory {
		return BashOutput{}, fmt.Errorf("Working directory is not a directory: %s", target.Canonical)
	}

	shell := bashShell(deps.Shell)

	timeout := BashDefaultTimeoutMS
	if input.Timeout != nil {
		timeout = *input.Timeout
	}
	maxBytes := BashMaxCaptureBytes
	result, err := deps.Process.Run(execCtx, shell, bashShellArgs(input.Command), core.RunOptions{
		Dir:            target.Canonical,
		CombineOutput:  true,
		Timeout:        time.Duration(timeout) * time.Millisecond,
		MaxOutputBytes: &maxBytes,
		// stdin is left nil, which reads from the null device.
		Detached:       goruntime.GOOS != "windows",
		ForceKillAfter: 3 * time.Second,
	})
	if err != nil {
		if bashIsTimeout(err) {
			return bashTimeoutOutput(timeout, warnings), nil
		}
		return BashOutput{}, err
	}

	output := string(result.Output)
	if output == "" {
		output = "(no output)"
	}
	if result.OutputTruncated {
		output = output + "\n\n[output capture truncated at the in-memory safety limit]"
	}
	exit := result.ExitCode
	out := BashOutput{
		Exit:      &exit,
		Output:    output,
		Truncated: result.OutputTruncated,
	}
	if len(warnings) > 0 {
		out.Warnings = warnings
	}
	return out, nil
}

func bashTimeoutOutput(timeout int, warnings []string) BashOutput {
	timedOut := true
	out := BashOutput{
		Output: fmt.Sprintf(
			"Command exceeded timeout of %d ms. Retry with a larger timeout if the command is expected to take longer.",
			timeout,
		),
		Truncated: false,
		Timeout:   &timedOut,
	}
	if len(warnings) > 0 {
		out.Warnings = warnings
	}
	return out
}

func bashIsTimeout(err error) bool {
	return errors.Is(err, core.ErrTimedOut)
}

func RegisterBash(tools Tools, deps BashDeps) (func(), error) {
	return tools.Register(map[string]Tool{
		BashName: MakeBash(deps),
	})
}
