package tar1090_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/nint8835/planespotter/pkg/tar1090"
)

func TestFetchAircraft(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "aircraft.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/data/aircraft.json" {
			t.Fatalf("request path = %q, want /data/aircraft.json", r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Fatalf("request query = %q, want empty", r.URL.RawQuery)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("Accept header = %q, want application/json", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	client, err := tar1090.NewClient(server.URL + "/tar1090/?foo=bar")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	response, err := client.FetchAircraft(context.Background())
	if err != nil {
		t.Fatalf("FetchAircraft() error = %v", err)
	}

	if response.Now != 1700000000.125 {
		t.Fatalf("Now = %f, want 1700000000.125", response.Now)
	}
	if response.Messages != 42 {
		t.Fatalf("Messages = %d, want 42", response.Messages)
	}
	if len(response.Aircraft) != 2 {
		t.Fatalf("Aircraft length = %d, want 2", len(response.Aircraft))
	}

	aircraft := response.Aircraft[0]
	if aircraft.Hex != "abc123" {
		t.Fatalf("Hex = %q, want abc123", aircraft.Hex)
	}
	if aircraft.AltitudeBaro.Feet == nil || *aircraft.AltitudeBaro.Feet != 12000 {
		t.Fatalf("AltitudeBaro.Feet = %v, want 12000", aircraft.AltitudeBaro.Feet)
	}
	if aircraft.Latitude == nil || *aircraft.Latitude != 10.0 {
		t.Fatalf("Latitude = %v, want 10.0", aircraft.Latitude)
	}
	if aircraft.DBFlags != tar1090.DBFlagMilitary|tar1090.DBFlagPIA {
		t.Fatalf("DBFlags = %d, want military and PIA", aircraft.DBFlags)
	}
}

func TestBarometricAltitudeGround(t *testing.T) {
	var aircraft tar1090.Aircraft
	if err := json.Unmarshal([]byte(`{"alt_baro":"ground"}`), &aircraft); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if !aircraft.AltitudeBaro.Ground {
		t.Fatal("Ground = false, want true")
	}
	if aircraft.AltitudeBaro.Feet != nil {
		t.Fatalf("Feet = %v, want nil", aircraft.AltitudeBaro.Feet)
	}
}

func TestNewClientAcceptsHTTPClientOption(t *testing.T) {
	httpClient := http.DefaultClient

	if _, err := tar1090.NewClient("http://example.test", tar1090.WithHTTPClient(httpClient)); err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
}

func TestNewClientAcceptsAircraftPathOption(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/custom/aircraft.json" {
			t.Fatalf("request path = %q, want /custom/aircraft.json", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"now":1700000000.125,"messages":0,"aircraft":[]}`))
	}))
	defer server.Close()

	client, err := tar1090.NewClient(server.URL, tar1090.WithAircraftPath("custom/aircraft.json"))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if _, err := client.FetchAircraft(context.Background()); err != nil {
		t.Fatalf("FetchAircraft() error = %v", err)
	}
}

func TestNewClientRejectsInvalidURL(t *testing.T) {
	for _, rawURL := range []string{"", "localhost:1090", "ftp://example.test"} {
		t.Run(rawURL, func(t *testing.T) {
			if _, err := tar1090.NewClient(rawURL); err == nil {
				t.Fatal("NewClient() error = nil, want error")
			}
		})
	}
}

func TestNewClientRejectsNilHTTPClientOption(t *testing.T) {
	if _, err := tar1090.NewClient("http://example.test", tar1090.WithHTTPClient(nil)); err == nil {
		t.Fatal("NewClient() error = nil, want error")
	}
}

func TestNewClientRejectsEmptyAircraftPathOption(t *testing.T) {
	if _, err := tar1090.NewClient("http://example.test", tar1090.WithAircraftPath("")); err == nil {
		t.Fatal("NewClient() error = nil, want error")
	}
}

func TestFetchAircraftStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no aircraft here", http.StatusTeapot)
	}))
	defer server.Close()

	client, err := tar1090.NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if _, err := client.FetchAircraft(context.Background()); err == nil {
		t.Fatal("FetchAircraft() error = nil, want error")
	}
}
