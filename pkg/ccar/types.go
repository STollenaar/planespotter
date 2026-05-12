package ccar

import (
	"fmt"
	"strconv"
	"strings"
)

// Record is an aircraft record from CARSCURR.TXT with associated owner rows from CARSOWNR.TXT.
type Record struct {
	mark                         string
	registrationSubType          string
	commonName                   string
	modelName                    string
	manufacturersSerialNumber    string
	idPlateManufacturersName     string
	basisForRegistration         string
	aircraftCategory             string
	dateOfImport                 string
	engineManufacturer           string
	powergliderFlag              string
	engineCategory               string
	numberOfEngines              string
	numberOfSeats                string
	aircraftWeightKilos          string
	saleReported                 string
	issueDate                    string
	effectiveDate                string
	ineffectiveDate              string
	registeredPurpose            string
	flightAuthority              string
	manufactureOrAssembly        string
	countryManufactureOrAssembly string
	dateManufactureAssembly      string
	baseOfOperationsCountry      string
	baseProvinceOrState          string
	cityAirport                  string
	typeCertificateNumber        string
	registrationAuthStatus       string
	multipleOwnerFlag            string
	modifiedDate                 string
	modeSTransponderBinary       string
	physicalFileRegion           string
	exMilitaryMark               string
	trimmedMark                  string
	owners                       []owner
}

// Registration returns the Canadian registration with the C- prefix restored.
func (r Record) Registration() string {
	mark := strings.TrimSpace(r.mark)
	if mark == "" {
		mark = strings.TrimSpace(r.trimmedMark)
	}
	if mark == "" {
		return ""
	}
	return "C-" + mark
}

// ModeSHex returns the hexadecimal Mode S code for the aircraft when available.
func (r Record) ModeSHex() string {
	binary := strings.TrimSpace(r.modeSTransponderBinary)
	if binary == "" {
		return ""
	}
	value, err := strconv.ParseUint(binary, 2, 32)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%06X", value)
}

// OwnerName returns the preferred active owner name for display.
func (r Record) OwnerName() string {
	for _, owner := range r.owners {
		if owner.activeFlag == "A" {
			if name := owner.DisplayName(); name != "" {
				return name
			}
		}
	}
	for _, owner := range r.owners {
		if name := owner.DisplayName(); name != "" {
			return name
		}
	}
	return ""
}

type owner struct {
	mark               string
	fullName           string
	tradeName          string
	streetName         string
	streetName2        string
	city               string
	provinceOrState    string
	postalCode         string
	country            string
	typeOfOwner        string
	activeFlag         string
	careOf             string
	region             string
	ownerNameOldFormat string
	mailRecipient      string
	trimmedMark        string
}

// DisplayName returns the best public owner name from the owner row.
func (o owner) DisplayName() string {
	if name := strings.TrimSpace(o.fullName); name != "" {
		return name
	}
	if name := strings.TrimSpace(o.tradeName); name != "" {
		return name
	}
	return strings.TrimSpace(o.ownerNameOldFormat)
}
