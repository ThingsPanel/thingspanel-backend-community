package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestLegacyConversionCreatesReviewDraftWithoutSnapshotIdentity(t *testing.T) {
	input := `{"code":0,"data":{"name":"Sensor","version":"0.9.0","template_definition":{"web_chart_config":null,"app_chart_config":null,"telemetry":[{"data_name":"Temperature","data_identifier":"temperature","data_type":"Number","read_write_flag":"R"}],"attributes":[],"events":[],"commands":[]}}}`
	var output bytes.Buffer
	if err := convertDraft(strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	var report map[string]interface{}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report["status"] != "DRAFT_REQUIRES_REVIEW" || bytes.Contains(output.Bytes(), []byte("source_snapshot_id")) || bytes.Contains(output.Bytes(), []byte("dependency_checksum")) {
		t.Fatal("migration draft fabricated published provenance")
	}
	if !bytes.Contains(output.Bytes(), []byte(`"kind": "TELEMETRY"`)) {
		t.Fatal("telemetry was lost")
	}
}

func TestLegacyConversionRejectsAmbiguousInput(t *testing.T) {
	var output bytes.Buffer
	err := convertDraft(strings.NewReader(`{"name":"Sensor","template_definition":{"web_chart_config":null,"app_chart_config":null,"telemetry":[{"data_name":"Temperature","data_identifier":"temperature","data_type":"guess-me"}]}}`), &output)
	if err == nil || output.Len() != 0 {
		t.Fatal("lossy conversion returned a usable draft")
	}
}
