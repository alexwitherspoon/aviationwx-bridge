package time

import (
	"os"
	"strings"
	"testing"
	"time"
)

// requireExiftool requires exiftool to be available for the test
// Tests will FAIL if exiftool is not installed (fail closed approach)
func requireExiftool(t *testing.T) *ExifToolHelper {
	t.Helper()
	helper, err := DefaultExifToolHelper()
	if err != nil {
		t.Fatalf("exiftool is required but not available: %v\n\nInstall exiftool:\n  macOS: brew install exiftool\n  Ubuntu/Debian: sudo apt-get install libimage-exiftool-perl", err)
	}
	return helper
}

func TestExifToolHelper_NewExifToolHelper(t *testing.T) {
	helper := requireExiftool(t)

	if helper == nil {
		t.Fatal("Expected non-nil helper")
	}

	if helper.exiftoolPath == "" {
		t.Error("Expected non-empty exiftool path")
	}
}

func TestExifToolHelper_IsAvailable(t *testing.T) {
	helper := requireExiftool(t)

	if !helper.IsAvailable() {
		t.Error("Expected exiftool to be available")
	}
}

func TestExifToolHelper_GetVersion(t *testing.T) {
	helper := requireExiftool(t)

	version, err := helper.GetVersion()
	if err != nil {
		t.Fatalf("GetVersion() error = %v", err)
	}

	if version == "" {
		t.Error("Expected non-empty version string")
	}

	t.Logf("exiftool version: %s", version)
}

func TestExifToolHelper_ReadEXIF_NoFile(t *testing.T) {
	helper := requireExiftool(t)

	// Note: exiftool may return an empty result rather than an error for nonexistent files
	result, err := helper.ReadEXIF("/nonexistent/path/to/image.jpg")

	// Either an error or an empty/unsuccessful result is acceptable
	if err == nil && result != nil && result.Success {
		// If it claims success, check that there's no meaningful data
		if result.DateTimeOriginal != nil {
			t.Error("Should not have DateTimeOriginal for nonexistent file")
		}
	}
	// Test passes if we got an error or empty result
	t.Logf("ReadEXIF on nonexistent file: err=%v, result=%+v", err, result)
}

func TestExifToolHelper_ReadEXIF_ValidJPEG(t *testing.T) {
	helper := requireExiftool(t)

	// Create a temp file with valid JPEG data
	tmpFile, err := os.CreateTemp("", "exiftool-test-*.jpg")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write minimal JPEG
	jpegData := createTestJPEG(1024)
	if _, err := tmpFile.Write(jpegData); err != nil {
		t.Fatalf("Failed to write JPEG data: %v", err)
	}
	tmpFile.Close()

	// Read EXIF (should succeed even with no EXIF data)
	result, err := helper.ReadEXIF(tmpFile.Name())
	if err != nil {
		t.Fatalf("ReadEXIF() error = %v", err)
	}

	if !result.Success {
		t.Error("Expected Success to be true")
	}

	// Should not have bridge stamp yet
	if result.IsBridgeStamped {
		t.Error("Expected image to not be bridge-stamped")
	}
}

func TestExifToolHelper_WriteEXIF(t *testing.T) {
	helper := requireExiftool(t)

	// Create temp file
	tmpFile, err := os.CreateTemp("", "exiftool-write-test-*.jpg")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write minimal JPEG
	jpegData := createTestJPEG(1024)
	if _, err := tmpFile.Write(jpegData); err != nil {
		t.Fatalf("Failed to write JPEG data: %v", err)
	}
	tmpFile.Close()

	// Write EXIF data
	opts := ExifWriteOptions{
		DateTimeOriginal:   "2024:12:25 10:30:00",
		OffsetTimeOriginal: "+00:00",
		UserComment:        "AviationWX-Bridge:UTC:v1:bridge_clock:high",
	}

	if err := helper.WriteEXIF(tmpFile.Name(), opts); err != nil {
		t.Fatalf("WriteEXIF() error = %v", err)
	}

	// Read back and verify
	result, err := helper.ReadEXIF(tmpFile.Name())
	if err != nil {
		t.Fatalf("ReadEXIF() after write error = %v", err)
	}

	if !result.Success {
		t.Error("Expected Success to be true after write")
	}

	if result.DateTimeOriginal == nil {
		t.Error("Expected DateTimeOriginal to be set")
	} else if *result.DateTimeOriginal != "2024:12:25 10:30:00" {
		t.Errorf("DateTimeOriginal = %s, want 2024:12:25 10:30:00", *result.DateTimeOriginal)
	}

	if result.OffsetTimeOriginal == nil {
		t.Error("Expected OffsetTimeOriginal to be set")
	} else if *result.OffsetTimeOriginal != "+00:00" {
		t.Errorf("OffsetTimeOriginal = %s, want +00:00", *result.OffsetTimeOriginal)
	}

	if !result.IsBridgeStamped {
		t.Error("Expected image to be bridge-stamped after write")
	}

	if result.UserComment == nil {
		t.Error("Expected UserComment to be set")
	} else if !strings.Contains(*result.UserComment, "AviationWX-Bridge") {
		t.Errorf("UserComment should contain AviationWX-Bridge, got: %s", *result.UserComment)
	}
}

func TestExifToolHelper_WriteEXIFToData(t *testing.T) {
	helper := requireExiftool(t)

	jpegData := createTestJPEG(1024)

	opts := ExifWriteOptions{
		DateTimeOriginal:   "2024:12:25 15:00:00",
		OffsetTimeOriginal: "+00:00",
		UserComment:        "AviationWX-Bridge:UTC:v1:bridge_clock:high",
	}

	modifiedData, err := helper.WriteEXIFToData(jpegData, opts)
	if err != nil {
		t.Fatalf("WriteEXIFToData() error = %v", err)
	}

	if len(modifiedData) == 0 {
		t.Error("Expected non-empty modified data")
	}

	// Modified data should be larger (has EXIF now)
	if len(modifiedData) <= len(jpegData) {
		t.Error("Modified data should be larger than original (has EXIF added)")
	}

	// Verify the modified data by writing to temp file and reading back
	tmpFile, err := os.CreateTemp("", "exiftool-verify-*.jpg")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(modifiedData); err != nil {
		t.Fatalf("Failed to write modified data: %v", err)
	}
	tmpFile.Close()

	result, err := helper.ReadEXIF(tmpFile.Name())
	if err != nil {
		t.Fatalf("ReadEXIF() on modified data error = %v", err)
	}

	if !result.IsBridgeStamped {
		t.Error("Modified data should be bridge-stamped")
	}

	if result.DateTimeOriginal == nil || *result.DateTimeOriginal != "2024:12:25 15:00:00" {
		t.Errorf("DateTimeOriginal not correctly written, got: %v", result.DateTimeOriginal)
	}
}

func TestExifToolHelper_ValidateEXIF(t *testing.T) {
	helper := requireExiftool(t)

	// Create temp file with bridge-stamped EXIF
	tmpFile, err := os.CreateTemp("", "exiftool-validate-*.jpg")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	jpegData := createTestJPEG(1024)
	if _, err := tmpFile.Write(jpegData); err != nil {
		t.Fatalf("Failed to write JPEG: %v", err)
	}
	tmpFile.Close()

	// Write valid bridge EXIF
	opts := ExifWriteOptions{
		DateTimeOriginal:   "2024:12:25 10:30:00",
		OffsetTimeOriginal: "+00:00",
		UserComment:        "AviationWX-Bridge:UTC:v1:bridge_clock:high",
	}
	if err := helper.WriteEXIF(tmpFile.Name(), opts); err != nil {
		t.Fatalf("WriteEXIF() error = %v", err)
	}

	// Validate
	result, err := helper.ValidateEXIF(tmpFile.Name())
	if err != nil {
		t.Fatalf("ValidateEXIF() error = %v", err)
	}

	if !result.Valid {
		t.Errorf("Expected valid result, got errors: %v", result.Errors)
	}

	if !result.HasBridgeMarker {
		t.Error("Expected HasBridgeMarker to be true")
	}

	if result.DateTimeOriginal == nil {
		t.Error("Expected DateTimeOriginal to be set")
	}
}

func TestExifToolHelper_ValidateEXIF_Invalid(t *testing.T) {
	helper := requireExiftool(t)

	// Create temp file with no EXIF
	tmpFile, err := os.CreateTemp("", "exiftool-validate-invalid-*.jpg")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	jpegData := createTestJPEG(1024)
	if _, err := tmpFile.Write(jpegData); err != nil {
		t.Fatalf("Failed to write JPEG: %v", err)
	}
	tmpFile.Close()

	// Validate (should fail - no bridge marker)
	result, err := helper.ValidateEXIF(tmpFile.Name())
	if err != nil {
		t.Fatalf("ValidateEXIF() error = %v", err)
	}

	if result.Valid {
		t.Error("Expected invalid result for unstamped image")
	}

	if result.HasBridgeMarker {
		t.Error("Expected HasBridgeMarker to be false")
	}

	if len(result.Errors) == 0 {
		t.Error("Expected validation errors")
	}
}

func TestExifToolHelper_ParseCameraTime(t *testing.T) {
	helper := requireExiftool(t)

	tests := []struct {
		name     string
		result   *ExifReadResult
		wantTime bool
	}{
		{
			name:     "nil result",
			result:   nil,
			wantTime: false,
		},
		{
			name: "no DateTimeOriginal",
			result: &ExifReadResult{
				Success: true,
			},
			wantTime: false,
		},
		{
			name: "valid DateTimeOriginal",
			result: &ExifReadResult{
				Success:          true,
				DateTimeOriginal: strPtr("2024:12:25 10:30:00"),
			},
			wantTime: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsedTime, ok := helper.ParseCameraTime(tc.result)
			if ok != tc.wantTime {
				t.Errorf("ParseCameraTime() ok = %v, want %v", ok, tc.wantTime)
			}
			if tc.wantTime && parsedTime == nil {
				t.Error("Expected non-nil time")
			}
			if tc.wantTime && parsedTime != nil {
				expected := time.Date(2024, 12, 25, 10, 30, 0, 0, time.UTC)
				if !parsedTime.Equal(expected) {
					t.Errorf("ParseCameraTime() = %v, want %v", parsedTime, expected)
				}
			}
		})
	}
}

func TestStampBridgeEXIFWithTool(t *testing.T) {
	// Skip if exiftool not available
	if _, err := DefaultExifToolHelper(); err != nil {
		t.Skipf("exiftool not available: %v", err)
	}

	observation := ObservationResult{
		Time:       time.Date(2024, 12, 25, 10, 30, 0, 0, time.UTC),
		Source:     SourceBridgeClock,
		Confidence: ConfidenceHigh,
	}

	jpegData := createTestJPEG(1024)
	result := StampBridgeEXIFWithTool(jpegData, observation)

	if !result.Stamped {
		t.Error("Expected image to be stamped")
	}

	if len(result.Data) <= len(jpegData) {
		t.Error("Stamped data should be larger than original")
	}

	if result.Marker == "" {
		t.Error("Expected non-empty marker")
	}

	if !strings.Contains(result.Marker, "AviationWX-Bridge") {
		t.Errorf("Marker should contain AviationWX-Bridge, got: %s", result.Marker)
	}

	if !strings.Contains(result.Marker, string(SourceBridgeClock)) {
		t.Errorf("Marker should contain source, got: %s", result.Marker)
	}
}

func TestStampBridgeEXIFWithTool_WithWarning(t *testing.T) {
	// Skip if exiftool not available
	if _, err := DefaultExifToolHelper(); err != nil {
		t.Skipf("exiftool not available: %v", err)
	}

	observation := ObservationResult{
		Time:       time.Now().UTC(),
		Source:     SourceCameraEXIF,
		Confidence: ConfidenceMedium,
		Warning: &TimeWarning{
			Code:    "camera_clock_drift",
			Message: "Camera clock is drifting",
		},
	}

	jpegData := createTestJPEG(1024)
	result := StampBridgeEXIFWithTool(jpegData, observation)

	if !result.Stamped {
		t.Error("Expected image to be stamped")
	}

	if !strings.Contains(result.Marker, "warn:camera_clock_drift") {
		t.Errorf("Marker should contain warning code, got: %s", result.Marker)
	}
}

func TestGetExifToolPath(t *testing.T) {
	// This test documents the behavior of GetExifToolPath
	// It may or may not find exiftool depending on the environment

	path, err := GetExifToolPath()
	if err != nil {
		t.Logf("exiftool not found (this is OK in some environments): %v", err)
		return
	}

	if path == "" {
		t.Error("Expected non-empty path when exiftool is found")
	}

	t.Logf("exiftool found at: %s", path)
}

func TestExifToolHelper_Timeout(t *testing.T) {
	helper := requireExiftool(t)

	// Set very short timeout
	helper.SetTimeout(1 * time.Nanosecond)

	// This should timeout (or might succeed on very fast systems)
	tmpFile, err := os.CreateTemp("", "exiftool-timeout-*.jpg")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	jpegData := createTestJPEG(1024)
	tmpFile.Write(jpegData)
	tmpFile.Close()

	// Try to read - may timeout or succeed depending on system speed
	_, err = helper.ReadEXIF(tmpFile.Name())
	// We don't fail on error here since timeout behavior varies
	t.Logf("ReadEXIF with 1ns timeout: err=%v", err)
}

// Helper function
func strPtr(s string) *string {
	return &s
}

// Benchmark tests

func BenchmarkExifToolHelper_ReadEXIF(b *testing.B) {
	helper, err := DefaultExifToolHelper()
	if err != nil {
		b.Skipf("exiftool not available: %v", err)
	}

	// Create temp file once
	tmpFile, err := os.CreateTemp("", "exiftool-bench-*.jpg")
	if err != nil {
		b.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	jpegData := createTestJPEG(50 * 1024) // 50KB
	tmpFile.Write(jpegData)
	tmpFile.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		helper.ReadEXIF(tmpFile.Name())
	}
}

func BenchmarkExifToolHelper_WriteEXIF(b *testing.B) {
	helper, err := DefaultExifToolHelper()
	if err != nil {
		b.Skipf("exiftool not available: %v", err)
	}

	jpegData := createTestJPEG(50 * 1024) // 50KB

	opts := ExifWriteOptions{
		DateTimeOriginal:   "2024:12:25 10:30:00",
		OffsetTimeOriginal: "+00:00",
		UserComment:        "AviationWX-Bridge:UTC:v1:bridge_clock:high",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Create temp file for each iteration
		tmpFile, _ := os.CreateTemp("", "exiftool-bench-write-*.jpg")
		tmpFile.Write(jpegData)
		tmpFile.Close()
		helper.WriteEXIF(tmpFile.Name(), opts)
		os.Remove(tmpFile.Name())
	}
}

func BenchmarkStampBridgeEXIFWithTool(b *testing.B) {
	if _, err := DefaultExifToolHelper(); err != nil {
		b.Skipf("exiftool not available: %v", err)
	}

	observation := ObservationResult{
		Time:       time.Now().UTC(),
		Source:     SourceBridgeClock,
		Confidence: ConfidenceHigh,
	}

	jpegData := createTestJPEG(50 * 1024) // 50KB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		StampBridgeEXIFWithTool(jpegData, observation)
	}
}
