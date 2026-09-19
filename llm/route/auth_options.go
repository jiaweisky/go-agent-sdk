package route

import (
	"os"
)

type ApiKeyMode string

const (
	ApiKeyModeOptional ApiKeyMode = "optional"
	ApiKeyModeRequired ApiKeyMode = "required"
)

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
