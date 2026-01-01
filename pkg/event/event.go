package event

import browserv1 "github.com/alcounit/browser-controller/apis/browser/v1"

type EventType string

const (
	EventTypeAdded    EventType = "ADDED"
	EventTypeModified EventType = "MODIFIED"
	EventTypeDeleted  EventType = "DELETED"
)

type BrowserEvent struct {
	EventType EventType          `json:"eventType" `
	Browser   *browserv1.Browser `json:"browser" `
}
