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

	"github.com/user/go-agent-sdk/llm/schema"
)

const bodyLimit = 16384
const maxRetries = 2
const baseDelayMs = 500
const maxDelayMs = 10000
const redacted = "<redacted>"

var sensitiveNameRE = regexp.MustCompile(`(?i)authorization|api[-_]?key|access[-_]?token|refresh[-_]?token|id[-_]?token|token|secret|credential|signature|x-amz-signature`)
var shortQueryNameRE = regexp.MustCompile(`(?i)^(key|sig)$`)
var sensitiveBodyFieldSource = `authorization|api[-_]?key|access[-_]?token|refresh[-_]?token|id[-_]?token|token|secret|credential|signature|x-amz-signature|key`
var sensitiveBodyFieldRE = regexp.MustCompile(`(?i)(?:` + sensitiveBodyFieldSource + `)`)
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
		if sensitiveNameRE.MatchString(k) {
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
	for _, k := range []string{"x-request-id", "request-id", "x-amzn-requestid", "x-amz-request-id", "x-goog-request-id", "cf-ray"} {
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

func rateLimitDetails(headers map[string]string, retryAfter *int) *schema.HttpRateLimitDetails {
	limit := make(map[string]string)
	remaining := make(map[string]string)
	reset := make(map[string]string)

	for name, value := range headers {
		name = strings.ToLower(name)
		if m := openaiLimitRE.FindStringSubmatch(name); m != nil {
			limit[m[1]] = value
			continue
		}
		if m := openaiRemainingRE.FindStringSubmatch(name); m != nil {
			remaining[m[1]] = value
			continue
		}
		if m := openaiResetRE.FindStringSubmatch(name); m != nil {
			reset[m[1]] = value
			continue
		}
		if m := anthropicRE.FindStringSubmatch(name); m != nil {
			switch m[2] {
			case "limit":
				limit[m[1]] = value
			case "remaining":
				remaining[m[1]] = value
			case "reset":
				reset[m[1]] = value
			}
		}
	}
	if retryAfter == nil && len(limit) == 0 && len(remaining) == 0 && len(reset) == 0 {
		return nil
	}
	rl := &schema.HttpRateLimitDetails{RetryAfterMs: retryAfter}
	if len(limit) > 0 {
		rl.Limit = limit
	}
	if len(remaining) > 0 {
		rl.Remaining = remaining
	}
	if len(reset) > 0 {
		rl.Reset = reset
	}
	return rl
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
	redacted := redactBody(body, req)
	if len(redacted) <= bodyLimit {
		return redacted, false
	}
	return redacted[:bodyLimit], true
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
		Reason: classifyStatusReason(status, redactedBody, msg, ra, rl, httpCtx),
	}
}

func classifyStatusReason(status int, body, msg string, retryAfterMs *int, rateLimit *schema.HttpRateLimitDetails, httpCtx *schema.HttpContext) schema.LLMErrorReason {
	if contentPolicyRE.MatchString(body) {
		return schema.NewContentPolicyReason(msg, schema.WithHttpContext(*httpCtx))
	}
	if status == 401 {
		return schema.NewAuthenticationReason(msg, schema.AuthKindInvalid, schema.WithHttpContext(*httpCtx))
	}
	if status == 403 {
		return schema.NewAuthenticationReason(msg, schema.AuthKindInsufficientPermissions, schema.WithHttpContext(*httpCtx))
	}
	if status == 429 {
		if quotaExceededRE.MatchString(body) {
			return schema.NewQuotaExceededReason(msg, schema.WithHttpContext(*httpCtx))
		}
		opts := []schema.ReasonOption{schema.WithHttpContext(*httpCtx)}
		if retryAfterMs != nil {
			opts = append(opts, schema.WithRetryAfterMs(*retryAfterMs))
		}
		return schema.NewRateLimitReason(msg, opts...)
	}
	if status == 400 || status == 404 || status == 409 || status == 413 || status == 422 {
		opts := []schema.ReasonOption{schema.WithHttpContext(*httpCtx)}
		if schema.IsContextOverflow(body) {
			opts = append(opts, schema.WithClassification(schema.ClassificationContextOverflow))
		}
		return schema.NewInvalidRequestReason(msg, opts...)
	}
	if status >= 500 || retryableStatus(status) {
		opts := []schema.ReasonOption{schema.WithHttpContext(*httpCtx)}
		if retryAfterMs != nil {
			opts = append(opts, schema.WithRetryAfterMs(*retryAfterMs))
		}
		return schema.NewProviderInternalReason(msg, status, opts...)
	}
	return schema.NewUnknownProviderReason(msg, schema.WithStatus(status), schema.WithHttpContext(*httpCtx))
}

func requestDetails(req *http.Request) schema.HttpRequestDetails {
	if req == nil {
		return schema.HttpRequestDetails{}
	}
	headers := make(map[string]string, len(req.Header))
	for k, v := range req.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	return schema.HttpRequestDetails{
		Method:  req.Method,
		URL:     redactURL(req.URL.String()),
		Headers: redactHeaders(headers),
	}
}

func httpContextFrom(req *http.Request, status int, body string, truncated bool, headers map[string]string, reqID string, rateLimit *schema.HttpRateLimitDetails) *schema.HttpContext {
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

// transportError mirrors toHttpError: it carries the redacted URL and request
// details alongside the message, and classifies the failure kind.
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

// canceledTransportError reports a request aborted by its context so callers can
// detect an interrupt via errors.Is(err, context.Canceled).
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

type Executor struct {
	client *http.Client
}

func NewExecutor(client *http.Client) *Executor {
	if client == nil {
		client = http.DefaultClient
	}
	return &Executor{client: client}
}

func (e *Executor) Do(req *http.Request) (*http.Response, error) {
	return e.client.Do(req)
}

func (e *Executor) DoWithRetries(req *http.Request) (*http.Response, *schema.LLMError) {
	var reqBody []byte
	if req.Body != nil {
		var err error
		reqBody, err = io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, transportError(req, fmt.Errorf("failed to read request body: %w", err))
		}
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
		if reqBody != nil {
			req.Body = io.NopCloser(bytes.NewReader(reqBody))
		}
		resp, err := e.client.Do(req)
		if err != nil {
			if ctxErr := req.Context().Err(); ctxErr != nil {
				return nil, canceledTransportError(ctxErr)
			}
			lastErr = transportError(req, err)
			// TS TransportReason is not retryable; unlike TS we still retry a
			// failed transport because a transient network error is worth a
			// second attempt. Status errors follow the reason's Retryable().
			continue
		}
		if resp.StatusCode < 400 {
			// Success: hand the response back with its body unread so the
			// caller can stream it (SSE and friends).
			return resp, nil
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		bodyStr := ""
		if readErr == nil {
			bodyStr = string(body)
		}
		headers := make(map[string]string)
		for k, v := range resp.Header {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}
		lastErr = classifyResponse(req, resp.StatusCode, bodyStr, headers)
		if !lastErr.Retryable() {
			return nil, lastErr
		}
	}
	return nil, lastErr
}
