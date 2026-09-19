package schema

type SessionID string

func (id SessionID) String() string {
	return string(id)
}

func NewSessionID(id ...string) SessionID {
	if len(id) == 0 || id[0] == "" {
		return SessionID("ses_" + Descending())
	}
	return SessionID(id[0])
}
