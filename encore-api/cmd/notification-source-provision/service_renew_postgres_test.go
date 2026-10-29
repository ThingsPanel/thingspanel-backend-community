package main

import (
	"context"
	"database/sql"
	"encore.app/l3-core/notification"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPostgresServiceRenewPreservesScopeAndRejectsRevokedOrExpiredPair(t *testing.T) {
	dsn := os.Getenv("TEST_NOTIFICATION_RENEW_DSN")
	if dsn == "" {
		t.Skip("explicit notification renewal integration database not supplied")
	}
	base, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal("integration database unavailable")
	}
	t.Cleanup(func() { _ = base.Close() })
	schema := fmt.Sprintf("renew_test_%d", time.Now().UnixNano())
	if _, err = base.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal("isolated schema unavailable")
	}
	t.Cleanup(func() {
		if _, err := base.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); err != nil {
			t.Error("isolated renewal schema cleanup failed")
		}
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("invalid integration configuration")
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatal("isolated connection unavailable")
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE notification_credentials(id text PRIMARY KEY,purpose text,token_hash text,tenant_id text,deployment_id text,allowed_tenant_ids jsonb,instance_id text,expires_at timestamptz,revoked_at timestamptz)`)
	if err != nil {
		t.Fatal(err)
	}
	store, err := notification.NewStore(notification.StoreOptions{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	sourceHash := strings.Repeat("a", 64)
	projectionHash := strings.Repeat("b", 64)
	old := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	reset := func() {
		t.Helper()
		if _, err := db.Exec(`TRUNCATE notification_credentials;`); err != nil {
			t.Fatal(err)
		}
		for _, v := range []struct{ id, purpose, hash string }{{"source", "source", sourceHash}, {"projection", "projection", projectionHash}} {
			if _, err := db.Exec(`INSERT INTO notification_credentials VALUES($1,$2,$3,NULL,'deployment-a','["tenant-a"]',NULL,$4,NULL)`, v.id, v.purpose, v.hash, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, name := range []string{"valid", "wrong_tenant", "wrong_deployment", "wrong_hash", "revoked", "expired"} {
		t.Run(name, func(t *testing.T) {
			reset()
			deployment, tenant, hash := "deployment-a", "tenant-a", sourceHash
			switch name {
			case "wrong_tenant":
				tenant = "tenant-b"
			case "wrong_deployment":
				deployment = "deployment-b"
			case "wrong_hash":
				hash = strings.Repeat("c", 64)
			case "revoked":
				_, err = db.Exec(`UPDATE notification_credentials SET revoked_at=now() WHERE id='projection'`)
			case "expired":
				_, err = db.Exec(`UPDATE notification_credentials SET expires_at=now()-interval '1 minute' WHERE id='projection'`)
			}
			if err != nil {
				t.Fatal(err)
			}
			next := time.Now().UTC().Add(6 * time.Hour)
			err := store.RenewSourceProjectionCredentialPair(context.Background(), deployment, []string{tenant}, hash, projectionHash, next)
			if (err == nil) != (name == "valid") {
				t.Fatalf("unexpected renewal outcome for %s", name)
			}
			var actual time.Time
			if err := db.QueryRow(`SELECT expires_at FROM notification_credentials WHERE id='source'`).Scan(&actual); err != nil {
				t.Fatal(err)
			}
			if name == "valid" {
				if actual.Before(old.Add(4 * time.Hour)) {
					t.Fatal("valid renewal did not extend lease")
				}
			} else if !actual.Equal(old) {
				t.Fatal("failed pair renewal changed the other credential")
			}
		})
	}
}
