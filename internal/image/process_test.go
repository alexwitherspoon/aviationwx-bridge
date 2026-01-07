package image

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/config"
)

// createTestJPEG creates a test JPEG image with the given dimensions
func createTestJPEG(width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Fill with a simple pattern
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8(x % 256),
				G: uint8(y % 256),
				B: uint8((x + y) % 256),
				A: 255,
			})
		}
	}

	var buf bytes.Buffer
	jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	return buf.Bytes()
}

// createTestPNG creates a test PNG image with the given dimensions
func createTestPNG(width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Fill with a simple pattern
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8(x % 256),
				G: uint8(y % 256),
				B: 128,
				A: 255,
			})
		}
	}

	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func TestNewProcessor(t *testing.T) {
	cfg := &config.ImageProcessing{
		MaxWidth:  1920,
		MaxHeight: 1080,
		Quality:   85,
	}

	p := NewProcessor(cfg)

	if p == nil {
		t.Fatal("NewProcessor returned nil")
	}
	if p.config != cfg {
		t.Error("Processor config not set correctly")
	}
}

func TestNewProcessor_NilConfig(t *testing.T) {
	p := NewProcessor(nil)

	if p == nil {
		t.Fatal("NewProcessor returned nil for nil config")
	}
	if p.config != nil {
		t.Error("Processor config should be nil")
	}
}

func TestProcessor_Process_NoProcessingNeeded(t *testing.T) {
	// nil config means no processing
	p := NewProcessor(nil)

	originalData := createTestJPEG(800, 600)
	result, err := p.Process(originalData)

	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Should return original data unchanged
	if !bytes.Equal(result, originalData) {
		t.Error("Expected original data to be returned unchanged")
	}
}

func TestProcessor_Process_NoProcessingConfig(t *testing.T) {
	// Config with no processing values set
	cfg := &config.ImageProcessing{
		MaxWidth:  0,
		MaxHeight: 0,
		Quality:   0,
	}
	p := NewProcessor(cfg)

	originalData := createTestJPEG(800, 600)
	result, err := p.Process(originalData)

	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Should return original data unchanged
	if !bytes.Equal(result, originalData) {
		t.Error("Expected original data to be returned unchanged")
	}
}

func TestProcessor_Process_Resize(t *testing.T) {
	cfg := &config.ImageProcessing{
		MaxWidth:  400,
		MaxHeight: 300,
		Quality:   85,
	}
	p := NewProcessor(cfg)

	// Create a larger image
	originalData := createTestJPEG(800, 600)
	result, err := p.Process(originalData)

	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Decode result to check dimensions
	img, _, err := image.Decode(bytes.NewReader(result))
	if err != nil {
		t.Fatalf("Failed to decode result: %v", err)
	}

	bounds := img.Bounds()
	if bounds.Dx() > 400 || bounds.Dy() > 300 {
		t.Errorf("Image should be resized to fit within 400x300, got %dx%d",
			bounds.Dx(), bounds.Dy())
	}
}

func TestProcessor_Process_ResizeWidthOnly(t *testing.T) {
	cfg := &config.ImageProcessing{
		MaxWidth:  400,
		MaxHeight: 0, // No height limit
		Quality:   85,
	}
	p := NewProcessor(cfg)

	// Create a wider image
	originalData := createTestJPEG(800, 400)
	result, err := p.Process(originalData)

	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	img, _, err := image.Decode(bytes.NewReader(result))
	if err != nil {
		t.Fatalf("Failed to decode result: %v", err)
	}

	bounds := img.Bounds()
	if bounds.Dx() > 400 {
		t.Errorf("Image width should be <= 400, got %d", bounds.Dx())
	}
}

func TestProcessor_Process_ResizeHeightOnly(t *testing.T) {
	cfg := &config.ImageProcessing{
		MaxWidth:  0, // No width limit
		MaxHeight: 300,
		Quality:   85,
	}
	p := NewProcessor(cfg)

	// Create a taller image
	originalData := createTestJPEG(400, 600)
	result, err := p.Process(originalData)

	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	img, _, err := image.Decode(bytes.NewReader(result))
	if err != nil {
		t.Fatalf("Failed to decode result: %v", err)
	}

	bounds := img.Bounds()
	if bounds.Dy() > 300 {
		t.Errorf("Image height should be <= 300, got %d", bounds.Dy())
	}
}

func TestProcessor_Process_NoResizeIfSmaller(t *testing.T) {
	cfg := &config.ImageProcessing{
		MaxWidth:  1920,
		MaxHeight: 1080,
		Quality:   85,
	}
	p := NewProcessor(cfg)

	// Create a smaller image
	originalData := createTestJPEG(640, 480)
	result, err := p.Process(originalData)

	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	img, _, err := image.Decode(bytes.NewReader(result))
	if err != nil {
		t.Fatalf("Failed to decode result: %v", err)
	}

	bounds := img.Bounds()
	// Image should not be upscaled
	if bounds.Dx() != 640 || bounds.Dy() != 480 {
		t.Errorf("Small image should not be resized, expected 640x480, got %dx%d",
			bounds.Dx(), bounds.Dy())
	}
}

func TestProcessor_Process_QualityOnly(t *testing.T) {
	cfg := &config.ImageProcessing{
		MaxWidth:  0,
		MaxHeight: 0,
		Quality:   50, // Low quality
	}
	p := NewProcessor(cfg)

	originalData := createTestJPEG(800, 600)
	result, err := p.Process(originalData)

	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Result should be smaller due to lower quality
	// Note: This isn't guaranteed, but typically true for JPEG
	if len(result) >= len(originalData) {
		t.Logf("Warning: low quality image not smaller (original: %d, result: %d)",
			len(originalData), len(result))
	}

	// Verify it's still a valid JPEG
	_, _, err = image.Decode(bytes.NewReader(result))
	if err != nil {
		t.Fatalf("Result is not a valid image: %v", err)
	}
}

func TestProcessor_Process_PNG_Input(t *testing.T) {
	cfg := &config.ImageProcessing{
		MaxWidth:  400,
		MaxHeight: 300,
		Quality:   85,
	}
	p := NewProcessor(cfg)

	// Create a PNG input
	originalData := createTestPNG(800, 600)
	result, err := p.Process(originalData)

	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Result should be JPEG (we always encode to JPEG)
	img, format, err := image.Decode(bytes.NewReader(result))
	if err != nil {
		t.Fatalf("Failed to decode result: %v", err)
	}

	if format != "jpeg" {
		t.Errorf("Expected JPEG output, got %s", format)
	}

	bounds := img.Bounds()
	if bounds.Dx() > 400 || bounds.Dy() > 300 {
		t.Errorf("Image should be resized to fit within 400x300, got %dx%d",
			bounds.Dx(), bounds.Dy())
	}
}

func TestProcessor_Process_InvalidData(t *testing.T) {
	cfg := &config.ImageProcessing{
		MaxWidth:  400,
		MaxHeight: 300,
		Quality:   85,
	}
	p := NewProcessor(cfg)

	// Try to process invalid data
	invalidData := []byte("this is not an image")
	_, err := p.Process(invalidData)

	if err == nil {
		t.Error("Expected error for invalid image data")
	}
}

func TestEstimateSize(t *testing.T) {
	tests := []struct {
		width   int
		height  int
		quality int
	}{
		{640, 480, 70},
		{1280, 720, 80},
		{1920, 1080, 90},
	}

	for _, tt := range tests {
		size := EstimateSize(tt.width, tt.height, tt.quality)

		if size <= 0 {
			t.Errorf("EstimateSize(%d, %d, %d) should be > 0, got %d",
				tt.width, tt.height, tt.quality, size)
		}

		// Higher quality should estimate larger size
		lowQualitySize := EstimateSize(tt.width, tt.height, 50)
		if size <= lowQualitySize {
			t.Errorf("Higher quality (%d) should estimate larger than lower (50)",
				tt.quality)
		}
	}
}

func TestPresets(t *testing.T) {
	t.Run("PresetLow", func(t *testing.T) {
		p := PresetLow()
		if p.MaxWidth != 640 {
			t.Errorf("PresetLow MaxWidth = %d, want 640", p.MaxWidth)
		}
		if p.MaxHeight != 480 {
			t.Errorf("PresetLow MaxHeight = %d, want 480", p.MaxHeight)
		}
		if p.Quality != 70 {
			t.Errorf("PresetLow Quality = %d, want 70", p.Quality)
		}
	})

	t.Run("PresetMedium", func(t *testing.T) {
		p := PresetMedium()
		if p.MaxWidth != 1280 {
			t.Errorf("PresetMedium MaxWidth = %d, want 1280", p.MaxWidth)
		}
		if p.MaxHeight != 720 {
			t.Errorf("PresetMedium MaxHeight = %d, want 720", p.MaxHeight)
		}
		if p.Quality != 80 {
			t.Errorf("PresetMedium Quality = %d, want 80", p.Quality)
		}
	})

	t.Run("PresetHigh", func(t *testing.T) {
		p := PresetHigh()
		if p.MaxWidth != 1920 {
			t.Errorf("PresetHigh MaxWidth = %d, want 1920", p.MaxWidth)
		}
		if p.MaxHeight != 1080 {
			t.Errorf("PresetHigh MaxHeight = %d, want 1080", p.MaxHeight)
		}
		if p.Quality != 90 {
			t.Errorf("PresetHigh Quality = %d, want 90", p.Quality)
		}
	})

	t.Run("PresetOriginal", func(t *testing.T) {
		p := PresetOriginal()
		if p != nil {
			t.Error("PresetOriginal should return nil")
		}
	})
}

func TestResizeImage_MaintainsAspectRatio(t *testing.T) {
	// Create a 4:3 image
	src := image.NewRGBA(image.Rect(0, 0, 800, 600))

	// Resize to smaller dimensions
	result := resizeImage(src, 400, 300)

	bounds := result.Bounds()
	if bounds.Dx() != 400 || bounds.Dy() != 300 {
		t.Errorf("resizeImage dimensions wrong: got %dx%d, want 400x300",
			bounds.Dx(), bounds.Dy())
	}
}

func TestResizeImage_SmallestDimension(t *testing.T) {
	// Create a small image
	src := image.NewRGBA(image.Rect(0, 0, 10, 10))

	// Resize to 1x1
	result := resizeImage(src, 1, 1)

	bounds := result.Bounds()
	if bounds.Dx() != 1 || bounds.Dy() != 1 {
		t.Errorf("resizeImage to 1x1 failed: got %dx%d",
			bounds.Dx(), bounds.Dy())
	}
}

func TestProcessor_Resize_AspectRatio(t *testing.T) {
	// Test that aspect ratio is maintained when resizing
	cfg := &config.ImageProcessing{
		MaxWidth:  200,
		MaxHeight: 200,
		Quality:   85,
	}
	p := NewProcessor(cfg)

	// Create a 2:1 aspect ratio image (800x400)
	originalData := createTestJPEG(800, 400)
	result, err := p.Process(originalData)

	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	img, _, err := image.Decode(bytes.NewReader(result))
	if err != nil {
		t.Fatalf("Failed to decode result: %v", err)
	}

	bounds := img.Bounds()
	// With 200x200 max and 2:1 source, result should be 200x100
	if bounds.Dx() != 200 {
		t.Errorf("Width should be 200, got %d", bounds.Dx())
	}
	if bounds.Dy() != 100 {
		t.Errorf("Height should be 100 (maintaining 2:1 ratio), got %d", bounds.Dy())
	}
}

func BenchmarkProcessor_Process(b *testing.B) {
	cfg := &config.ImageProcessing{
		MaxWidth:  1280,
		MaxHeight: 720,
		Quality:   80,
	}
	p := NewProcessor(cfg)

	// Create test image once
	originalData := createTestJPEG(1920, 1080)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := p.Process(originalData)
		if err != nil {
			b.Fatalf("Process failed: %v", err)
		}
	}
}

func BenchmarkProcessor_Process_NoResize(b *testing.B) {
	cfg := &config.ImageProcessing{
		MaxWidth:  0,
		MaxHeight: 0,
		Quality:   80,
	}
	p := NewProcessor(cfg)

	originalData := createTestJPEG(640, 480)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := p.Process(originalData)
		if err != nil {
			b.Fatalf("Process failed: %v", err)
		}
	}
}
