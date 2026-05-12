package ccar_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"

	"github.com/nint8835/planespotter/pkg/ccar"
)

const (
	currentFile = "carscurr.txt"
	ownerFile   = "carsownr.txt"
)

func TestLookupFindsRecordByRegistrationAndModeS(t *testing.T) {
	cacheDir := t.TempDir()
	writeTestDatabase(t, cacheDir, "TST1", "101010111100110111101111", "Example Registry Owner Ltd.")

	client, err := ccar.NewClient(cacheDir)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	record, err := client.Lookup(context.Background(), "C-TST1", "")
	if err != nil {
		t.Fatalf("Lookup() by registration error = %v", err)
	}
	if record == nil {
		t.Fatal("Lookup() by registration = nil, want record")
	}
	if record.Registration() != "C-TST1" {
		t.Fatalf("Registration() = %q, want C-TST1", record.Registration())
	}
	if record.OwnerName() != "Example Registry Owner Ltd." {
		t.Fatalf("OwnerName() = %q, want Example Registry Owner Ltd.", record.OwnerName())
	}

	record, err = client.Lookup(context.Background(), "", "abcdef")
	if err != nil {
		t.Fatalf("Lookup() by Mode S error = %v", err)
	}
	if record == nil || record.Registration() != "C-TST1" {
		t.Fatalf("Lookup() by Mode S = %#v, want TST1 record", record)
	}
}

func writeTestDatabase(t *testing.T, cacheDir string, mark string, modeSBinary string, ownerName string) {
	t.Helper()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("create cache dir: %v", err)
	}
	aircraftData := []byte(csvLine(aircraftRow(mark, modeSBinary)) + "\n1 rows selected.\n\x1a")
	if err := os.WriteFile(filepath.Join(cacheDir, currentFile), aircraftData, 0o644); err != nil {
		t.Fatalf("write current file: %v", err)
	}
	ownerData := []byte(csvLine(ownerRow(mark, ownerName)) + "\n1 rows selected.\n\x1a")
	if err := os.WriteFile(filepath.Join(cacheDir, ownerFile), ownerData, 0o644); err != nil {
		t.Fatalf("write owner file: %v", err)
	}
}

func aircraftRow(mark string, modeSBinary string) []string {
	row := make([]string, 47)
	row[0] = mark
	row[1] = "Continuing Registration"
	row[3] = "Example"
	row[4] = "MODEL-1"
	row[5] = "TEST-001"
	row[7] = "EXAMPLE AIRCRAFT CO."
	row[10] = "Aeroplane"
	row[15] = "Piston"
	row[17] = "1"
	row[19] = "1200"
	row[21] = "2026/01/02"
	row[22] = "2026/01/02"
	row[24] = "Private"
	row[26] = "Certificate of Airworthiness"
	row[29] = "CANADA"
	row[32] = "CANADA"
	row[34] = "Ontario"
	row[36] = "Exampleville"
	row[38] = "Registered"
	row[41] = "2026/01/02"
	row[42] = modeSBinary
	row[46] = mark
	return row
}

func ownerRow(mark string, ownerName string) []string {
	row := make([]string, 20)
	row[0] = mark
	row[1] = ownerName
	row[5] = "Exampleville"
	row[6] = "Ontario"
	row[8] = "A1A1A1"
	row[9] = "CANADA"
	row[11] = "Entity"
	row[13] = "A"
	row[15] = "Ontario"
	row[17] = ownerName
	row[18] = "Y"
	row[19] = mark
	return row
}

func csvLine(row []string) string {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	if err := writer.Write(row); err != nil {
		panic(err)
	}
	writer.Flush()
	return buffer.String()
}
