package route

import (
	"os"
)

// BearerAuthOptions is the explicit auth and API key BearerAuth resolves, in
// that order, before falling back to envVar.
type BearerAuthOptions struct {
	Auth   *Auth
	APIKey *string
}

// BearerAuth builds the auth for an API-key provider: an explicit Auth wins,
// then the API key, then the first set variable from envVar.
func BearerAuth(options BearerAuthOptions, envVar []string) *Auth {
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
