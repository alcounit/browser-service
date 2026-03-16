package browserconfig

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	browserconfigv1 "github.com/alcounit/browser-controller/apis/browserconfig/v1"
	"github.com/alcounit/browser-service/pkg/client"
	"github.com/alcounit/browser-service/pkg/event"
)

type EventStream interface {
	Events() <-chan *event.BrowserConfigEvent
	Errors() <-chan error
	Close()
}

type eventStream struct {
	eventCh chan *event.BrowserConfigEvent
	errCh   chan error
	cancel  context.CancelFunc
}

func (s *eventStream) Events() <-chan *event.BrowserConfigEvent {
	return s.eventCh
}

func (s *eventStream) Errors() <-chan error {
	return s.errCh
}

func (s *eventStream) Close() {
	s.cancel()
}

func WithName(name string) event.EventsOption {
	return func(v url.Values) {
		if name != "" {
			v.Set("name", name)
		}
	}
}

type Client interface {
	Create(ctx context.Context, namespace string, cfg *browserconfigv1.BrowserConfig) (*browserconfigv1.BrowserConfig, error)
	Get(ctx context.Context, namespace, name string) (*browserconfigv1.BrowserConfig, error)
	Delete(ctx context.Context, namespace, name string) error
	List(ctx context.Context, namespace string) ([]*browserconfigv1.BrowserConfig, error)
	Events(ctx context.Context, namespace string, opts ...event.EventsOption) (EventStream, error)
}

type restClient struct {
	*client.RestClient
}

func NewClient(config client.ClientConfig) (Client, error) {
	rc, err := client.NewRestClient(config)
	if err != nil {
		return nil, err
	}
	return &restClient{rc}, nil
}

func (c *restClient) Create(ctx context.Context, namespace string, cfg *browserconfigv1.BrowserConfig) (*browserconfigv1.BrowserConfig, error) {
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	if cfg == nil {
		return nil, fmt.Errorf("browser config is required")
	}

	path := fmt.Sprintf("/api/v1/namespaces/%s/browserconfigs", namespace)

	resp, err := c.DoRequest(ctx, http.MethodPost, path, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create browser config: %w", err)
	}

	var result browserconfigv1.BrowserConfig
	if err := c.HandleResponse(resp, &result); err != nil {
		return nil, err
	}

	c.Config.Logger.Info().Str("namespace", namespace).Str("browserConfigName", result.Name).Msg("browser config created successfully")

	return &result, nil
}

func (c *restClient) Get(ctx context.Context, namespace, name string) (*browserconfigv1.BrowserConfig, error) {
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	path := fmt.Sprintf("/api/v1/namespaces/%s/browserconfigs/%s", namespace, name)

	resp, err := c.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get browser config: %w", err)
	}

	var result browserconfigv1.BrowserConfig
	if err := c.HandleResponse(resp, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *restClient) Delete(ctx context.Context, namespace, name string) error {
	if namespace == "" {
		return fmt.Errorf("namespace is required")
	}
	if name == "" {
		return fmt.Errorf("name is required")
	}

	path := fmt.Sprintf("/api/v1/namespaces/%s/browserconfigs/%s", namespace, name)

	resp, err := c.DoRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("failed to delete browser config: %w", err)
	}

	if err := c.HandleResponse(resp, nil); err != nil {
		return err
	}

	c.Config.Logger.Info().Str("namespace", namespace).Str("browserConfigName", name).Msg("browser config deleted successfully")

	return nil
}

func (c *restClient) List(ctx context.Context, namespace string) ([]*browserconfigv1.BrowserConfig, error) {
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}

	path := fmt.Sprintf("/api/v1/namespaces/%s/browserconfigs", namespace)

	resp, err := c.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list browser configs: %w", err)
	}

	var result []*browserconfigv1.BrowserConfig
	if err := c.HandleResponse(resp, &result); err != nil {
		return nil, err
	}

	c.Config.Logger.Debug().Str("namespace", namespace).Int("count", len(result)).Msg("browser configs listed successfully")

	return result, nil
}

func (c *restClient) Events(ctx context.Context, namespace string, opts ...event.EventsOption) (EventStream, error) {
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}

	streamCtx, cancel := context.WithCancel(ctx)

	eventCh := make(chan *event.BrowserConfigEvent, 16)
	errCh := make(chan error, 1)

	stream := &eventStream{eventCh: eventCh, errCh: errCh, cancel: cancel}

	go func() {
		defer close(eventCh)
		defer close(errCh)

		baseURL, err := url.Parse(c.Config.BaseURL)
		if err != nil {
			errCh <- fmt.Errorf("invalid base URL: %w", err)
			return
		}

		baseURL.Path = fmt.Sprintf("/api/v1/namespaces/%s/browserconfigs/events", namespace)

		if opts != nil {
			query := baseURL.Query()
			for _, opt := range opts {
				opt(query)
			}
			baseURL.RawQuery = query.Encode()
		}

		req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, baseURL.String(), nil)
		if err != nil {
			errCh <- err
			return
		}

		req.Header.Set("Accept", "text/event-stream")

		resp, err := c.Config.HTTPClient.Do(req)
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			errCh <- fmt.Errorf("config events stream failed (%d): %s", resp.StatusCode, body)
			return
		}

		const sseDataPrefix = "data: "

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 1<<20), 1<<20)

		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, sseDataPrefix) {
				continue
			}

			var evt event.BrowserConfigEvent
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, sseDataPrefix)), &evt); err != nil {
				errCh <- err
				return
			}

			select {
			case eventCh <- &evt:
			case <-streamCtx.Done():
				return
			}
		}

		if err := scanner.Err(); err != nil && streamCtx.Err() == nil {
			errCh <- err
		}
	}()

	return stream, nil
}
