package route

import (
	"fmt"

	"github.com/jiaweisky/go-agent-sdk/llm/schema"
)

// AuthInput is what an auth scheme sees: the request, its URL and body, and the
// headers accumulated so far.
type AuthInput struct {
	Request *schema.LLMRequest
	URL     string
	Body    string
	Headers map[string]string
}

// Credential resolves a secret on demand, so a key is read only when a request
// needs it.
type Credential struct {
	load func() (string, error)
}

// NewCredential builds a credential from a loader.
func NewCredential(load func() (string, error)) *Credential {
	return &Credential{load: load}
}

// NewValue builds a credential holding an already known secret. An empty secret
// fails when it is read.
func NewValue(secret string) *Credential {
	return &Credential{
		load: func() (string, error) {
			if secret == "" {
				return "", fmt.Errorf("Missing auth credential: value")
			}
			return secret, nil
		},
	}
}

// OrElse falls back to that credential when c cannot produce a secret.
func (c *Credential) OrElse(that *Credential) *Credential {
	return &Credential{
		load: func() (string, error) {
			s, err := c.load()
			if err != nil {
				return that.load()
			}
			return s, nil
		},
	}
}

// Bearer applies the credential as an "Authorization: Bearer" header.
func (c *Credential) Bearer() *Auth {
	return Custom(func(input AuthInput) (map[string]string, error) {
		secret, err := c.load()
		if err != nil {
			return nil, err
		}
		return setHeader(input.Headers, "authorization", "Bearer "+secret), nil
	})
}

// Header applies the credential as a header with the given name.
func (c *Credential) Header(name string) *Auth {
	return Custom(func(input AuthInput) (map[string]string, error) {
		secret, err := c.load()
		if err != nil {
			return nil, err
		}
		return setHeader(input.Headers, name, secret), nil
	})
}

// BearerHeader applies the credential as "Bearer <secret>" in a named header.
func (c *Credential) BearerHeader(name string) *Auth {
	return Custom(func(input AuthInput) (map[string]string, error) {
		secret, err := c.load()
		if err != nil {
			return nil, err
		}
		return setHeader(input.Headers, name, "Bearer "+secret), nil
	})
}

func setHeader(headers map[string]string, name, value string) map[string]string {
	if headers == nil {
		headers = make(map[string]string)
	}
	headers[name] = value
	return headers
}

// Auth applies a credential convention to a request's headers.
type Auth struct {
	apply func(input AuthInput) (map[string]string, error)
}

// Apply returns the request headers with the scheme applied. A nil Auth leaves
// them unchanged.
func (a *Auth) Apply(input AuthInput) (map[string]string, error) {
	if a == nil || a.apply == nil {
		return input.Headers, nil
	}
	return a.apply(input)
}

// AndThen applies a, then that, on its headers.
func (a *Auth) AndThen(that *Auth) *Auth {
	return Custom(func(input AuthInput) (map[string]string, error) {
		headers, err := a.Apply(input)
		if err != nil {
			return nil, err
		}
		next := input
		next.Headers = headers
		return that.Apply(next)
	})
}

// OrElse applies a, falling back to that when a fails.
func (a *Auth) OrElse(that *Auth) *Auth {
	return Custom(func(input AuthInput) (map[string]string, error) {
		headers, err := a.Apply(input)
		if err != nil {
			return that.Apply(input)
		}
		return headers, nil
	})
}

// Custom builds an auth scheme from a header transform.
func Custom(apply func(input AuthInput) (map[string]string, error)) *Auth {
	return &Auth{apply: apply}
}

// None sends no credential.
func None() *Auth {
	return Custom(func(input AuthInput) (map[string]string, error) {
		return input.Headers, nil
	})
}

// RemoveHeader deletes a header from the request.
func RemoveHeader(name string) *Auth {
	return Custom(func(input AuthInput) (map[string]string, error) {
		delete(input.Headers, name)
		return input.Headers, nil
	})
}

// Bearer applies a secret or a credential as a bearer token.
func Bearer(source any) *Auth {
	cred, ok := credentialFrom(source)
	if !ok {
		return missingCredential()
	}
	return cred.Bearer()
}

// Header applies a secret or a credential as a named header.
func Header(name string, source any) *Auth {
	cred, ok := credentialFrom(source)
	if !ok {
		return missingCredential()
	}
	return cred.Header(name)
}

// BearerHeader applies a secret or a credential as "Bearer" in a named header.
func BearerHeader(name string, source any) *Auth {
	cred, ok := credentialFrom(source)
	if !ok {
		return missingCredential()
	}
	return cred.BearerHeader(name)
}

// credentialFrom normalizes a secret or a credential into a Credential.
func credentialFrom(source any) (*Credential, bool) {
	switch s := source.(type) {
	case string:
		return NewValue(s), true
	case *Credential:
		return s, true
	default:
		return nil, false
	}
}

func missingCredential() *Auth {
	return Custom(func(AuthInput) (map[string]string, error) {
		return nil, fmt.Errorf("Missing auth credential: value")
	})
}
