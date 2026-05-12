package ccar

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultDownloadURL     = "https://wwwapps.tc.gc.ca/Saf-Sec-Sur/2/CCARCS-RIACC/download/ccarcsdb.zip"
	defaultRefreshInterval = 14 * 24 * time.Hour

	currentFile = "carscurr.txt"
	ownerFile   = "carsownr.txt"
)

// Client looks up aircraft records in a cached copy of the Canadian Civil Aircraft Registry.
type Client struct {
	cacheDir        string
	downloadURL     string
	refreshInterval time.Duration
	httpClient      *http.Client
	now             func() time.Time

	mu      sync.Mutex
	loaded  bool
	byMark  map[string]*Record
	byModeS map[string]*Record
}

// NewClient creates a CCAR client using cacheDir as its on-disk database location.
func NewClient(cacheDir string) (*Client, error) {
	cacheDir = strings.TrimSpace(cacheDir)
	if cacheDir == "" {
		return nil, fmt.Errorf("ccar cache directory is required")
	}

	client := &Client{
		cacheDir:        cacheDir,
		downloadURL:     defaultDownloadURL,
		refreshInterval: defaultRefreshInterval,
		httpClient:      http.DefaultClient,
		now:             time.Now,
	}

	return client, nil
}

// Lookup returns a CCAR record by Canadian registration or Mode S hex.
func (c *Client) Lookup(ctx context.Context, registration string, modeSHex string) (*Record, error) {
	if err := c.ensureLoaded(ctx); err != nil {
		return nil, fmt.Errorf("load CCAR database: %w", err)
	}

	if mark := markFromRegistration(registration); mark != "" {
		if record := c.byMark[mark]; record != nil {
			return record, nil
		}
	}
	if modeSHex = normalizeModeSHex(modeSHex); modeSHex != "" {
		if record := c.byModeS[modeSHex]; record != nil {
			return record, nil
		}
	}

	return nil, nil
}

func (c *Client) ensureLoaded(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.loaded {
		if c.cacheIsFresh() {
			return nil
		}
		if err := c.download(ctx); err != nil {
			slog.WarnContext(ctx, "Using loaded CCAR database after refresh failed", "cache_dir", c.cacheDir, "error", err)
			return nil
		}
	}

	if !c.loaded {
		if err := c.ensureFreshCache(ctx); err != nil {
			if !databaseFilesExist(c.cacheDir) {
				return fmt.Errorf("refresh CCAR database: %w", err)
			}
			slog.WarnContext(ctx, "Using stale CCAR database after refresh failed", "cache_dir", c.cacheDir, "error", err)
		}
	}

	if err := c.reloadRecords(); err != nil {
		if c.loaded {
			slog.WarnContext(ctx, "Keeping loaded CCAR database after reload failed", "cache_dir", c.cacheDir, "error", err)
			return nil
		}
		return fmt.Errorf("reload CCAR records: %w", err)
	}

	return nil
}

func (c *Client) reloadRecords() error {
	records, err := loadRecords(c.cacheDir)
	if err != nil {
		return fmt.Errorf("load CCAR records: %w", err)
	}

	byMark := make(map[string]*Record, len(records))
	byModeS := make(map[string]*Record, len(records))
	for i := range records {
		record := &records[i]
		byMark[record.mark] = record
		if modeSHex := record.ModeSHex(); modeSHex != "" {
			byModeS[modeSHex] = record
		}
	}
	c.byMark = byMark
	c.byModeS = byModeS
	c.loaded = true

	return nil
}

func (c *Client) cacheIsFresh() bool {
	if !databaseFilesExist(c.cacheDir) {
		return false
	}
	updatedAt, err := cacheUpdatedAt(c.cacheDir)
	return err == nil && c.now().Sub(updatedAt) < c.refreshInterval
}

func (c *Client) ensureFreshCache(ctx context.Context) error {
	if c.cacheIsFresh() {
		return nil
	}

	slog.InfoContext(ctx, "Downloading CCAR database", "url", c.downloadURL, "cache_dir", c.cacheDir)
	return c.download(ctx)
}

func (c *Client) download(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.downloadURL, nil)
	if err != nil {
		return fmt.Errorf("create CCAR download request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download CCAR database: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("download CCAR database: unexpected status %s", resp.Status)
	}

	tmp, err := os.CreateTemp("", "ccarcsdb-*.zip")
	if err != nil {
		return fmt.Errorf("create temporary CCAR zip: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary CCAR zip: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary CCAR zip: %w", err)
	}

	if err := extractDatabase(tmpPath, c.cacheDir); err != nil {
		return fmt.Errorf("extract CCAR database: %w", err)
	}
	return nil
}

func databaseFilesExist(cacheDir string) bool {
	for _, name := range []string{currentFile, ownerFile} {
		if info, err := os.Stat(filepath.Join(cacheDir, name)); err != nil || info.IsDir() {
			return false
		}
	}
	return true
}

func cacheUpdatedAt(cacheDir string) (time.Time, error) {
	var updatedAt time.Time
	for _, name := range []string{currentFile, ownerFile} {
		info, err := os.Stat(filepath.Join(cacheDir, name))
		if err != nil {
			return time.Time{}, fmt.Errorf("stat CCAR file %s: %w", name, err)
		}
		if updatedAt.IsZero() || info.ModTime().Before(updatedAt) {
			updatedAt = info.ModTime()
		}
	}
	if updatedAt.IsZero() {
		return time.Time{}, fmt.Errorf("ccar cache timestamp unavailable")
	}
	return updatedAt, nil
}

func extractDatabase(zipPath string, cacheDir string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open CCAR zip: %w", err)
	}
	defer func() {
		_ = reader.Close()
	}()

	if err := os.MkdirAll(filepath.Dir(cacheDir), 0o755); err != nil {
		return fmt.Errorf("create CCAR cache parent directory: %w", err)
	}
	tmpDir, err := os.MkdirTemp(filepath.Dir(cacheDir), ".ccarcsdb-*")
	if err != nil {
		return fmt.Errorf("create temporary CCAR directory: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	found := map[string]bool{}
	for _, file := range reader.File {
		name := strings.ToLower(filepath.Base(file.Name))
		if name != currentFile && name != ownerFile {
			continue
		}
		if err := extractZipFile(file, filepath.Join(tmpDir, name)); err != nil {
			return fmt.Errorf("extract CCAR zip entry %s: %w", file.Name, err)
		}
		found[name] = true
	}
	for _, name := range []string{currentFile, ownerFile} {
		if !found[name] {
			return fmt.Errorf("CCAR zip missing %s", name)
		}
	}

	backupDir := cacheDir + ".old"
	_ = os.RemoveAll(backupDir)
	if err := os.Rename(cacheDir, backupDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("replace CCAR cache: %w", err)
	}
	if err := os.Rename(tmpDir, cacheDir); err != nil {
		if restoreErr := os.Rename(backupDir, cacheDir); restoreErr != nil && !errors.Is(restoreErr, os.ErrNotExist) {
			return fmt.Errorf("install CCAR cache: %w; restore previous cache: %w", err, restoreErr)
		}
		return fmt.Errorf("install CCAR cache: %w", err)
	}
	_ = os.RemoveAll(backupDir)

	return nil
}

func extractZipFile(file *zip.File, destination string) error {
	source, err := file.Open()
	if err != nil {
		return fmt.Errorf("open CCAR zip entry %s: %w", file.Name, err)
	}
	defer func() {
		_ = source.Close()
	}()

	destinationFile, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create CCAR file %s: %w", destination, err)
	}
	defer func() {
		_ = destinationFile.Close()
	}()

	if _, err := io.Copy(destinationFile, source); err != nil {
		return fmt.Errorf("extract CCAR file %s: %w", file.Name, err)
	}

	return nil
}

func loadRecords(cacheDir string) ([]Record, error) {
	recordsByMark := map[string]*Record{}
	if err := readCSV(filepath.Join(cacheDir, currentFile), func(row []string) error {
		record, err := aircraftRecord(row)
		if err != nil {
			return fmt.Errorf("parse aircraft record: %w", err)
		}
		recordsByMark[record.mark] = record
		return nil
	}); err != nil {
		return nil, err
	}

	if err := readCSV(filepath.Join(cacheDir, ownerFile), func(row []string) error {
		owner, err := ownerRecord(row)
		if err != nil {
			return fmt.Errorf("parse owner record: %w", err)
		}
		if record := recordsByMark[owner.mark]; record != nil {
			record.owners = append(record.owners, *owner)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	records := make([]Record, 0, len(recordsByMark))
	for _, record := range recordsByMark {
		records = append(records, *record)
	}
	return records, nil
}

func readCSV(path string, handle func([]string) error) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open CCAR file %s: %w", path, err)
	}
	defer func() {
		_ = file.Close()
	}()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read CCAR file %s: %w", path, err)
		}
		for i := range row {
			row[i] = cleanValue(row[i])
		}
		if skipExportLine(row) {
			continue
		}
		if err := handle(row); err != nil {
			return fmt.Errorf("parse CCAR file %s: %w", path, err)
		}
	}

	return nil
}

func cleanValue(value string) string {
	value = strings.ReplaceAll(value, "\x00", "")
	value = strings.Trim(value, "\x1a")
	value = strings.TrimSpace(value)
	return strings.ToValidUTF8(value, "")
}

func skipExportLine(row []string) bool {
	if len(row) == 0 {
		return true
	}
	if len(row) != 1 {
		return false
	}

	value := strings.TrimSpace(row[0])
	if value == "" {
		return true
	}
	return strings.HasSuffix(value, " rows selected.")
}

func aircraftRecord(row []string) (*Record, error) {
	if len(row) < 47 {
		return nil, fmt.Errorf("aircraft row has %d fields, want at least 47", len(row))
	}

	return &Record{
		mark:                         row[0],
		registrationSubType:          row[1],
		commonName:                   row[3],
		modelName:                    row[4],
		manufacturersSerialNumber:    row[5],
		idPlateManufacturersName:     row[7],
		basisForRegistration:         row[8],
		aircraftCategory:             row[10],
		dateOfImport:                 row[12],
		engineManufacturer:           row[13],
		powergliderFlag:              row[14],
		engineCategory:               row[15],
		numberOfEngines:              row[17],
		numberOfSeats:                row[18],
		aircraftWeightKilos:          row[19],
		saleReported:                 row[20],
		issueDate:                    row[21],
		effectiveDate:                row[22],
		ineffectiveDate:              row[23],
		registeredPurpose:            row[24],
		flightAuthority:              row[26],
		manufactureOrAssembly:        row[28],
		countryManufactureOrAssembly: row[29],
		dateManufactureAssembly:      row[31],
		baseOfOperationsCountry:      row[32],
		baseProvinceOrState:          row[34],
		cityAirport:                  row[36],
		typeCertificateNumber:        row[37],
		registrationAuthStatus:       row[38],
		multipleOwnerFlag:            row[40],
		modifiedDate:                 row[41],
		modeSTransponderBinary:       row[42],
		physicalFileRegion:           row[43],
		exMilitaryMark:               row[45],
		trimmedMark:                  row[46],
	}, nil
}

func ownerRecord(row []string) (*owner, error) {
	if len(row) < 20 {
		return nil, fmt.Errorf("owner row has %d fields, want at least 20", len(row))
	}

	return &owner{
		mark:               row[0],
		fullName:           row[1],
		tradeName:          row[2],
		streetName:         row[3],
		streetName2:        row[4],
		city:               row[5],
		provinceOrState:    row[6],
		postalCode:         row[8],
		country:            row[9],
		typeOfOwner:        row[11],
		activeFlag:         row[13],
		careOf:             row[14],
		region:             row[15],
		ownerNameOldFormat: row[17],
		mailRecipient:      row[18],
		trimmedMark:        row[19],
	}, nil
}

func markFromRegistration(registration string) string {
	registration = strings.ToUpper(strings.TrimSpace(registration))
	registration = strings.ReplaceAll(registration, " ", "")
	if strings.HasPrefix(registration, "C-") && len(registration) == 6 {
		return registration[2:]
	}
	if strings.HasPrefix(registration, "C") && len(registration) == 5 {
		return registration[1:]
	}
	if len(registration) == 4 {
		return registration
	}
	return ""
}

func normalizeModeSHex(modeSHex string) string {
	modeSHex = strings.ToUpper(strings.TrimSpace(modeSHex))
	if modeSHex == "" {
		return ""
	}
	value, err := strconv.ParseUint(modeSHex, 16, 32)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%06X", value)
}
