package route

import (
	"os"
)

// ApiKeyMode says whether an API key is required.
type ApiKeyMode string

const (
	ApiKeyModeOptional ApiKeyMode = "optional"
	ApiKeyModeRequired ApiKeyMode = "required"
)

// BearerAuth builds the auth for an API-key provider: an explicit Auth wins,
// then the API key, then the first set variable from envVar.
func BearerAuth(options struct {
	Auth   *Auth
	APIKey *string
}, envVar []string) *Auth {
	if options.Auth != nil {
		return options.Auth
	}
	cred := NewValue("")
	if options.APIKey != nil {
		cred = NewValue(*options.APIKey)
	}
	for _, name := range envVar {
		envVal := os.Getenv(name)
		if envVal != "" {
			cred = cred.OrElse(NewValue(envVal))
		}
	}
	return cred.Bearer()
}
