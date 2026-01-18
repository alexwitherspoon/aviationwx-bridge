// Package image provides image processing utilities for bandwidth control
package image

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"runtime"

	// Import image decoders
	_ "image/gif"
	_ "image/png"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/config"
)

// Processor handles image resizing and quality adjustment
type Processor struct {
	config *config.ImageProcessing
}

// NewProcessor creates a new image processor with the given settings
func NewProcessor(cfg *config.ImageProcessing) *Processor {
	return &Processor{config: cfg}
}

// Process applies configured transformations to image data
// Returns the processed JPEG image data, or the original if no processing is needed
func (p *Processor) Process(data []byte) ([]byte, error) {
	// Default: no processing, return original image as-is
	if p.config == nil || !p.config.NeedsProcessing() {
		return data, nil
	}

	// Check if any processing is needed
	needsResize := p.config.MaxWidth > 0 || p.config.MaxHeight > 0
	_ = p.config.Quality > 0 // Quality is applied during encoding

	// Decode the image
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	// Resize if needed
	if needsResize {
		img = p.resize(img)
	}

	// Encode as JPEG with quality setting
	var buf bytes.Buffer
	quality := p.config.GetQuality()

	opts := &jpeg.Options{Quality: quality}
	if err := jpeg.Encode(&buf, img, opts); err != nil {
		return nil, fmt.Errorf("failed to encode JPEG: %w", err)
	}

	// Log size reduction if significant
	originalSize := len(data)
	newSize := buf.Len()
	if newSize < originalSize {
		// Successfully reduced size
		_ = format // Suppress unused variable warning
	}

	return buf.Bytes(), nil
}

// resize scales the image to fit within MaxWidth and MaxHeight
// Maintains aspect ratio - image will fit within the bounds
func (p *Processor) resize(img image.Image) image.Image {
	bounds := img.Bounds()
	origWidth := bounds.Dx()
	origHeight := bounds.Dy()

	maxW := p.config.MaxWidth
	maxH := p.config.MaxHeight

	// If no limits, return original
	if maxW <= 0 && maxH <= 0 {
		return img
	}

	// Calculate scale factor
	scaleW := 1.0
	scaleH := 1.0

	if maxW > 0 && origWidth > maxW {
		scaleW = float64(maxW) / float64(origWidth)
	}
	if maxH > 0 && origHeight > maxH {
		scaleH = float64(maxH) / float64(origHeight)
	}

	// Use the smaller scale to fit within both bounds
	scale := scaleW
	if scaleH < scale {
		scale = scaleH
	}

	// If no scaling needed, return original
	if scale >= 1.0 {
		return img
	}

	newWidth := int(float64(origWidth) * scale)
	newHeight := int(float64(origHeight) * scale)

	// Create new image with scaled dimensions
	return resizeImage(img, newWidth, newHeight)
}

// resizeImage performs bilinear interpolation resize
// This is a simple implementation - for better quality, consider using
// golang.org/x/image/draw or similar
//
// Includes cooperative yielding via runtime.Gosched() every 50 rows to allow
// web handlers and other goroutines to run on resource-constrained devices.
func resizeImage(src image.Image, width, height int) image.Image {
	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	dst := image.NewRGBA(image.Rect(0, 0, width, height))

	for y := 0; y < height; y++ {
		// Cooperative yielding: allow web handlers to run during resize
		// This prevents the resize loop from starving the admin UI
		if y%50 == 0 && y > 0 {
			runtime.Gosched()
		}

		for x := 0; x < width; x++ {
			// Map destination coordinates to source
			srcX := float64(x) * float64(srcW) / float64(width)
			srcY := float64(y) * float64(srcH) / float64(height)

			// Simple nearest-neighbor for now (fast)
			// For better quality, implement bilinear interpolation
			px := int(srcX)
			py := int(srcY)

			if px >= srcW {
				px = srcW - 1
			}
			if py >= srcH {
				py = srcH - 1
			}

			dst.Set(x, y, src.At(bounds.Min.X+px, bounds.Min.Y+py))
		}
	}

	return dst
}

// EstimateSize estimates the output file size for given dimensions and quality
// Returns approximate bytes
func EstimateSize(width, height, quality int) int {
	// Rough estimation: JPEG typically compresses to ~0.5-2 bytes per pixel
	// at quality 85, less at lower quality
	pixels := width * height
	bitsPerPixel := 0.5 + (float64(quality)/100.0)*1.5
	return int(float64(pixels) * bitsPerPixel)
}

// PresetLow returns settings for low bandwidth (640x480, quality 70)
func PresetLow() *config.ImageProcessing {
	return &config.ImageProcessing{
		MaxWidth:  640,
		MaxHeight: 480,
		Quality:   70,
	}
}

// PresetMedium returns settings for medium bandwidth (1280x720, quality 80)
func PresetMedium() *config.ImageProcessing {
	return &config.ImageProcessing{
		MaxWidth:  1280,
		MaxHeight: 720,
		Quality:   80,
	}
}

// PresetHigh returns settings for high quality (1920x1080, quality 90)
func PresetHigh() *config.ImageProcessing {
	return &config.ImageProcessing{
		MaxWidth:  1920,
		MaxHeight: 1080,
		Quality:   90,
	}
}

// PresetOriginal returns nil (no processing - use original image as-is)
// This is the default behavior
func PresetOriginal() *config.ImageProcessing {
	return nil
}
