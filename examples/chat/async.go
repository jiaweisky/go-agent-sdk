package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/pkg/agent"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// asyncCommand toggles out-of-band interaction handling. With it on, permission
// and question requests block the turn until answered with /allow, /always,
// /deny, /answer or /reject instead of prompting inline via the handlers.
func (c *chat) asyncCommand(input string) {
	switch arg := commandArgument(input, "/async"); arg {
	case "":
		c.console.write(fmt.Sprintf("async interactions: %v\n", c.isAsync()))
	case "on":
		c.setAsync(true)
	case "off":
		c.setAsync(false)
	default:
		c.console.write("usage: /async [on|off]\n")
	}
}

// isAsync reports whether out-of-band interaction handling is on. It takes
// c.mu, so it is for the command path; the event path reads c.async directly
// because chat.handle already holds c.mu.
func (c *chat) isAsync() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.async
}

func (c *chat) setAsync(on bool) {
	c.mu.Lock()
	c.async = on
	c.mu.Unlock()
	c.client.HoldInteractions(on)
	if on {
		c.console.write("async interactions on: requests wait for /allow, /always, /deny, /answer, /reject\n")
	} else {
		c.console.write("async interactions off: the interactive handlers answer inline\n")
	}
}

// writePermissionAsked caches and renders a held permission request. The caller
// must hold c.mu (it runs on the event path), like the other render helpers.
func (c *chat) writePermissionAsked(request agent.PermissionRequest) {
	c.permissions = append(c.permissions, request)
	index := len(c.permissions)
	c.console.write(fmt.Sprintf("%s[permission] #%d %s %s%s\n",
		yellow, index, request.Action, strings.Join(request.Resources, ", "), reset))
	c.console.write(fmt.Sprintf("%s  /allow %d | /always %d | /deny %d%s\n", dim, index, index, index, reset))
}

// writeQuestionAsked caches and renders a held question request. The caller must
// hold c.mu.
func (c *chat) writeQuestionAsked(request agent.QuestionRequest) {
	c.questions = append(c.questions, request)
	index := len(c.questions)
	c.console.write(fmt.Sprintf("%s[question] #%d%s\n", yellow, index, reset))
	for _, question := range request.Questions {
		c.console.write(fmt.Sprintf("  %s\n", question.Question))
		for i, option := range question.Options {
			c.console.write(fmt.Sprintf("    %d) %s - %s\n", i+1, option.Label, option.Description))
		}
	}
	c.console.write(fmt.Sprintf("%s  /answer %d <text> | /reject %d%s\n", dim, index, index, reset))
}

// removePermission drops a settled request. The caller must hold c.mu.
func (c *chat) removePermission(requestID string) {
	c.permissions = removeRequestByID(c.permissions, requestID, func(r agent.PermissionRequest) string { return r.ID })
}

// removeQuestion drops a settled question. The caller must hold c.mu.
func (c *chat) removeQuestion(requestID string) {
	c.questions = removeRequestByID(c.questions, requestID, func(r agent.QuestionRequest) string { return r.ID })
}

// removeRequestByID drops the first item whose id matches, preserving order.
func removeRequestByID[T any](items []T, id string, idOf func(T) string) []T {
	for i, item := range items {
		if idOf(item) == id {
			return append(items[:i], items[i+1:]...)
		}
	}
	return items
}

// permissionsCommand lists the pending permission requests.
func (c *chat) permissionsCommand() {
	c.mu.Lock()
	pending := append([]agent.PermissionRequest(nil), c.permissions...)
	c.mu.Unlock()
	if len(pending) == 0 {
		c.console.write("(no pending permissions; run /async on to hold requests)\n")
		return
	}
	c.console.write(fmt.Sprintf("%sPending permissions (%d):%s\n", bold, len(pending), reset))
	for i, request := range pending {
		c.console.write(
			fmt.Sprintf("  %s#%d%s %s %s\n", cyan, i+1, reset, request.Action, strings.Join(request.Resources, ", ")),
		)
	}
}

// permissionReplyCommand resolves the pending permission named by field 0, with
// any remaining fields as the rejection message.
func (c *chat) permissionReplyCommand(input, name string, reply agent.PermissionReply) {
	fields := commandFields(input, name)
	if len(fields) == 0 {
		c.console.write(fmt.Sprintf("usage: %s <#n> [message]\n", name))
		return
	}
	request, ok := c.resolvePermission(fields[0])
	if !ok {
		return
	}
	message := strings.Join(fields[1:], " ")
	if err := c.client.Permission().Reply(agent.PermissionReplyInput{
		RequestID: request.ID, Reply: reply, Message: message,
	}); err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	c.console.write(fmt.Sprintf("%spermission %s: %s%s\n", dim, reply, request.Action, reset))
}

// questionsCommand lists the pending question requests.
func (c *chat) questionsCommand() {
	c.mu.Lock()
	pending := append([]agent.QuestionRequest(nil), c.questions...)
	c.mu.Unlock()
	if len(pending) == 0 {
		c.console.write("(no pending questions; run /async on to hold requests)\n")
		return
	}
	c.console.write(fmt.Sprintf("%sPending questions (%d):%s\n", bold, len(pending), reset))
	for i, request := range pending {
		c.console.write(fmt.Sprintf("  %s#%d%s %s\n", cyan, i+1, reset, firstQuestion(request)))
	}
}

// answerCommand answers the pending question named by field 0 with the rest of
// the line as the answer to every question it asked.
func (c *chat) answerCommand(input string) {
	fields := commandFields(input, "/answer")
	if len(fields) < 2 {
		c.console.write("usage: /answer <#n> <text>\n")
		return
	}
	request, ok := c.resolveQuestion(fields[0])
	if !ok {
		return
	}
	answer := strings.Join(fields[1:], " ")
	answers := make([]schema.QuestionAnswer, len(request.Questions))
	for i := range answers {
		answers[i] = schema.QuestionAnswer{answer}
	}
	if err := c.client.Question().Reply(request.ID, answers); err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	c.console.write(fmt.Sprintf("%sanswered question #%s%s\n", dim, fields[0], reset))
}

// rejectCommand dismisses the pending question named by its argument.
func (c *chat) rejectCommand(input string) {
	fields := commandFields(input, "/reject")
	if len(fields) == 0 {
		c.console.write("usage: /reject <#n>\n")
		return
	}
	request, ok := c.resolveQuestion(fields[0])
	if !ok {
		return
	}
	if err := c.client.Question().Reject(request.ID); err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	c.console.write(fmt.Sprintf("%sdismissed question #%s%s\n", dim, fields[0], reset))
}

// resolvePermission maps a "#n" selector to the pending permission request.
func (c *chat) resolvePermission(arg string) (agent.PermissionRequest, bool) {
	c.mu.Lock()
	pending := append([]agent.PermissionRequest(nil), c.permissions...)
	c.mu.Unlock()
	return resolvePending(c, pending, arg, "permission")
}

// resolveQuestion maps a "#n" selector to the pending question request.
func (c *chat) resolveQuestion(arg string) (agent.QuestionRequest, bool) {
	c.mu.Lock()
	pending := append([]agent.QuestionRequest(nil), c.questions...)
	c.mu.Unlock()
	return resolvePending(c, pending, arg, "question")
}

// resolvePending maps a "#n" selector to a cached pending request.
func resolvePending[T any](c *chat, items []T, arg, kind string) (T, bool) {
	index := pendingIndex(arg, len(items))
	if index < 1 {
		var zero T
		c.console.write(fmt.Sprintf("unknown pending %s: %s\n", kind, arg))
		return zero, false
	}
	return items[index-1], true
}

// pendingIndex parses a "#n" selector into a 1-based index within size, or -1.
func pendingIndex(arg string, size int) int {
	index, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(arg), "#"))
	if err != nil || index < 1 || index > size {
		return -1
	}
	return index
}

// firstQuestion is a one-line summary of a request's first question.
func firstQuestion(request agent.QuestionRequest) string {
	if len(request.Questions) == 0 {
		return "(no question)"
	}
	return request.Questions[0].Question
}

// submitDuringTurn routes a line typed during a turn: interaction replies are
// dispatched, anything else steers the running turn.
func (c *chat) submitDuringTurn(text string) {
	if c.handleInteractionCommand(strings.TrimSpace(text)) {
		return
	}
	c.steer(text)
}

// handleInteractionCommand dispatches the out-of-band interaction commands,
// reporting whether the input was one. It is shared by the main dispatcher and
// the turn watcher, so replies work while a held turn is blocked.
func (c *chat) handleInteractionCommand(input string) bool {
	switch {
	case input == "/async" || strings.HasPrefix(input, "/async "):
		c.asyncCommand(input)
	case input == "/permissions":
		c.permissionsCommand()
	case input == "/allow" || strings.HasPrefix(input, "/allow "):
		c.permissionReplyCommand(input, "/allow", agent.PermissionReplyOnce)
	case input == "/always" || strings.HasPrefix(input, "/always "):
		c.permissionReplyCommand(input, "/always", agent.PermissionReplyAlways)
	case input == "/deny" || strings.HasPrefix(input, "/deny "):
		c.permissionReplyCommand(input, "/deny", agent.PermissionReplyReject)
	case input == "/questions":
		c.questionsCommand()
	case input == "/answer" || strings.HasPrefix(input, "/answer "):
		c.answerCommand(input)
	case input == "/reject" || strings.HasPrefix(input, "/reject "):
		c.rejectCommand(input)
	default:
		return false
	}
	return true
}
