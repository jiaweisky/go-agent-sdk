package route

import (
	"fmt"

	"github.com/user/go-agent-sdk/llm/schema"
)

type AuthInput struct {
	Request *schema.LLMRequest
	Method  string
	URL     string
	Body    string
	Headers map[string]string
}

type Credential struct {
	load func() (string, error)
}

func NewCredential(load func() (string, error)) *Credential {
	return &Credential{load: load}
}

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

func (c *Credential) Bearer() *Auth {
	return Custom(func(input AuthInput) (map[string]string, error) {
		secret, err := c.load()
		if err != nil {
			return nil, err
		}
		if input.Headers == nil {
			input.Headers = make(map[string]string)
		}
		input.Headers["authorization"] = "Bearer " + secret
		return input.Headers, nil
	})
}

func (c *Credential) Header(name string) *Auth {
	return Custom(func(input AuthInput) (map[string]string, error) {
		secret, err := c.load()
		if err != nil {
			return nil, err
		}
		if input.Headers == nil {
			input.Headers = make(map[string]string)
		}
		input.Headers[name] = secret
		return input.Headers, nil
	})
}

type Auth struct {
	apply func(input AuthInput) (map[string]string, error)
}

func (a *Auth) Apply(input AuthInput) (map[string]string, error) {
	if a == nil || a.apply == nil {
		return input.Headers, nil
	}
	return a.apply(input)
}

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

func (a *Auth) OrElse(that *Auth) *Auth {
	return Custom(func(input AuthInput) (map[string]string, error) {
		headers, err := a.Apply(input)
		if err != nil {
			return that.Apply(input)
		}
		return headers, nil
	})
}

func Custom(apply func(input AuthInput) (map[string]string, error)) *Auth {
	return &Auth{apply: apply}
}

func None() *Auth {
	return Custom(func(input AuthInput) (map[string]string, error) {
		return input.Headers, nil
	})
}

func SetHeaders(headers map[string]string) *Auth {
	return Custom(func(input AuthInput) (map[string]string, error) {
		if input.Headers == nil {
			input.Headers = make(map[string]string)
		}
		for k, v := range headers {
			input.Headers[k] = v
		}
		return input.Headers, nil
	})
}

func RemoveHeader(name string) *Auth {
	return Custom(func(input AuthInput) (map[string]string, error) {
		delete(input.Headers, name)
		return input.Headers, nil
	})
}

func Bearer(source any) *Auth {
	switch s := source.(type) {
	case string:
		return NewValue(s).Bearer()
	case *Credential:
		return s.Bearer()
	default:
		return Custom(func(input AuthInput) (map[string]string, error) {
			return nil, fmt.Errorf("Missing auth credential: value")
		})
	}
}

func APIKey(source any) *Auth {
	return Bearer(source)
}

func Header(name string, source any) *Auth {
	switch s := source.(type) {
	case string:
		return NewValue(s).Header(name)
	case *Credential:
		return s.Header(name)
	default:
		return Custom(func(input AuthInput) (map[string]string, error) {
			return nil, fmt.Errorf("Missing auth credential: value")
		})
	}
}

func BearerHeader(name string, source any) *Auth {
	inner := func(s any) *Auth {
		switch v := s.(type) {
		case string:
			return Custom(func(input AuthInput) (map[string]string, error) {
				if input.Headers == nil {
					input.Headers = make(map[string]string)
				}
				input.Headers[name] = "Bearer " + v
				return input.Headers, nil
			})
		case *Credential:
			return Custom(func(input AuthInput) (map[string]string, error) {
				secret, err := v.load()
				if err != nil {
					return nil, err
				}
				if input.Headers == nil {
					input.Headers = make(map[string]string)
				}
				input.Headers[name] = "Bearer " + secret
				return input.Headers, nil
			})
		default:
			return Custom(func(input AuthInput) (map[string]string, error) {
				return nil, fmt.Errorf("Missing auth credential: value")
			})
		}
	}
	return inner(source)
}

func ToLLMError(err error) *schema.LLMError {
	if e, ok := err.(*schema.LLMError); ok {
		return e
	}
	return &schema.LLMError{
		Module: "Auth",
		Method: "apply",
		Reason: schema.NewAuthenticationReason(err.Error(), schema.AuthKindMissing),
	}
}
