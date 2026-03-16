package event

import (
	"net/url"

	browserv1 "github.com/alcounit/browser-controller/apis/browser/v1"
	browserconfigv1 "github.com/alcounit/browser-controller/apis/browserconfig/v1"
)

type EventType string

type EventsOption func(v url.Values)

const (
	EventTypeAdded    EventType = "ADDED"
	EventTypeModified EventType = "MODIFIED"
	EventTypeDeleted  EventType = "DELETED"
)

type BrowserEvent struct {
	EventType EventType          `json:"eventType" `
	Browser   *browserv1.Browser `json:"browser" `
}

type BrowserConfigEvent struct {
	EventType     EventType                      `json:"eventType"`
	BrowserConfig *browserconfigv1.BrowserConfig `json:"browserConfig"`
}
