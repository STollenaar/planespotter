package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/nint8835/planespotter/pkg/config"
)

// Server serves the planespotter HTTP API.
type Server struct {
	httpServer *http.Server
}

// NewServer creates a server for the planespotter HTTP API from application configuration.
func NewServer(cfg config.Config) (*Server, error) {
	checker, err := newTar1090Healthchecker(cfg.Tar1090URL)
	if err != nil {
		return nil, fmt.Errorf("create healthchecker: %w", err)
	}

	handler, err := NewHandler(checker)
	if err != nil {
		return nil, err
	}

	return &Server{
		httpServer: &http.Server{
			Addr:              cfg.HTTPAddr,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}, nil
}

// NewHandler creates an HTTP handler for the planespotter API.
func NewHandler(checker Healthchecker) (http.Handler, error) {
	strictHandler := NewStrictHandler(&server{checker: checker}, nil)
	return Handler(strictHandler), nil
}

// Run serves the HTTP API until ctx is canceled or the server fails.
func (s *Server) Run(ctx context.Context) error {
	go s.shutdown(ctx)

	slog.Info("Starting HTTP server", "addr", s.httpServer.Addr)
	err := s.httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return err
}

type server struct {
	checker Healthchecker
}

func (s *server) GetHealthcheck(
	ctx context.Context,
	_ GetHealthcheckRequestObject,
) (GetHealthcheckResponseObject, error) {
	if err := s.checker.CheckHealth(ctx); err != nil {
		slog.WarnContext(ctx, "Healthcheck failed", "err", err)
		message := err.Error()
		return GetHealthcheck503JSONResponse{
			Status: Unhealthy,
			Error:  &message,
		}, nil
	}

	return GetHealthcheck200JSONResponse{
		Status: Ok,
	}, nil
}

func (s *Server) shutdown(ctx context.Context) {
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Warn("Error shutting down HTTP server", "err", err)
	}
}
