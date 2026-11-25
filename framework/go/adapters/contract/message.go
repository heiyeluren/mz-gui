package contract

import "context"

type NameValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Message struct {
	Meta        RequestMeta `json:"meta"`
	Topic       string      `json:"topic"`
	PayloadRef  ArtifactRef `json:"payload_ref"`
	ContentType string      `json:"content_type"`
}

type PublishAck struct {
	MessageID modelID `json:"message_id"`
}

type SubscriptionRequest struct {
	Meta   RequestMeta `json:"meta"`
	Topic  string      `json:"topic"`
	Cursor string      `json:"cursor,omitempty"`
}

type DeliveredMessage struct {
	DeliveryID DeliveryID `json:"delivery_id"`
	Sequence   uint64     `json:"sequence"`
	Message    Message    `json:"message"`
	Terminal   bool       `json:"terminal"`
}

type MessageStream interface {
	Next(context.Context) (DeliveredMessage, error)
	Close(context.Context) error
}

type NackRequest struct {
	Meta       RequestMeta `json:"meta"`
	DeliveryID DeliveryID  `json:"delivery_id"`
	RetryAfter int64       `json:"retry_after_ms,omitempty"`
}
