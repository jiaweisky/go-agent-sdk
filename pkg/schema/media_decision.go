package schema

// MediaDecision reports one media decision for host metrics/observability: an
// omitted attachment ("omit", with Reason), or an adaptation ("upload",
// "downsample" or "spool"). The common encode path is not reported.
type MediaDecision struct {
	SessionID string
	MessageID string
	Name      string
	MIME      string
	Action    string // "omit" | "upload" | "downsample" | "spool"
	Reason    string // set for omit
	// Detail is a human hint for the reason (e.g. the accepted size limit), so a
	// host can tell the caller how to fix it.
	Detail string
}
