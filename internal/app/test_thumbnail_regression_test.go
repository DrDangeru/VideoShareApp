package app

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func testThumbnailDataURL(t *testing.T, format string, width, height int) string {
	t.Helper()

	imageFixture := image.NewRGBA(image.Rect(0, 0, width, height))
	imageFixture.Set(0, 0, color.RGBA{R: 30, G: 90, B: 150, A: 255})
	var encoded bytes.Buffer
	switch format {
	case "jpeg":
		if err := jpeg.Encode(&encoded, imageFixture, &jpeg.Options{Quality: 90}); err != nil {
			t.Fatalf("encode JPEG fixture: %v", err)
		}
	case "png":
		if err := png.Encode(&encoded, imageFixture); err != nil {
			t.Fatalf("encode PNG fixture: %v", err)
		}
	default:
		t.Fatalf("unsupported test image format %q", format)
	}
	return "data:image/" + format + ";base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())
}

func TestDecodeThumbnail(t *testing.T) {
	validJPEG := testThumbnailDataURL(t, "jpeg", 2, 2)
	validPNG := testThumbnailDataURL(t, "png", 3, 2)
	oversizedDataURL := "data:image/jpeg;base64," +
		string(bytes.Repeat([]byte{'A'}, base64.StdEncoding.EncodedLen(maxThumbnailBytes)+1))
	oneByteOversized := "data:image/jpeg;base64," +
		base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0}, maxThumbnailBytes+1))
	if len(strings.TrimPrefix(oneByteOversized, "data:image/jpeg;base64,")) !=
		base64.StdEncoding.EncodedLen(maxThumbnailBytes) {
		t.Fatal("size-boundary fixture must match maximum encoded length")
	}
	largeDimension := testThumbnailDataURL(t, "png", maxThumbnailDimension+1, 1)
	largePixelCount := testPNGHeaderDataURL(t, maxThumbnailDimension, maxThumbnailDimension)
	zeroDimension := testPNGHeaderDataURL(t, 0, 1)

	for _, tc := range []struct {
		name    string
		input   string
		wantNil bool
		wantErr bool
	}{
		{"empty optional thumbnail", "", true, false},
		{"JPEG accepted", validJPEG, false, false},
		{"PNG normalized to JPEG", validPNG, false, false},
		{"unsupported SVG rejected", "data:image/svg+xml;base64,PHN2Zz4=", true, true},
		{"malformed base64 rejected", "data:image/jpeg;base64,%%%", true, true},
		{"HTML declared as JPEG rejected", "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("<html>fixture</html>")), true, true},
		{"declared image mismatch rejected", "data:image/jpeg;base64," + validPNG[len("data:image/png;base64,"):], true, true},
		{"truncated JPEG rejected", "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte{0xff, 0xd8, 0xff, 0xe0}), true, true},
		{"oversized payload rejected before decode", oversizedDataURL, true, true},
		{"decoded payload one byte over limit rejected", oneByteOversized, true, true},
		{"oversized dimension rejected", largeDimension, true, true},
		{"zero dimension rejected", zeroDimension, true, true},
		{"oversized pixel count rejected", largePixelCount, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decoded, err := decodeThumbnail(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("decodeThumbnail error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantNil {
				if decoded != nil {
					t.Fatalf("decoded bytes length = %d, want nil", len(decoded))
				}
				return
			}
			if len(decoded) == 0 || len(decoded) > maxThumbnailBytes {
				t.Fatalf("normalized thumbnail length = %d, want 1..%d", len(decoded), maxThumbnailBytes)
			}
			config, format, err := image.DecodeConfig(bytes.NewReader(decoded))
			if err != nil {
				t.Fatalf("decode normalized thumbnail config: %v", err)
			}
			if format != "jpeg" || config.Width < 1 || config.Height < 1 {
				t.Errorf("normalized thumbnail = %s %dx%d, want non-empty JPEG", format, config.Width, config.Height)
			}
		})
	}
}

func testPNGHeaderDataURL(t *testing.T, width, height int) string {
	t.Helper()

	header := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	header = append(header, 0, 0, 0, 13, 'I', 'H', 'D', 'R')
	var dimensions [13]byte
	binary.BigEndian.PutUint32(dimensions[0:4], uint32(width))
	binary.BigEndian.PutUint32(dimensions[4:8], uint32(height))
	dimensions[8] = 8
	dimensions[9] = 6
	header = append(header, dimensions[:]...)
	chunk := append([]byte("IHDR"), dimensions[:]...)
	var checksum [4]byte
	binary.BigEndian.PutUint32(checksum[:], crc32.ChecksumIEEE(chunk))
	header = append(header, checksum[:]...)
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(header)
}
