package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	browserv1 "github.com/alcounit/browser-controller/apis/browser/v1"
	"github.com/alcounit/browser-service/pkg/event"
	"github.com/rs/zerolog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewClientValidation(t *testing.T) {
	_, err := NewClient(ClientConfig{})
	if err == nil {
		t.Fatal("expected error for missing base URL")
	}

	_, err = NewClient(ClientConfig{BaseURL: "http://[::1"})
	if err == nil {
		t.Fatal("expected error for invalid base URL")
	}

	c, err := NewClient(ClientConfig{BaseURL: "http://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected client instance")
	}
}

func TestErrorResponseString(t *testing.T) {
	resp := ErrorResponse{Error: "bad", Message: "oops"}
	if got := resp.String(); got != "bad: oops" {
		t.Fatalf("unexpected string: %s", got)
	}

	resp.Details = "detail"
	if got := resp.String(); got != "bad: oops (detail)" {
		t.Fatalf("unexpected string with details: %s", got)
	}
}

func TestAPIErrorError(t *testing.T) {
	err := (&APIError{StatusCode: 404}).Error()
	if err != "API error 404" {
		t.Fatalf("unexpected error: %s", err)
	}

	err = (&APIError{StatusCode: 500, Response: &ErrorResponse{Error: "boom", Message: "fail"}}).Error()
	if err != "API error 500: boom: fail" {
		t.Fatalf("unexpected error with response: %s", err)
	}
}

func TestIsNotFound(t *testing.T) {
	if IsNotFound(errors.New("no")) {
		t.Fatal("expected false for non API error")
	}

	if !IsNotFound(&APIError{StatusCode: http.StatusNotFound}) {
		t.Fatal("expected true for 404 API error")
	}
}

func TestCreateBrowserSuccess(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody browserv1.Browser

	client := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		gotMethod = req.Method
		gotPath = req.URL.Path

		body, _ := io.ReadAll(req.Body)
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Fatalf("failed to unmarshal request: %v", err)
		}

		respBody, _ := json.Marshal(&gotBody)
		return response(http.StatusOK, string(respBody)), nil
	})

	browser := &browserv1.Browser{}
	browser.Name = "br"
	browser.Spec.BrowserName = "chrome"
	browser.Spec.BrowserVersion = "120"

	result, err := client.CreateBrowser(context.Background(), "default", browser)
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
	client, err := NewClient(ClientConfig{BaseURL: "http://example.com"})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	if _, err := client.CreateBrowser(context.Background(), "", &browserv1.Browser{}); err == nil {
		t.Fatal("expected error for missing namespace")
	}
	if _, err := client.CreateBrowser(context.Background(), "default", nil); err == nil {
		t.Fatal("expected error for nil browser")
	}
}

func TestCreateBrowserRequestError(t *testing.T) {
	client := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("request failed")
	})

	_, err := client.CreateBrowser(context.Background(), "default", &browserv1.Browser{
		Spec: browserv1.BrowserSpec{BrowserName: "chrome", BrowserVersion: "120"},
	})
	if err == nil || !strings.Contains(err.Error(), "failed to create browser") {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func TestHandleResponseInvalidJSONError(t *testing.T) {
	client := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusInternalServerError, "broken"), nil
	})

	_, err := client.GetBrowser(context.Background(), "default", "name")
	if err == nil {
		t.Fatal("expected error")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Response == nil || apiErr.Response.Error != "Unknown error" {
		t.Fatalf("unexpected API error response: %+v", apiErr.Response)
	}
}

func TestHandleResponseJSONError(t *testing.T) {
	client := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		body, _ := json.Marshal(&ErrorResponse{Error: "bad", Message: "nope", Details: "details"})
		return response(http.StatusBadRequest, string(body)), nil
	})

	_, err := client.GetBrowser(context.Background(), "default", "name")
	if err == nil {
		t.Fatal("expected error")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Response == nil || apiErr.Response.Error != "bad" {
		t.Fatalf("unexpected API error response: %+v", apiErr.Response)
	}
}

func TestListBrowsersSuccess(t *testing.T) {
	client := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		body, _ := json.Marshal([]*browserv1.Browser{
			{ObjectMeta: metav1.ObjectMeta{Name: "one"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "two"}},
		})
		return response(http.StatusOK, string(body)), nil
	})

	browsers, err := client.ListBrowsers(context.Background(), "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(browsers) != 2 {
		t.Fatalf("expected 2 browsers, got %d", len(browsers))
	}
}

func TestListBrowsersRequestError(t *testing.T) {
	client := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("request failed")
	})

	_, err := client.ListBrowsers(context.Background(), "default")
	if err == nil || !strings.Contains(err.Error(), "failed to list browsers") {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func TestDeleteBrowserSuccess(t *testing.T) {
	client := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodDelete {
			t.Fatalf("expected DELETE, got %s", req.Method)
		}
		return response(http.StatusNoContent, ""), nil
	})

	if err := client.DeleteBrowser(context.Background(), "default", "name"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteBrowserRequestError(t *testing.T) {
	client := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("request failed")
	})

	err := client.DeleteBrowser(context.Background(), "default", "name")
	if err == nil || !strings.Contains(err.Error(), "failed to delete browser") {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func TestEventsSuccess(t *testing.T) {
	events := []event.BrowserEvent{
		{EventType: event.EventTypeAdded, Browser: &browserv1.Browser{ObjectMeta: metav1.ObjectMeta{Name: "one"}}},
		{EventType: event.EventTypeDeleted, Browser: &browserv1.Browser{ObjectMeta: metav1.ObjectMeta{Name: "two"}}},
	}
	client := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		for _, evt := range events {
			if err := enc.Encode(evt); err != nil {
				t.Fatalf("encode error: %v", err)
			}
		}
		resp := response(http.StatusOK, buf.String())
		resp.Header.Set("Content-Type", "text/event-stream")
		return resp, nil
	})

	stream, err := client.Events(context.Background(), "default")
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

func TestEventsMissingNamespace(t *testing.T) {
	client, err := NewClient(ClientConfig{BaseURL: "http://example.com"})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	if _, err := client.Events(context.Background(), ""); err == nil {
		t.Fatal("expected error for missing namespace")
	}
}

func TestEventsRequestError(t *testing.T) {
	client := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("request failed")
	})

	stream, err := client.Events(context.Background(), "default")
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
	client := &browserClient{
		config: ClientConfig{
			BaseURL:    "http://example.com\x7f",
			HTTPClient: &http.Client{},
			Logger:     zerolog.Nop(),
		},
	}

	stream, err := client.Events(context.Background(), "default")
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
	client := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusInternalServerError, "bad"), nil
	})

	stream, err := client.Events(context.Background(), "default")
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
	client := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		resp := response(http.StatusOK, "not-json")
		resp.Header.Set("Content-Type", "text/event-stream")
		return resp, nil
	})

	stream, err := client.Events(context.Background(), "default")
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
	client := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		gotContentType = req.Header.Get("Content-Type")
		return response(http.StatusOK, `{}`), nil
	})

	_, err := client.CreateBrowser(context.Background(), "default", &browserv1.Browser{
		Spec: browserv1.BrowserSpec{BrowserName: "chrome", BrowserVersion: "120"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotContentType != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", gotContentType)
	}
}

func TestHandleResponseUnmarshalError(t *testing.T) {
	client := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"name":`), nil
	})

	_, err := client.GetBrowser(context.Background(), "default", "name")
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestHandleResponseEmptyBodyWithResult(t *testing.T) {
	client := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, ""), nil
	})

	result, err := client.GetBrowser(context.Background(), "default", "name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestHandleResponseReadError(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := io.NopCloser(errorReader{})
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       body,
			Header:     make(http.Header),
		}, nil
	})

	httpClient := &http.Client{Transport: transport}
	client, err := NewClient(ClientConfig{
		BaseURL:    "http://example.com",
		HTTPClient: httpClient,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, err = client.GetBrowser(context.Background(), "default", "name")
	if err == nil {
		t.Fatal("expected read error")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newClientWithTransport(t *testing.T, rt roundTripFunc) Client {
	t.Helper()

	client, err := NewClient(ClientConfig{
		BaseURL:    "http://example.com",
		HTTPClient: &http.Client{Transport: rt},
		Logger:     zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	return client
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

type errorReader struct{}

func (errorReader) Read(p []byte) (int, error) {
	return 0, errors.New("read failed")
}

func (errorReader) Close() error {
	return nil
}

func TestDoRequestCreateRequestError(t *testing.T) {
	client := &browserClient{
		config: ClientConfig{
			BaseURL: "http://example.com",
			Logger:  zerolog.Nop(),
		},
	}

	_, err := client.doRequest(context.Background(), "GET", " /bad", nil)
	if err == nil {
		t.Fatal("expected error for invalid request")
	}
}

func TestHandleResponseNoBodyNoResult(t *testing.T) {
	client := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, ""), nil
	})

	if err := client.DeleteBrowser(context.Background(), "default", "name"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleResponseWithBodyNoResult(t *testing.T) {
	client := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"ok":true}`), nil
	})

	if err := client.DeleteBrowser(context.Background(), "default", "name"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDoRequestBodyMarshalError(t *testing.T) {
	client := &browserClient{
		config: ClientConfig{
			BaseURL: "http://example.com",
			Logger:  zerolog.Nop(),
		},
	}

	_, err := client.doRequest(context.Background(), http.MethodPost, "/path", struct {
		Bad func()
	}{Bad: func() {}})
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestListBrowsersMissingNamespace(t *testing.T) {
	client, err := NewClient(ClientConfig{BaseURL: "http://example.com"})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	if _, err := client.ListBrowsers(context.Background(), ""); err == nil {
		t.Fatal("expected error for missing namespace")
	}
}

func TestDeleteBrowserMissingParams(t *testing.T) {
	client, err := NewClient(ClientConfig{BaseURL: "http://example.com"})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	if err := client.DeleteBrowser(context.Background(), "", "name"); err == nil {
		t.Fatal("expected error for missing namespace")
	}
	if err := client.DeleteBrowser(context.Background(), "default", ""); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestGetBrowserRequestError(t *testing.T) {
	client := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("request failed")
	})

	_, err := client.GetBrowser(context.Background(), "default", "name")
	if err == nil || !strings.Contains(err.Error(), "failed to get browser") {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func TestGetBrowserMissingParams(t *testing.T) {
	client, err := NewClient(ClientConfig{BaseURL: "http://example.com"})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	if _, err := client.GetBrowser(context.Background(), "", "name"); err == nil {
		t.Fatal("expected error for missing namespace")
	}
	if _, err := client.GetBrowser(context.Background(), "default", ""); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestDoRequestWithBodyMarshals(t *testing.T) {
	var gotBody bytes.Buffer
	client := newClientWithTransport(t, func(req *http.Request) (*http.Response, error) {
		io.Copy(&gotBody, req.Body)
		return response(http.StatusOK, `{}`), nil
	})

	_, err := client.CreateBrowser(context.Background(), "default", &browserv1.Browser{
		Spec: browserv1.BrowserSpec{BrowserName: "chrome", BrowserVersion: "120"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotBody.Len() == 0 {
		t.Fatal("expected request body to be written")
	}
}
