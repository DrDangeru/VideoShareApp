package app

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/jpeg"
	_ "image/png"
	"mime"
	"net/http"
	"strings"
)

const (
	maxThumbnailBytes     = 2 << 20
	maxThumbnailDimension = 4096
	maxThumbnailPixels    = 16_000_000
)

// decodeThumbnail bounds encoded and decoded resources before normalizing accepted images to JPEG.
func decodeThumbnail(dataURL string) ([]byte, error) {
	if dataURL == "" {
		return nil, nil
	}

	var declaredType string
	var encoded string
	switch {
	case strings.HasPrefix(dataURL, "data:image/jpeg;base64,"):
		declaredType = "image/jpeg"
		encoded = strings.TrimPrefix(dataURL, "data:image/jpeg;base64,")
	case strings.HasPrefix(dataURL, "data:image/png;base64,"):
		declaredType = "image/png"
		encoded = strings.TrimPrefix(dataURL, "data:image/png;base64,")
	default:
		return nil, errors.New("unsupported thumbnail data URL")
	}

	if len(encoded) > base64.StdEncoding.EncodedLen(maxThumbnailBytes) {
		return nil, errors.New("thumbnail exceeds size limit")
	}
	original, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("invalid thumbnail base64")
	}
	if len(original) > maxThumbnailBytes {
		return nil, errors.New("thumbnail exceeds size limit")
	}

	detectedType, _, err := mime.ParseMediaType(http.DetectContentType(original))
	if err != nil || detectedType != declaredType {
		return nil, errors.New("thumbnail content does not match declared image type")
	}

	config, format, err := image.DecodeConfig(bytes.NewReader(original))
	if err != nil || format != strings.TrimPrefix(declaredType, "image/") {
		return nil, errors.New("invalid thumbnail image")
	}
	if config.Width < 1 || config.Height < 1 ||
		config.Width > maxThumbnailDimension || config.Height > maxThumbnailDimension ||
		int64(config.Width)*int64(config.Height) > maxThumbnailPixels {
		return nil, errors.New("thumbnail dimensions exceed limit")
	}

	decoded, format, err := image.Decode(bytes.NewReader(original))
	if err != nil || format != strings.TrimPrefix(declaredType, "image/") {
		return nil, errors.New("invalid thumbnail image")
	}
	var normalized bytes.Buffer
	if err := jpeg.Encode(&normalized, decoded, &jpeg.Options{Quality: 80}); err != nil {
		return nil, errors.New("thumbnail normalization failed")
	}
	if normalized.Len() > maxThumbnailBytes {
		return nil, errors.New("thumbnail exceeds size limit")
	}
	return normalized.Bytes(), nil
}
