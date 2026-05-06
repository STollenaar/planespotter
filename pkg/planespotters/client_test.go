package planespotters_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/nint8835/planespotter/pkg/planespotters"
)

func TestClientReturnsFirstLargeThumbnail(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	var gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		gotUserAgent = r.Header.Get("User-Agent")
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q, want application/json", r.Header.Get("Accept"))
		}
		_, _ = w.Write([]byte(`{
			"photos": [{
				"thumbnail": {"src": "https://example.test/thumb.jpg"},
				"thumbnail_large": {"src": "https://example.test/large.jpg"}
			}]
		}`))
	}))
	defer server.Close()

	client := newTestClient(t, server)

	imageURL, err := client.AircraftPhoto(context.Background(), planespotters.Aircraft{
		Hex:          "abc123",
		Registration: "C-GABC",
		ICAOType:     "B738",
	})
	if err != nil {
		t.Fatalf("AircraftPhoto() error = %v", err)
	}

	if imageURL != "https://example.test/large.jpg" {
		t.Fatalf("image url = %q, want large thumbnail", imageURL)
	}
	if gotPath != "/pub/photos/hex/ABC123" {
		t.Fatalf("path = %q, want /pub/photos/hex/ABC123", gotPath)
	}
	if gotQuery.Get("reg") != "C-GABC" {
		t.Fatalf("reg query = %q, want C-GABC", gotQuery.Get("reg"))
	}
	if gotQuery.Get("icaoType") != "B738" {
		t.Fatalf("icaoType query = %q, want B738", gotQuery.Get("icaoType"))
	}
	if gotUserAgent == "" {
		t.Fatalf("User-Agent = empty, want net/http default without requiring an email")
	}
}

func TestClientUsesOptionalUserAgent(t *testing.T) {
	var gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"photos":[]}`))
	}))
	defer server.Close()

	client, err := planespotters.NewClient(
		planespotters.WithBaseURL(server.URL+"/pub/photos/"),
		planespotters.WithHTTPClient(server.Client()),
		planespotters.WithUserAgent("planespotter test"),
	)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if _, err := client.AircraftPhoto(context.Background(), planespotters.Aircraft{Hex: "abc123"}); err != nil {
		t.Fatalf("AircraftPhoto() error = %v", err)
	}

	if gotUserAgent != "planespotter test" {
		t.Fatalf("User-Agent = %q, want configured value", gotUserAgent)
	}
}

func TestClientSupportsImagesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"images":[{"thumbnail_large":{"src":"https://example.test/image-large.jpg"}}]}`))
	}))
	defer server.Close()

	client := newTestClient(t, server)

	imageURL, err := client.AircraftPhoto(context.Background(), planespotters.Aircraft{Hex: "abc123"})
	if err != nil {
		t.Fatalf("AircraftPhoto() error = %v", err)
	}

	if imageURL != "https://example.test/image-large.jpg" {
		t.Fatalf("image url = %q, want image large thumbnail", imageURL)
	}
}

func newTestClient(t *testing.T, server *httptest.Server) *planespotters.Client {
	t.Helper()

	client, err := planespotters.NewClient(
		planespotters.WithBaseURL(server.URL+"/pub/photos/"),
		planespotters.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	return client
}
