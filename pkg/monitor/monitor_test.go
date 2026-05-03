package monitor_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	adsbdb "github.com/nint8835/go-adsbdb"

	"github.com/nint8835/planespotter/pkg/config"
	"github.com/nint8835/planespotter/pkg/messaging"
	"github.com/nint8835/planespotter/pkg/monitor"
)

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

func TestNewInitializesEmptySeenAircraftWhenFileDoesNotExist(t *testing.T) {
	server := aircraftServer(t, http.StatusOK, `{"now":1,"messages":0,"aircraft":[]}`)
	defer server.Close()

	path := filepath.Join(t.TempDir(), "state", "seen.json")
	mon := newTestMonitor(t, server.URL, path)

	if err := mon.FetchAndCheck(context.Background()); err != nil {
		t.Fatalf("FetchAndCheck() error = %v", err)
	}
	assertFileDoesNotExist(t, path)
}

func TestFetchAndCheckSkipsExistingSeenAircraft(t *testing.T) {
	server := aircraftServer(
		t,
		http.StatusOK,
		`{"now":1,"messages":0,"aircraft":[{"hex":"abc123"}]}`,
	)
	defer server.Close()

	path := filepath.Join(t.TempDir(), "seen.json")
	writeSeenFixture(t, path, map[string]bool{"abc123": true})

	mon := newTestMonitor(t, server.URL, path)
	if err := mon.FetchAndCheck(context.Background()); err != nil {
		t.Fatalf("FetchAndCheck() error = %v", err)
	}

	assertSeenFile(t, path, map[string]bool{"abc123": true})
}

func TestFetchAndCheckPersistsNewAircraft(t *testing.T) {
	server := aircraftServer(
		t,
		http.StatusOK,
		`{"now":1,"messages":0,"aircraft":[{"hex":"abc123"}]}`,
	)
	defer server.Close()

	path := filepath.Join(t.TempDir(), "nested", "seen.json")
	mon := newTestMonitor(t, server.URL, path)
	if err := mon.FetchAndCheck(context.Background()); err != nil {
		t.Fatalf("FetchAndCheck() error = %v", err)
	}

	assertSeenFile(t, path, map[string]bool{"abc123": true})
}

func TestFetchAndCheckPersistsMultipleNewAircraft(t *testing.T) {
	server := aircraftServer(
		t,
		http.StatusOK,
		`{"now":1,"messages":0,"aircraft":[{"hex":"abc123"},{"hex":"def456"}]}`,
	)
	defer server.Close()

	path := filepath.Join(t.TempDir(), "seen.json")
	mon := newTestMonitor(t, server.URL, path)
	if err := mon.FetchAndCheck(context.Background()); err != nil {
		t.Fatalf("FetchAndCheck() error = %v", err)
	}

	assertSeenFile(t, path, map[string]bool{"abc123": true, "def456": true})
}

func TestFetchAndCheckIgnoresAircraftAboveMaxBarometricAltitude(t *testing.T) {
	server := aircraftServer(
		t,
		http.StatusOK,
		`{"now":1,"messages":0,"aircraft":[`+
			`{"hex":"above","alt_baro":12000},`+
			`{"hex":"at","alt_baro":10000},`+
			`{"hex":"below","alt_baro":9000}`+
			`]}`,
	)
	defer server.Close()

	path := filepath.Join(t.TempDir(), "seen.json")
	sender := &recordingMessageSender{}
	adsbdbClient := &recordingADSBDBClient{}
	mon := newTestMonitorWithConfigAndOptions(
		t,
		config.Config{
			Tar1090URL:       server.URL,
			MonitorInterval:  time.Minute,
			MaxAltitude:      10000,
			SeenAircraftPath: path,
		},
		monitor.WithADSBDBClient(adsbdbClient),
		monitor.WithMessageSender(sender),
	)
	if err := mon.FetchAndCheck(context.Background()); err != nil {
		t.Fatalf("FetchAndCheck() error = %v", err)
	}

	assertSeenFile(t, path, map[string]bool{"at": true, "below": true})
	if !reflect.DeepEqual(adsbdbClient.identifiers, []string{"at", "below"}) {
		t.Fatalf("adsbdb Aircraft() identifiers = %#v, want at and below", adsbdbClient.identifiers)
	}
	if len(sender.messages) != 2 {
		t.Fatalf("sent message count = %d, want 2", len(sender.messages))
	}
}

func TestFetchAndCheckUsesGeometricAltitudeWhenBarometricAltitudeIsUnavailable(t *testing.T) {
	server := aircraftServer(
		t,
		http.StatusOK,
		`{"now":1,"messages":0,"aircraft":[`+
			`{"hex":"above","alt_geom":12000},`+
			`{"hex":"ground","alt_baro":"ground","alt_geom":12000},`+
			`{"hex":"below","alt_geom":9000}`+
			`]}`,
	)
	defer server.Close()

	path := filepath.Join(t.TempDir(), "seen.json")
	mon := newTestMonitorWithConfigAndOptions(
		t,
		config.Config{
			Tar1090URL:       server.URL,
			MonitorInterval:  time.Minute,
			MaxAltitude:      10000,
			SeenAircraftPath: path,
		},
		monitor.WithADSBDBClient(&recordingADSBDBClient{}),
	)
	if err := mon.FetchAndCheck(context.Background()); err != nil {
		t.Fatalf("FetchAndCheck() error = %v", err)
	}

	assertSeenFile(t, path, map[string]bool{"below": true, "ground": true})
}

func TestFetchAndCheckIgnoresAircraftWithoutAltitudeWhenMaxAltitudeIsEnabled(t *testing.T) {
	server := aircraftServer(
		t,
		http.StatusOK,
		`{"now":1,"messages":0,"aircraft":[{"hex":"abc123"}]}`,
	)
	defer server.Close()

	path := filepath.Join(t.TempDir(), "seen.json")
	sender := &recordingMessageSender{}
	adsbdbClient := &recordingADSBDBClient{}
	mon := newTestMonitorWithConfigAndOptions(
		t,
		config.Config{
			Tar1090URL:       server.URL,
			MonitorInterval:  time.Minute,
			MaxAltitude:      10000,
			SeenAircraftPath: path,
		},
		monitor.WithADSBDBClient(adsbdbClient),
		monitor.WithMessageSender(sender),
	)
	if err := mon.FetchAndCheck(context.Background()); err != nil {
		t.Fatalf("FetchAndCheck() error = %v", err)
	}

	assertFileDoesNotExist(t, path)
	if len(adsbdbClient.identifiers) != 0 {
		t.Fatalf("adsbdb Aircraft() identifiers = %#v, want none", adsbdbClient.identifiers)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("sent message count = %d, want 0", len(sender.messages))
	}
}

func TestFetchAndCheckAllowsAircraftWithoutAltitudeWhenMaxAltitudeIsDisabled(t *testing.T) {
	server := aircraftServer(
		t,
		http.StatusOK,
		`{"now":1,"messages":0,"aircraft":[{"hex":"abc123"}]}`,
	)
	defer server.Close()

	path := filepath.Join(t.TempDir(), "seen.json")
	mon := newTestMonitorWithConfigAndOptions(
		t,
		config.Config{
			Tar1090URL:       server.URL,
			MonitorInterval:  time.Minute,
			MaxAltitude:      0,
			SeenAircraftPath: path,
		},
		monitor.WithADSBDBClient(&recordingADSBDBClient{}),
	)
	if err := mon.FetchAndCheck(context.Background()); err != nil {
		t.Fatalf("FetchAndCheck() error = %v", err)
	}

	assertSeenFile(t, path, map[string]bool{"abc123": true})
}

func TestFetchAndCheckEnhancesNewAircraftWithHex(t *testing.T) {
	server := aircraftServer(
		t,
		http.StatusOK,
		`{"now":1,"messages":0,"aircraft":[{"hex":"abc123"},{"hex":"def456"}]}`,
	)
	defer server.Close()

	path := filepath.Join(t.TempDir(), "seen.json")
	adsbdbClient := &recordingADSBDBClient{}
	mon := newTestMonitorWithADSBDB(t, server.URL, path, adsbdbClient)
	if err := mon.FetchAndCheck(context.Background()); err != nil {
		t.Fatalf("FetchAndCheck() error = %v", err)
	}

	want := []string{"abc123", "def456"}
	if !reflect.DeepEqual(adsbdbClient.identifiers, want) {
		t.Fatalf("adsbdb Aircraft() identifiers = %#v, want %#v", adsbdbClient.identifiers, want)
	}
}

func TestFetchAndCheckLooksUpFlightRouteForCallsign(t *testing.T) {
	server := aircraftServer(
		t,
		http.StatusOK,
		`{"now":1,"messages":0,"aircraft":[{"hex":"abc123","flight":"ABC123  "}]}`,
	)
	defer server.Close()

	path := filepath.Join(t.TempDir(), "seen.json")
	sender := &recordingMessageSender{}
	adsbdbClient := &recordingADSBDBClient{
		route: adsbdb.FlightRoute{
			Callsign: "ABC123",
			Origin: adsbdb.Airport{
				IATACode:     "YYT",
				Municipality: "St. John's",
			},
			Destination: adsbdb.Airport{
				IATACode:     "YYZ",
				Municipality: "Toronto",
			},
		},
	}
	mon := newTestMonitorWithOptions(
		t,
		server.URL,
		path,
		monitor.WithADSBDBClient(adsbdbClient),
		monitor.WithMessageSender(sender),
	)
	if err := mon.FetchAndCheck(context.Background()); err != nil {
		t.Fatalf("FetchAndCheck() error = %v", err)
	}

	if !reflect.DeepEqual(adsbdbClient.callsigns, []string{"ABC123"}) {
		t.Fatalf("adsbdb Callsign() callsigns = %#v, want ABC123", adsbdbClient.callsigns)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("sent message count = %d, want 1", len(sender.messages))
	}
	if sender.messages[0].Route == nil || sender.messages[0].Route.Callsign != "ABC123" {
		t.Fatalf("sent route = %#v, want ABC123 route", sender.messages[0].Route)
	}
}

func TestFetchAndCheckCorrectsKnownBadAirlineData(t *testing.T) {
	tests := []struct {
		name         string
		callsign     string
		callsignICAO *string
		wantAirline  adsbdb.Airline
	}{
		{
			name:     "air canada rouge",
			callsign: "ROU123",
			wantAirline: adsbdb.Airline{
				Name:       "Air Canada Rouge",
				ICAO:       "ROU",
				IATA:       new("RV"),
				Country:    "Canada",
				CountryISO: "CA",
				Callsign:   new("ROUGE"),
			},
		},
		{
			name:         "provincial airlines",
			callsign:     "PVL7682",
			callsignICAO: new("PVL7682"),
			wantAirline: adsbdb.Airline{
				Name:       "PAL Airlines",
				ICAO:       "PVL",
				IATA:       new("PB"),
				Country:    "Canada",
				CountryISO: "CA",
				Callsign:   new("PROVINCIAL"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := aircraftServer(
				t,
				http.StatusOK,
				`{"now":1,"messages":0,"aircraft":[{"hex":"abc123","flight":"`+tt.callsign+`  "}]}`,
			)
			defer server.Close()

			path := filepath.Join(t.TempDir(), "seen.json")
			sender := &recordingMessageSender{}
			adsbdbClient := &recordingADSBDBClient{
				route: adsbdb.FlightRoute{
					Callsign:     tt.callsign,
					CallsignICAO: tt.callsignICAO,
					Airline: &adsbdb.Airline{
						Name:       "Incorrect Airline",
						ICAO:       "BAD",
						Country:    "United States",
						CountryISO: "US",
					},
				},
			}
			mon := newTestMonitorWithOptions(
				t,
				server.URL,
				path,
				monitor.WithADSBDBClient(adsbdbClient),
				monitor.WithMessageSender(sender),
			)
			if err := mon.FetchAndCheck(context.Background()); err != nil {
				t.Fatalf("FetchAndCheck() error = %v", err)
			}

			if len(sender.messages) != 1 {
				t.Fatalf("sent message count = %d, want 1", len(sender.messages))
			}
			if sender.messages[0].Route == nil {
				t.Fatal("sent route is nil")
			}
			if !reflect.DeepEqual(sender.messages[0].Route.Airline, &tt.wantAirline) {
				t.Fatalf("sent airline = %#v, want %#v", sender.messages[0].Route.Airline, &tt.wantAirline)
			}
		})
	}
}

func TestFetchAndCheckWaitsForCallsignBeforePosting(t *testing.T) {
	server := aircraftSequenceServer(t, []aircraftResponse{
		{statusCode: http.StatusOK, body: `{"now":1,"messages":0,"aircraft":[{"hex":"abc123"}]}`},
		{
			statusCode: http.StatusOK,
			body:       `{"now":2,"messages":1,"aircraft":[{"hex":"abc123","flight":"ABC123  "}]}`,
		},
	})
	defer server.Close()

	path := filepath.Join(t.TempDir(), "seen.json")
	sender := &recordingMessageSender{}
	adsbdbClient := &recordingADSBDBClient{
		route: adsbdb.FlightRoute{
			Callsign: "ABC123",
			Origin: adsbdb.Airport{
				IATACode:     "YYT",
				Municipality: "St. John's",
			},
			Destination: adsbdb.Airport{
				IATACode:     "YYZ",
				Municipality: "Toronto",
			},
		},
	}
	mon := newTestMonitorWithConfigAndOptions(
		t,
		config.Config{
			Tar1090URL:           server.URL,
			MonitorInterval:      time.Minute,
			SeenAircraftPath:     path,
			CallsignWaitReceives: 3,
		},
		monitor.WithADSBDBClient(adsbdbClient),
		monitor.WithMessageSender(sender),
	)

	if err := mon.FetchAndCheck(context.Background()); err != nil {
		t.Fatalf("FetchAndCheck() first error = %v", err)
	}
	assertFileDoesNotExist(t, path)
	if len(sender.messages) != 0 {
		t.Fatalf("sent message count after first fetch = %d, want 0", len(sender.messages))
	}
	if len(adsbdbClient.identifiers) != 0 {
		t.Fatalf("adsbdb Aircraft() identifiers after first fetch = %#v, want none", adsbdbClient.identifiers)
	}

	if err := mon.FetchAndCheck(context.Background()); err != nil {
		t.Fatalf("FetchAndCheck() second error = %v", err)
	}
	assertSeenFile(t, path, map[string]bool{"abc123": true})
	if len(sender.messages) != 1 {
		t.Fatalf("sent message count after second fetch = %d, want 1", len(sender.messages))
	}
	if sender.messages[0].Aircraft.Flight != "ABC123  " {
		t.Fatalf("sent aircraft flight = %q, want ABC123 with padding", sender.messages[0].Aircraft.Flight)
	}
	if !reflect.DeepEqual(adsbdbClient.identifiers, []string{"abc123"}) {
		t.Fatalf("adsbdb Aircraft() identifiers = %#v, want abc123", adsbdbClient.identifiers)
	}
	if !reflect.DeepEqual(adsbdbClient.callsigns, []string{"ABC123"}) {
		t.Fatalf("adsbdb Callsign() callsigns = %#v, want ABC123", adsbdbClient.callsigns)
	}
}

func TestFetchAndCheckPreservesPendingAircraftIdentityWhenCallsignArrives(t *testing.T) {
	server := aircraftSequenceServer(t, []aircraftResponse{
		{
			statusCode: http.StatusOK,
			body: `{"now":1,"messages":0,"aircraft":[` +
				`{"hex":"c00841","r":"C-GSPR","t":"BE30","desc":"BEECH SUPER KING AIR 350","ownOp":"PAL Aerospace"}` +
				`]}`,
		},
		{
			statusCode: http.StatusOK,
			body:       `{"now":2,"messages":1,"aircraft":[{"hex":"c00841","flight":"SPR08"}]}`,
		},
	})
	defer server.Close()

	path := filepath.Join(t.TempDir(), "seen.json")
	sender := &recordingMessageSender{}
	mon := newTestMonitorWithConfigAndOptions(
		t,
		config.Config{
			Tar1090URL:           server.URL,
			MonitorInterval:      time.Minute,
			SeenAircraftPath:     path,
			CallsignWaitReceives: 3,
		},
		monitor.WithADSBDBClient(&recordingADSBDBClient{}),
		monitor.WithMessageSender(sender),
	)

	if err := mon.FetchAndCheck(context.Background()); err != nil {
		t.Fatalf("FetchAndCheck() first error = %v", err)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("sent message count after first fetch = %d, want 0", len(sender.messages))
	}

	if err := mon.FetchAndCheck(context.Background()); err != nil {
		t.Fatalf("FetchAndCheck() second error = %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("sent message count after second fetch = %d, want 1", len(sender.messages))
	}
	got := sender.messages[0].Aircraft
	if got.Flight != "SPR08" {
		t.Fatalf("sent aircraft flight = %q, want SPR08", got.Flight)
	}
	if got.AircraftType != "BE30" {
		t.Fatalf("sent aircraft type = %q, want BE30", got.AircraftType)
	}
	if got.Description != "BEECH SUPER KING AIR 350" {
		t.Fatalf("sent aircraft description = %q, want BEECH SUPER KING AIR 350", got.Description)
	}
	if got.Registration != "C-GSPR" {
		t.Fatalf("sent aircraft registration = %q, want C-GSPR", got.Registration)
	}
	if got.OwnOp != "PAL Aerospace" {
		t.Fatalf("sent aircraft operator = %q, want PAL Aerospace", got.OwnOp)
	}
}

func TestFetchAndCheckPostsWithoutCallsignAfterWaitReceives(t *testing.T) {
	server := aircraftSequenceServer(t, []aircraftResponse{
		{statusCode: http.StatusOK, body: `{"now":1,"messages":0,"aircraft":[{"hex":"abc123"}]}`},
		{statusCode: http.StatusOK, body: `{"now":2,"messages":1,"aircraft":[{"hex":"abc123"}]}`},
	})
	defer server.Close()

	path := filepath.Join(t.TempDir(), "seen.json")
	sender := &recordingMessageSender{}
	adsbdbClient := &recordingADSBDBClient{}
	mon := newTestMonitorWithConfigAndOptions(
		t,
		config.Config{
			Tar1090URL:           server.URL,
			MonitorInterval:      time.Minute,
			SeenAircraftPath:     path,
			CallsignWaitReceives: 2,
		},
		monitor.WithADSBDBClient(adsbdbClient),
		monitor.WithMessageSender(sender),
	)

	if err := mon.FetchAndCheck(context.Background()); err != nil {
		t.Fatalf("FetchAndCheck() first error = %v", err)
	}
	assertFileDoesNotExist(t, path)
	if len(sender.messages) != 0 {
		t.Fatalf("sent message count after first fetch = %d, want 0", len(sender.messages))
	}

	if err := mon.FetchAndCheck(context.Background()); err != nil {
		t.Fatalf("FetchAndCheck() second error = %v", err)
	}
	assertSeenFile(t, path, map[string]bool{"abc123": true})
	if len(sender.messages) != 1 {
		t.Fatalf("sent message count after second fetch = %d, want 1", len(sender.messages))
	}
	if sender.messages[0].Aircraft.Flight != "" {
		t.Fatalf("sent aircraft flight = %q, want empty", sender.messages[0].Aircraft.Flight)
	}
	if !reflect.DeepEqual(adsbdbClient.identifiers, []string{"abc123"}) {
		t.Fatalf("adsbdb Aircraft() identifiers = %#v, want abc123", adsbdbClient.identifiers)
	}
	if len(adsbdbClient.callsigns) != 0 {
		t.Fatalf("adsbdb Callsign() callsigns = %#v, want none", adsbdbClient.callsigns)
	}
}

func TestFetchAndCheckPostsPendingAircraftWhenNoLongerReceived(t *testing.T) {
	server := aircraftSequenceServer(t, []aircraftResponse{
		{
			statusCode: http.StatusOK,
			body:       `{"now":1,"messages":0,"aircraft":[{"hex":"abc123","r":"C-GABC","t":"B738"}]}`,
		},
		{statusCode: http.StatusOK, body: `{"now":2,"messages":1,"aircraft":[]}`},
	})
	defer server.Close()

	path := filepath.Join(t.TempDir(), "seen.json")
	sender := &recordingMessageSender{}
	adsbdbClient := &recordingADSBDBClient{}
	mon := newTestMonitorWithConfigAndOptions(
		t,
		config.Config{
			Tar1090URL:           server.URL,
			MonitorInterval:      time.Minute,
			SeenAircraftPath:     path,
			CallsignWaitReceives: 3,
		},
		monitor.WithADSBDBClient(adsbdbClient),
		monitor.WithMessageSender(sender),
	)

	if err := mon.FetchAndCheck(context.Background()); err != nil {
		t.Fatalf("FetchAndCheck() first error = %v", err)
	}
	assertFileDoesNotExist(t, path)
	if len(sender.messages) != 0 {
		t.Fatalf("sent message count after first fetch = %d, want 0", len(sender.messages))
	}

	if err := mon.FetchAndCheck(context.Background()); err != nil {
		t.Fatalf("FetchAndCheck() second error = %v", err)
	}
	assertSeenFile(t, path, map[string]bool{"abc123": true})
	if len(sender.messages) != 1 {
		t.Fatalf("sent message count after second fetch = %d, want 1", len(sender.messages))
	}
	if sender.messages[0].Aircraft.Registration != "C-GABC" {
		t.Fatalf("sent aircraft registration = %q, want C-GABC", sender.messages[0].Aircraft.Registration)
	}
	if !reflect.DeepEqual(adsbdbClient.identifiers, []string{"abc123"}) {
		t.Fatalf("adsbdb Aircraft() identifiers = %#v, want abc123", adsbdbClient.identifiers)
	}
	if len(adsbdbClient.callsigns) != 0 {
		t.Fatalf("adsbdb Callsign() callsigns = %#v, want none", adsbdbClient.callsigns)
	}
}

func TestFetchAndCheckSendsNewAircraftMessages(t *testing.T) {
	server := aircraftServer(
		t,
		http.StatusOK,
		`{"now":1,"messages":0,"aircraft":[{"hex":"abc123"}]}`,
	)
	defer server.Close()

	path := filepath.Join(t.TempDir(), "seen.json")
	sender := &recordingMessageSender{}
	adsbdbClient := &recordingADSBDBClient{
		aircraft: adsbdb.Aircraft{
			ModeS:           "abc123",
			Registration:    "C-GABC",
			RegisteredOwner: "Example Owner",
		},
	}
	mon := newTestMonitorWithOptions(
		t,
		server.URL,
		path,
		monitor.WithADSBDBClient(adsbdbClient),
		monitor.WithMessageSender(sender),
	)
	if err := mon.FetchAndCheck(context.Background()); err != nil {
		t.Fatalf("FetchAndCheck() error = %v", err)
	}

	if len(sender.messages) != 1 {
		t.Fatalf("sent message count = %d, want 1", len(sender.messages))
	}
	got := sender.messages[0]
	if got.Aircraft.Hex != "abc123" {
		t.Fatalf("sent aircraft hex = %q, want abc123", got.Aircraft.Hex)
	}
	if got.Details == nil || got.Details.Registration != "C-GABC" {
		t.Fatalf("sent aircraft details = %#v, want registration C-GABC", got.Details)
	}
}

func TestFetchAndCheckPersistsAircraftWhenEnhancementFails(t *testing.T) {
	server := aircraftServer(
		t,
		http.StatusOK,
		`{"now":1,"messages":0,"aircraft":[{"hex":"abc123"}]}`,
	)
	defer server.Close()

	path := filepath.Join(t.TempDir(), "seen.json")
	mon := newTestMonitorWithADSBDB(t, server.URL, path, &recordingADSBDBClient{
		err: errors.New("lookup failed"),
	})
	if err := mon.FetchAndCheck(context.Background()); err != nil {
		t.Fatalf("FetchAndCheck() error = %v", err)
	}

	assertSeenFile(t, path, map[string]bool{"abc123": true})
}

func TestFetchAndCheckSendsMessageWhenEnhancementFails(t *testing.T) {
	server := aircraftServer(
		t,
		http.StatusOK,
		`{"now":1,"messages":0,"aircraft":[{"hex":"abc123"}]}`,
	)
	defer server.Close()

	path := filepath.Join(t.TempDir(), "seen.json")
	sender := &recordingMessageSender{}
	mon := newTestMonitorWithOptions(
		t,
		server.URL,
		path,
		monitor.WithADSBDBClient(&recordingADSBDBClient{err: errors.New("lookup failed")}),
		monitor.WithMessageSender(sender),
	)
	if err := mon.FetchAndCheck(context.Background()); err != nil {
		t.Fatalf("FetchAndCheck() error = %v", err)
	}

	if len(sender.messages) != 1 {
		t.Fatalf("sent message count = %d, want 1", len(sender.messages))
	}
	if sender.messages[0].Details != nil {
		t.Fatalf("sent aircraft details = %#v, want nil", sender.messages[0].Details)
	}
}

func TestFetchAndCheckSendsMessageWhenRouteLookupFails(t *testing.T) {
	server := aircraftServer(
		t,
		http.StatusOK,
		`{"now":1,"messages":0,"aircraft":[{"hex":"abc123","flight":"ABC123"}]}`,
	)
	defer server.Close()

	path := filepath.Join(t.TempDir(), "seen.json")
	sender := &recordingMessageSender{}
	mon := newTestMonitorWithOptions(
		t,
		server.URL,
		path,
		monitor.WithADSBDBClient(&recordingADSBDBClient{routeErr: errors.New("route failed")}),
		monitor.WithMessageSender(sender),
	)
	if err := mon.FetchAndCheck(context.Background()); err != nil {
		t.Fatalf("FetchAndCheck() error = %v", err)
	}

	if len(sender.messages) != 1 {
		t.Fatalf("sent message count = %d, want 1", len(sender.messages))
	}
	if sender.messages[0].Route != nil {
		t.Fatalf("sent route = %#v, want nil", sender.messages[0].Route)
	}
}

func TestFetchAndCheckReturnsSendErrorWithoutWritingState(t *testing.T) {
	server := aircraftServer(
		t,
		http.StatusOK,
		`{"now":1,"messages":0,"aircraft":[{"hex":"abc123"}]}`,
	)
	defer server.Close()

	path := filepath.Join(t.TempDir(), "seen.json")
	mon := newTestMonitorWithOptions(
		t,
		server.URL,
		path,
		monitor.WithADSBDBClient(&recordingADSBDBClient{}),
		monitor.WithMessageSender(&recordingMessageSender{err: errors.New("send failed")}),
	)
	if err := mon.FetchAndCheck(context.Background()); err == nil {
		t.Fatal("FetchAndCheck() error = nil, want error")
	}

	assertFileDoesNotExist(t, path)
}

func TestFetchAndCheckPersistsSuccessfulAircraftBeforeLaterSendError(t *testing.T) {
	server := aircraftServer(
		t,
		http.StatusOK,
		`{"now":1,"messages":0,"aircraft":[{"hex":"abc123"},{"hex":"def456"}]}`,
	)
	defer server.Close()

	path := filepath.Join(t.TempDir(), "seen.json")
	mon := newTestMonitorWithOptions(
		t,
		server.URL,
		path,
		monitor.WithADSBDBClient(&recordingADSBDBClient{}),
		monitor.WithMessageSender(&recordingMessageSender{
			errAfterMessages: 1,
			err:              errors.New("send failed"),
		}),
	)
	if err := mon.FetchAndCheck(context.Background()); err == nil {
		t.Fatal("FetchAndCheck() error = nil, want error")
	}

	assertSeenFile(t, path, map[string]bool{"abc123": true})
}

func TestFetchAndCheckIgnoresEmptyHex(t *testing.T) {
	server := aircraftServer(t, http.StatusOK, `{"now":1,"messages":0,"aircraft":[{"hex":""}]}`)
	defer server.Close()

	path := filepath.Join(t.TempDir(), "seen.json")
	mon := newTestMonitor(t, server.URL, path)
	if err := mon.FetchAndCheck(context.Background()); err != nil {
		t.Fatalf("FetchAndCheck() error = %v", err)
	}

	assertFileDoesNotExist(t, path)
}

func TestFetchAndCheckReturnsFetchErrorWithoutWritingState(t *testing.T) {
	server := aircraftServer(t, http.StatusInternalServerError, "fetch failed")
	defer server.Close()

	path := filepath.Join(t.TempDir(), "seen.json")
	mon := newTestMonitor(t, server.URL, path)
	if err := mon.FetchAndCheck(context.Background()); err == nil {
		t.Fatal("FetchAndCheck() error = nil, want error")
	}

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state file exists after fetch error, stat error = %v", err)
	}
}

func TestNewReturnsErrorForInvalidSeenJSON(t *testing.T) {
	server := aircraftServer(t, http.StatusOK, `{"now":1,"messages":0,"aircraft":[]}`)
	defer server.Close()

	path := filepath.Join(t.TempDir(), "seen.json")
	if err := os.WriteFile(path, []byte(`{`), 0o644); err != nil {
		t.Fatalf("write invalid seen fixture: %v", err)
	}

	_, err := monitor.New(config.Config{
		Tar1090URL:       server.URL,
		MonitorInterval:  time.Minute,
		SeenAircraftPath: path,
	})
	if err == nil {
		t.Fatal("New() error = nil, want error")
	}
}

func TestRunFetchesImmediately(t *testing.T) {
	server := aircraftServer(
		t,
		http.StatusOK,
		`{"now":1,"messages":0,"aircraft":[{"hex":"abc123"}]}`,
	)
	defer server.Close()

	path := filepath.Join(t.TempDir(), "seen.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mon := newTestMonitor(t, server.URL, path)

	errc := make(chan error, 1)
	go func() {
		errc <- mon.Run(ctx)
	}()

	deadline := time.After(time.Second)
	for {
		if seenFileMatches(path, map[string]bool{"abc123": true}) {
			cancel()
			break
		}

		select {
		case err := <-errc:
			t.Fatalf("Run() returned before first poll was persisted: %v", err)
		case <-deadline:
			t.Fatal("timed out waiting for first poll to be persisted")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want %v", err, context.Canceled)
	}
}

func newTestMonitor(t *testing.T, tar1090URL string, path string) *monitor.Monitor {
	t.Helper()

	return newTestMonitorWithADSBDB(t, tar1090URL, path, &recordingADSBDBClient{})
}

func newTestMonitorWithADSBDB(
	t *testing.T,
	tar1090URL string,
	path string,
	adsbdbClient *recordingADSBDBClient,
) *monitor.Monitor {
	t.Helper()

	return newTestMonitorWithOptions(
		t,
		tar1090URL,
		path,
		monitor.WithADSBDBClient(adsbdbClient),
	)
}

func newTestMonitorWithOptions(
	t *testing.T,
	tar1090URL string,
	path string,
	opts ...monitor.Option,
) *monitor.Monitor {
	t.Helper()

	return newTestMonitorWithConfigAndOptions(t, config.Config{
		Tar1090URL:           tar1090URL,
		MonitorInterval:      time.Minute,
		CallsignWaitReceives: 0,
		SeenAircraftPath:     path,
	}, opts...)
}

func newTestMonitorWithConfigAndOptions(
	t *testing.T,
	cfg config.Config,
	opts ...monitor.Option,
) *monitor.Monitor {
	t.Helper()

	monitor, err := monitor.New(cfg, opts...)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return monitor
}

type aircraftResponse struct {
	statusCode int
	body       string
}

type recordingADSBDBClient struct {
	identifiers []string
	callsigns   []string
	aircraft    adsbdb.Aircraft
	route       adsbdb.FlightRoute
	err         error
	routeErr    error
}

func (c *recordingADSBDBClient) Aircraft(_ context.Context, identifier string) (adsbdb.Aircraft, error) {
	c.identifiers = append(c.identifiers, identifier)
	return c.aircraft, c.err
}

func (c *recordingADSBDBClient) Callsign(_ context.Context, callsign string) (adsbdb.FlightRoute, error) {
	c.callsigns = append(c.callsigns, callsign)
	return c.route, c.routeErr
}

type recordingMessageSender struct {
	messages         []messaging.AircraftMessage
	errAfterMessages int
	err              error
}

func (s *recordingMessageSender) SendAircraft(_ context.Context, message messaging.AircraftMessage) error {
	s.messages = append(s.messages, message)
	if s.errAfterMessages > 0 {
		if len(s.messages) > s.errAfterMessages {
			return s.err
		}
		return nil
	}
	return s.err
}

func aircraftServer(t *testing.T, statusCode int, body string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}))
}

func aircraftSequenceServer(t *testing.T, responses []aircraftResponse) *httptest.Server {
	t.Helper()

	requestCount := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		response := responses[requestCount]
		if requestCount < len(responses)-1 {
			requestCount++
		}

		w.WriteHeader(response.statusCode)
		_, _ = w.Write([]byte(response.body))
	}))
}

func writeSeenFixture(t *testing.T, path string, seenAircraft map[string]bool) {
	t.Helper()

	data, err := json.Marshal(seenAircraft)
	if err != nil {
		t.Fatalf("marshal seen fixture: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write seen fixture: %v", err)
	}
}

func assertSeenFile(t *testing.T, path string, want map[string]bool) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read seen file: %v", err)
	}

	var got map[string]bool
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode seen file: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("seen file length = %d, want %d; got %#v", len(got), len(want), got)
	}
	for hex, wantSeen := range want {
		if got[hex] != wantSeen {
			t.Fatalf("seen file[%q] = %v, want %v; got %#v", hex, got[hex], wantSeen, got)
		}
	}
}

func assertFileDoesNotExist(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file exists, stat error = %v", err)
	}
}

func seenFileMatches(path string, want map[string]bool) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	var got map[string]bool
	if err := json.Unmarshal(data, &got); err != nil {
		return false
	}

	if len(got) != len(want) {
		return false
	}
	for hex, wantSeen := range want {
		if got[hex] != wantSeen {
			return false
		}
	}

	return true
}
