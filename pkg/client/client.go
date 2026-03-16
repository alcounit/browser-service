package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/rs/zerolog"
)

type ClientConfig struct {
	BaseURL    string
	HTTPClient *http.Client
	Logger     zerolog.Logger
}

type RestClient struct {
	Config ClientConfig
}

func NewRestClient(config ClientConfig) (*RestClient, error) {
	if config.BaseURL == "" {
		return nil, fmt.Errorf("base URL is required")
	}

	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{
			Timeout: 10 * time.Second,
		}
	}

	if _, err := url.Parse(config.BaseURL); err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	return &RestClient{Config: config}, nil
}

func (c *RestClient) DoRequest(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(bodyBytes)
	}

	rawURL := c.Config.BaseURL + path

	req, err := http.NewRequestWithContext(ctx, method, rawURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	c.Config.Logger.Debug().Str("method", method).Str("url", rawURL).Msg("making API request")

	resp, err := c.Config.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

func (c *RestClient) HandleResponse(resp *http.Response, result any) error {
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errResp ErrorResponse
		if err := json.Unmarshal(body, &errResp); err != nil {
			return &APIError{
				StatusCode: resp.StatusCode,
				Response:   &ErrorResponse{Error: "Unknown error", Message: string(body)},
			}
		}
		return &APIError{StatusCode: resp.StatusCode, Response: &errResp}
	}

	if result != nil && len(body) > 0 {
		if err := json.Unmarshal(body, result); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}
	}

	return nil
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
