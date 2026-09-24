package schema

import "fmt"

// ProviderFailureClassification labels a provider failure the caller may handle
// specially (a context overflow, for example).
type ProviderFailureClassification string

const ClassificationContextOverflow ProviderFailureClassification = "context-overflow"

// HttpRequestDetails is the redacted request a failed call sent.
type HttpRequestDetails struct {
	Method  string
	URL     string
	Headers map[string]string
}

// HttpResponseDetails is the status and headers a failed call received.
type HttpResponseDetails struct {
	Status  int
	Headers map[string]string
}

// HttpRateLimitDetails is a rate-limited response's headers.
type HttpRateLimitDetails struct {
	RetryAfterMs *int
	Limit        map[string]string
	Remaining    map[string]string
	Reset        map[string]string
}

// HttpContext is what the transport saw when a call failed.
type HttpContext struct {
	Request       HttpRequestDetails
	Response      *HttpResponseDetails
	Body          *string
	BodyTruncated *bool
	RequestID     *string
	RateLimit     *HttpRateLimitDetails
}

// AuthenticationKind says why authentication failed.
type AuthenticationKind string

const (
	AuthKindMissing                 AuthenticationKind = "missing"
	AuthKindInvalid                 AuthenticationKind = "invalid"
	AuthKindExpired                 AuthenticationKind = "expired"
	AuthKindInsufficientPermissions AuthenticationKind = "insufficient-permissions"
	AuthKindUnknown                 AuthenticationKind = "unknown"
)

// LLMErrorReasonKind classifies an error reason.
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

// LLMErrorReason is a classified failure, with the details each kind carries.
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

	URL *string
	Raw *string

	// TransportKind names the transport failure ("Timeout", "TransportError").
	TransportKind *string

	ProviderMetadata ProviderMetadata
	Http             *HttpContext
}

// ReasonOption sets an optional field of a reason.
type ReasonOption func(*LLMErrorReason)

// WithParameter records the request parameter the failure relates to.
func WithParameter(p string) ReasonOption {
	return func(r *LLMErrorReason) { r.Parameter = &p }
}

// WithClassification records a provider failure classification.
func WithClassification(c ProviderFailureClassification) ReasonOption {
	return func(r *LLMErrorReason) { r.Classification = &c }
}

// WithProviderMetadata records the provider metadata of the failed call.
func WithProviderMetadata(m ProviderMetadata) ReasonOption {
	return func(r *LLMErrorReason) { r.ProviderMetadata = m }
}

// WithHttpContext records what the transport saw.
func WithHttpContext(h HttpContext) ReasonOption {
	return func(r *LLMErrorReason) { r.Http = &h }
}

// WithRetryAfterMs records how long to wait before retrying.
func WithRetryAfterMs(ms int) ReasonOption {
	return func(r *LLMErrorReason) { r.RetryAfterMs = &ms }
}

// WithRateLimit records the rate-limit headers of the response.
func WithRateLimit(rl HttpRateLimitDetails) ReasonOption {
	return func(r *LLMErrorReason) { r.RateLimit = &rl }
}

// WithStatus records the HTTP status of the failed call.
func WithStatus(s int) ReasonOption {
	return func(r *LLMErrorReason) { r.Status = &s }
}

// WithURL records the redacted URL of the failed call.
func WithURL(url string) ReasonOption {
	return func(r *LLMErrorReason) { r.URL = &url }
}

// WithRoute sets the route a reason refers to (NoRouteReason.route and
// InvalidProviderOutputReason.route).
func WithRoute(route string) ReasonOption {
	return func(r *LLMErrorReason) { r.Route = RouteID(route) }
}

// WithTransportKind mirrors TransportReason.kind.
func WithTransportKind(kind string) ReasonOption {
	return func(r *LLMErrorReason) { r.TransportKind = &kind }
}

// WithRaw records the provider's raw error payload.
func WithRaw(raw string) ReasonOption {
	return func(r *LLMErrorReason) { r.Raw = &raw }
}

// applyReasons applies the reason options to a base reason.
func applyReasons(r LLMErrorReason, opts []ReasonOption) LLMErrorReason {
	for _, opt := range opts {
		opt(&r)
	}
	return r
}

// NewInvalidRequestReason reports a request the provider rejected.
func NewInvalidRequestReason(message string, opts ...ReasonOption) LLMErrorReason {
	return applyReasons(LLMErrorReason{Kind: ReasonInvalidRequest, Message: message}, opts)
}

// NewNoRouteReason reports that no route matches the provider, model and route id.
func NewNoRouteReason(route RouteID, provider ProviderID, model ModelID) LLMErrorReason {
	return LLMErrorReason{
		Kind:     ReasonNoRoute,
		Message:  fmt.Sprintf("No LLM route for %s/%s using %s", provider, model, route),
		Route:    route,
		Provider: provider,
		Model:    model,
	}
}

// NewAuthenticationReason reports a credential the provider refused.
func NewAuthenticationReason(message string, kind AuthenticationKind, opts ...ReasonOption) LLMErrorReason {
	return applyReasons(LLMErrorReason{Kind: ReasonAuthentication, Message: message, AuthKind: &kind}, opts)
}

// NewRateLimitReason reports a rate limit. It is retryable.
func NewRateLimitReason(message string, opts ...ReasonOption) LLMErrorReason {
	return applyReasons(LLMErrorReason{Kind: ReasonRateLimit, Message: message}, opts)
}

// NewQuotaExceededReason reports an exhausted quota.
func NewQuotaExceededReason(message string, opts ...ReasonOption) LLMErrorReason {
	return applyReasons(LLMErrorReason{Kind: ReasonQuotaExceeded, Message: message}, opts)
}

// NewContentPolicyReason reports content the provider refused to process.
func NewContentPolicyReason(message string, opts ...ReasonOption) LLMErrorReason {
	return applyReasons(LLMErrorReason{Kind: ReasonContentPolicy, Message: message}, opts)
}

// NewProviderInternalReason reports a provider-side failure. It is retryable.
func NewProviderInternalReason(message string, status int, opts ...ReasonOption) LLMErrorReason {
	return applyReasons(LLMErrorReason{Kind: ReasonProviderInternal, Message: message, Status: &status}, opts)
}

// NewTransportReason reports a failure below HTTP: DNS, connect, TLS, a timeout.
func NewTransportReason(message string, opts ...ReasonOption) LLMErrorReason {
	return applyReasons(LLMErrorReason{Kind: ReasonTransport, Message: message}, opts)
}

// NewInvalidProviderOutputReason reports a response that did not match the
// protocol.
func NewInvalidProviderOutputReason(message string, opts ...ReasonOption) LLMErrorReason {
	return applyReasons(LLMErrorReason{Kind: ReasonInvalidProviderOutput, Message: message}, opts)
}

// NewUnknownProviderReason reports a provider the SDK does not know.
func NewUnknownProviderReason(message string, opts ...ReasonOption) LLMErrorReason {
	return applyReasons(LLMErrorReason{Kind: ReasonUnknownProvider, Message: message}, opts)
}

// Retryable reports whether the reason is worth retrying.
func (r *LLMErrorReason) Retryable() bool {
	switch r.Kind {
	case ReasonRateLimit, ReasonProviderInternal:
		return true
	default:
		return false
	}
}

// RetryAfterMsValue returns how long to wait before retrying, or nil.
func (r *LLMErrorReason) RetryAfterMsValue() *int {
	switch r.Kind {
	case ReasonRateLimit, ReasonProviderInternal:
		return r.RetryAfterMs
	default:
		return nil
	}
}

// Error implements error.
func (r *LLMErrorReason) Error() string {
	return r.Message
}

// LLMError wraps a reason with the module and method that produced it.
type LLMError struct {
	Module string
	Method string
	Reason LLMErrorReason
}

// Error renders "module.method: message".
func (e *LLMError) Error() string {
	return fmt.Sprintf("%s.%s: %s", e.Module, e.Method, e.Reason.Message)
}

// Unwrap returns the reason, so errors.As reaches it.
func (e *LLMError) Unwrap() error {
	return &e.Reason
}

// Retryable reports whether the reason is worth retrying.
func (e *LLMError) Retryable() bool {
	return e.Reason.Retryable()
}

// RetryAfterMs returns how long to wait before retrying, or nil.
func (e *LLMError) RetryAfterMs() *int {
	return e.Reason.RetryAfterMsValue()
}

// ToolFailure is an error a tool returned to the model, with optional metadata.
type ToolFailure struct {
	Message  string
	Cause    error
	Metadata map[string]any
}

// Error implements error.
func (e *ToolFailure) Error() string {
	return fmt.Sprintf("tool failure: %s", e.Message)
}

// Unwrap returns the underlying cause, or nil.
func (e *ToolFailure) Unwrap() error {
	return e.Cause
}
