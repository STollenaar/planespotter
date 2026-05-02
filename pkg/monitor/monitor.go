package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	adsbdb "github.com/nint8835/go-adsbdb"

	"github.com/nint8835/planespotter/pkg/config"
	"github.com/nint8835/planespotter/pkg/tar1090"
)

// Monitor periodically fetches tar1090 aircraft data and posts newly-seen aircraft.
type Monitor struct {
	cfg          config.Config
	adsbdb       aircraftLookupClient
	client       *tar1090.Client
	seenAircraft map[string]bool
}

type aircraftLookupClient interface {
	Aircraft(ctx context.Context, identifier string) (adsbdb.Aircraft, error)
}

// Option configures a Monitor.
type Option func(*Monitor) error

// WithADSBDBClient configures the ADS-B DB client used to enrich aircraft data.
func WithADSBDBClient(client interface {
	Aircraft(ctx context.Context, identifier string) (adsbdb.Aircraft, error)
}) Option {
	return func(m *Monitor) error {
		if client == nil {
			return fmt.Errorf("adsbdb client is nil")
		}

		m.adsbdb = client
		return nil
	}
}

// New creates a monitor from application configuration.
func New(cfg config.Config, opts ...Option) (*Monitor, error) {
	client, err := tar1090.NewClient(cfg.Tar1090URL)
	if err != nil {
		return nil, fmt.Errorf("create tar1090 client: %w", err)
	}
	adsbdbClient, err := adsbdb.NewClient()
	if err != nil {
		return nil, fmt.Errorf("create adsbdb client: %w", err)
	}

	monitor := &Monitor{
		cfg:    cfg,
		adsbdb: adsbdbClient,
		client: client,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(monitor); err != nil {
			return nil, err
		}
	}

	seenAircraft, err := monitor.loadSeenAircraft()
	if err != nil {
		return nil, fmt.Errorf("load seen aircraft: %w", err)
	}
	monitor.seenAircraft = seenAircraft

	return monitor, nil
}

// Run fetches aircraft immediately, then continues fetching on the configured interval.
func (m *Monitor) Run(ctx context.Context) error {
	if err := m.FetchAndCheck(ctx); err != nil {
		return fmt.Errorf("fetch and check: %w", err)
	}

	ticker := time.NewTicker(m.cfg.MonitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := m.FetchAndCheck(ctx); err != nil {
				return fmt.Errorf("fetch and check: %w", err)
			}
		}
	}
}

// FetchAndCheck fetches aircraft, posts newly-seen aircraft, and persists seen state.
func (m *Monitor) FetchAndCheck(ctx context.Context) error {
	response, err := m.client.FetchAircraft(ctx)
	if err != nil {
		return fmt.Errorf("fetch aircraft: %w", err)
	}

	seenNewAircraft := false
	for _, aircraft := range response.Aircraft {
		if aircraft.Hex == "" || m.seenAircraft[aircraft.Hex] {
			continue
		}

		if err := m.postAircraft(ctx, aircraft); err != nil {
			return fmt.Errorf("post aircraft %s: %w", aircraft.Hex, err)
		}

		m.seenAircraft[aircraft.Hex] = true
		seenNewAircraft = true
	}

	if seenNewAircraft {
		if err := m.saveSeenAircraft(); err != nil {
			return fmt.Errorf("save seen aircraft: %w", err)
		}
	}

	return nil
}

func (m *Monitor) postAircraft(ctx context.Context, aircraft tar1090.Aircraft) error {
	_, err := m.adsbdb.Aircraft(ctx, aircraft.Hex)
	if err != nil {
		slog.WarnContext(ctx, "Error looking up aircraft details", "hex", aircraft.Hex, "error", err)
	}

	return nil
}

func (m *Monitor) loadSeenAircraft() (map[string]bool, error) {
	file, err := os.Open(m.cfg.SeenAircraftPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]bool{}, nil
		}
		return nil, fmt.Errorf("open seen aircraft file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	var seenAircraft map[string]bool
	if err := json.NewDecoder(file).Decode(&seenAircraft); err != nil {
		return nil, fmt.Errorf("decode seen aircraft file: %w", err)
	}
	if seenAircraft == nil {
		seenAircraft = map[string]bool{}
	}

	return seenAircraft, nil
}

func (m *Monitor) saveSeenAircraft() error {
	if err := os.MkdirAll(filepath.Dir(m.cfg.SeenAircraftPath), 0o755); err != nil {
		return fmt.Errorf("create seen aircraft directory: %w", err)
	}

	file, err := os.Create(m.cfg.SeenAircraftPath)
	if err != nil {
		return fmt.Errorf("create seen aircraft file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(m.seenAircraft); err != nil {
		return fmt.Errorf("encode seen aircraft: %w", err)
	}

	return nil
}
