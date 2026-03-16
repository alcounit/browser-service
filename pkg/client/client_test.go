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
)

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

func TestNewRestClientValidation(t *testing.T) {
	_, err := NewRestClient(ClientConfig{})
	if err == nil {
		t.Fatal("expected error for missing base URL")
	}

	_, err = NewRestClient(ClientConfig{BaseURL: "http://[::1"})
	if err == nil {
		t.Fatal("expected error for invalid base URL")
	}

	rc, err := NewRestClient(ClientConfig{BaseURL: "http://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc == nil {
		t.Fatal("expected non-nil RestClient")
	}
	if rc.Config.HTTPClient == nil {
		t.Fatal("expected default HTTPClient to be set")
	}
}

func TestDoRequestWithBody(t *testing.T) {
	var gotMethod, gotContentType string
	var gotBody []byte

	transport := rtFunc(func(req *http.Request) (*http.Response, error) {
		gotMethod = req.Method
		gotContentType = req.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(req.Body)
		return okResponse(`{}`), nil
	})

	rc := &RestClient{Config: ClientConfig{
		BaseURL:    "http://example.com",
		HTTPClient: &http.Client{Transport: transport},
	}}

	payload := map[string]string{"key": "value"}
	resp, err := rc.DoRequest(context.Background(), http.MethodPost, "/path", payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()

	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, got %s", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %s", gotContentType)
	}
	if !bytes.Contains(gotBody, []byte("value")) {
		t.Fatal("expected body to contain payload")
	}
}

func TestDoRequestWithoutBody(t *testing.T) {
	var gotContentType string

	transport := rtFunc(func(req *http.Request) (*http.Response, error) {
		gotContentType = req.Header.Get("Content-Type")
		return okResponse(`{}`), nil
	})

	rc := &RestClient{Config: ClientConfig{
		BaseURL:    "http://example.com",
		HTTPClient: &http.Client{Transport: transport},
	}}

	resp, err := rc.DoRequest(context.Background(), http.MethodGet, "/path", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()

	if gotContentType != "" {
		t.Fatalf("expected no Content-Type for nil body, got %s", gotContentType)
	}
}

func TestDoRequestMarshalError(t *testing.T) {
	rc := &RestClient{Config: ClientConfig{
		BaseURL:    "http://example.com",
		HTTPClient: &http.Client{},
	}}

	_, err := rc.DoRequest(context.Background(), http.MethodPost, "/path", struct{ Bad func() }{Bad: func() {}})
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestDoRequestCreateError(t *testing.T) {
	rc := &RestClient{Config: ClientConfig{
		BaseURL:    "http://example.com",
		HTTPClient: &http.Client{},
	}}

	_, err := rc.DoRequest(context.Background(), "GET", " /bad path", nil)
	if err == nil {
		t.Fatal("expected error for invalid request URL")
	}
}

func TestDoRequestHTTPError(t *testing.T) {
	transport := rtFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})

	rc := &RestClient{Config: ClientConfig{
		BaseURL:    "http://example.com",
		HTTPClient: &http.Client{Transport: transport},
	}}

	_, err := rc.DoRequest(context.Background(), http.MethodGet, "/path", nil)
	if err == nil {
		t.Fatal("expected HTTP error")
	}
	if !strings.Contains(err.Error(), "request failed") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestHandleResponseSuccess(t *testing.T) {
	rc := &RestClient{}

	var result map[string]string
	resp := okResponse(`{"key":"value"}`)

	if err := rc.HandleResponse(resp, &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["key"] != "value" {
		t.Fatalf("unexpected result: %v", result)
	}
}

func TestHandleResponseNoResult(t *testing.T) {
	rc := &RestClient{}
	if err := rc.HandleResponse(okResponse(`{"key":"value"}`), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleResponseEmptyBody(t *testing.T) {
	rc := &RestClient{}
	var result map[string]string
	if err := rc.HandleResponse(okResponse(""), &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleResponseUnmarshalError(t *testing.T) {
	rc := &RestClient{}
	var result map[string]string
	if err := rc.HandleResponse(okResponse(`{bad`), &result); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestHandleResponseErrorStatusJSON(t *testing.T) {
	rc := &RestClient{}

	body, _ := json.Marshal(ErrorResponse{Error: "Not Found", Message: "gone"})
	resp := &http.Response{
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
	}

	err := rc.HandleResponse(resp, nil)
	if err == nil {
		t.Fatal("expected API error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("unexpected status: %d", apiErr.StatusCode)
	}
}

func TestHandleResponseErrorStatusNonJSON(t *testing.T) {
	rc := &RestClient{}

	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader("plain error text")),
		Header:     make(http.Header),
	}

	err := rc.HandleResponse(resp, nil)
	if err == nil {
		t.Fatal("expected API error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Response.Error != "Unknown error" {
		t.Fatalf("expected Unknown error, got %s", apiErr.Response.Error)
	}
}

func TestHandleResponseReadError(t *testing.T) {
	rc := &RestClient{}

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       errReader{},
		Header:     make(http.Header),
	}

	if err := rc.HandleResponse(resp, nil); err == nil {
		t.Fatal("expected read error")
	}
}

// test helpers

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func okResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read error") }
func (errReader) Close() error             { return nil }
