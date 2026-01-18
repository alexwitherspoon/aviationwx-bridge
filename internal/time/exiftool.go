package time

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ExifToolHelper wraps exiftool CLI for EXIF operations
// Uses the same tool as aviationwx.org server for 100% compatibility
type ExifToolHelper struct {
	exiftoolPath string
	timeout      time.Duration
	useNice      bool // Run exiftool with nice for lower CPU priority
	niceLevel    int  // Nice level (default: 19 = lowest priority)
}

// ExifReadResult is the result from reading EXIF via exiftool
type ExifReadResult struct {
	Success            bool    `json:"success"`
	Error              string  `json:"error,omitempty"`
	DateTimeOriginal   *string `json:"datetime_original"`
	OffsetTimeOriginal *string `json:"offset_time_original"`
	UserComment        *string `json:"user_comment"`
	IsBridgeStamped    bool    `json:"is_bridge_stamped"`
	GPSDateTime        *string `json:"gps_datetime"`
}

// ExifValidateResult is the result from validating bridge-written EXIF
type ExifValidateResult struct {
	Valid              bool     `json:"valid"`
	DateTimeOriginal   *string  `json:"datetime_original"`
	OffsetTimeOriginal *string  `json:"offset_time_original"`
	HasBridgeMarker    bool     `json:"has_bridge_marker"`
	BridgeMarker       *string  `json:"bridge_marker"`
	MarkerVersion      *string  `json:"marker_version"`
	TimeSource         *string  `json:"time_source"`
	Confidence         *string  `json:"confidence"`
	Errors             []string `json:"errors"`
}

// ExifWriteOptions defines what to write to EXIF
type ExifWriteOptions struct {
	DateTimeOriginal   string // Format: "2024:12:25 10:30:00"
	OffsetTimeOriginal string // Format: "+00:00" for UTC
	UserComment        string // Bridge marker
}

// NewExifToolHelper creates a new exiftool helper
func NewExifToolHelper() (*ExifToolHelper, error) {
	// Find exiftool binary
	exiftoolPath, err := exec.LookPath("exiftool")
	if err != nil {
		return nil, fmt.Errorf("exiftool not found in PATH: %w", err)
	}

	// Enable nice on Linux to run exiftool at lower priority
	// This protects the web UI from exiftool CPU spikes
	useNice := runtime.GOOS == "linux"

	return &ExifToolHelper{
		exiftoolPath: exiftoolPath,
		timeout:      10 * time.Second,
		useNice:      useNice,
		niceLevel:    19, // Lowest priority
	}, nil
}

// SetTimeout sets the maximum time for exiftool execution
func (h *ExifToolHelper) SetTimeout(d time.Duration) {
	h.timeout = d
}

// SetNice enables or disables nice for exiftool subprocess
func (h *ExifToolHelper) SetNice(enabled bool, level int) {
	h.useNice = enabled
	if level >= -20 && level <= 19 {
		h.niceLevel = level
	}
}

// createCommand creates an exec.Cmd for exiftool, optionally wrapped with nice
func (h *ExifToolHelper) createCommand(ctx context.Context, args ...string) *exec.Cmd {
	if h.useNice {
		// Prepend nice command
		niceArgs := []string{"-n", fmt.Sprintf("%d", h.niceLevel), h.exiftoolPath}
		niceArgs = append(niceArgs, args...)
		return exec.CommandContext(ctx, "nice", niceArgs...)
	}
	return exec.CommandContext(ctx, h.exiftoolPath, args...)
}

// ReadEXIF reads EXIF data from an image file
func (h *ExifToolHelper) ReadEXIF(imagePath string) (*ExifReadResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
	defer cancel()

	// Use exiftool to extract relevant fields as JSON
	// Wrapped with nice on Linux to run at lower priority
	cmd := h.createCommand(ctx,
		"-json",
		"-DateTimeOriginal",
		"-OffsetTimeOriginal",
		"-UserComment",
		"-GPSDateTime",
		imagePath,
	)

	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("exiftool read timeout after %v", h.timeout)
		}
		// exiftool returns non-zero for files without EXIF, which is okay
		return &ExifReadResult{Success: true}, nil
	}

	return h.parseReadOutput(output)
}

// parseReadOutput parses exiftool JSON output
func (h *ExifToolHelper) parseReadOutput(output []byte) (*ExifReadResult, error) {
	var exifData []map[string]interface{}
	if err := json.Unmarshal(output, &exifData); err != nil {
		return nil, fmt.Errorf("parse exiftool output: %w", err)
	}

	result := &ExifReadResult{Success: true}

	if len(exifData) == 0 {
		return result, nil
	}

	data := exifData[0]

	// DateTimeOriginal
	if v, ok := data["DateTimeOriginal"].(string); ok {
		result.DateTimeOriginal = &v
	}

	// OffsetTimeOriginal
	if v, ok := data["OffsetTimeOriginal"].(string); ok {
		result.OffsetTimeOriginal = &v
	}

	// UserComment
	if v, ok := data["UserComment"].(string); ok {
		result.UserComment = &v
		result.IsBridgeStamped = strings.Contains(v, "AviationWX-Bridge")
	}

	// GPSDateTime
	if v, ok := data["GPSDateTime"].(string); ok {
		result.GPSDateTime = &v
	}

	return result, nil
}

// WriteEXIF writes EXIF data to an image file
// This modifies the file in place (exiftool's default behavior)
func (h *ExifToolHelper) WriteEXIF(imagePath string, opts ExifWriteOptions) error {
	ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
	defer cancel()

	args := []string{
		"-overwrite_original", // Don't create backup files
	}

	if opts.DateTimeOriginal != "" {
		args = append(args, fmt.Sprintf("-DateTimeOriginal=%s", opts.DateTimeOriginal))
	}
	if opts.OffsetTimeOriginal != "" {
		args = append(args, fmt.Sprintf("-OffsetTimeOriginal=%s", opts.OffsetTimeOriginal))
	}
	if opts.UserComment != "" {
		args = append(args, fmt.Sprintf("-UserComment=%s", opts.UserComment))
	}

	args = append(args, imagePath)

	// Wrapped with nice on Linux to run at lower priority
	cmd := h.createCommand(ctx, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("exiftool write timeout after %v", h.timeout)
		}
		return fmt.Errorf("exiftool write failed: %w: %s", err, string(output))
	}

	return nil
}

// WriteEXIFToData writes EXIF to image data and returns the modified data
// This writes to a temp file and reads it back
func (h *ExifToolHelper) WriteEXIFToData(imageData []byte, opts ExifWriteOptions) ([]byte, error) {
	// Create temp file
	tmpFile, err := os.CreateTemp("", "aviationwx-*.jpg")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Write image data
	if _, err := tmpFile.Write(imageData); err != nil {
		_ = tmpFile.Close()
		return nil, fmt.Errorf("write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return nil, fmt.Errorf("close temp file: %w", err)
	}

	// Write EXIF
	if err := h.WriteEXIF(tmpPath, opts); err != nil {
		return nil, err
	}

	// Read back modified file
	modifiedData, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("read modified file: %w", err)
	}

	return modifiedData, nil
}

// ValidateEXIF validates that bridge-written EXIF is readable
func (h *ExifToolHelper) ValidateEXIF(imagePath string) (*ExifValidateResult, error) {
	readResult, err := h.ReadEXIF(imagePath)
	if err != nil {
		return &ExifValidateResult{
			Valid:  false,
			Errors: []string{err.Error()},
		}, nil
	}

	result := &ExifValidateResult{
		Errors: []string{},
	}

	// Check DateTimeOriginal
	if readResult.DateTimeOriginal != nil {
		result.DateTimeOriginal = readResult.DateTimeOriginal
		// Validate format: "YYYY:MM:DD HH:MM:SS"
		if len(*readResult.DateTimeOriginal) < 19 {
			result.Errors = append(result.Errors, "invalid_datetime_format")
		}
	} else {
		result.Errors = append(result.Errors, "missing_datetime_original")
	}

	// Check OffsetTimeOriginal
	if readResult.OffsetTimeOriginal != nil {
		result.OffsetTimeOriginal = readResult.OffsetTimeOriginal
		if *readResult.OffsetTimeOriginal != "+00:00" {
			result.Errors = append(result.Errors, "offset_not_utc")
		}
	}

	// Check UserComment for bridge marker
	if readResult.UserComment != nil && readResult.IsBridgeStamped {
		result.HasBridgeMarker = true
		result.BridgeMarker = readResult.UserComment

		// Parse marker: AviationWX-Bridge:UTC:v1:source:confidence[:warn:code]
		parts := strings.Split(*readResult.UserComment, ":")
		if len(parts) >= 5 {
			result.MarkerVersion = &parts[2]
			result.TimeSource = &parts[3]
			result.Confidence = &parts[4]
		}
	} else {
		result.Errors = append(result.Errors, "missing_bridge_marker")
	}

	// Valid if we have datetime and bridge marker
	result.Valid = result.DateTimeOriginal != nil && result.HasBridgeMarker

	return result, nil
}

// ParseCameraTime parses the camera's EXIF DateTimeOriginal
func (h *ExifToolHelper) ParseCameraTime(result *ExifReadResult) (*time.Time, bool) {
	if result == nil || result.DateTimeOriginal == nil {
		return nil, false
	}

	// EXIF format: "2024:12:25 10:30:00"
	t, err := time.Parse("2006:01:02 15:04:05", *result.DateTimeOriginal)
	if err != nil {
		return nil, false
	}

	return &t, true
}

// IsAvailable checks if exiftool is available
func (h *ExifToolHelper) IsAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Don't use nice for version check - it's quick
	cmd := exec.CommandContext(ctx, h.exiftoolPath, "-ver")
	return cmd.Run() == nil
}

// GetVersion returns the exiftool version
func (h *ExifToolHelper) GetVersion() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Don't use nice for version check - it's quick
	cmd := exec.CommandContext(ctx, h.exiftoolPath, "-ver")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

// DefaultExifToolHelper creates a helper, searching common locations
func DefaultExifToolHelper() (*ExifToolHelper, error) {
	// Try PATH first
	if h, err := NewExifToolHelper(); err == nil {
		return h, nil
	}

	// Try common locations
	paths := []string{
		"/usr/bin/exiftool",
		"/usr/local/bin/exiftool",
		"/opt/homebrew/bin/exiftool",
	}

	// Enable nice on Linux to run exiftool at lower priority
	useNice := runtime.GOOS == "linux"

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return &ExifToolHelper{
				exiftoolPath: path,
				timeout:      10 * time.Second,
				useNice:      useNice,
				niceLevel:    19,
			}, nil
		}
	}

	return nil, fmt.Errorf("exiftool not found")
}

// StampBridgeEXIFWithTool stamps EXIF using exiftool instead of manual injection
// This is the preferred method for production use as it ensures compatibility
// with the aviationwx.org server which also uses exiftool.
func StampBridgeEXIFWithTool(imageData []byte, obs ObservationResult) EXIFStampResult {
	helper, err := DefaultExifToolHelper()
	if err != nil {
		return EXIFStampResult{
			Data:           imageData,
			Stamped:        false,
			ObservationUTC: obs.Time,
		}
	}

	// Format timestamp for EXIF
	dateTimeOriginal := obs.Time.Format("2006:01:02 15:04:05")

	// Build user comment marker
	marker := fmt.Sprintf("AviationWX-Bridge:UTC:v1:%s:%s",
		obs.Source, obs.Confidence)

	if obs.Warning != nil {
		marker += fmt.Sprintf(":warn:%s", obs.Warning.Code)
	}

	opts := ExifWriteOptions{
		DateTimeOriginal:   dateTimeOriginal,
		OffsetTimeOriginal: "+00:00",
		UserComment:        marker,
	}

	modifiedData, err := helper.WriteEXIFToData(imageData, opts)
	if err != nil {
		return EXIFStampResult{
			Data:           imageData,
			Stamped:        false,
			ObservationUTC: obs.Time,
			Marker:         marker,
		}
	}

	return EXIFStampResult{
		Data:           modifiedData,
		Stamped:        true,
		ObservationUTC: obs.Time,
		Marker:         marker,
	}
}

// GetExifToolPath returns the resolved path to exiftool binary
func GetExifToolPath() (string, error) {
	// Check environment variable first
	if path := os.Getenv("AVIATIONWX_EXIFTOOL_PATH"); path != "" {
		if _, err := os.Stat(path); err == nil {
			return filepath.Abs(path)
		}
	}

	// Try PATH
	if path, err := exec.LookPath("exiftool"); err == nil {
		return path, nil
	}

	// Try common locations
	paths := []string{
		"/usr/bin/exiftool",
		"/usr/local/bin/exiftool",
		"/opt/homebrew/bin/exiftool",
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("exiftool not found")
}
