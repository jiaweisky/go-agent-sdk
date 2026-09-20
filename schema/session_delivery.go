package schema

// SessionDelivery mirrors SessionDelivery.Delivery.
type SessionDelivery string

const (
	SessionDeliverySteer SessionDelivery = "steer"
	SessionDeliveryQueue SessionDelivery = "queue"
)
