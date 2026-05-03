package messaging_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	adsbdb "github.com/nint8835/go-adsbdb"

	"github.com/nint8835/planespotter/pkg/messaging"
	"github.com/nint8835/planespotter/pkg/tar1090"
)

func TestDiscordSenderSendsAircraftMessageToThread(t *testing.T) {
	var gotThreadID string
	var gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		gotThreadID = r.URL.Query().Get("thread_id")
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sender, err := messaging.NewDiscordSender(server.URL, "123456")
	if err != nil {
		t.Fatalf("NewDiscordSender() error = %v", err)
	}

	err = sender.SendAircraft(context.Background(), messaging.AircraftMessage{
		Aircraft: tar1090.Aircraft{
			Hex:          "c12345",
			Flight:       "ABC123",
			Registration: "C-GABC",
			AircraftType: "B738",
			Description:  "BOEING 737-800",
		},
		Route: &adsbdb.FlightRoute{
			Airline: &adsbdb.Airline{Name: "Example Air"},
			Origin: adsbdb.Airport{
				IATACode:     "YYT",
				Municipality: "St. John's",
			},
			Destination: adsbdb.Airport{
				IATACode:     "YYZ",
				Municipality: "Toronto",
			},
		},
	})
	if err != nil {
		t.Fatalf("SendAircraft() error = %v", err)
	}

	if gotThreadID != "123456" {
		t.Fatalf("thread_id = %q, want 123456", gotThreadID)
	}
	embeds, ok := gotPayload["embeds"].([]any)
	if !ok || len(embeds) != 1 {
		t.Fatalf("embeds = %#v, want one embed", gotPayload["embeds"])
	}
	embed, ok := embeds[0].(map[string]any)
	if !ok {
		t.Fatalf("embed = %#v, want object", embeds[0])
	}
	author, ok := embed["author"].(map[string]any)
	if !ok {
		t.Fatalf("author = %#v, want object", embed["author"])
	}
	if author["name"] != "New aircraft spotted" {
		t.Fatalf("author name = %#v, want new aircraft label", author["name"])
	}
	if embed["title"] != "ABC123" {
		t.Fatalf("embed title = %#v, want flight title", embed["title"])
	}
	if _, ok := embed["description"]; ok {
		t.Fatalf("description = %#v, want no description", embed["description"])
	}
	fields, ok := embed["fields"].([]any)
	if !ok {
		t.Fatalf("fields = %#v, want list", embed["fields"])
	}
	assertEmbedField(t, fields, "Aircraft", "C-GABC · B738 (BOEING 737-800)")
	assertEmbedField(t, fields, "Operator", "Example Air")
	assertEmbedField(t, fields, "Route", "YYT (St. John's) -> YYZ (Toronto)")
	footer, ok := embed["footer"].(map[string]any)
	if !ok {
		t.Fatalf("footer = %#v, want object", embed["footer"])
	}
	if footer["text"] != "Mode S C12345" {
		t.Fatalf("footer text = %#v, want Mode S C12345", footer["text"])
	}
	allowedMentions, ok := gotPayload["allowed_mentions"].(map[string]any)
	if !ok {
		t.Fatalf("allowed_mentions = %#v, want object", gotPayload["allowed_mentions"])
	}
	if len(allowedMentions) != 0 {
		t.Fatalf("allowed_mentions = %#v, want no mention parse values", allowedMentions)
	}
}

func assertEmbedField(t *testing.T, fields []any, name string, value string) {
	t.Helper()

	for _, rawField := range fields {
		field, ok := rawField.(map[string]any)
		if !ok {
			t.Fatalf("field = %#v, want object", rawField)
		}
		if field["name"] == name {
			if field["value"] != value {
				t.Fatalf("%s field value = %#v, want %q", name, field["value"], value)
			}
			if _, ok := field["inline"]; ok {
				t.Fatalf("%s field inline = %#v, want omitted", name, field["inline"])
			}
			return
		}
	}

	t.Fatalf("missing %s field in %#v", name, fields)
}

func TestDiscordSenderReturnsErrorForNonSuccessResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"bad webhook"}`))
	}))
	defer server.Close()

	sender, err := messaging.NewDiscordSender(server.URL, "")
	if err != nil {
		t.Fatalf("NewDiscordSender() error = %v", err)
	}

	err = sender.SendAircraft(context.Background(), messaging.AircraftMessage{
		Aircraft: tar1090.Aircraft{Hex: "c12345"},
	})
	if err == nil {
		t.Fatal("SendAircraft() error = nil, want error")
	}
}
