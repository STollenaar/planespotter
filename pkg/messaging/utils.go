package messaging

import (
	"fmt"
	"strings"

	adsbdb "github.com/nint8835/go-adsbdb"

	"github.com/nint8835/planespotter/pkg/tar1090"
)

func routeDescription(route *adsbdb.FlightRoute) string {
	if route == nil {
		return ""
	}

	airports := []string{airport(route.Origin)}
	if route.Midpoint != nil {
		airports = append(airports, airport(*route.Midpoint))
	}
	airports = append(airports, airport(route.Destination))

	return strings.Join(airports, " -> ")
}

func airport(airport adsbdb.Airport) string {
	code := firstNonEmpty(airport.IATACode, airport.ICAOCode)
	if code == "" {
		return airport.Name
	}
	if airport.Municipality == "" {
		return code
	}
	return fmt.Sprintf("%s (%s)", code, airport.Municipality)
}

func airline(route *adsbdb.FlightRoute) string {
	if route == nil || route.Airline == nil {
		return ""
	}
	return route.Airline.Name
}

func detailRegistration(details *adsbdb.Aircraft) string {
	if details == nil {
		return ""
	}
	return details.Registration
}

func detailType(details *adsbdb.Aircraft) string {
	if details == nil {
		return ""
	}
	return firstNonEmpty(details.ICAOType, details.Type, details.Manufacturer)
}

func detailOwner(details *adsbdb.Aircraft) string {
	if details == nil {
		return ""
	}
	return details.RegisteredOwner
}

func identityLine(aircraft tar1090.Aircraft, details *adsbdb.Aircraft) string {
	return strings.Join(nonEmptyValues(
		firstNonEmpty(aircraft.Registration, detailRegistration(details)),
		typeDescription(aircraft, details),
	), " · ")
}

func typeDescription(aircraft tar1090.Aircraft, details *adsbdb.Aircraft) string {
	aircraftType := firstNonEmpty(aircraft.AircraftType, detailType(details))
	if aircraft.Description == "" {
		return aircraftType
	}
	if aircraftType == "" {
		return aircraft.Description
	}
	return fmt.Sprintf("%s (%s)", aircraftType, aircraft.Description)
}

func nonEmptyValues(values ...string) []string {
	var nonEmpty []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			nonEmpty = append(nonEmpty, value)
		}
	}
	return nonEmpty
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
