package mosquittodriver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	bc "project/internal/brokercontract"
)

func TestRewriteMintsDistinctIngestionIDForEachDevicePublish(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(testGrant())
	}))
	defer srv.Close()
	d, err := New(Config{OwnerURL: srv.URL, Token: strings.Repeat("x", 32), Cell: "community", Instance: "node-a", MaxPending: 2, RuntimeFencing: false})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if !d.Begin(1, "client-a", "user", "pass", "127.0.0.1", 4) || !poll(t, d).Allow {
		t.Fatal("device auth failed")
	}
	var firstID string
	for i := 0; i < 2; i++ {
		topic, body, retain, err := d.Rewrite(1, "devices/telemetry", []byte(`{"value":4}`), 1, false)
		if err != nil || retain {
			t.Fatalf("Rewrite #%d: retain=%v err=%v", i, retain, err)
		}
		env, err := bc.ParseIngress(topic, body, bc.TrustedMetadata{BrokerCell: "community", BrokerInstance: "node-a"})
		if err != nil {
			t.Fatalf("ParseIngress #%d: %v", i, err)
		}
		if env.Metadata.IngestionID == "" || !strings.HasPrefix(env.Metadata.IngestionID, "mosq-") {
			t.Fatalf("Rewrite #%d omitted broker ingestion ID: %q", i, env.Metadata.IngestionID)
		}
		if i == 0 {
			firstID = env.Metadata.IngestionID
		} else if env.Metadata.IngestionID == firstID {
			t.Fatal("two distinct device publishes reused one ingestion ID")
		}
	}
}
