package route

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jiaweisky/go-agent-sdk/llm/schema"
)

const bodyLimit = 16384
const maxRetries = 2
const baseDelayMs = 500
const maxDelayMs = 10000
const redacted = "<redacted>"

var sensitiveNameRE = regexp.MustCompile(
	`(?i)authorization|api[-_]?key|access[-_]?token|refresh[-_]?token|` +
		`id[-_]?token|token|secret|credential|signature|x-amz-signature`,
)
var shortQueryNameRE = regexp.MustCompile(`(?i)^(key|sig)$`)

var sensitiveBodyFieldSource = `authorization|api[-_]?key|access[-_]?token|refresh[-_]?token|` +
	`id[-_]?token|token|secret|credential|signature|x-amz-signature|key`
var redactJSONFieldRE = regexp.MustCompile(`("(?:` + sensitiveBodyFieldSource + `)"\s*:\s*)"[^"]*"`)
var redactQueryFieldRE = regexp.MustCompile(`((?:` + sensitiveBodyFieldSource + `)=)[^&\s"]+`)

var openaiLimitRE = regexp.MustCompile(`^x-ratelimit-limit-(.+)$`)
var openaiRemainingRE = regexp.MustCompile(`^x-ratelimit-remaining-(.+)$`)
var openaiResetRE = regexp.MustCompile(`^x-ratelimit-reset-(.+)$`)
var anthropicRE = regexp.MustCompile(`^anthropic-ratelimit-(.+)-(limit|remaining|reset)$`)
var contentPolicyRE = regexp.MustCompile(`(?i)content[-_\s]?policy|content_filter|safety`)
var quotaExceededRE = regexp.MustCompile(`(?i)insufficient[-_\s]?quota|quota[-_\s]?exceeded`)

func isSensitiveHeaderName(name string) bool { return sensitiveNameRE.MatchString(name) }
func isSensitiveQueryName(name string) bool {
	return sensitiveNameRE.MatchString(name) || shortQueryNameRE.MatchString(name)
}

func redactHeaders(headers map[string]string) map[string]string {
	result := make(map[string]string, len(headers))
	for k, v := range headers {
		if isSensitiveHeaderName(k) {
			result[k] = redacted
		} else {
			result[k] = v
		}
	}
	return result
}

func redactURL(value string) string {
	u, err := url.Parse(value)
	if err != nil {
		return redacted
	}
	q := u.Query()
	for key := range q {
		if isSensitiveQueryName(key) {
			q.Set(key, redacted)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func requestID(headers map[string]string) string {
	for _, k := range []string{
		"x-request-id", "request-id", "x-amzn-requestid",
		"x-amz-request-id", "x-goog-request-id", "cf-ray",
	} {
		if v, ok := headers[k]; ok {
			return v
		}
	}
	return ""
}

func retryableStatus(status int) bool {
	return status == 429 || status == 503 || status == 504 || status == 529
}

func retryAfterMs(headers map[string]string) *int {
	if v, ok := headers["retry-after-ms"]; ok {
		if ms, err := strconv.Atoi(v); err == nil && ms >= 0 {
			return &ms
		}
	}
	if v, ok := headers["retry-after"]; ok {
		if sec, err := strconv.Atoi(v); err == nil && sec >= 0 {
			ms := sec * 1000
			return &ms
		}
		if t, err := http.ParseTime(v); err == nil {
			ms := int(time.Until(t).Milliseconds())
			if ms < 0 {
				ms = 0
			}
			return &ms
		}
	}
	return nil
}

type rateLimitBuckets struct {
	limit     map[string]string
	remaining map[string]string
	reset     map[string]string
}

func rateLimitDetails(headers map[string]string, retryAfter *int) *schema.HttpRateLimitDetails {
	b := rateLimitBuckets{
		limit:     make(map[string]string),
		remaining: make(map[string]string),
		reset:     make(map[string]string),
	}
	for name, value := range headers {
		collectRateLimitHeader(strings.ToLower(name), value, &b)
	}
	if retryAfter == nil && len(b.limit) == 0 && len(b.remaining) == 0 && len(b.reset) == 0 {
		return nil
	}
	rl := &schema.HttpRateLimitDetails{RetryAfterMs: retryAfter}
	if len(b.limit) > 0 {
		rl.Limit = b.limit
	}
	if len(b.remaining) > 0 {
		rl.Remaining = b.remaining
	}
	if len(b.reset) > 0 {
		rl.Reset = b.reset
	}
	return rl
}

func collectRateLimitHeader(name, value string, b *rateLimitBuckets) {
	if m := openaiLimitRE.FindStringSubmatch(name); m != nil {
		b.limit[m[1]] = value
		return
	}
	if m := openaiRemainingRE.FindStringSubmatch(name); m != nil {
		b.remaining[m[1]] = value
		return
	}
	if m := openaiResetRE.FindStringSubmatch(name); m != nil {
		b.reset[m[1]] = value
		return
	}
	if m := anthropicRE.FindStringSubmatch(name); m != nil {
		switch m[2] {
		case "limit":
			b.limit[m[1]] = value
		case "remaining":
			b.remaining[m[1]] = value
		case "reset":
			b.reset[m[1]] = value
		}
	}
}

func secretValues(req *http.Request) map[string]struct{} {
	vals := make(map[string]struct{})
	add := func(v string) {
		if len(v) >= 4 {
			vals[v] = struct{}{}
			vals[url.QueryEscape(v)] = struct{}{}
		}
	}
	for name, values := range req.Header {
		if isSensitiveHeaderName(name) {
			for _, v := range values {
				add(v)
				if strings.HasPrefix(strings.ToLower(v), "bearer ") {
					add(v[7:])
				}
			}
		}
	}
	q := req.URL.Query()
	for key, qv := range q {
		if isSensitiveQueryName(key) {
			for _, v := range qv {
				add(v)
			}
		}
	}
	return vals
}

func redactBody(body string, req *http.Request) string {
	text := redactJSONFieldRE.ReplaceAllString(body, `$1"`+redacted+`"`)
	text = redactQueryFieldRE.ReplaceAllString(text, `$1`+redacted)
	for secret := range secretValues(req) {
		text = strings.ReplaceAll(text, secret, redacted)
	}
	return text
}

func responseBody(body string, req *http.Request) (string, bool) {
	text := redactBody(body, req)
	if len(text) <= bodyLimit {
		return text, false
	}
	return text[:bodyLimit], true
}

func providerMessage(status int, body string) string {
	if body != "" && len(body) <= 500 {
		return "Provider request failed with HTTP " + strconv.Itoa(status) + ": " + body
	}
	return "Provider request failed with HTTP " + strconv.Itoa(status)
}

func classifyResponse(req *http.Request, status int, body string, headers map[string]string) *schema.LLMError {
	if status < 400 {
		return nil
	}
	norm := make(map[string]string, len(headers))
	for k, v := range headers {
		norm[strings.ToLower(k)] = v
	}
	ra := retryAfterMs(norm)
	rl := rateLimitDetails(norm, ra)

	redactedBody, truncated := responseBody(body, req)
	msg := providerMessage(status, redactedBody)
	httpCtx := httpContextFrom(req, status, redactedBody, truncated, headers, requestID(norm), rl)

	return &schema.LLMError{
		Module: "RequestExecutor",
		Method: "execute",
		Reason: classifyStatusReason(status, redactedBody, msg, ra, httpCtx),
	}
}

func classifyStatusReason(
	status int,
	body, msg string,
	retryAfterMs *int,
	httpCtx *schema.HttpContext,
) schema.LLMErrorReason {
	if contentPolicyRE.MatchString(body) {
		return schema.NewContentPolicyReason(msg, schema.WithHttpContext(*httpCtx))
	}
	switch status {
	case 401:
		return schema.NewAuthenticationReason(msg, schema.AuthKindInvalid, schema.WithHttpContext(*httpCtx))
	case 403:
		return schema.NewAuthenticationReason(msg, schema.AuthKindInsufficientPermissions, schema.WithHttpContext(*httpCtx))
	case 429:
		return classifyRateLimitReason(body, msg, retryAfterMs, httpCtx)
	case 400, 404, 409, 413, 422:
		return classifyInvalidRequestReason(body, msg, httpCtx)
	}
	if status >= 500 || retryableStatus(status) {
		return classifyServerReason(msg, status, retryAfterMs, httpCtx)
	}
	return schema.NewUnknownProviderReason(msg, schema.WithStatus(status), schema.WithHttpContext(*httpCtx))
}

func classifyRateLimitReason(body, msg string, retryAfterMs *int, httpCtx *schema.HttpContext) schema.LLMErrorReason {
	if quotaExceededRE.MatchString(body) {
		return schema.NewQuotaExceededReason(msg, schema.WithHttpContext(*httpCtx))
	}
	opts := []schema.ReasonOption{schema.WithHttpContext(*httpCtx)}
	if retryAfterMs != nil {
		opts = append(opts, schema.WithRetryAfterMs(*retryAfterMs))
	}
	return schema.NewRateLimitReason(msg, opts...)
}

func classifyInvalidRequestReason(body, msg string, httpCtx *schema.HttpContext) schema.LLMErrorReason {
	opts := []schema.ReasonOption{schema.WithHttpContext(*httpCtx)}
	if schema.IsContextOverflow(body) {
		opts = append(opts, schema.WithClassification(schema.ClassificationContextOverflow))
	}
	return schema.NewInvalidRequestReason(msg, opts...)
}

func classifyServerReason(
	msg string,
	status int,
	retryAfterMs *int,
	httpCtx *schema.HttpContext,
) schema.LLMErrorReason {
	opts := []schema.ReasonOption{schema.WithHttpContext(*httpCtx)}
	if retryAfterMs != nil {
		opts = append(opts, schema.WithRetryAfterMs(*retryAfterMs))
	}
	return schema.NewProviderInternalReason(msg, status, opts...)
}

func requestDetails(req *http.Request) schema.HttpRequestDetails {
	if req == nil {
		return schema.HttpRequestDetails{}
	}
	headers := firstHeaderValues(req.Header)
	return schema.HttpRequestDetails{
		Method:  req.Method,
		URL:     redactURL(req.URL.String()),
		Headers: redactHeaders(headers),
	}
}

func httpContextFrom(
	req *http.Request,
	status int,
	body string,
	truncated bool,
	headers map[string]string,
	reqID string,
	rateLimit *schema.HttpRateLimitDetails,
) *schema.HttpContext {
	hc := &schema.HttpContext{
		Request:   requestDetails(req),
		Response:  &schema.HttpResponseDetails{Status: status, Headers: redactHeaders(headers)},
		RateLimit: rateLimit,
	}
	if body != "" {
		hc.Body = strPtr(body)
	}
	if truncated {
		t := true
		hc.BodyTruncated = &t
	}
	if reqID != "" {
		hc.RequestID = strPtr(reqID)
	}
	return hc
}

// transportError builds the error for a failed request: the redacted URL and
// request details, and the classified failure kind.
func transportError(req *http.Request, err error) *schema.LLMError {
	message := "HTTP transport failed"
	kind := "TransportError"
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			kind = "Timeout"
			message = err.Error()
		} else {
			message = "HTTP transport failed: " + err.Error()
		}
	}
	opts := []schema.ReasonOption{schema.WithTransportKind(kind)}
	if req != nil && req.URL != nil {
		opts = append(opts, schema.WithURL(redactURL(req.URL.String())))
		opts = append(opts, schema.WithHttpContext(schema.HttpContext{Request: requestDetails(req)}))
	}
	return &schema.LLMError{
		Module: "RequestExecutor",
		Method: "execute",
		Reason: schema.NewTransportReason(message, opts...),
	}
}

func strPtr(s string) *string { return &s }

func retryDelay(attempt int, retryAfterMs *int) time.Duration {
	if retryAfterMs != nil && *retryAfterMs >= 0 {
		d := *retryAfterMs
		if d > maxDelayMs {
			d = maxDelayMs
		}
		return time.Duration(d) * time.Millisecond
	}
	delay := baseDelayMs * int(math.Pow(2, float64(attempt)))
	jitter := int(float64(delay) * 0.2)
	actual := delay - jitter + rand.Intn(2*jitter+1)
	if actual > maxDelayMs {
		actual = maxDelayMs
	}
	return time.Duration(actual) * time.Millisecond
}

// canceledTransportError builds the error for a request aborted by its context.
func canceledTransportError(err error) *schema.LLMError {
	kind := "TransportError"
	if errors.Is(err, context.DeadlineExceeded) {
		kind = "Timeout"
	}
	return &schema.LLMError{
		Module: "RequestExecutor",
		Method: "execute",
		Reason: schema.NewTransportReason(
			fmt.Sprintf("HTTP request canceled: %v", err),
			schema.WithTransportKind(kind),
		),
	}
}

// sleepContext waits for d, returning false if ctx is cancelled first.
func sleepContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// Executor runs HTTP requests, retrying the retryable failures.
type Executor struct {
	client *http.Client
}

// NewExecutor builds an executor over an HTTP client.
func NewExecutor(client *http.Client) *Executor {
	if client == nil {
		client = http.DefaultClient
	}
	return &Executor{client: client}
}

// DoWithRetries sends a request, retrying while the failure is retryable and the
// attempt budget allows.
func (e *Executor) DoWithRetries(req *http.Request) (*http.Response, *schema.LLMError) {
	reqBody, llmErr := readRequestBody(req)
	if llmErr != nil {
		return nil, llmErr
	}

	var lastErr *schema.LLMError
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := req.Context().Err(); err != nil {
			return nil, canceledTransportError(err)
		}
		if attempt > 0 {
			var ra *int
			if lastErr != nil {
				ra = lastErr.RetryAfterMs()
			}
			if !sleepContext(req.Context(), retryDelay(attempt-1, ra)) {
				return nil, canceledTransportError(req.Context().Err())
			}
		}
		resp, llmErr, retry := e.attempt(req, reqBody)
		if llmErr == nil {
			return resp, nil
		}
		lastErr = llmErr
		if !retry {
			return nil, lastErr
		}
	}
	return nil, lastErr
}

// readRequestBody buffers the request body so every attempt can resend it.
func readRequestBody(req *http.Request) ([]byte, *schema.LLMError) {
	if req.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(req.Body)
	req.Body.Close()
	if err != nil {
		return nil, transportError(req, fmt.Errorf("failed to read request body: %w", err))
	}
	return body, nil
}

// attempt sends one request. A transport failure is always retryable; a status
// failure follows its reason's Retryable().
func (e *Executor) attempt(req *http.Request, reqBody []byte) (*http.Response, *schema.LLMError, bool) {
	if reqBody != nil {
		req.Body = io.NopCloser(bytes.NewReader(reqBody))
	}
	resp, err := e.client.Do(req)
	if err != nil {
		if ctxErr := req.Context().Err(); ctxErr != nil {
			return nil, canceledTransportError(ctxErr), false
		}
		return nil, transportError(req, err), true
	}
	if resp.StatusCode < 400 {
		// Success: hand the response back with its body unread so the caller can
		// stream it (SSE and friends).
		return resp, nil, false
	}
	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	bodyStr := ""
	if readErr == nil {
		bodyStr = string(body)
	}
	llmErr := classifyResponse(req, resp.StatusCode, bodyStr, firstHeaderValues(resp.Header))
	return nil, llmErr, llmErr.Retryable()
}

// firstHeaderValues flattens headers to their first value.
func firstHeaderValues(headers http.Header) map[string]string {
	result := make(map[string]string, len(headers))
	for k, v := range headers {
		if len(v) > 0 {
			result[k] = v[0]
		}
	}
	return result
}
