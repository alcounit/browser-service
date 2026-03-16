package browserconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	browserconfigv1 "github.com/alcounit/browser-controller/apis/browserconfig/v1"
	"github.com/alcounit/browser-service/pkg/client"
	"github.com/alcounit/browser-service/pkg/event"
	"github.com/rs/zerolog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewClientValidation(t *testing.T) {
	_, err := NewClient(client.ClientConfig{})
	if err == nil {
		t.Fatal("expected error for missing base URL")
	}

	_, err = NewClient(client.ClientConfig{BaseURL: "http://[::1"})
	if err == nil {
		t.Fatal("expected error for invalid base URL")
	}

	c, err := NewClient(client.ClientConfig{BaseURL: "http://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected client instance")
	}
}

func TestCreateSuccess(t *testing.T) {
	var gotMethod, gotPath string

	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		gotMethod = req.Method
		gotPath = req.URL.Path
		body, _ := json.Marshal(&browserconfigv1.BrowserConfig{ObjectMeta: metav1.ObjectMeta{Name: "cfg"}})
		return response(http.StatusOK, string(body)), nil
	})

	result, err := c.Create(context.Background(), "default", newTestConfig("cfg"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, got %s", gotMethod)
	}
	if gotPath != "/api/v1/namespaces/default/browserconfigs" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	_ = result
}

func TestCreateValidation(t *testing.T) {
	c, _ := NewClient(client.ClientConfig{BaseURL: "http://example.com"})
	if _, err := c.Create(context.Background(), "", &browserconfigv1.BrowserConfig{}); err == nil {
		t.Fatal("expected error for missing namespace")
	}
	if _, err := c.Create(context.Background(), "default", nil); err == nil {
		t.Fatal("expected error for nil config")
	}
}

func TestCreateRequestError(t *testing.T) {
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("request failed")
	})

	_, err := c.Create(context.Background(), "default", newTestConfig("cfg"))
	if err == nil || !strings.Contains(err.Error(), "failed to create browser config") {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func TestGetSuccess(t *testing.T) {
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", req.Method)
		}
		if req.URL.Path != "/api/v1/namespaces/default/browserconfigs/cfg" {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		body, _ := json.Marshal(&browserconfigv1.BrowserConfig{ObjectMeta: metav1.ObjectMeta{Name: "cfg"}})
		return response(http.StatusOK, string(body)), nil
	})

	result, err := c.Get(context.Background(), "default", "cfg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Name != "cfg" {
		t.Fatalf("unexpected name: %s", result.Name)
	}
}

func TestGetValidation(t *testing.T) {
	c, _ := NewClient(client.ClientConfig{BaseURL: "http://example.com"})
	if _, err := c.Get(context.Background(), "", "cfg"); err == nil {
		t.Fatal("expected error for missing namespace")
	}
	if _, err := c.Get(context.Background(), "default", ""); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestGetRequestError(t *testing.T) {
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("request failed")
	})

	_, err := c.Get(context.Background(), "default", "cfg")
	if err == nil || !strings.Contains(err.Error(), "failed to get browser config") {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func TestDeleteSuccess(t *testing.T) {
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodDelete {
			t.Fatalf("expected DELETE, got %s", req.Method)
		}
		return response(http.StatusNoContent, ""), nil
	})

	if err := c.Delete(context.Background(), "default", "cfg"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteValidation(t *testing.T) {
	c, _ := NewClient(client.ClientConfig{BaseURL: "http://example.com"})
	if err := c.Delete(context.Background(), "", "cfg"); err == nil {
		t.Fatal("expected error for missing namespace")
	}
	if err := c.Delete(context.Background(), "default", ""); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestDeleteRequestError(t *testing.T) {
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("request failed")
	})

	err := c.Delete(context.Background(), "default", "cfg")
	if err == nil || !strings.Contains(err.Error(), "failed to delete browser config") {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func TestListSuccess(t *testing.T) {
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		body, _ := json.Marshal([]*browserconfigv1.BrowserConfig{
			{ObjectMeta: metav1.ObjectMeta{Name: "one"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "two"}},
		})
		return response(http.StatusOK, string(body)), nil
	})

	configs, err := c.List(context.Background(), "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(configs))
	}
}

func TestListValidation(t *testing.T) {
	c, _ := NewClient(client.ClientConfig{BaseURL: "http://example.com"})
	if _, err := c.List(context.Background(), ""); err == nil {
		t.Fatal("expected error for missing namespace")
	}
}

func TestListRequestError(t *testing.T) {
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("request failed")
	})

	_, err := c.List(context.Background(), "default")
	if err == nil || !strings.Contains(err.Error(), "failed to list browser configs") {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func TestEventsSuccess(t *testing.T) {
	evts := []event.BrowserConfigEvent{
		{EventType: event.EventTypeAdded, BrowserConfig: &browserconfigv1.BrowserConfig{ObjectMeta: metav1.ObjectMeta{Name: "one"}}},
	}

	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		var buf bytes.Buffer
		for _, evt := range evts {
			data, _ := json.Marshal(evt)
			fmt.Fprintf(&buf, "data: %s\n\n", data)
		}
		resp := response(http.StatusOK, buf.String())
		resp.Header.Set("Content-Type", "text/event-stream")
		return resp, nil
	})

	stream, err := c.Events(context.Background(), "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case evt := <-stream.Events():
		if evt.BrowserConfig.Name != "one" || evt.EventType != event.EventTypeAdded {
			t.Fatalf("unexpected event: %+v", evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}

	stream.Close()
}

func TestEventsWithNameOption(t *testing.T) {
	var gotQuery string

	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		gotQuery = req.URL.RawQuery
		body, _ := json.Marshal(event.BrowserConfigEvent{
			EventType:     event.EventTypeAdded,
			BrowserConfig: &browserconfigv1.BrowserConfig{ObjectMeta: metav1.ObjectMeta{Name: "cfg"}},
		})
		resp := response(http.StatusOK, fmt.Sprintf("data: %s\n\n", body))
		resp.Header.Set("Content-Type", "text/event-stream")
		return resp, nil
	})

	stream, err := c.Events(context.Background(), "default", WithName("cfg"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-stream.Events():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}

	if !strings.Contains(gotQuery, "name=cfg") {
		t.Fatalf("expected name filter in query, got %q", gotQuery)
	}

	stream.Close()
}

func TestEventsMissingNamespace(t *testing.T) {
	c, _ := NewClient(client.ClientConfig{BaseURL: "http://example.com"})
	if _, err := c.Events(context.Background(), ""); err == nil {
		t.Fatal("expected error for missing namespace")
	}
}

func TestEventsRequestError(t *testing.T) {
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("request failed")
	})

	stream, err := c.Events(context.Background(), "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case err := <-stream.Errors():
		if err == nil {
			t.Fatal("expected error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for error")
	}
}

func TestEventsRequestCreateError(t *testing.T) {
	c := &restClient{
		RestClient: &client.RestClient{Config: client.ClientConfig{
			BaseURL:    "http://example.com\x7f",
			HTTPClient: &http.Client{},
			Logger:     zerolog.Nop(),
		}},
	}

	stream, err := c.Events(context.Background(), "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case err := <-stream.Errors():
		if err == nil {
			t.Fatal("expected error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for error")
	}
}

func TestEventsHTTPError(t *testing.T) {
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusInternalServerError, "bad"), nil
	})

	stream, err := c.Events(context.Background(), "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case err := <-stream.Errors():
		if err == nil {
			t.Fatal("expected error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for error")
	}
}

func TestEventsDecodeError(t *testing.T) {
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		resp := response(http.StatusOK, "data: not-json\n\n")
		resp.Header.Set("Content-Type", "text/event-stream")
		return resp, nil
	})

	stream, err := c.Events(context.Background(), "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case err := <-stream.Errors():
		if err == nil {
			t.Fatal("expected error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for decode error")
	}
}

func TestCreateHandleResponseError(t *testing.T) {
	body, _ := json.Marshal(&client.ErrorResponse{Error: "Internal Server Error", Message: "create failed"})
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusInternalServerError, string(body)), nil
	})

	_, err := c.Create(context.Background(), "default", newTestConfig("cfg"))
	if err == nil {
		t.Fatal("expected error from HandleResponse")
	}
	if _, ok := err.(*client.APIError); !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
}

func TestGetHandleResponseError(t *testing.T) {
	body, _ := json.Marshal(&client.ErrorResponse{Error: "Internal Server Error", Message: "get failed"})
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusInternalServerError, string(body)), nil
	})

	_, err := c.Get(context.Background(), "default", "name")
	if err == nil {
		t.Fatal("expected error from HandleResponse")
	}
	if _, ok := err.(*client.APIError); !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
}

func TestDeleteHandleResponseError(t *testing.T) {
	body, _ := json.Marshal(&client.ErrorResponse{Error: "Internal Server Error", Message: "delete failed"})
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusInternalServerError, string(body)), nil
	})

	err := c.Delete(context.Background(), "default", "name")
	if err == nil {
		t.Fatal("expected error from HandleResponse")
	}
	if _, ok := err.(*client.APIError); !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
}

func TestListHandleResponseError(t *testing.T) {
	body, _ := json.Marshal(&client.ErrorResponse{Error: "Internal Server Error", Message: "list failed"})
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusInternalServerError, string(body)), nil
	})

	_, err := c.List(context.Background(), "default")
	if err == nil {
		t.Fatal("expected error from HandleResponse")
	}
	if _, ok := err.(*client.APIError); !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
}

func TestEventsBaseURLParseError(t *testing.T) {
	c := &restClient{
		RestClient: &client.RestClient{Config: client.ClientConfig{
			BaseURL:    "http://[::1", // missing closing bracket — url.Parse fails
			HTTPClient: &http.Client{},
			Logger:     zerolog.Nop(),
		}},
	}

	stream, err := c.Events(context.Background(), "default")
	if err != nil {
		t.Fatalf("unexpected synchronous error: %v", err)
	}

	select {
	case err := <-stream.Errors():
		if err == nil {
			t.Fatal("expected url parse error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for url parse error")
	}
}

func TestEventsStreamContextDone(t *testing.T) {
	var body bytes.Buffer
	for i := 0; i < 20; i++ {
		data, _ := json.Marshal(event.BrowserConfigEvent{EventType: event.EventTypeAdded})
		fmt.Fprintf(&body, "data: %s\n\n", data)
	}

	reqReceived := make(chan struct{})
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		close(reqReceived)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(body.Bytes())),
			Header:     make(http.Header),
		}, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c, err := NewClient(client.ClientConfig{
		BaseURL:    "http://example.com",
		HTTPClient: &http.Client{Transport: transport},
		Logger:     zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	stream, err := c.Events(ctx, "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-reqReceived:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for request")
	}

	cancel()

	for range stream.Events() {
	}
}

func TestEventsScannerError(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       errorReader{},
			Header:     make(http.Header),
		}, nil
	})

	c, err := NewClient(client.ClientConfig{
		BaseURL:    "http://example.com",
		HTTPClient: &http.Client{Transport: transport},
		Logger:     zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	stream, err := c.Events(context.Background(), "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case err := <-stream.Errors():
		if err == nil {
			t.Fatal("expected scanner error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for scanner error")
	}
}

// test helpers

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newClientWithTransport(t *testing.T, rt roundTripFunc) Client {
	t.Helper()

	c, err := NewClient(client.ClientConfig{
		BaseURL:    "http://example.com",
		HTTPClient: &http.Client{Transport: rt},
		Logger:     zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	return c
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

type errorReader struct{}

func (errorReader) Read(p []byte) (int, error) { return 0, errors.New("read failed") }
func (errorReader) Close() error               { return nil }

func newTestConfig(name string) *browserconfigv1.BrowserConfig {
	return &browserconfigv1.BrowserConfig{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: browserconfigv1.BrowserConfigSpec{
			Browsers: map[string]map[string]*browserconfigv1.BrowserVersionConfigSpec{
				"chrome": {"120": {Image: "chrome:120"}},
			},
		},
	}
}
