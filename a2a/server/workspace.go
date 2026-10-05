package server

import (
	"context"

	"github.com/jiaweisky/go-agent-sdk/schema"
)

// WorkspaceRequest identifies the inbound call a workspace is resolved for.
type WorkspaceRequest struct {
	// User is the authenticated caller's name (empty when unauthenticated).
	User string
	// AgentID is the exposed agent the call targets.
	AgentID string
	// ContextID is the A2A context, which groups an inbound conversation.
	ContextID string
}

// Workspace is the execution context an inbound call runs in.
type Workspace struct {
	// Directory is the project/worktree root the session runs in.
	Directory string
	// Root is the SDK state root for the workspace.
	Root string
	// Permissions is the authorization ruleset the session runs under. When
	// empty the factory's configured ruleset applies.
	Permissions schema.PermissionRuleset
}

// WorkspaceResolver maps an inbound call to the workspace it executes in. It is
// the multi-tenant seam: the host decides, from the caller identity and
// context, which project/root and ruleset a session gets. When no resolver is
// configured every call uses the factory's defaults.
type WorkspaceResolver interface {
	Resolve(ctx context.Context, req WorkspaceRequest) (Workspace, error)
}

// WorkspaceResolverFunc adapts a function to a WorkspaceResolver.
type WorkspaceResolverFunc func(ctx context.Context, req WorkspaceRequest) (Workspace, error)

// Resolve implements WorkspaceResolver.
func (f WorkspaceResolverFunc) Resolve(ctx context.Context, req WorkspaceRequest) (Workspace, error) {
	return f(ctx, req)
}
