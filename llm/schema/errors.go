package schema

import "fmt"

type ProviderFailureClassification string

const ClassificationContextOverflow ProviderFailureClassification = "context-overflow"

type HttpRequestDetails struct {
	Method  string
	URL     string
	Headers map[string]string
}

type HttpResponseDetails struct {
	Status  int
	Headers map[string]string
}

type HttpRateLimitDetails struct {
	RetryAfterMs *int
	Limit        map[string]string
	Remaining    map[string]string
	Reset        map[string]string
}

type HttpContext struct {
	Request       HttpRequestDetails
	Response      *HttpResponseDetails
	Body          *string
	BodyTruncated *bool
	RequestID     *string
	RateLimit     *HttpRateLimitDetails
}

type AuthenticationKind string

const (
	AuthKindMissing                 AuthenticationKind = "missing"
	AuthKindInvalid                 AuthenticationKind = "invalid"
	AuthKindExpired                 AuthenticationKind = "expired"
	AuthKindInsufficientPermissions AuthenticationKind = "insufficient-permissions"
	AuthKindUnknown                 AuthenticationKind = "unknown"
)

type LLMErrorReasonKind int

const (
	ReasonInvalidRequest LLMErrorReasonKind = iota
	ReasonNoRoute
	ReasonAuthentication
	ReasonRateLimit
	ReasonQuotaExceeded
	ReasonContentPolicy
	ReasonProviderInternal
	ReasonTransport
	ReasonInvalidProviderOutput
	ReasonUnknownProvider
)

type LLMErrorReason struct {
	Kind    LLMErrorReasonKind
	Message string

	Parameter      *string
	Classification *ProviderFailureClassification

	Route    RouteID
	Provider ProviderID
	Model    ModelID

	AuthKind *AuthenticationKind

	RetryAfterMs *int
	Status       *int

	RateLimit *HttpRateLimitDetails

	URL    *string
	Raw    *string
	Route2 *string

	ProviderMetadata ProviderMetadata
	Http             *HttpContext
}

type ReasonOption func(*LLMErrorReason)

func WithParameter(p string) ReasonOption {
	return func(r *LLMErrorReason) { r.Parameter = &p }
}

func WithClassification(c ProviderFailureClassification) ReasonOption {
	return func(r *LLMErrorReason) { r.Classification = &c }
}

func WithProviderMetadata(m ProviderMetadata) ReasonOption {
	return func(r *LLMErrorReason) { r.ProviderMetadata = m }
}

func WithHttpContext(h HttpContext) ReasonOption {
	return func(r *LLMErrorReason) { r.Http = &h }
}

func WithRetryAfterMs(ms int) ReasonOption {
	return func(r *LLMErrorReason) { r.RetryAfterMs = &ms }
}

func WithRateLimit(rl HttpRateLimitDetails) ReasonOption {
	return func(r *LLMErrorReason) { r.RateLimit = &rl }
}

func WithStatus(s int) ReasonOption {
	return func(r *LLMErrorReason) { r.Status = &s }
}

func WithURL(url string) ReasonOption {
	return func(r *LLMErrorReason) { r.URL = &url }
}

func WithRoute(route string) ReasonOption {
	return func(r *LLMErrorReason) { r.Route2 = &route }
}

func WithRaw(raw string) ReasonOption {
	return func(r *LLMErrorReason) { r.Raw = &raw }
}

func NewInvalidRequestReason(message string, opts ...ReasonOption) LLMErrorReason {
	r := LLMErrorReason{Kind: ReasonInvalidRequest, Message: message}
	for _, opt := range opts {
		opt(&r)
	}
	return r
}

func NewNoRouteReason(route RouteID, provider ProviderID, model ModelID) LLMErrorReason {
	return LLMErrorReason{
		Kind:     ReasonNoRoute,
		Message:  fmt.Sprintf("No LLM route for %s/%s using %s", provider, model, route),
		Route:    route,
		Provider: provider,
		Model:    model,
	}
}

func NewAuthenticationReason(message string, kind AuthenticationKind, opts ...ReasonOption) LLMErrorReason {
	r := LLMErrorReason{Kind: ReasonAuthentication, Message: message, AuthKind: &kind}
	for _, opt := range opts {
		opt(&r)
	}
	return r
}

func NewRateLimitReason(message string, opts ...ReasonOption) LLMErrorReason {
	r := LLMErrorReason{Kind: ReasonRateLimit, Message: message}
	for _, opt := range opts {
		opt(&r)
	}
	return r
}

func NewQuotaExceededReason(message string, opts ...ReasonOption) LLMErrorReason {
	r := LLMErrorReason{Kind: ReasonQuotaExceeded, Message: message}
	for _, opt := range opts {
		opt(&r)
	}
	return r
}

func NewContentPolicyReason(message string, opts ...ReasonOption) LLMErrorReason {
	r := LLMErrorReason{Kind: ReasonContentPolicy, Message: message}
	for _, opt := range opts {
		opt(&r)
	}
	return r
}

func NewProviderInternalReason(message string, status int, opts ...ReasonOption) LLMErrorReason {
	r := LLMErrorReason{Kind: ReasonProviderInternal, Message: message, Status: &status}
	for _, opt := range opts {
		opt(&r)
	}
	return r
}

func NewTransportReason(message string, opts ...ReasonOption) LLMErrorReason {
	r := LLMErrorReason{Kind: ReasonTransport, Message: message}
	for _, opt := range opts {
		opt(&r)
	}
	return r
}

func NewInvalidProviderOutputReason(message string, opts ...ReasonOption) LLMErrorReason {
	r := LLMErrorReason{Kind: ReasonInvalidProviderOutput, Message: message}
	for _, opt := range opts {
		opt(&r)
	}
	return r
}

func NewUnknownProviderReason(message string, opts ...ReasonOption) LLMErrorReason {
	r := LLMErrorReason{Kind: ReasonUnknownProvider, Message: message}
	for _, opt := range opts {
		opt(&r)
	}
	return r
}

func (r *LLMErrorReason) Retryable() bool {
	switch r.Kind {
	case ReasonRateLimit, ReasonProviderInternal:
		return true
	default:
		return false
	}
}

func (r *LLMErrorReason) RetryAfterMsValue() *int {
	switch r.Kind {
	case ReasonRateLimit, ReasonProviderInternal:
		return r.RetryAfterMs
	default:
		return nil
	}
}

func (r *LLMErrorReason) Error() string {
	return r.Message
}

type LLMError struct {
	Module string
	Method string
	Reason LLMErrorReason
}

func (e *LLMError) Error() string {
	return fmt.Sprintf("%s.%s: %s", e.Module, e.Method, e.Reason.Message)
}

func (e *LLMError) Unwrap() error {
	return &e.Reason
}

func (e *LLMError) Retryable() bool {
	return e.Reason.Retryable()
}

func (e *LLMError) RetryAfterMs() *int {
	return e.Reason.RetryAfterMsValue()
}

type ToolFailure struct {
	Message  string
	Cause    error
	Metadata map[string]any
}

func (e *ToolFailure) Error() string {
	return fmt.Sprintf("tool failure: %s", e.Message)
}

func (e *ToolFailure) Unwrap() error {
	return e.Cause
}
