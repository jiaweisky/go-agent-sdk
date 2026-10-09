package schema

// SessionDelivery says when an admitted prompt runs: queued after the current
// turn, or steering into it.
type SessionDelivery string

const (
	SessionDeliverySteer SessionDelivery = "steer"
	SessionDeliveryQueue SessionDelivery = "queue"
)
