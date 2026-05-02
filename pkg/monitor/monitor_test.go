package monitor_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	adsbdb "github.com/nint8835/go-adsbdb"

	"github.com/nint8835/planespotter/pkg/config"
	"github.com/nint8835/planespotter/pkg/monitor"
)

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

	monitor, err := monitor.New(config.Config{
		Tar1090URL:       tar1090URL,
		MonitorInterval:  time.Minute,
		SeenAircraftPath: path,
	}, monitor.WithADSBDBClient(adsbdbClient))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return monitor
}

type recordingADSBDBClient struct {
	identifiers []string
	err         error
}

func (c *recordingADSBDBClient) Aircraft(_ context.Context, identifier string) (adsbdb.Aircraft, error) {
	c.identifiers = append(c.identifiers, identifier)
	return adsbdb.Aircraft{}, c.err
}

func aircraftServer(t *testing.T, statusCode int, body string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
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
