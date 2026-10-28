package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDecodeCursorRoundTripAndRejectMalformed(t *testing.T) {
	want := Cursor{CreatedAt: time.Date(2026, 10, 20, 1, 2, 3, 0, time.UTC), ID: "effect:abc"}
	got, err := decodeCursor(encodeCursor(want))
	if err != nil {
		t.Fatal(err)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || got.ID != want.ID {
		t.Fatalf("got %#v want %#v", got, want)
	}
	for _, bad := range []string{"%%%", "e30", strings.Repeat("a", 2049)} {
		if _, err := decodeCursor(bad); err == nil {
			t.Errorf("decodeCursor(%q) unexpectedly succeeded", bad)
		}
	}
}

func TestRunRequiresTenantAndBoundsLimit(t *testing.T) {
	for _, args := range [][]string{{}, {"--limit", "0", "--tenant", "t"}, {"--limit", "101", "--tenant", "t"}, {"--tenant", "t", "unexpected"}} {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), args, func(string) string { return "" }, &stdout, &stderr)
		if code != 64 {
			t.Errorf("args %v: code=%d stderr=%q", args, code, stderr.String())
		}
		if stdout.Len() != 0 {
			t.Errorf("invalid arguments emitted output: %q", stdout.String())
		}
	}
}

func TestRunNeverPrintsDSNOnConnectionError(t *testing.T) {
	const secret = "do-not-print-this-password"
	dsn := "postgres://audit:" + secret + "@127.0.0.1:1/never?connect_timeout=1"
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--tenant", "tenant-a"}, func(key string) string {
		if key == "TP_BROKER_AUDIT_DSN" {
			return dsn
		}
		return ""
	}, &stdout, &stderr)
	if code != 69 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), secret) || strings.Contains(stdout.String()+stderr.String(), dsn) {
		t.Fatalf("database secret leaked: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRecordJSONContainsNoPayloadOrSecrets(t *testing.T) {
	b, err := json.Marshal(Output{TenantID: "t", Records: []Record{{Kind: "quarantine", ID: "quarantine:q1", Status: "rejected", Count: 2, Reason: "permanent_ingress_rejection"}}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, forbidden := range []string{"payload", "voucher", "token", "password", "canonical_request", "parameters", "raw", "internal-secret"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Errorf("output contains forbidden field/value %q: %s", forbidden, text)
		}
	}
}

func TestReadDSNFromEnvironmentOrFile(t *testing.T) {
	if got, err := readDSN("", func(key string) string {
		if key == "TP_BROKER_AUDIT_DSN" {
			return " postgres://audit:secret@localhost/db "
		}
		return ""
	}); err != nil || got != "postgres://audit:secret@localhost/db" {
		t.Fatalf("environment DSN got=%q err=%v", got, err)
	}
	file := t.TempDir() + "/dsn"
	if err := os.WriteFile(file, []byte("postgres://audit:file-secret@localhost/db\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := readDSN(file, func(string) string { return "" }); err != nil || got != "postgres://audit:file-secret@localhost/db" {
		t.Fatalf("file DSN got=%q err=%v", got, err)
	}
	if _, err := readDSN("", func(string) string { return "" }); err == nil {
		t.Fatal("unset env DSN accepted")
	}
}
