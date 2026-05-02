package tar1090

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const aircraftPath = "/data/aircraft.json"

// Client fetches aircraft data from a tar1090 instance.
type Client struct {
	baseURL      *url.URL
	aircraftPath string
	httpClient   *http.Client
}

// Option configures a Client.
type Option func(*Client) error

// WithHTTPClient configures the client to use the provided HTTP client.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) error {
		if httpClient == nil {
			return fmt.Errorf("http client is nil")
		}

		c.httpClient = httpClient
		return nil
	}
}

// WithAircraftPath configures the path used to fetch aircraft data.
func WithAircraftPath(aircraftPath string) Option {
	return func(c *Client) error {
		if aircraftPath == "" {
			return fmt.Errorf("aircraft path is empty")
		}
		if !strings.HasPrefix(aircraftPath, "/") {
			aircraftPath = "/" + aircraftPath
		}

		c.aircraftPath = aircraftPath
		return nil
	}
}

// NewClient creates a tar1090 client.
func NewClient(instanceURL string, opts ...Option) (*Client, error) {
	baseURL, err := url.Parse(instanceURL)
	if err != nil {
		return nil, fmt.Errorf("parse instance URL: %w", err)
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return nil, fmt.Errorf("instance URL must use http or https")
	}
	if baseURL.Host == "" {
		return nil, fmt.Errorf("instance URL must include a host")
	}

	client := &Client{
		baseURL:      baseURL,
		aircraftPath: aircraftPath,
		httpClient:   http.DefaultClient,
	}

	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(client); err != nil {
			return nil, err
		}
	}

	return client, nil
}

// FetchAircraft fetches and decodes /data/aircraft.json from the tar1090 instance.
func (c *Client) FetchAircraft(ctx context.Context) (AircraftResponse, error) {
	if c == nil {
		return AircraftResponse{}, fmt.Errorf("client is nil")
	}

	aircraftURL := c.baseURL.ResolveReference(&url.URL{Path: c.aircraftPath})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, aircraftURL.String(), nil)
	if err != nil {
		return AircraftResponse{}, fmt.Errorf("create aircraft request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return AircraftResponse{}, fmt.Errorf("fetch aircraft data: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return AircraftResponse{}, fmt.Errorf(
			"fetch aircraft data: unexpected status %s: %s",
			resp.Status,
			strings.TrimSpace(string(body)),
		)
	}

	var aircraft AircraftResponse
	if err := json.NewDecoder(resp.Body).Decode(&aircraft); err != nil {
		return AircraftResponse{}, fmt.Errorf("decode aircraft data: %w", err)
	}

	return aircraft, nil
}
