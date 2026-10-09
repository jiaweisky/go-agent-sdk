// Package session declares the public session-store contracts the SDK exposes,
// so hosts and the A2A server can hold a turn store without depending on the
// internal persistence implementation.
package session

import "github.com/jiaweisky/go-agent-sdk/schema"

// TurnStore persists core turn records. A turn is one terminal-bound unit of
// work in a session: the local subagent's delegated run or an A2A task.
type TurnStore interface {
	Create(info schema.TurnInfo) error
	Update(info schema.TurnInfo) error
	Get(id schema.TurnID) (*schema.TurnInfo, error)
	ActiveForSession(sessionID schema.SessionID) (*schema.TurnInfo, error)
	ListForSession(sessionID schema.SessionID) ([]schema.TurnInfo, error)
	ListAll() ([]schema.TurnInfo, error)
	FindByMetadata(key, value string) ([]schema.TurnInfo, error)
	LatestSeq(sessionID schema.SessionID) (int64, error)
}
