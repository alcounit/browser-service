package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	browserv1 "github.com/alcounit/browser-controller/apis/browser/v1"
	"github.com/alcounit/browser-service/pkg/event"
	"github.com/rs/zerolog"
)

type ClientConfig struct {
	BaseURL    string
	HTTPClient *http.Client
	Logger     zerolog.Logger
}

type BrowserEventStream interface {
	Events() <-chan *event.BrowserEvent
	Errors() <-chan error
	Close()
}

type browserEventStream struct {
	eventCh chan *event.BrowserEvent
	errCh   chan error
	cancel  context.CancelFunc
}

func (s *browserEventStream) Events() <-chan *event.BrowserEvent {
	return s.eventCh
}

func (s *browserEventStream) Errors() <-chan error {
	return s.errCh
}

func (s *browserEventStream) Close() {
	s.cancel()
}

type Client interface {
	CreateBrowser(ctx context.Context, namespace string, browser *browserv1.Browser) (*browserv1.Browser, error)

	GetBrowser(ctx context.Context, namespace, name string) (*browserv1.Browser, error)

	DeleteBrowser(ctx context.Context, namespace, name string) error

	ListBrowsers(ctx context.Context, namespace string) ([]*browserv1.Browser, error)

	Events(ctx context.Context, namespace string) (BrowserEventStream, error)
}

type browserClient struct {
	config ClientConfig
}

func NewClient(config ClientConfig) (Client, error) {
	if config.BaseURL == "" {
		return nil, fmt.Errorf("base URL is required")
	}

	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{
			Timeout: 0 * time.Second,
		}
	}

	if _, err := url.Parse(config.BaseURL); err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	return &browserClient{
		config: config,
	}, nil
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Details string `json:"details"`
}

func (e *ErrorResponse) String() string {
	if e.Details != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Error, e.Message, e.Details)
	}
	return fmt.Sprintf("%s: %s", e.Error, e.Message)
}

type APIError struct {
	StatusCode int
	Response   *ErrorResponse
}

func (e *APIError) Error() string {
	if e.Response != nil {
		return fmt.Sprintf("API error %d: %s", e.StatusCode, e.Response.String())
	}
	return fmt.Sprintf("API error %d", e.StatusCode)
}

func IsNotFound(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.StatusCode == http.StatusNotFound
	}
	return false
}

func (c *browserClient) doRequest(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(bodyBytes)
	}

	url := c.config.BaseURL + path

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	c.config.Logger.Debug().
		Str("method", method).
		Str("url", url).
		Msg("making API request")

	resp, err := c.config.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

func (c *browserClient) handleResponse(resp *http.Response, result interface{}) error {
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errorResp ErrorResponse
		if err := json.Unmarshal(body, &errorResp); err != nil {
			return &APIError{
				StatusCode: resp.StatusCode,
				Response:   &ErrorResponse{Error: "Unknown error", Message: string(body)},
			}
		}
		return &APIError{
			StatusCode: resp.StatusCode,
			Response:   &errorResp,
		}
	}

	if result != nil && len(body) > 0 {
		if err := json.Unmarshal(body, result); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}
	}

	return nil
}

func (c *browserClient) CreateBrowser(ctx context.Context, namespace string, browser *browserv1.Browser) (*browserv1.Browser, error) {
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	if browser == nil {
		return nil, fmt.Errorf("browser is required")
	}

	path := fmt.Sprintf("/api/v1/namespaces/%s/browsers", namespace)

	resp, err := c.doRequest(ctx, http.MethodPost, path, browser)
	if err != nil {
		return nil, fmt.Errorf("failed to create browser: %w", err)
	}

	var result browserv1.Browser
	if err := c.handleResponse(resp, &result); err != nil {
		return nil, err
	}

	c.config.Logger.Info().
		Str("namespace", namespace).
		Str("browserName", result.Name).
		Msg("browser created successfully")

	return &result, nil
}

func (c *browserClient) GetBrowser(ctx context.Context, namespace, name string) (*browserv1.Browser, error) {
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	path := fmt.Sprintf("/api/v1/namespaces/%s/browsers/%s", namespace, name)

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get browser: %w", err)
	}

	var result browserv1.Browser
	if err := c.handleResponse(resp, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *browserClient) DeleteBrowser(ctx context.Context, namespace, name string) error {
	if namespace == "" {
		return fmt.Errorf("namespace is required")
	}
	if name == "" {
		return fmt.Errorf("name is required")
	}

	path := fmt.Sprintf("/api/v1/namespaces/%s/browsers/%s", namespace, name)

	resp, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("failed to delete browser: %w", err)
	}

	if err := c.handleResponse(resp, nil); err != nil {
		return err
	}

	c.config.Logger.Info().
		Str("namespace", namespace).
		Str("browserName", name).
		Msg("browser deleted successfully")

	return nil
}

func (c *browserClient) ListBrowsers(ctx context.Context, namespace string) ([]*browserv1.Browser, error) {
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}

	path := fmt.Sprintf("/api/v1/namespaces/%s/browsers", namespace)

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list browsers: %w", err)
	}

	var result []*browserv1.Browser
	if err := c.handleResponse(resp, &result); err != nil {
		return nil, err
	}

	c.config.Logger.Debug().
		Str("namespace", namespace).
		Int("count", len(result)).
		Msg("browsers listed successfully")

	return result, nil
}

func (c *browserClient) Events(ctx context.Context, namespace string) (BrowserEventStream, error) {
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}

	streamCtx, cancel := context.WithCancel(ctx)

	eventCh := make(chan *event.BrowserEvent, 16)
	errCh := make(chan error, 1)

	stream := &browserEventStream{
		eventCh: eventCh,
		errCh:   errCh,
		cancel:  cancel,
	}

	go func() {
		defer close(eventCh)
		defer close(errCh)

		url := fmt.Sprintf(
			"%s/api/v1/namespaces/%s/events",
			c.config.BaseURL,
			namespace,
		)

		req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, url, nil)
		if err != nil {
			errCh <- err
			return
		}

		req.Header.Set("Accept", "text/event-stream")

		resp, err := c.config.HTTPClient.Do(req)
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			errCh <- fmt.Errorf("events stream failed (%d): %s", resp.StatusCode, body)
			return
		}

		decoder := json.NewDecoder(resp.Body)

		for {
			select {
			case <-streamCtx.Done():
				return
			default:
			}

			var evt event.BrowserEvent
			if err := decoder.Decode(&evt); err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
					return
				}
				errCh <- err
				return
			}

			select {
			case eventCh <- &evt:
			case <-streamCtx.Done():
				return
			}
		}
	}()

	return stream, nil
}
