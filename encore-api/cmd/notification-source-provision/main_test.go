package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCurrentInfraEnvironmentPasswordIsRequiredAndResolved(t *testing.T) {
	p := filepath.Join(t.TempDir(), "infra.json")
	if err := os.WriteFile(p, []byte(`{"sql_servers":[{"host":"127.0.0.1:5432","tls_config":{"disable_tls":true},"databases":{"qa_notification":{"name":"notification","username":"qa","password":{"$env":"TEST_NOTIFICATION_PASSWORD"}}}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOTIFICATION_PROVISION_EXPECTED_DB", "qa_notification")
	t.Setenv("NOTIFICATION_PROVISION_EXPECTED_HOST", "127.0.0.1:5432")
	t.Setenv("TEST_NOTIFICATION_PASSWORD", "")
	if db, err := openNotificationDB(p); err == nil {
		db.Close()
		t.Fatal("missing referenced password accepted")
	}
	t.Setenv("TEST_NOTIFICATION_PASSWORD", "private-test-password")
	db, err := openNotificationDB(p)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
}
