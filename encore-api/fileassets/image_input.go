// Package fileassets contains bounded, side-effect-free validation for file
// inputs. It does not decode images or persist them to an object store.
package fileassets

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"strings"
)

const MaxTemplateImageBytes int64 = 10 * 1024 * 1024

const templateImagePurpose = "template-image"

// ValidationError is safe to expose after mapping its field and code to the
// API's standard validation response. It never includes file bytes or headers.
type ValidationError struct {
	Field   string
	Code    string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// ImageMetadata is derived from the uploaded bytes, not the filename or the
// caller's declared content type.
type ImageMetadata struct {
	ContentType string
	SizeBytes   int64
	Checksum    string
}

// ValidateTemplateImagePurpose validates the FileAssetUpload purpose field.
func ValidateTemplateImagePurpose(purpose string) error {
	if purpose != templateImagePurpose {
		return &ValidationError{Field: "purpose", Code: "invalid_value", Message: "purpose must be template-image"}
	}
	return nil
}

// ReadTemplateImageUpload reads at most MaxTemplateImageBytes+1 bytes, sniffs
// the actual content, and returns bytes with derived metadata. It performs no
// full image decoding; callers must continue to apply storage and serving
// policy appropriate for untrusted uploads.
func ReadTemplateImageUpload(reader io.Reader, declaredContentType string) ([]byte, ImageMetadata, error) {
	if reader == nil {
		return nil, ImageMetadata{}, &ValidationError{Field: "file", Code: "required", Message: "file is required"}
	}

	data, err := io.ReadAll(io.LimitReader(reader, MaxTemplateImageBytes+1))
	if err != nil {
		return nil, ImageMetadata{}, fmt.Errorf("read template image upload: %w", err)
	}
	if len(data) == 0 {
		return nil, ImageMetadata{}, &ValidationError{Field: "file", Code: "required", Message: "file is required"}
	}
	if int64(len(data)) > MaxTemplateImageBytes {
		return nil, ImageMetadata{}, &ValidationError{Field: "file", Code: "too_large", Message: "file exceeds the 10 MiB limit"}
	}

	contentType, ok := sniffImageContentType(data)
	if !ok {
		return nil, ImageMetadata{}, &ValidationError{Field: "file", Code: "unsupported_media_type", Message: "file must contain a PNG, JPEG, or WebP image"}
	}

	declared := strings.TrimSpace(declaredContentType)
	if declared != "" {
		mediaType, _, parseErr := mime.ParseMediaType(declared)
		if parseErr != nil {
			return nil, ImageMetadata{}, &ValidationError{Field: "contentType", Code: "invalid_value", Message: "declared content type is invalid"}
		}
		mediaType = strings.ToLower(mediaType)
		if mediaType != "application/octet-stream" && mediaType != contentType {
			return nil, ImageMetadata{}, &ValidationError{Field: "contentType", Code: "mismatch", Message: "declared content type does not match file contents"}
		}
	}

	checksum := sha256.Sum256(data)
	return data, ImageMetadata{
		ContentType: contentType,
		SizeBytes:   int64(len(data)),
		Checksum:    hex.EncodeToString(checksum[:]),
	}, nil
}

func sniffImageContentType(data []byte) (string, bool) {
	if len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n" {
		return "image/png", true
	}
	if len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
		return "image/jpeg", true
	}
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp", true
	}
	return "", false
}
