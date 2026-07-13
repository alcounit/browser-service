package browser

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

	browserv1 "github.com/alcounit/browser-controller/apis/browser/v1"
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

func TestCreateBrowserSuccess(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody browserv1.Browser

	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		gotMethod = req.Method
		gotPath = req.URL.Path

		body, _ := io.ReadAll(req.Body)
		json.Unmarshal(body, &gotBody)

		respBody, _ := json.Marshal(&gotBody)
		return response(http.StatusOK, string(respBody)), nil
	})

	browser := &browserv1.Browser{}
	browser.Name = "br"
	browser.Spec.BrowserName = "chrome"
	browser.Spec.BrowserVersion = "120"

	result, err := c.Create(context.Background(), "default", browser)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, got %s", gotMethod)
	}
	if gotPath != "/api/v1/namespaces/default/browsers" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if result.Name != "br" {
		t.Fatalf("unexpected result name: %s", result.Name)
	}
}

func TestCreateBrowserValidation(t *testing.T) {
	c, err := NewClient(client.ClientConfig{BaseURL: "http://example.com"})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	if _, err := c.Create(context.Background(), "", &browserv1.Browser{}); err == nil {
		t.Fatal("expected error for missing namespace")
	}
	if _, err := c.Create(context.Background(), "default", nil); err == nil {
		t.Fatal("expected error for nil browser")
	}
}

func TestCreateBrowserRequestError(t *testing.T) {
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("request failed")
	})

	_, err := c.Create(context.Background(), "default", &browserv1.Browser{
		Spec: browserv1.BrowserSpec{BrowserName: "chrome", BrowserVersion: "120"},
	})
	if err == nil || !strings.Contains(err.Error(), "failed to create browser") {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func TestHandleResponseInvalidJSONError(t *testing.T) {
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusInternalServerError, "broken"), nil
	})

	_, err := c.Get(context.Background(), "default", "name")
	if err == nil {
		t.Fatal("expected error")
	}

	apiErr, ok := err.(*client.APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Response == nil || apiErr.Response.Error != "Unknown error" {
		t.Fatalf("unexpected API error response: %+v", apiErr.Response)
	}
}

func TestHandleResponseEmptyErrorBody(t *testing.T) {
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusInternalServerError, ""), nil
	})

	_, err := c.Get(context.Background(), "default", "name")
	if err == nil {
		t.Fatal("expected error")
	}

	apiErr, ok := err.(*client.APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Response == nil || apiErr.Response.Error != "Unknown error" {
		t.Fatalf("unexpected API error response: %+v", apiErr.Response)
	}
	if apiErr.Response.Message != "" {
		t.Fatalf("expected empty message, got %q", apiErr.Response.Message)
	}
}

func TestHandleResponseJSONError(t *testing.T) {
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		body, _ := json.Marshal(&client.ErrorResponse{Error: "bad", Message: "nope", Details: "details"})
		return response(http.StatusBadRequest, string(body)), nil
	})

	_, err := c.Get(context.Background(), "default", "name")
	if err == nil {
		t.Fatal("expected error")
	}

	apiErr, ok := err.(*client.APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Response == nil || apiErr.Response.Error != "bad" {
		t.Fatalf("unexpected API error response: %+v", apiErr.Response)
	}
}

func TestListBrowsersSuccess(t *testing.T) {
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		body, _ := json.Marshal([]*browserv1.Browser{
			{ObjectMeta: metav1.ObjectMeta{Name: "one"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "two"}},
		})
		return response(http.StatusOK, string(body)), nil
	})

	browsers, err := c.List(context.Background(), "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(browsers) != 2 {
		t.Fatalf("expected 2 browsers, got %d", len(browsers))
	}
}

func TestListBrowsersMissingNamespace(t *testing.T) {
	c, _ := NewClient(client.ClientConfig{BaseURL: "http://example.com"})
	if _, err := c.List(context.Background(), ""); err == nil {
		t.Fatal("expected error for missing namespace")
	}
}

func TestListBrowsersRequestError(t *testing.T) {
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("request failed")
	})

	_, err := c.List(context.Background(), "default")
	if err == nil || !strings.Contains(err.Error(), "failed to list browsers") {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func TestDeleteBrowserSuccess(t *testing.T) {
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodDelete {
			t.Fatalf("expected DELETE, got %s", req.Method)
		}
		return response(http.StatusNoContent, ""), nil
	})

	if err := c.Delete(context.Background(), "default", "name"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteBrowserMissingParams(t *testing.T) {
	c, _ := NewClient(client.ClientConfig{BaseURL: "http://example.com"})
	if err := c.Delete(context.Background(), "", "name"); err == nil {
		t.Fatal("expected error for missing namespace")
	}
	if err := c.Delete(context.Background(), "default", ""); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestDeleteBrowserRequestError(t *testing.T) {
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("request failed")
	})

	err := c.Delete(context.Background(), "default", "name")
	if err == nil || !strings.Contains(err.Error(), "failed to delete browser") {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func TestGetBrowserMissingParams(t *testing.T) {
	c, _ := NewClient(client.ClientConfig{BaseURL: "http://example.com"})
	if _, err := c.Get(context.Background(), "", "name"); err == nil {
		t.Fatal("expected error for missing namespace")
	}
	if _, err := c.Get(context.Background(), "default", ""); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestGetBrowserRequestError(t *testing.T) {
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("request failed")
	})

	_, err := c.Get(context.Background(), "default", "name")
	if err == nil || !strings.Contains(err.Error(), "failed to get browser") {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func TestGetBrowserNotFound(t *testing.T) {
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusNotFound, `{"error":"Not Found","message":"browser not found"}`), nil
	})

	result, err := c.Get(context.Background(), "default", "name")
	if result != nil {
		t.Fatalf("expected nil result, got %v", result)
	}
	if !client.IsNotFound(err) {
		t.Fatalf("expected IsNotFound to be true, got %v", err)
	}
}

func TestEventsSuccess(t *testing.T) {
	events := []event.BrowserEvent{
		{EventType: event.EventTypeAdded, Browser: &browserv1.Browser{ObjectMeta: metav1.ObjectMeta{Name: "one"}}},
	}
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		var buf bytes.Buffer
		for _, evt := range events {
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
		if evt.Browser.Name != "one" || evt.EventType != event.EventTypeAdded {
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
		body, _ := json.Marshal(event.BrowserEvent{
			EventType: event.EventTypeAdded,
			Browser:   &browserv1.Browser{ObjectMeta: metav1.ObjectMeta{Name: "one"}},
		})
		resp := response(http.StatusOK, fmt.Sprintf("data: %s\n\n", body))
		resp.Header.Set("Content-Type", "text/event-stream")
		return resp, nil
	})

	stream, err := c.Events(context.Background(), "default", WithName("one"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-stream.Events():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}

	if !strings.Contains(gotQuery, "name=one") {
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

func TestDoRequestSetsContentType(t *testing.T) {
	var gotContentType string
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		gotContentType = req.Header.Get("Content-Type")
		return response(http.StatusOK, `{}`), nil
	})

	c.Create(context.Background(), "default", &browserv1.Browser{
		Spec: browserv1.BrowserSpec{BrowserName: "chrome", BrowserVersion: "120"},
	})

	if gotContentType != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", gotContentType)
	}
}

func TestHandleResponseUnmarshalError(t *testing.T) {
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"name":`), nil
	})

	_, err := c.Get(context.Background(), "default", "name")
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestHandleResponseEmptyBodyWithResult(t *testing.T) {
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, ""), nil
	})

	result, err := c.Get(context.Background(), "default", "name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestHandleResponseReadError(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(errorReader{}),
			Header:     make(http.Header),
		}, nil
	})

	c, err := NewClient(client.ClientConfig{
		BaseURL:    "http://example.com",
		HTTPClient: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, err = c.Get(context.Background(), "default", "name")
	if err == nil {
		t.Fatal("expected read error")
	}
}

func TestDoRequestCreateRequestError(t *testing.T) {
	c := &restClient{
		RestClient: &client.RestClient{Config: client.ClientConfig{
			BaseURL: "http://example.com",
			Logger:  zerolog.Nop(),
		}},
	}

	_, err := c.DoRequest(context.Background(), "GET", " /bad", nil)
	if err == nil {
		t.Fatal("expected error for invalid request")
	}
}

func TestDoRequestBodyMarshalError(t *testing.T) {
	c := &restClient{
		RestClient: &client.RestClient{Config: client.ClientConfig{
			BaseURL: "http://example.com",
			Logger:  zerolog.Nop(),
		}},
	}

	_, err := c.DoRequest(context.Background(), http.MethodPost, "/path", struct{ Bad func() }{Bad: func() {}})
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestHandleResponseNoBodyNoResult(t *testing.T) {
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, ""), nil
	})

	if err := c.Delete(context.Background(), "default", "name"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDoRequestWithBodyMarshals(t *testing.T) {
	var gotBody bytes.Buffer
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		io.Copy(&gotBody, req.Body)
		return response(http.StatusOK, `{}`), nil
	})

	c.Create(context.Background(), "default", &browserv1.Browser{
		Spec: browserv1.BrowserSpec{BrowserName: "chrome", BrowserVersion: "120"},
	})

	if gotBody.Len() == 0 {
		t.Fatal("expected request body to be written")
	}
}

func TestCreateBrowserHandleResponseError(t *testing.T) {
	body, _ := json.Marshal(&client.ErrorResponse{Error: "Internal Server Error", Message: "create failed"})
	c := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusInternalServerError, string(body)), nil
	})

	_, err := c.Create(context.Background(), "default", &browserv1.Browser{
		Spec: browserv1.BrowserSpec{BrowserName: "chrome", BrowserVersion: "120"},
	})
	if err == nil {
		t.Fatal("expected error from HandleResponse")
	}
	if _, ok := err.(*client.APIError); !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
}

func TestDeleteBrowserHandleResponseError(t *testing.T) {
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

func TestListBrowsersHandleResponseError(t *testing.T) {
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
	// Pre-build body with more events than eventCh capacity (16).
	var body bytes.Buffer
	for i := 0; i < 20; i++ {
		data, _ := json.Marshal(event.BrowserEvent{EventType: event.EventTypeAdded})
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

	// Wait until goroutine has made the HTTP request (response in hand).
	select {
	case <-reqReceived:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for request")
	}

	// Cancel — goroutine is processing in-memory events; once eventCh (cap 16)
	// is full, it blocks in the select and Done fires.
	cancel()

	// Drain buffered events and wait for the goroutine to exit.
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
