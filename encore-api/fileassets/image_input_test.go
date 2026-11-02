package fileassets

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func encodedFixture(t *testing.T, encode func(io.Writer, image.Image) error) []byte {
	t.Helper()
	var output bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 20, G: 80, B: 160, A: 255})
	require.NoError(t, encode(&output, img))
	return output.Bytes()
}

func TestReadTemplateImageUploadSniffsSupportedImageTypesAndHashesBytes(t *testing.T) {
	webp, err := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEAAUAmJaQAA3AA/v89WAAAAA==")
	require.NoError(t, err)
	fixtures := []struct {
		name         string
		data         []byte
		declaredType string
		contentType  string
	}{
		{name: "png", data: encodedFixture(t, png.Encode), declaredType: "image/png", contentType: "image/png"},
		{name: "jpeg", data: encodedFixture(t, func(w io.Writer, img image.Image) error { return jpeg.Encode(w, img, nil) }), declaredType: "application/octet-stream", contentType: "image/jpeg"},
		{name: "webp", data: webp, declaredType: "", contentType: "image/webp"},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			data, metadata, err := ReadTemplateImageUpload(bytes.NewReader(fixture.data), fixture.declaredType)
			require.NoError(t, err)
			require.Equal(t, fixture.data, data)
			hash := sha256.Sum256(fixture.data)
			require.Equal(t, ImageMetadata{
				ContentType: fixture.contentType,
				SizeBytes:   int64(len(fixture.data)),
				Checksum:    hex.EncodeToString(hash[:]),
			}, metadata)
		})
	}
}

func TestReadTemplateImageUploadRejectsSpoofedOrUnsupportedContent(t *testing.T) {
	for _, content := range [][]byte{
		[]byte("<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>"),
		[]byte("<!doctype html><html><body>not an image</body></html>"),
		[]byte("not an image"),
	} {
		_, _, err := ReadTemplateImageUpload(bytes.NewReader(content), "image/png")
		var validationErr *ValidationError
		require.ErrorAs(t, err, &validationErr)
		require.Equal(t, "file", validationErr.Field)
		require.Equal(t, "unsupported_media_type", validationErr.Code)
		require.NotContains(t, err.Error(), string(content))
	}
}

func TestReadTemplateImageUploadEnforcesEmptyAndSizeBoundaries(t *testing.T) {
	_, _, err := ReadTemplateImageUpload(bytes.NewReader(nil), "")
	var validationErr *ValidationError
	require.ErrorAs(t, err, &validationErr)
	require.Equal(t, "required", validationErr.Code)

	pngBytes := encodedFixture(t, png.Encode)
	exactLimit := append(pngBytes, bytes.Repeat([]byte{0}, int(MaxTemplateImageBytes)-len(pngBytes))...)
	require.Len(t, exactLimit, int(MaxTemplateImageBytes))
	data, metadata, err := ReadTemplateImageUpload(bytes.NewReader(exactLimit), "image/png")
	require.NoError(t, err)
	require.Len(t, data, int(MaxTemplateImageBytes))
	require.Equal(t, MaxTemplateImageBytes, metadata.SizeBytes)

	tooLarge := append(exactLimit, 0)
	_, _, err = ReadTemplateImageUpload(bytes.NewReader(tooLarge), "image/png")
	require.ErrorAs(t, err, &validationErr)
	require.Equal(t, "too_large", validationErr.Code)
}

func TestReadTemplateImageUploadChecksDeclaredMimeWithoutEchoingHeader(t *testing.T) {
	pngBytes := encodedFixture(t, png.Encode)
	for _, declared := range []string{"image/jpeg", "image/png; broken", "text/html<script>secret"} {
		_, _, err := ReadTemplateImageUpload(bytes.NewReader(pngBytes), declared)
		var validationErr *ValidationError
		require.ErrorAs(t, err, &validationErr)
		require.Equal(t, "contentType", validationErr.Field)
		require.NotContains(t, err.Error(), declared)
	}

	data, metadata, err := ReadTemplateImageUpload(bytes.NewReader(pngBytes), " IMAGE/PNG ; charset=binary ")
	require.NoError(t, err)
	require.Equal(t, pngBytes, data)
	require.Equal(t, "image/png", metadata.ContentType)
}

func TestReadTemplateImageUploadPreservesReaderErrors(t *testing.T) {
	readerErr := errors.New("underlying read failed")
	_, _, err := ReadTemplateImageUpload(errorReader{err: readerErr}, "image/png")
	require.ErrorIs(t, err, readerErr)
	var validationErr *ValidationError
	require.False(t, errors.As(err, &validationErr))
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

func TestValidateTemplateImagePurpose(t *testing.T) {
	require.NoError(t, ValidateTemplateImagePurpose("template-image"))
	err := ValidateTemplateImagePurpose("avatar")
	var validationErr *ValidationError
	require.ErrorAs(t, err, &validationErr)
	require.Equal(t, "purpose", validationErr.Field)
	require.Equal(t, "invalid_value", validationErr.Code)
	require.NotContains(t, err.Error(), "avatar")
}
