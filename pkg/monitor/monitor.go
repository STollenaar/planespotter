package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	adsbdb "github.com/nint8835/go-adsbdb"

	"github.com/nint8835/planespotter/pkg/ccar"
	"github.com/nint8835/planespotter/pkg/config"
	"github.com/nint8835/planespotter/pkg/messaging"
	"github.com/nint8835/planespotter/pkg/planespotters"
	"github.com/nint8835/planespotter/pkg/tar1090"
)

const userAgent = "planespotter (+https://github.com/nint8835/planespotter)"

// Monitor periodically fetches tar1090 aircraft data and posts newly-seen aircraft.
type Monitor struct {
	cfg          config.Config
	adsbdb       aircraftLookupClient
	ccar         ccarLookupClient
	photos       aircraftPhotoClient
	client       *tar1090.Client
	messages     aircraftMessageSender
	seenAircraft map[string]time.Time
	pending      map[string]pendingAircraft
}

type pendingAircraft struct {
	aircraft tar1090.Aircraft
	receives int
	lastSeen time.Time
}

type aircraftLookupClient interface {
	Aircraft(ctx context.Context, identifier string) (adsbdb.Aircraft, error)
	Callsign(ctx context.Context, callsign string) (adsbdb.FlightRoute, error)
}

type ccarLookupClient interface {
	Lookup(ctx context.Context, registration string, modeSHex string) (*ccar.Record, error)
}

type aircraftMessageSender interface {
	SendAircraft(ctx context.Context, message messaging.AircraftMessage) error
}

type aircraftPhotoClient interface {
	AircraftPhoto(ctx context.Context, aircraft planespotters.Aircraft) (planespotters.Photo, error)
}

// Option configures a Monitor.
type Option func(*Monitor) error

// WithADSBDBClient configures the ADS-B DB client used to enrich aircraft data.
func WithADSBDBClient(client interface {
	Aircraft(ctx context.Context, identifier string) (adsbdb.Aircraft, error)
	Callsign(ctx context.Context, callsign string) (adsbdb.FlightRoute, error)
}) Option {
	return func(m *Monitor) error {
		if client == nil {
			return fmt.Errorf("adsbdb client is nil")
		}

		m.adsbdb = client
		return nil
	}
}

// WithAircraftPhotoClient configures the client used to find fallback aircraft photos.
func WithAircraftPhotoClient(client interface {
	AircraftPhoto(ctx context.Context, aircraft planespotters.Aircraft) (planespotters.Photo, error)
}) Option {
	return func(m *Monitor) error {
		if client == nil {
			return fmt.Errorf("aircraft photo client is nil")
		}

		m.photos = client
		return nil
	}
}

// WithCCARClient configures the Canadian Civil Aircraft Registry client used to enrich aircraft data.
func WithCCARClient(client interface {
	Lookup(ctx context.Context, registration string, modeSHex string) (*ccar.Record, error)
}) Option {
	return func(m *Monitor) error {
		if client == nil {
			return fmt.Errorf("ccar client is nil")
		}

		m.ccar = client
		return nil
	}
}

// WithMessageSender configures the sender used to post newly-seen aircraft.
func WithMessageSender(sender interface {
	SendAircraft(ctx context.Context, message messaging.AircraftMessage) error
}) Option {
	return func(m *Monitor) error {
		if sender == nil {
			return fmt.Errorf("message sender is nil")
		}

		m.messages = sender
		return nil
	}
}

// New creates a monitor from application configuration.
func New(cfg config.Config, opts ...Option) (*Monitor, error) {
	if strings.TrimSpace(cfg.DataPath) == "" {
		cfg.DataPath = "."
	}

	slog.Debug(
		"Creating monitor",
		"tar1090_url", cfg.Tar1090URL,
		"monitor_interval", cfg.MonitorInterval,
		"max_altitude", cfg.MaxAltitude,
		"callsign_wait_receives", cfg.CallsignWaitReceives,
		"data_path", cfg.DataPath,
		"ccar_enabled", cfg.CCAREnabled,
	)

	client, err := tar1090.NewClient(cfg.Tar1090URL)
	if err != nil {
		return nil, fmt.Errorf("create tar1090 client: %w", err)
	}
	adsbdbClient, err := adsbdb.NewClient(
		adsbdb.WithUserAgent(userAgent),
	)
	if err != nil {
		return nil, fmt.Errorf("create adsbdb client: %w", err)
	}
	var messageSender aircraftMessageSender = messaging.NoopSender{}
	if cfg.DiscordWebhookURL != "" {
		messageSender, err = messaging.NewDiscordSender(
			cfg.DiscordWebhookURL,
			cfg.DiscordWebhookThreadID,
		)
		if err != nil {
			return nil, fmt.Errorf("create discord sender: %w", err)
		}
	}

	photoClient, err := planespotters.NewClient(
		planespotters.WithUserAgent(userAgent),
	)
	if err != nil {
		return nil, fmt.Errorf("create planespotters client: %w", err)
	}

	var ccarClient ccarLookupClient
	if cfg.CCAREnabled {
		ccarClient, err = ccar.NewClient(filepath.Join(cfg.DataPath, "ccarcsdb"))
		if err != nil {
			return nil, fmt.Errorf("create ccar client: %w", err)
		}
	}

	monitor := &Monitor{
		cfg:      cfg,
		adsbdb:   adsbdbClient,
		ccar:     ccarClient,
		photos:   photoClient,
		client:   client,
		messages: messageSender,
		pending:  map[string]pendingAircraft{},
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
	slog.Debug("Loaded seen aircraft", "count", len(seenAircraft))

	return monitor, nil
}

// Run fetches aircraft immediately, then continues fetching on the configured interval.
func (m *Monitor) Run(ctx context.Context) error {
	slog.DebugContext(ctx, "Starting monitor", "interval", m.cfg.MonitorInterval)

	if err := m.FetchAndCheck(ctx); err != nil {
		return fmt.Errorf("fetch and check: %w", err)
	}

	ticker := time.NewTicker(m.cfg.MonitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.DebugContext(ctx, "Stopping monitor", "error", ctx.Err())
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
	slog.DebugContext(ctx, "Fetching aircraft")

	response, err := m.client.FetchAircraft(ctx)
	if err != nil {
		return fmt.Errorf("fetch aircraft: %w", err)
	}
	slog.DebugContext(
		ctx,
		"Fetched aircraft",
		"aircraft_count", len(response.Aircraft),
		"messages", response.Messages,
		"now", response.Now,
	)

	seenNewAircraft := false
	seenAircraftChanged := false
	newAircraftCount := 0
	receivedAircraft := map[string]bool{}
	lastSeen := time.Unix(int64(response.Now), 0)
	for _, aircraft := range response.Aircraft {
		if aircraft.Hex == "" {
			m.logIgnoredAircraft(ctx, aircraft, "missing_hex")
			continue
		}
		receivedAircraft[aircraft.Hex] = true
		if reason, attrs := m.aircraftMaxAltitudeIgnoreReason(aircraft); reason != "" {
			m.logIgnoredAircraft(ctx, aircraft, reason, attrs...)
			continue
		}
		if _, ok := m.seenAircraft[aircraft.Hex]; ok {
			delete(m.pending, aircraft.Hex)
			if !m.seenAircraft[aircraft.Hex].Equal(lastSeen) {
				m.seenAircraft[aircraft.Hex] = lastSeen
				seenAircraftChanged = true
			}
			m.logIgnoredAircraft(ctx, aircraft, "already_seen")
			continue
		}
		if pending, ok := m.pending[aircraft.Hex]; ok {
			aircraft = mergePendingAircraft(pending.aircraft, aircraft)
		}

		if !m.shouldPostAircraft(ctx, aircraft, lastSeen) {
			continue
		}

		slog.InfoContext(
			ctx,
			"Found new aircraft",
			"hex", aircraft.Hex,
			"flight", aircraft.Flight,
			"registration", aircraft.Registration,
			"type", aircraft.AircraftType,
		)

		if err := m.postAndMarkSeen(ctx, aircraft, lastSeen); err != nil {
			return err
		}
		seenNewAircraft = true
		newAircraftCount++
	}
	for hex, pending := range m.pending {
		if _, seen := m.seenAircraft[hex]; receivedAircraft[hex] || seen {
			continue
		}

		slog.InfoContext(
			ctx,
			"Posting aircraft after it stopped being received before callsign was available",
			"hex", hex,
			"receives", pending.receives,
			"callsign_wait_receives", m.cfg.CallsignWaitReceives,
		)

		if err := m.postAndMarkSeen(ctx, pending.aircraft, pending.lastSeen); err != nil {
			return err
		}
		seenNewAircraft = true
		newAircraftCount++
	}

	if seenAircraftChanged {
		if err := m.saveSeenAircraft(); err != nil {
			return fmt.Errorf("save seen aircraft: %w", err)
		}
	}

	if seenNewAircraft {
		slog.DebugContext(
			ctx,
			"Saved seen aircraft",
			"new_aircraft_count", newAircraftCount,
			"seen_aircraft_count", len(m.seenAircraft),
			"path", m.seenAircraftPath(),
		)
	} else {
		slog.DebugContext(ctx, "No new aircraft found", "seen_aircraft_count", len(m.seenAircraft))
	}

	return nil
}

func (m *Monitor) postAndMarkSeen(ctx context.Context, aircraft tar1090.Aircraft, lastSeen time.Time) error {
	if err := m.postAircraft(ctx, aircraft); err != nil {
		return fmt.Errorf("post aircraft %s: %w", aircraft.Hex, err)
	}

	m.seenAircraft[aircraft.Hex] = lastSeen
	delete(m.pending, aircraft.Hex)
	if err := m.saveSeenAircraft(); err != nil {
		return fmt.Errorf("save seen aircraft: %w", err)
	}

	return nil
}

func (m *Monitor) shouldPostAircraft(ctx context.Context, aircraft tar1090.Aircraft, lastSeen time.Time) bool {
	if strings.TrimSpace(aircraft.Flight) != "" {
		delete(m.pending, aircraft.Hex)
		return true
	}

	if m.cfg.CallsignWaitReceives <= 0 {
		return true
	}

	pending := m.pending[aircraft.Hex]
	pending.aircraft = mergePendingAircraft(pending.aircraft, aircraft)
	pending.receives++
	pending.lastSeen = lastSeen
	m.pending[aircraft.Hex] = pending

	if pending.receives >= m.cfg.CallsignWaitReceives {
		slog.InfoContext(
			ctx,
			"Posting aircraft after waiting for callsign",
			"hex", aircraft.Hex,
			"receives", pending.receives,
			"callsign_wait_receives", m.cfg.CallsignWaitReceives,
		)
		return true
	}

	slog.InfoContext(
		ctx,
		"Waiting for aircraft callsign",
		"hex", aircraft.Hex,
		"receives", pending.receives,
		"callsign_wait_receives", m.cfg.CallsignWaitReceives,
	)
	return false
}

func mergePendingAircraft(previous tar1090.Aircraft, current tar1090.Aircraft) tar1090.Aircraft {
	fillString(&current.Registration, previous.Registration)
	fillString(&current.AircraftType, previous.AircraftType)
	fillString(&current.Description, previous.Description)
	fillString(&current.OwnOp, previous.OwnOp)
	fillString(&current.Year, previous.Year)
	if current.DBFlags == 0 {
		current.DBFlags = previous.DBFlags
	}

	return current
}

func fillString(value *string, fallback string) {
	if strings.TrimSpace(*value) == "" {
		*value = fallback
	}
}

func (m *Monitor) logIgnoredAircraft(
	ctx context.Context,
	aircraft tar1090.Aircraft,
	reason string,
	attrs ...any,
) {
	logAttrs := []any{
		"reason", reason,
		"hex", aircraft.Hex,
		"flight", aircraft.Flight,
		"registration", aircraft.Registration,
		"type", aircraft.AircraftType,
	}
	logAttrs = append(logAttrs, attrs...)

	slog.InfoContext(ctx, "Ignoring aircraft", logAttrs...)
}

func (m *Monitor) aircraftMaxAltitudeIgnoreReason(aircraft tar1090.Aircraft) (string, []any) {
	if m.cfg.MaxAltitude <= 0 {
		return "", nil
	}

	if aircraft.AltitudeBaro.Feet != nil {
		if *aircraft.AltitudeBaro.Feet > m.cfg.MaxAltitude {
			return "above_max_barometric_altitude", []any{
				"altitude_baro", *aircraft.AltitudeBaro.Feet,
				"max_altitude", m.cfg.MaxAltitude,
			}
		}
		return "", nil
	}
	if aircraft.AltitudeBaro.Ground {
		return "", nil
	}
	if aircraft.AltitudeGeom != nil {
		if *aircraft.AltitudeGeom > m.cfg.MaxAltitude {
			return "above_max_geometric_altitude", []any{
				"altitude_geom", *aircraft.AltitudeGeom,
				"max_altitude", m.cfg.MaxAltitude,
			}
		}
		return "", nil
	}

	return "missing_altitude", []any{
		"max_altitude", m.cfg.MaxAltitude,
	}
}

func (m *Monitor) postAircraft(ctx context.Context, aircraft tar1090.Aircraft) error {
	details, err := m.adsbdb.Aircraft(ctx, aircraft.Hex)
	var detailsPtr *adsbdb.Aircraft
	if err != nil {
		slog.WarnContext(ctx, "Error looking up aircraft details", "hex", aircraft.Hex, "error", err)
	} else {
		detailsPtr = &details
	}

	ccarRecord := m.ccarAircraft(ctx, aircraft, detailsPtr)

	route, err := m.flightRoute(ctx, aircraft)
	if err != nil {
		return err
	}

	photo := m.fallbackAircraftPhoto(ctx, aircraft, detailsPtr)

	if err := m.messages.SendAircraft(ctx, messaging.AircraftMessage{
		Aircraft:          aircraft,
		Details:           detailsPtr,
		CCAR:              ccarRecord,
		Route:             route,
		ImageURL:          photo.URL,
		ImageCopyright:    photo.Copyright,
		ImageCopyrightURL: photo.Link,
	}); err != nil {
		return fmt.Errorf("send aircraft message: %w", err)
	}

	return nil
}

func (m *Monitor) ccarAircraft(
	ctx context.Context,
	aircraft tar1090.Aircraft,
	details *adsbdb.Aircraft,
) *ccar.Record {
	if m.ccar == nil {
		return nil
	}

	registration := aircraft.Registration
	if strings.TrimSpace(registration) == "" && details != nil {
		registration = details.Registration
	}
	record, err := m.ccar.Lookup(ctx, registration, aircraft.Hex)
	if err != nil {
		slog.WarnContext(
			ctx,
			"Error looking up CCAR aircraft details",
			"hex", aircraft.Hex,
			"registration", registration,
			"error", err,
		)
		return nil
	}
	return record
}

func (m *Monitor) fallbackAircraftPhoto(
	ctx context.Context,
	aircraft tar1090.Aircraft,
	details *adsbdb.Aircraft,
) planespotters.Photo {
	if hasADSBDBPhoto(details) || m.photos == nil {
		return planespotters.Photo{}
	}

	photo, err := m.photos.AircraftPhoto(ctx, planespotters.Aircraft{
		Hex:          aircraft.Hex,
		Registration: aircraft.Registration,
		ICAOType:     aircraft.AircraftType,
	})
	if err != nil {
		slog.WarnContext(ctx, "Error looking up fallback aircraft photo", "hex", aircraft.Hex, "error", err)
		return planespotters.Photo{}
	}

	photo.URL = strings.TrimSpace(photo.URL)
	return photo
}

func hasADSBDBPhoto(details *adsbdb.Aircraft) bool {
	if details == nil {
		return false
	}
	return details.URLPhoto != nil && strings.TrimSpace(*details.URLPhoto) != ""
}

func (m *Monitor) flightRoute(ctx context.Context, aircraft tar1090.Aircraft) (*adsbdb.FlightRoute, error) {
	callsign := strings.TrimSpace(aircraft.Flight)
	if callsign == "" {
		return nil, nil
	}

	route, err := m.adsbdb.Callsign(ctx, callsign)
	if err != nil {
		slog.WarnContext(
			ctx,
			"Error looking up flight route",
			"hex", aircraft.Hex,
			"callsign", callsign,
			"error", err,
		)
		return nil, nil
	}

	correctAirline(callsign, &route)

	return &route, nil
}

func (m *Monitor) loadSeenAircraft() (map[string]time.Time, error) {
	file, err := os.Open(m.seenAircraftPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]time.Time{}, nil
		}
		return nil, fmt.Errorf("open seen aircraft file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	var rawSeenAircraft map[string]json.RawMessage
	if err := json.NewDecoder(file).Decode(&rawSeenAircraft); err != nil {
		return nil, fmt.Errorf("decode seen aircraft file: %w", err)
	}
	seenAircraft := map[string]time.Time{}
	legacySeen := time.Now()
	for hex, raw := range rawSeenAircraft {
		var lastSeenUnixTime int64
		if err := json.Unmarshal(raw, &lastSeenUnixTime); err == nil {
			seenAircraft[hex] = time.Unix(lastSeenUnixTime, 0)
			continue
		}

		var seen bool
		if err := json.Unmarshal(raw, &seen); err != nil {
			return nil, fmt.Errorf("decode seen aircraft file entry %q: %w", hex, err)
		}
		if seen {
			seenAircraft[hex] = legacySeen
		}
	}
	return seenAircraft, nil
}

func seenAircraftUnixTimes(seenAircraft map[string]time.Time) map[string]int64 {
	unixTimes := make(map[string]int64, len(seenAircraft))
	for hex, lastSeen := range seenAircraft {
		unixTimes[hex] = lastSeen.Unix()
	}
	return unixTimes
}

func (m *Monitor) saveSeenAircraft() error {
	if err := os.MkdirAll(m.cfg.DataPath, 0o755); err != nil {
		return fmt.Errorf("create seen aircraft directory: %w", err)
	}

	file, err := os.Create(m.seenAircraftPath())
	if err != nil {
		return fmt.Errorf("create seen aircraft file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(seenAircraftUnixTimes(m.seenAircraft)); err != nil {
		return fmt.Errorf("encode seen aircraft: %w", err)
	}

	return nil
}

func (m *Monitor) seenAircraftPath() string {
	return filepath.Join(m.cfg.DataPath, "seen.json")
}
