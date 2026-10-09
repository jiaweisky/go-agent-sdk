package schema

// MediaDecision reports one media decision for host metrics/observability: an
// omitted attachment ("omit", with Reason) or an adapted upload ("upload"). The
// common encode path is not reported.
type MediaDecision struct {
	SessionID string
	MessageID string
	Name      string
	MIME      string
	Action    string // "omit" | "upload"
	Reason    string // set for omit
}
