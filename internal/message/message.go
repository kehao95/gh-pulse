package message

import "encoding/json"

// EventMessage is the JSONL envelope for GitHub webhook events.
type EventMessage struct {
	Type       string          `json:"type"`
	Event      string          `json:"event"`
	DeliveryID string          `json:"delivery_id"`
	Payload    json.RawMessage `json:"payload"`
}
