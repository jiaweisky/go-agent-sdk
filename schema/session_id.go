package schema

type SessionID string

func (id SessionID) String() string {
	return string(id)
}

func NewSessionID() SessionID {
	return SessionID(generateID("ses", true))
}

// ParseSessionID validates an existing session id.
func ParseSessionID(id string) (SessionID, error) {
	if err := validateID("ses", id); err != nil {
		return "", err
	}
	return SessionID(id), nil
}
