package monitor

import (
	"strings"

	adsbdb "github.com/nint8835/go-adsbdb"
)

// adsbdb contains some incorrect airline data. This has been reported, but
// in the meantime manually apply the corrections ourselves.
var airlineCorrections = map[string]adsbdb.Airline{
	"PVL": {
		Name:       "PAL Airlines",
		ICAO:       "PVL",
		IATA:       new("PB"),
		Country:    "Canada",
		CountryISO: "CA",
		Callsign:   new("PROVINCIAL"),
	},
	"ROU": {
		Name:       "Air Canada Rouge",
		ICAO:       "ROU",
		IATA:       new("RV"),
		Country:    "Canada",
		CountryISO: "CA",
		Callsign:   new("ROUGE"),
	},
}

func correctAirline(callsign string, route *adsbdb.FlightRoute) {
	if route == nil {
		return
	}

	code := callsignAirlineCode(callsign)
	if code == "" {
		return
	}

	correction, ok := airlineCorrections[code]
	if !ok {
		return
	}

	route.Airline = &correction
}

func callsignAirlineCode(callsign string) string {
	callsign = strings.ToUpper(strings.TrimSpace(callsign))
	if len(callsign) < 3 {
		return ""
	}
	return callsign[:3]
}
