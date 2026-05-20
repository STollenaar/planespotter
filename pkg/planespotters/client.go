package planespotters

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const defaultPhotoAPI = "https://api.planespotters.net/pub/photos/"

// Aircraft identifies an aircraft for Planespotters.net photo lookups.
type Aircraft struct {
	Hex          string
	Registration string
	ICAOType     string
}

// Photo contains a Planespotters.net aircraft photo and its attribution metadata.
type Photo struct {
	URL       string
	Copyright string
	Link      string
}

// Client fetches aircraft photos from the Planespotters.net public API.
type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	userAgent  string
}

// Option configures a Client.
type Option func(*Client) error

// WithBaseURL configures the Planespotters-compatible photo API base URL.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) error {
		parsedURL, err := url.Parse(baseURL)
		if err != nil {
			return fmt.Errorf("parse planespotters base URL: %w", err)
		}
		if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
			return fmt.Errorf("planespotters base URL must use http or https")
		}
		if parsedURL.Host == "" {
			return fmt.Errorf("planespotters base URL must include a host")
		}

		c.baseURL = parsedURL
		return nil
	}
}

// WithHTTPClient configures the HTTP client used for API requests.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) error {
		if httpClient == nil {
			return fmt.Errorf("http client is nil")
		}

		c.httpClient = httpClient
		return nil
	}
}

// WithUserAgent configures an optional User-Agent header for API requests.
func WithUserAgent(userAgent string) Option {
	return func(c *Client) error {
		c.userAgent = strings.TrimSpace(userAgent)
		return nil
	}
}

// NewClient creates a Planespotters.net public API client.
func NewClient(opts ...Option) (*Client, error) {
	baseURL, err := url.Parse(defaultPhotoAPI)
	if err != nil {
		panic(err)
	}

	client := &Client{
		baseURL:    baseURL,
		httpClient: http.DefaultClient,
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

type photoResponse struct {
	Photos []photo `json:"photos"`
	Images []photo `json:"images"`
}

type photo struct {
	LargeThumbnail image  `json:"thumbnail_large"`
	Photographer   string `json:"photographer"`
	Link           string `json:"link"`
}

type image struct {
	Src string `json:"src"`
}

// AircraftPhoto returns the first large thumbnail photo for an aircraft.
func (c *Client) AircraftPhoto(ctx context.Context, aircraft Aircraft) (Photo, error) {
	if c == nil {
		return Photo{}, fmt.Errorf("planespotters client is nil")
	}

	hex := strings.ToUpper(strings.TrimSpace(aircraft.Hex))
	if hex == "" {
		return Photo{}, nil
	}

	photoPath := strings.TrimRight(c.baseURL.Path, "/") + "/hex/" + url.PathEscape(hex)
	photoURL := c.baseURL.ResolveReference(&url.URL{Path: photoPath})
	query := photoURL.Query()
	if registration := strings.TrimSpace(aircraft.Registration); registration != "" {
		query.Set("reg", registration)
	}
	if aircraftType := strings.TrimSpace(aircraft.ICAOType); aircraftType != "" {
		query.Set("icaoType", aircraftType)
	}
	photoURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, photoURL.String(), nil)
	if err != nil {
		return Photo{}, fmt.Errorf("create planespotters photo request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Photo{}, fmt.Errorf("fetch planespotters photo: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return Photo{}, fmt.Errorf(
			"fetch planespotters photo: unexpected status %s: %s",
			resp.Status,
			strings.TrimSpace(string(body)),
		)
	}

	var photoResponse photoResponse
	if err := json.NewDecoder(resp.Body).Decode(&photoResponse); err != nil {
		return Photo{}, fmt.Errorf("decode planespotters photo response: %w", err)
	}

	for _, photo := range append(photoResponse.Photos, photoResponse.Images...) {
		if thumbnail := strings.TrimSpace(photo.LargeThumbnail.Src); thumbnail != "" {
			return Photo{
				URL:       thumbnail,
				Copyright: copyright(photo.Photographer),
				Link:      strings.TrimSpace(photo.Link),
			}, nil
		}
	}

	return Photo{}, nil
}

func copyright(photographer string) string {
	photographer = strings.TrimSpace(photographer)
	if photographer == "" {
		return ""
	}

	return "Copyright © " + photographer
}
