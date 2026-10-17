package api

import (
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"testing"
)

func TestStreamOTAFirmwareMultipartStreamsFirmwareAndMetadata(t *testing.T) {
	body, contentType := streamOTAFirmwareMultipart(strings.NewReader("firmware-bytes"), "firmware.bin", "esp32s3", "1.2.3")
	defer body.Close()
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatal(err)
	}
	reader := multipart.NewReader(body, params["boundary"])
	part, err := reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	firmware, err := io.ReadAll(part)
	if err != nil || string(firmware) != "firmware-bytes" {
		t.Fatalf("firmware part = %q, err = %v", firmware, err)
	}
	fields := map[string]string{}
	for range 2 {
		part, err = reader.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		value, err := io.ReadAll(part)
		if err != nil {
			t.Fatal(err)
		}
		fields[part.FormName()] = string(value)
	}
	if fields["productKey"] != "esp32s3" || fields["version"] != "1.2.3" {
		t.Fatalf("metadata fields = %#v", fields)
	}
}
