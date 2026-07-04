package web_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nint8835/planespotter/pkg/web"
)

func TestHealthcheckReturnsOKWhenDependencyIsHealthy(t *testing.T) {
	handler := newTestHandler(t, fakeHealthchecker{})

	req := httptest.NewRequest(http.MethodGet, "/api/healthcheck", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var response web.HealthcheckResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Status != web.Ok {
		t.Fatalf("status field = %q, want %q", response.Status, web.Ok)
	}
	if response.Error != nil {
		t.Fatalf("error field = %q, want nil", *response.Error)
	}
}

func TestHealthcheckReturnsServiceUnavailableWhenDependencyIsUnhealthy(t *testing.T) {
	handler := newTestHandler(t, fakeHealthchecker{err: errors.New("tar1090 unavailable")})

	req := httptest.NewRequest(http.MethodGet, "/api/healthcheck", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	var response web.HealthcheckResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Status != web.Unhealthy {
		t.Fatalf("status field = %q, want %q", response.Status, web.Unhealthy)
	}
	if response.Error == nil || *response.Error != "tar1090 unavailable" {
		t.Fatalf("error field = %v, want %q", response.Error, "tar1090 unavailable")
	}
}

func newTestHandler(t *testing.T, checker web.Healthchecker) http.Handler {
	t.Helper()

	handler, err := web.NewHandler(checker)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	return handler
}

type fakeHealthchecker struct {
	err error
}

func (f fakeHealthchecker) CheckHealth(context.Context) error {
	return f.err
}
