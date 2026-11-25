package devicemetadata

import (
	"net/url"
	"testing"

	"encore.dev/beta/errs"
)

func TestNativeDeviceListSummarySelectionPreservesFilters(t *testing.T) {
	for _, tt := range []struct {
		query   string
		omit    bool
		invalid bool
	}{
		{query: ""},
		{query: "includeSummaries=true&connectivity=online"},
		{query: "includeSummaries=false", omit: true},
		{query: "includeSummaries=false&alarmState=any", omit: true},
		{query: "includeSummaries=false&connectivity=offline", invalid: true},
		{query: "includeSummaries=false&alarmState=active", invalid: true},
		{query: "includeSummaries=", invalid: true},
	} {
		t.Run(tt.query, func(t *testing.T) {
			values, err := url.ParseQuery(tt.query)
			if err != nil {
				t.Fatal(err)
			}
			query, err := decodeNativeDeviceListQuery(values)
			if (err != nil) != tt.invalid || (!tt.invalid && query.omitSummaries != tt.omit) {
				t.Fatalf("omit = %v, error = %v", query.omitSummaries, err)
			}
		})
	}
}

func TestNativeDeviceSummaryIDsRejectDuplicateAndOutOfScopeIDs(t *testing.T) {
	const allowed = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	identity := nativeDeviceReadIdentity{deviceIDs: []string{allowed}}
	ids, err := decodeNativeDeviceSummaryIDs([]byte(`{"deviceIds":["AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA"]}`), identity)
	if err != nil || len(ids) != 1 || ids[0] != allowed {
		t.Fatalf("canonical IDs = %v, error = %v", ids, err)
	}
	for _, tt := range []struct {
		body string
		code errs.ErrCode
	}{
		{`{"deviceIds":[]}`, errs.InvalidArgument},
		{`{"deviceIds":["aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA"]}`, errs.InvalidArgument},
		{`{"deviceIds":["bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"]}`, errs.NotFound},
		{`{"deviceIds":["aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"],"tenantId":"another"}`, errs.InvalidArgument},
	} {
		if _, err := decodeNativeDeviceSummaryIDs([]byte(tt.body), identity); errs.Code(err) != tt.code {
			t.Errorf("%s: got %v, want %v", tt.body, err, tt.code)
		}
	}
}
